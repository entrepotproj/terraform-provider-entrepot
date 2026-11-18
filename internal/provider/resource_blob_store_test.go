package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

func TestAccBlobStore(t *testing.T) {
	s := testServer(t)
	name := "bs-" + randSuffix()
	cfg := func(dir string) string {
		return s.ProviderBlock() + fmt.Sprintf(`
resource "entrepot_blob_store" "test" {
  name = %q
  type = "file"
  file = { path = %q }
}`, name, s.DataDir+"/"+dir)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				Config: cfg("one"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_blob_store.test", "name", name),
					resource.TestCheckResourceAttr("entrepot_blob_store.test", "file.path", s.DataDir+"/one"),
				),
			},
			// In-place update of the path.
			{
				Config: cfg("two"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_blob_store.test", plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr("entrepot_blob_store.test", "file.path", s.DataDir+"/two"),
			},
			{ResourceName: "entrepot_blob_store.test", ImportState: true, ImportStateId: name, ImportStateVerify: true},
			// Deleted outside Terraform → recreated.
			{
				PreConfig: func() {
					if _, err := s.API.DeleteBlobStoreWithResponse(context.Background(), name); err != nil {
						t.Fatal(err)
					}
				},
				Config: cfg("two"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_blob_store.test", plancheck.ResourceActionCreate),
				}},
			},
		},
	})
}

func TestAccBlobStoreInvalidPath(t *testing.T) {
	s := testServer(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{{
			Config: s.ProviderBlock() + `
resource "entrepot_blob_store" "bad" {
  name = "outside-roots"
  type = "file"
  file = { path = "/etc" }
}`,
			ExpectError: regexpMust(`allowedRoots`),
		}},
	})
}
