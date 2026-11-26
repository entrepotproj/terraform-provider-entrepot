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

func TestAccRepositoryScanHoldAndBlockOnError(t *testing.T) {
	s := testServer(t)
	requireScanFeatures(t, s)
	sfx := randSuffix()
	cfg := func(scanExtra string) string {
		return s.ProviderBlock() + fmt.Sprintf(`
resource "entrepot_scanner" "s" {
  name = "hold-grype-%[1]s"
  type = "grype"
}

resource "entrepot_repository" "test" {
  name       = "hold-npm-%[1]s"
  format     = "npm"
  kind       = "hosted"
  blob_store = "default"
  scan = {
    scanners            = [entrepot_scanner.s.name]
    mode                = "quarantine"
    block_until_scanned = true
%[2]s
  }
}

data "entrepot_repository" "test" {
  name = entrepot_repository.test.name
}`, sfx, scanExtra)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				Config: cfg("    hold_seconds   = 60\n    block_on_error = true"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_repository.test", "scan.hold_seconds", "60"),
					resource.TestCheckResourceAttr("entrepot_repository.test", "scan.block_on_error", "true"),
					resource.TestCheckResourceAttr("data.entrepot_repository.test", "scan.hold_seconds", "60"),
					resource.TestCheckResourceAttr("data.entrepot_repository.test", "scan.block_on_error", "true"),
				),
			},
			{
				Config: cfg("    hold_seconds   = 300\n    block_on_error = false"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_repository.test", plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_repository.test", "scan.hold_seconds", "300"),
					resource.TestCheckResourceAttr("entrepot_repository.test", "scan.block_on_error", "false"),
				),
			},
			// Back to the server defaults: no hold, fail open.
			{
				Config: cfg("    hold_seconds = 0"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_repository.test", "scan.hold_seconds", "0"),
					resource.TestCheckResourceAttr("entrepot_repository.test", "scan.block_on_error", "false"),
					resource.TestCheckResourceAttr("data.entrepot_repository.test", "scan.hold_seconds", "0"),
				),
			},
			// Unset attributes keep the server's values (computed): no diff.
			{Config: cfg(""), PlanOnly: true},
			{ResourceName: "entrepot_repository.test", ImportState: true, ImportStateId: "hold-npm-" + sfx, ImportStateVerify: true},
		},
	})
}
