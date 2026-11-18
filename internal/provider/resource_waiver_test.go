package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccWaiver(t *testing.T) {
	s := testServer(t)
	vuln := "CVE-2024-" + randSuffix()
	var id string
	cfg := func(body string) string {
		return s.ProviderBlock() + fmt.Sprintf(`
resource "entrepot_waiver" "test" {
  vuln_id = %q
%s
}`, vuln, body)
	}
	// An offset timestamp is echoed by the server in UTC; the configured spelling is kept.
	first := cfg(`
  repository_pattern = "npm-*"
  reason             = "not reachable from our code"
  expires_at         = "2030-01-01T02:00:00+02:00"`)
	second := cfg(`
  component_pattern = "lodash@4.17.*"
  reason            = "accepted until upgrade"`)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				Config: first,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith("entrepot_waiver.test", "id", func(v string) error {
						if v == "" {
							return fmt.Errorf("empty id")
						}
						id = v
						return nil
					}),
					resource.TestCheckResourceAttr("entrepot_waiver.test", "repository_pattern", "npm-*"),
					resource.TestCheckResourceAttr("entrepot_waiver.test", "component_pattern", "*"),
					resource.TestCheckResourceAttr("entrepot_waiver.test", "expires_at", "2030-01-01T02:00:00+02:00"),
					resource.TestCheckResourceAttrSet("entrepot_waiver.test", "created_by"),
					resource.TestMatchResourceAttr("entrepot_waiver.test", "created_at", regexpMust(`^\d{4}-\d\d-\d\dT.*Z$`)),
				),
			},
			// In place: patterns, reason, and expiry removed.
			{
				Config: second,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_waiver.test", plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith("entrepot_waiver.test", "id", func(v string) error {
						if v != id {
							return fmt.Errorf("id changed from %s to %s", id, v)
						}
						return nil
					}),
					resource.TestCheckResourceAttr("entrepot_waiver.test", "repository_pattern", "*"),
					resource.TestCheckResourceAttr("entrepot_waiver.test", "component_pattern", "lodash@4.17.*"),
					resource.TestCheckResourceAttr("entrepot_waiver.test", "reason", "accepted until upgrade"),
					resource.TestCheckNoResourceAttr("entrepot_waiver.test", "expires_at"),
				),
			},
			{
				ResourceName: "entrepot_waiver.test", ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: func(st *terraform.State) (string, error) {
					return st.RootModule().Resources["entrepot_waiver.test"].Primary.ID, nil
				},
			},
			// An expiry in UTC form, then import of that.
			{
				Config: cfg(`
  component_pattern = "lodash@4.17.*"
  expires_at        = "2031-06-30T12:00:00Z"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_waiver.test", "expires_at", "2031-06-30T12:00:00Z"),
					resource.TestCheckResourceAttr("entrepot_waiver.test", "reason", ""),
				),
			},
			{
				ResourceName: "entrepot_waiver.test", ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: func(st *terraform.State) (string, error) {
					return st.RootModule().Resources["entrepot_waiver.test"].Primary.ID, nil
				},
			},
			// Deleted outside Terraform → recreated.
			{
				PreConfig: func() {
					if _, err := s.API.DeleteWaiverWithResponse(context.Background(), id); err != nil {
						t.Fatal(err)
					}
				},
				Config: second,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_waiver.test", plancheck.ResourceActionCreate),
				}},
			},
		},
	})
}

func TestAccWaiverValidation(t *testing.T) {
	s := testServer(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				Config: s.ProviderBlock() + `
resource "entrepot_waiver" "bad" {
  vuln_id = "*"
}`,
				ExpectError: regexpMust(`Waiver too broad`),
			},
			{
				Config: s.ProviderBlock() + `
resource "entrepot_waiver" "bad" {
  vuln_id    = "CVE-2024-1"
  expires_at = "2030-01-01"
}`,
				ExpectError: regexpMust(`not an RFC 3339 timestamp`),
			},
			{
				Config: s.ProviderBlock() + `
resource "entrepot_waiver" "bad" {
  vuln_id = " CVE-2024-1"
}`,
				ExpectError: regexpMust(`must not start or end with whitespace`),
			},
		},
	})
}
