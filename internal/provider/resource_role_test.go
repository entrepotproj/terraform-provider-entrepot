package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

func TestAccRole(t *testing.T) {
	s := testServer(t)
	name := "role-" + randSuffix()
	cfg := func(desc, actions string) string {
		return s.ProviderBlock() + fmt.Sprintf(`
resource "entrepot_role" "test" {
  name        = %q
  description = %q
  actions     = %s
}`, name, desc, actions)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				Config: cfg("uploaders", `["read", "write"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_role.test", "id", name),
					resource.TestCheckResourceAttr("entrepot_role.test", "description", "uploaders"),
					resource.TestCheckResourceAttr("entrepot_role.test", "actions.#", "2"),
					resource.TestCheckTypeSetElemAttr("entrepot_role.test", "actions.*", "write"),
				),
			},
			// Description and actions update in place; set order does not matter.
			{
				Config: cfg("publishers", `["delete", "write", "read"]`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_role.test", plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_role.test", "description", "publishers"),
					resource.TestCheckResourceAttr("entrepot_role.test", "actions.#", "3"),
					resource.TestCheckTypeSetElemAttr("entrepot_role.test", "actions.*", "delete"),
				),
			},
			{ResourceName: "entrepot_role.test", ImportState: true, ImportStateId: name, ImportStateVerify: true},
			// Deleted outside Terraform → recreated.
			{
				PreConfig: func() {
					if _, err := s.API.DeleteRoleWithResponse(context.Background(), name); err != nil {
						t.Fatal(err)
					}
				},
				Config: cfg("publishers", `["delete", "write", "read"]`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_role.test", plancheck.ResourceActionCreate),
				}},
			},
		},
	})
}

func TestAccRoleRename(t *testing.T) {
	s := testServer(t)
	sfx := randSuffix()
	cfg := func(name string) string {
		return s.ProviderBlock() + fmt.Sprintf(`
resource "entrepot_role" "test" {
  name    = %q
  actions = ["read"]
}`, name)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{Config: cfg("first-" + sfx), Check: resource.TestCheckResourceAttr("entrepot_role.test", "description", "")},
			{
				Config: cfg("second-" + sfx),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_role.test", plancheck.ResourceActionDestroyBeforeCreate),
				}},
				Check: resource.TestCheckResourceAttr("entrepot_role.test", "name", "second-"+sfx),
			},
		},
	})
}

func TestAccRoleValidation(t *testing.T) {
	s := testServer(t)
	role := func(name, actions string) string {
		return s.ProviderBlock() + fmt.Sprintf(`
resource "entrepot_role" "bad" {
  name    = %q
  actions = %s
}`, name, actions)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{Config: role("viewer", `["read"]`), ExpectError: regexpMust(`(?s)Built-in role.*read-only`)},
			{Config: role("ok-"+randSuffix(), `["read", "fly"]`), ExpectError: regexpMust(`value must be one of`)},
			{Config: role("ok-"+randSuffix(), `[]`), ExpectError: regexpMust(`at least 1`)},
			{Config: role("Not_Valid", `["read"]`), ExpectError: regexpMust(`lowercase`)},
		},
	})
}
