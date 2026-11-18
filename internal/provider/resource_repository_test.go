package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

func TestAccRepositoryHostedProxyGroup(t *testing.T) {
	s := testServer(t)
	sfx := randSuffix()
	cfg := func(desc string, anon bool, ttl int) string {
		return s.ProviderBlock() + fmt.Sprintf(`
resource "entrepot_repository" "hosted" {
  name        = "npm-internal-%[1]s"
  format      = "npm"
  kind        = "hosted"
  blob_store  = "default"
  description = %[2]q
}

resource "entrepot_repository" "proxy" {
  name           = "npmjs-%[1]s"
  format         = "npm"
  kind           = "proxy"
  blob_store     = "default"
  anonymous_read = %[3]t
  proxy = {
    remote_url           = "https://registry.npmjs.org"
    username             = "svc"
    password             = "s3cret"
    metadata_ttl_seconds = %[4]d
  }
}

resource "entrepot_repository" "group" {
  name       = "npm-%[1]s"
  format     = "npm"
  kind       = "group"
  blob_store = "default"
  group = {
    members = [entrepot_repository.hosted.name, entrepot_repository.proxy.name]
  }
}

resource "entrepot_repository" "apt" {
  name       = "apt-%[1]s"
  format     = "apt"
  kind       = "hosted"
  blob_store = "default"
  hosted = {
    apt = { distributions = [{ name = "bookworm", components = ["main", "contrib"], architectures = ["amd64", "all"] }] }
  }
}

resource "entrepot_repository" "docker" {
  name       = "docker-%[1]s"
  format     = "docker"
  kind       = "hosted"
  blob_store = "default"
}
`, sfx, desc, anon, ttl)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				Config: cfg("first", true, 600),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_repository.hosted", "hosted.allow_redeploy", "false"),
					resource.TestCheckResourceAttr("entrepot_repository.docker", "hosted.allow_redeploy", "true"),
					resource.TestCheckResourceAttr("entrepot_repository.proxy", "proxy.metadata_ttl_seconds", "600"),
					resource.TestCheckResourceAttr("entrepot_repository.proxy", "url", s.URL+"/repository/npmjs-"+sfx+"/"),
					resource.TestCheckResourceAttr("entrepot_repository.group", "group.prefer_hosted", "true"),
					resource.TestCheckResourceAttr("entrepot_repository.group", "group.members.#", "2"),
					resource.TestCheckResourceAttr("entrepot_repository.apt", "hosted.apt.distributions.0.components.#", "2"),
				),
			},
			// Mutable fields update in place.
			{
				Config: cfg("second", false, 1200),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_repository.hosted", plancheck.ResourceActionUpdate),
					plancheck.ExpectResourceAction("entrepot_repository.proxy", plancheck.ResourceActionUpdate),
					plancheck.ExpectResourceAction("entrepot_repository.group", plancheck.ResourceActionNoop),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_repository.hosted", "description", "second"),
					resource.TestCheckResourceAttr("entrepot_repository.proxy", "anonymous_read", "false"),
				),
			},
			{ResourceName: "entrepot_repository.proxy", ImportState: true, ImportStateId: "npmjs-" + sfx,
				ImportStateVerify: true, ImportStateVerifyIgnore: []string{"proxy.password"}},
			{ResourceName: "entrepot_repository.apt", ImportState: true, ImportStateId: "apt-" + sfx, ImportStateVerify: true},
			{ResourceName: "entrepot_repository.group", ImportState: true, ImportStateId: "npm-" + sfx, ImportStateVerify: true},
			// Out-of-band deletion is recreated.
			{
				PreConfig: func() {
					if _, err := s.API.DeleteRepositoryWithResponse(context.Background(), "docker-"+sfx); err != nil {
						t.Fatal(err)
					}
				},
				Config: cfg("second", false, 1200),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_repository.docker", plancheck.ResourceActionCreate),
				}},
			},
		},
	})
}

func TestAccRepositoryImmutableForcesReplace(t *testing.T) {
	s := testServer(t)
	name := "raw-" + randSuffix()
	cfg := func(format string) string {
		return s.ProviderBlock() + fmt.Sprintf(`
resource "entrepot_repository" "r" {
  name       = %q
  format     = %q
  kind       = "hosted"
  blob_store = "default"
}`, name, format)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{Config: cfg("raw")},
			{
				Config: cfg("helm"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_repository.r", plancheck.ResourceActionDestroyBeforeCreate),
				}},
				Check: resource.TestCheckResourceAttr("entrepot_repository.r", "format", "helm"),
			},
		},
	})
}

func TestAccRepositoryValidation(t *testing.T) {
	s := testServer(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{{
			Config: s.ProviderBlock() + `
resource "entrepot_repository" "bad" {
  name       = "bad-proxy"
  format     = "npm"
  kind       = "proxy"
  blob_store = "default"
}`,
			ExpectError: regexpMust(`requires proxy`),
		}},
	})
}
