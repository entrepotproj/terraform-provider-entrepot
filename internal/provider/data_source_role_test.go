package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/geoffo-dev/terraform-provider-entrepot/internal/client"
)

func TestAccDataSourceRole(t *testing.T) {
	s := testServer(t)
	custom := "auditor-" + randSuffix()
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				PreConfig: func() {
					res, err := s.API.CreateRoleWithResponse(context.Background(), client.RoleInput{
						Name: custom, Description: ptr("read and delete"), Actions: []client.Action{"read", "delete"},
					})
					if err = check(res, err, 201); err != nil {
						t.Fatal(err)
					}
				},
				Config: s.ProviderBlock() + fmt.Sprintf(`
data "entrepot_role" "developer" {
  name = "developer"
}

data "entrepot_role" "custom" {
  name = %q
}`, custom),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.entrepot_role.developer", "id", "developer"),
					resource.TestCheckResourceAttr("data.entrepot_role.developer", "builtin", "true"),
					resource.TestCheckResourceAttr("data.entrepot_role.developer", "actions.#", "2"),
					resource.TestCheckTypeSetElemAttr("data.entrepot_role.developer", "actions.*", "write"),
					resource.TestCheckResourceAttrSet("data.entrepot_role.developer", "description"),
					resource.TestCheckResourceAttr("data.entrepot_role.custom", "builtin", "false"),
					resource.TestCheckResourceAttr("data.entrepot_role.custom", "description", "read and delete"),
					resource.TestCheckTypeSetElemAttr("data.entrepot_role.custom", "actions.*", "delete"),
				),
			},
			{
				Config:      s.ProviderBlock() + `data "entrepot_role" "missing" { name = "no-such-role" }`,
				ExpectError: regexpMust(`Role not found`),
			},
		},
	})
}
