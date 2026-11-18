package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

func TestAccRoutingRule(t *testing.T) {
	s := testServer(t)
	name := "rr-" + randSuffix()
	cfg := func(name, body string) string {
		return s.ProviderBlock() + fmt.Sprintf(`
resource "entrepot_routing_rule" "test" {
  name = %q
%s
}`, name, body)
	}
	block := cfg(name, `
  description = "never fetch internal names upstream"
  mode        = "block"
  matchers    = ["^@acme/", "^acme-"]`)
	allow := cfg(name, `
  mode     = "allow"
  matchers = ["^(left-pad|lodash)$"]`)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				Config: block,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_routing_rule.test", "id", name),
					resource.TestCheckResourceAttr("entrepot_routing_rule.test", "mode", "block"),
					resource.TestCheckResourceAttr("entrepot_routing_rule.test", "matchers.#", "2"),
					resource.TestCheckResourceAttr("entrepot_routing_rule.test", "matchers.1", "^acme-"),
					resource.TestCheckResourceAttr("entrepot_routing_rule.test", "repositories.#", "0"),
				),
			},
			{
				Config: allow,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_routing_rule.test", plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_routing_rule.test", "mode", "allow"),
					resource.TestCheckResourceAttr("entrepot_routing_rule.test", "description", ""),
					resource.TestCheckResourceAttr("entrepot_routing_rule.test", "matchers.#", "1"),
				),
			},
			{ResourceName: "entrepot_routing_rule.test", ImportState: true, ImportStateId: name, ImportStateVerify: true},
			{
				PreConfig: func() {
					if _, err := s.API.DeleteRoutingRuleWithResponse(context.Background(), name); err != nil {
						t.Fatal(err)
					}
				},
				Config: allow,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_routing_rule.test", plancheck.ResourceActionCreate),
				}},
			},
			{
				Config: cfg(name+"-b", `
  mode     = "allow"
  matchers = ["^(left-pad|lodash)$"]`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_routing_rule.test", plancheck.ResourceActionDestroyBeforeCreate),
				}},
				Check: resource.TestCheckResourceAttr("entrepot_routing_rule.test", "name", name+"-b"),
			},
		},
	})
}

func TestAccRoutingRuleValidation(t *testing.T) {
	s := testServer(t)
	cfg := func(matchers string) string {
		return s.ProviderBlock() + fmt.Sprintf(`
resource "entrepot_routing_rule" "bad" {
  name     = "bad"
  mode     = "block"
  matchers = %s
}`, matchers)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{Config: cfg(`["^ok-", "(unclosed"]`), ExpectError: regexpMust(`(?s)Invalid regular expression.*unclosed`)},
			{Config: cfg(`["  "]`), ExpectError: regexpMust(`matchers must not be empty`)},
			{Config: cfg(`[]`), ExpectError: regexpMust(`at least 1`)},
		},
	})
}
