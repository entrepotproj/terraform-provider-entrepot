package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
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
		}, {
			Config: s.ProviderBlock() + `
resource "entrepot_repository" "bad" {
  name       = "bad-docker-host"
  format     = "npm"
  kind       = "hosted"
  blob_store = "default"
  docker     = { host = "npm.example.com" }
}`,
			ExpectError: regexpMust(`docker is only valid for format = "docker"`),
		}, {
			Config: s.ProviderBlock() + `
resource "entrepot_repository" "bad" {
  name       = "bad-docker-host"
  format     = "docker"
  kind       = "hosted"
  blob_store = "default"
  docker     = { host = "https://hub.example.com" }
}`,
			ExpectError: regexpMust(`must be a hostname`),
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

func TestAccRepositoryDockerHost(t *testing.T) {
	s := testServer(t)
	requireDockerHost(t, s)
	sfx := randSuffix()
	cfg := func(docker string) string {
		return s.ProviderBlock() + fmt.Sprintf(`
resource "entrepot_repository" "test" {
  name       = "dh-%[1]s"
  format     = "docker"
  kind       = "hosted"
  blob_store = "default"
%[2]s
}

data "entrepot_repository" "test" {
  name       = entrepot_repository.test.name
  depends_on = [entrepot_repository.test]
}`, sfx, docker)
	}
	// The server lowercases the hostname; the configured spelling is kept.
	first := cfg(fmt.Sprintf(`  docker = { host = "Hub-%s.Example.test" }`, sfx))
	second := cfg(fmt.Sprintf(`  docker = { host = "registry-%s.example.test" }`, sfx))
	none := cfg("")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				Config: first,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_repository.test", "docker.host", "Hub-"+sfx+".Example.test"),
					resource.TestCheckResourceAttr("entrepot_repository.test", "url", "hub-"+sfx+".example.test"),
					resource.TestCheckResourceAttr("data.entrepot_repository.test", "docker.host", "hub-"+sfx+".example.test"),
					resource.TestCheckResourceAttr("data.entrepot_repository.test", "url", "hub-"+sfx+".example.test"),
				),
			},
			{Config: first, PlanOnly: true},
			{
				Config: second,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_repository.test", plancheck.ResourceActionUpdate),
					plancheck.ExpectUnknownValue("entrepot_repository.test", tfjsonpath.New("url")),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_repository.test", "docker.host", "registry-"+sfx+".example.test"),
					resource.TestCheckResourceAttr("entrepot_repository.test", "url", "registry-"+sfx+".example.test"),
				),
			},
			// Without a hostname the repository is addressed by path again.
			{
				Config: none,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("entrepot_repository.test", "docker"),
					resource.TestMatchResourceAttr("entrepot_repository.test", "url", regexpMust(`/dh-`+sfx+`$`)),
					resource.TestCheckNoResourceAttr("data.entrepot_repository.test", "docker"),
				),
			},
			{Config: none, PlanOnly: true},
			{ResourceName: "entrepot_repository.test", ImportState: true, ImportStateId: "dh-" + sfx, ImportStateVerify: true},
		},
	})
}
