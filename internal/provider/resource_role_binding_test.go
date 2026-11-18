package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// stateID returns the id attribute of a resource in state (for ImportStateIdFunc).
func stateID(addr string) resource.ImportStateIdFunc {
	return func(st *terraform.State) (string, error) {
		rs, ok := st.RootModule().Resources[addr]
		if !ok {
			return "", fmt.Errorf("%s not in state", addr)
		}
		return rs.Primary.ID, nil
	}
}

func TestAccRoleBinding(t *testing.T) {
	s := testServer(t)
	sfx := randSuffix()
	var firstID string
	cfg := func(pattern string) string {
		return s.ProviderBlock() + fmt.Sprintf(`
resource "entrepot_role" "publisher" {
  name    = "publisher-%[1]s"
  actions = ["read", "write"]
}

resource "entrepot_service_account" "ci" {
  name = "ci-%[1]s"
}

resource "entrepot_role_binding" "ci" {
  role               = entrepot_role.publisher.name
  subject_type       = "user"
  subject            = entrepot_service_account.ci.username
  repository_pattern = %[2]q
}

resource "entrepot_role_binding" "devs" {
  role         = "developer"
  subject_type = "group"
  subject      = "developers-%[1]s"
}

resource "entrepot_role_binding" "everyone" {
  role               = "viewer"
  subject_type       = "authenticated"
  repository_pattern = "public-%[1]s-*"
}
`, sfx, pattern)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				Config: cfg("npm-*"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_role_binding.ci", "subject", "sa:ci-"+sfx),
					resource.TestCheckResourceAttr("entrepot_role_binding.ci", "repository_pattern", "npm-*"),
					resource.TestCheckResourceAttrSet("entrepot_role_binding.ci", "created_at"),
					resource.TestCheckResourceAttr("entrepot_role_binding.devs", "repository_pattern", "*"),
					resource.TestCheckNoResourceAttr("entrepot_role_binding.everyone", "subject"),
					func(st *terraform.State) error {
						firstID = st.RootModule().Resources["entrepot_role_binding.ci"].Primary.ID
						return nil
					},
				),
			},
			{ResourceName: "entrepot_role_binding.ci", ImportState: true, ImportStateIdFunc: stateID("entrepot_role_binding.ci"), ImportStateVerify: true},
			{ResourceName: "entrepot_role_binding.everyone", ImportState: true, ImportStateIdFunc: stateID("entrepot_role_binding.everyone"), ImportStateVerify: true},
			// Bindings are immutable: any change replaces them.
			{
				Config: cfg("pypi-*"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_role_binding.ci", plancheck.ResourceActionDestroyBeforeCreate),
					plancheck.ExpectResourceAction("entrepot_role_binding.devs", plancheck.ResourceActionNoop),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_role_binding.ci", "repository_pattern", "pypi-*"),
					func(st *terraform.State) error {
						if id := st.RootModule().Resources["entrepot_role_binding.ci"].Primary.ID; id == firstID {
							return fmt.Errorf("binding was not replaced (id %s)", id)
						}
						return nil
					},
				),
			},
			// Deleted outside Terraform → recreated.
			{
				PreConfig: func() {
					res, err := s.API.ListRoleBindingsWithResponse(context.Background())
					if err != nil || res.JSON200 == nil {
						t.Fatal(err)
					}
					for _, b := range *res.JSON200 {
						if b.Subject == "developers-"+sfx {
							if _, err := s.API.DeleteRoleBindingWithResponse(context.Background(), b.Id); err != nil {
								t.Fatal(err)
							}
						}
					}
				},
				Config: cfg("pypi-*"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_role_binding.devs", plancheck.ResourceActionCreate),
				}},
			},
			// Deleting the role (outside Terraform) cascades to its bindings.
			{
				PreConfig: func() {
					if _, err := s.API.DeleteRoleWithResponse(context.Background(), "publisher-"+sfx); err != nil {
						t.Fatal(err)
					}
				},
				Config: cfg("pypi-*"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_role.publisher", plancheck.ResourceActionCreate),
					plancheck.ExpectResourceAction("entrepot_role_binding.ci", plancheck.ResourceActionCreate),
				}},
			},
		},
	})
}

func TestAccRoleBindingValidation(t *testing.T) {
	s := testServer(t)
	binding := func(body string) string {
		return s.ProviderBlock() + "resource \"entrepot_role_binding\" \"bad\" {\n" + body + "\n}"
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{Config: binding(`role = "viewer"
subject_type = "authenticated"
subject = "alice"`), ExpectError: regexpMust(`subject must be omitted`)},
			{Config: binding(`role = "viewer"
subject_type = "user"`), ExpectError: regexpMust(`subject is required`)},
			{Config: binding(`role = "viewer"
subject_type = "robot"
subject = "x"`), ExpectError: regexpMust(`value must be one of`)},
			{Config: binding(`role = "no-such-role-` + randSuffix() + `"
subject_type = "user"
subject = "alice"`), ExpectError: regexpMust(`(?i)not found`)},
		},
	})
}
