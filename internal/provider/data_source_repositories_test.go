package provider

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// allFormatsAre checks every listed repository has the given format.
func allFormatsAre(name, format string) resource.TestCheckFunc {
	return func(st *terraform.State) error {
		rs, ok := st.RootModule().Resources[name]
		if !ok {
			return fmt.Errorf("%s not found", name)
		}
		n, _ := strconv.Atoi(rs.Primary.Attributes["repositories.#"])
		for i := 0; i < n; i++ {
			if f := rs.Primary.Attributes[fmt.Sprintf("repositories.%d.format", i)]; f != format {
				return fmt.Errorf("repositories.%d.format = %q, want %q", i, f, format)
			}
		}
		return nil
	}
}

func TestAccDataSourceRepositories(t *testing.T) {
	s := testServer(t)
	sfx := randSuffix()
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{{
			Config: s.ProviderBlock() + fmt.Sprintf(`
resource "entrepot_repository" "npm" {
  name           = "dsl-npm-%[1]s"
  format         = "npm"
  kind           = "hosted"
  blob_store     = "default"
  anonymous_read = true
}

resource "entrepot_repository" "npmjs" {
  name       = "dsl-npmjs-%[1]s"
  format     = "npm"
  kind       = "proxy"
  blob_store = "default"
  online     = false
  proxy      = { remote_url = "https://registry.npmjs.org" }
}

resource "entrepot_repository" "raw" {
  name       = "dsl-raw-%[1]s"
  format     = "raw"
  kind       = "hosted"
  blob_store = "default"
}

data "entrepot_repositories" "npm" {
  format     = "npm"
  depends_on = [entrepot_repository.npm, entrepot_repository.npmjs, entrepot_repository.raw]
}

data "entrepot_repositories" "all" {
  depends_on = [entrepot_repository.npm, entrepot_repository.npmjs, entrepot_repository.raw]
}
`, sfx),
			Check: resource.ComposeAggregateTestCheckFunc(
				allFormatsAre("data.entrepot_repositories.npm", "npm"),
				resource.TestCheckTypeSetElemNestedAttrs("data.entrepot_repositories.npm", "repositories.*", map[string]string{
					"name": "dsl-npm-" + sfx, "format": "npm", "kind": "hosted", "blob_store": "default",
					"online": "true", "anonymous_read": "true", "url": s.URL + "/repository/dsl-npm-" + sfx + "/",
				}),
				resource.TestCheckTypeSetElemNestedAttrs("data.entrepot_repositories.npm", "repositories.*", map[string]string{
					"name": "dsl-npmjs-" + sfx, "kind": "proxy", "online": "false", "anonymous_read": "false",
				}),
				resource.TestCheckTypeSetElemAttr("data.entrepot_repositories.npm", "names.*", "dsl-npmjs-"+sfx),
				resource.TestCheckTypeSetElemAttr("data.entrepot_repositories.all", "names.*", "dsl-raw-"+sfx),
				resource.TestCheckTypeSetElemAttr("data.entrepot_repositories.all", "names.*", "dsl-npm-"+sfx),
			),
		}},
	})
}
