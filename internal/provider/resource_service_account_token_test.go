package provider

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/entrepotproj/terraform-provider-entrepot/internal/acctest"
	"github.com/entrepotproj/terraform-provider-entrepot/internal/client"
)

// checkTokenPrincipal authenticates with the token in state and checks the
// principal the server sees.
func checkTokenPrincipal(s *acctest.Server, addr, wantName string, wantScopes []string) resource.TestCheckFunc {
	return func(st *terraform.State) error {
		rs, ok := st.RootModule().Resources[addr]
		if !ok {
			return fmt.Errorf("%s not in state", addr)
		}
		secret := rs.Primary.Attributes["secret"]
		if secret == "" {
			return fmt.Errorf("%s: empty secret", addr)
		}
		api, err := client.New(client.Options{URL: s.URL, Token: secret})
		if err != nil {
			return err
		}
		res, err := api.GetMeWithResponse(context.Background())
		if err = check(res, err, 200); err != nil {
			return fmt.Errorf("%s: authenticating with the token: %w", addr, err)
		}
		me := res.JSON200
		if me.Name != wantName {
			return fmt.Errorf("%s: principal %q, want %q", addr, me.Name, wantName)
		}
		got := slices.Clone(me.Scopes)
		slices.Sort(got)
		if !slices.Equal(got, wantScopes) {
			return fmt.Errorf("%s: scopes %v, want %v", addr, got, wantScopes)
		}
		return nil
	}
}

func TestAccServiceAccountToken(t *testing.T) {
	s := testServer(t)
	sa := "tok-" + randSuffix()
	var firstID string
	cfg := func(scopes string) string {
		return s.ProviderBlock() + fmt.Sprintf(`
resource "entrepot_service_account" "ci" {
  name = %q
}

resource "entrepot_service_account_token" "full" {
  service_account = entrepot_service_account.ci.name
  name            = "full"
  expires_in_days = 30
}

resource "entrepot_service_account_token" "scoped" {
  service_account = entrepot_service_account.ci.name
  name            = "scoped"
  scopes          = %s
}
`, sa, scopes)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				Config: cfg(`["read"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("entrepot_service_account_token.full", "secret", regexpMust(`^ent_`)),
					resource.TestCheckResourceAttrSet("entrepot_service_account_token.full", "hint"),
					resource.TestCheckResourceAttrSet("entrepot_service_account_token.full", "created_at"),
					resource.TestCheckResourceAttr("entrepot_service_account_token.full", "scopes.#", "0"),
					resource.TestCheckResourceAttrWith("entrepot_service_account_token.full", "expires_at", func(v string) error {
						exp, err := time.Parse(time.RFC3339, v)
						if err != nil {
							return err
						}
						if d := time.Until(exp); d < 29*24*time.Hour || d > 31*24*time.Hour {
							return fmt.Errorf("expires_at %s is not ~30 days away", v)
						}
						return nil
					}),
					resource.TestCheckNoResourceAttr("entrepot_service_account_token.scoped", "expires_at"),
					resource.TestCheckResourceAttr("entrepot_service_account_token.scoped", "scopes.#", "1"),
					resource.TestCheckTypeSetElemAttr("entrepot_service_account_token.scoped", "scopes.*", "read"),
					checkTokenPrincipal(s, "entrepot_service_account_token.full", "sa:"+sa, []string{}),
					checkTokenPrincipal(s, "entrepot_service_account_token.scoped", "sa:"+sa, []string{"read"}),
					func(st *terraform.State) error {
						firstID = st.RootModule().Resources["entrepot_service_account_token.scoped"].Primary.ID
						return nil
					},
				),
			},
			{
				ResourceName: "entrepot_service_account_token.full", ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: func(st *terraform.State) (string, error) {
					return sa + "/" + st.RootModule().Resources["entrepot_service_account_token.full"].Primary.ID, nil
				},
				ImportStateVerifyIgnore: []string{"secret", "expires_in_days"},
			},
			// Tokens are immutable: new scopes issue a new token.
			{
				Config: cfg(`["read", "write"]`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_service_account_token.scoped", plancheck.ResourceActionDestroyBeforeCreate),
					plancheck.ExpectResourceAction("entrepot_service_account_token.full", plancheck.ResourceActionNoop),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					checkTokenPrincipal(s, "entrepot_service_account_token.scoped", "sa:"+sa, []string{"read", "write"}),
					func(st *terraform.State) error {
						if st.RootModule().Resources["entrepot_service_account_token.scoped"].Primary.ID == firstID {
							return fmt.Errorf("token was not replaced")
						}
						return nil
					},
				),
			},
			// Revoked outside Terraform → a new token is issued.
			{
				PreConfig: func() {
					res, err := s.API.ListServiceAccountTokensWithResponse(context.Background(), sa)
					if err != nil || res.JSON200 == nil {
						t.Fatal(err)
					}
					for _, tok := range *res.JSON200 {
						if tok.Name == "full" {
							if _, err := s.API.DeleteServiceAccountTokenWithResponse(context.Background(), sa, tok.Id); err != nil {
								t.Fatal(err)
							}
						}
					}
				},
				Config: cfg(`["read", "write"]`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_service_account_token.full", plancheck.ResourceActionCreate),
				}},
				Check: checkTokenPrincipal(s, "entrepot_service_account_token.full", "sa:"+sa, []string{}),
			},
			// Deleting the service account revokes its tokens → all recreated.
			{
				PreConfig: func() {
					if _, err := s.API.DeleteServiceAccountWithResponse(context.Background(), sa); err != nil {
						t.Fatal(err)
					}
				},
				Config: cfg(`["read", "write"]`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_service_account.ci", plancheck.ResourceActionCreate),
					plancheck.ExpectResourceAction("entrepot_service_account_token.full", plancheck.ResourceActionCreate),
					plancheck.ExpectResourceAction("entrepot_service_account_token.scoped", plancheck.ResourceActionCreate),
				}},
			},
		},
	})
}

// After import, setting expires_in_days only records it (no new token).
func TestAccServiceAccountTokenImportExpiry(t *testing.T) {
	s := testServer(t)
	sa := "imp-" + randSuffix()
	ctx := context.Background()
	res, err := s.API.CreateServiceAccountWithResponse(ctx, client.ServiceAccountInput{Name: sa})
	if err = check(res, err, 201); err != nil {
		t.Fatal(err)
	}
	tok, err := s.API.CreateServiceAccountTokenWithResponse(ctx, sa, client.ApiTokenInput{Name: "deploy", ExpiresInDays: ptr(7)})
	if err = check(tok, err, 201); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = s.API.DeleteServiceAccountWithResponse(ctx, sa) })
	config := s.ProviderBlock() + fmt.Sprintf(`
import {
  to = entrepot_service_account_token.deploy
  id = "%s/%s"
}

resource "entrepot_service_account_token" "deploy" {
  service_account = %q
  name            = "deploy"
  expires_in_days = 7
}
`, sa, tok.JSON201.Token.Id, sa)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{{
			Config: config,
			ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
				plancheck.ExpectResourceAction("entrepot_service_account_token.deploy", plancheck.ResourceActionUpdate),
			}},
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("entrepot_service_account_token.deploy", "id", tok.JSON201.Token.Id),
				resource.TestCheckNoResourceAttr("entrepot_service_account_token.deploy", "secret"),
				resource.TestCheckResourceAttr("entrepot_service_account_token.deploy", "expires_in_days", "7"),
			),
		}},
	})
}

func TestAccServiceAccountTokenValidation(t *testing.T) {
	s := testServer(t)
	token := func(body string) string {
		return s.ProviderBlock() + "resource \"entrepot_service_account_token\" \"bad\" {\n  service_account = \"x\"\n  name = \"t\"\n" + body + "\n}"
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{Config: token(`scopes = ["read", "root"]`), ExpectError: regexpMust(`value must be one of`)},
			{Config: token(`expires_in_days = 0`), ExpectError: regexpMust(`at least 1`)},
			{Config: token(`scopes = ["read"]`), ExpectError: regexpMust(`(?i)not found`)},
		},
	})
}
