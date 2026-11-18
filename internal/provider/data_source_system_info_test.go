package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccDataSourceSystemInfo(t *testing.T) {
	s := testServer(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{{
			Config: s.ProviderBlock() + `data "entrepot_system_info" "this" {}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet("data.entrepot_system_info.this", "version"),
				resource.TestCheckResourceAttr("data.entrepot_system_info.this", "base_url", s.URL),
				resource.TestMatchResourceAttr("data.entrepot_system_info.this", "formats.#", regexpMust(`^[1-9][0-9]*$`)),
				resource.TestCheckTypeSetElemNestedAttrs("data.entrepot_system_info.this", "formats.*", map[string]string{"name": "npm"}),
				resource.TestCheckTypeSetElemAttr("data.entrepot_system_info.this", "formats.0.kinds.*", "hosted"),
				resource.TestCheckResourceAttr("data.entrepot_system_info.this", "oidc_enabled", "false"),
				resource.TestMatchResourceAttr("data.entrepot_system_info.this", "anonymous_access", regexpMust(`^(true|false)$`)),
				resource.TestCheckResourceAttrSet("data.entrepot_system_info.this", "gitlab_instances.#"),
			),
		}},
	})
}
