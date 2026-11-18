package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

func TestAccServiceAccount(t *testing.T) {
	s := testServer(t)
	name := "sa-" + randSuffix()
	cfg := func(desc string) string {
		return s.ProviderBlock() + fmt.Sprintf(`
resource "entrepot_service_account" "test" {
  name        = %q
  description = %q
}`, name, desc)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				Config: cfg("CI publisher"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_service_account.test", "id", name),
					resource.TestCheckResourceAttr("entrepot_service_account.test", "username", "sa:"+name),
					resource.TestCheckResourceAttr("entrepot_service_account.test", "description", "CI publisher"),
					resource.TestCheckResourceAttrSet("entrepot_service_account.test", "user_id"),
				),
			},
			{ResourceName: "entrepot_service_account.test", ImportState: true, ImportStateId: name, ImportStateVerify: true},
			// The "sa:" prefix is accepted on import.
			{ResourceName: "entrepot_service_account.test", ImportState: true, ImportStateId: "sa:" + name, ImportStateVerify: true},
			// There is no update endpoint: a new description replaces the account.
			{
				Config: cfg("CI publisher (prod)"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_service_account.test", plancheck.ResourceActionDestroyBeforeCreate),
				}},
				Check: resource.TestCheckResourceAttr("entrepot_service_account.test", "description", "CI publisher (prod)"),
			},
			// Deleted outside Terraform → recreated.
			{
				PreConfig: func() {
					if _, err := s.API.DeleteServiceAccountWithResponse(context.Background(), name); err != nil {
						t.Fatal(err)
					}
				},
				Config: cfg("CI publisher (prod)"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_service_account.test", plancheck.ResourceActionCreate),
				}},
			},
		},
	})
}

func TestAccServiceAccountValidation(t *testing.T) {
	s := testServer(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{{
			Config: s.ProviderBlock() + `
resource "entrepot_service_account" "bad" {
  name = "sa:ci"
}`,
			ExpectError: regexpMust(`lowercase`),
		}},
	})
}
