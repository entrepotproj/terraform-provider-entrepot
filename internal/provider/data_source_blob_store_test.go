package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccDataSourceBlobStore(t *testing.T) {
	s := testServer(t)
	sfx := randSuffix()
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				Config: s.ProviderBlock() + fmt.Sprintf(`
resource "entrepot_blob_store" "bs" {
  name = "ds-bs-%[1]s"
  type = "file"
  file = { path = "%[2]s/ds-%[1]s" }
}

resource "entrepot_repository" "raw" {
  name       = "ds-raw-%[1]s"
  format     = "raw"
  kind       = "hosted"
  blob_store = entrepot_blob_store.bs.name
}

data "entrepot_blob_store" "bs" {
  name       = entrepot_blob_store.bs.name
  depends_on = [entrepot_repository.raw]
}

data "entrepot_blob_store" "default" {
  name = "default"
}
`, sfx, s.DataDir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.entrepot_blob_store.bs", "id", "ds-bs-"+sfx),
					resource.TestCheckResourceAttr("data.entrepot_blob_store.bs", "type", "file"),
					resource.TestCheckResourceAttr("data.entrepot_blob_store.bs", "file.path", s.DataDir+"/ds-"+sfx),
					resource.TestCheckNoResourceAttr("data.entrepot_blob_store.bs", "s3"),
					resource.TestCheckResourceAttr("data.entrepot_blob_store.bs", "usage.repositories", "1"),
					resource.TestCheckResourceAttr("data.entrepot_blob_store.bs", "usage.blob_count", "0"),
					resource.TestCheckResourceAttr("data.entrepot_blob_store.bs", "usage.total_bytes", "0"),
					resource.TestCheckResourceAttr("data.entrepot_blob_store.default", "type", "file"),
					resource.TestCheckResourceAttrSet("data.entrepot_blob_store.default", "file.path"),
				),
			},
			{
				Config:      s.ProviderBlock() + `data "entrepot_blob_store" "missing" { name = "no-such-store" }`,
				ExpectError: regexpMust(`Blob store not found`),
			},
		},
	})
}
