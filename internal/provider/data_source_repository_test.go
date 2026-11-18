package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccDataSourceRepository(t *testing.T) {
	s := testServer(t)
	sfx := randSuffix()
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				Config: s.ProviderBlock() + fmt.Sprintf(`
resource "entrepot_scanner" "s" {
  name = "dsr-grype-%[1]s"
  type = "grype"
}

resource "entrepot_cleanup_policy" "p" {
  name     = "dsr-old-%[1]s"
  criteria = { published_older_than_days = 365 }
}

resource "entrepot_routing_rule" "r" {
  name     = "dsr-block-%[1]s"
  mode     = "block"
  matchers = ["^@acme/"]
}

resource "entrepot_repository" "hosted" {
  name             = "dsr-npm-%[1]s"
  format           = "npm"
  kind             = "hosted"
  blob_store       = "default"
  description      = "internal packages"
  anonymous_read   = true
  scan             = { scanners = [entrepot_scanner.s.name], mode = "quarantine", severity_threshold = "critical" }
  cleanup_policies = [entrepot_cleanup_policy.p.name]
}

resource "entrepot_repository" "proxy" {
  name       = "dsr-npmjs-%[1]s"
  format     = "npm"
  kind       = "proxy"
  blob_store = "default"
  proxy = {
    remote_url           = "https://registry.npmjs.org"
    username             = "svc"
    password             = "s3cret"
    metadata_ttl_seconds = 600
    routing_rule         = entrepot_routing_rule.r.name
  }
}

resource "entrepot_repository" "group" {
  name       = "dsr-group-%[1]s"
  format     = "npm"
  kind       = "group"
  blob_store = "default"
  group      = { members = [entrepot_repository.hosted.name, entrepot_repository.proxy.name] }
}

resource "entrepot_repository" "apt" {
  name       = "dsr-apt-%[1]s"
  format     = "apt"
  kind       = "hosted"
  blob_store = "default"
  hosted = {
    apt = { distributions = [{ name = "bookworm", components = ["main"], architectures = ["amd64"] }] }
  }
}

data "entrepot_repository" "hosted" {
  name = entrepot_repository.hosted.name
}

data "entrepot_repository" "proxy" {
  name = entrepot_repository.proxy.name
}

data "entrepot_repository" "group" {
  name = entrepot_repository.group.name
}

data "entrepot_repository" "apt" {
  name = entrepot_repository.apt.name
}
`, sfx),
				Check: resource.ComposeAggregateTestCheckFunc(
					// Hosted: every scalar attribute matches the resource.
					resource.TestCheckResourceAttrPair("data.entrepot_repository.hosted", "id", "entrepot_repository.hosted", "id"),
					resource.TestCheckResourceAttrPair("data.entrepot_repository.hosted", "url", "entrepot_repository.hosted", "url"),
					resource.TestCheckResourceAttr("data.entrepot_repository.hosted", "format", "npm"),
					resource.TestCheckResourceAttr("data.entrepot_repository.hosted", "kind", "hosted"),
					resource.TestCheckResourceAttr("data.entrepot_repository.hosted", "blob_store", "default"),
					resource.TestCheckResourceAttr("data.entrepot_repository.hosted", "description", "internal packages"),
					resource.TestCheckResourceAttr("data.entrepot_repository.hosted", "online", "true"),
					resource.TestCheckResourceAttr("data.entrepot_repository.hosted", "anonymous_read", "true"),
					resource.TestCheckResourceAttr("data.entrepot_repository.hosted", "hosted.allow_redeploy", "false"),
					resource.TestCheckResourceAttr("data.entrepot_repository.hosted", "scan.scanners.0", "dsr-grype-"+sfx),
					resource.TestCheckResourceAttr("data.entrepot_repository.hosted", "scan.mode", "quarantine"),
					resource.TestCheckResourceAttr("data.entrepot_repository.hosted", "scan.severity_threshold", "critical"),
					resource.TestCheckResourceAttrPair("data.entrepot_repository.hosted", "scan.on_upload", "entrepot_repository.hosted", "scan.on_upload"),
					resource.TestCheckResourceAttr("data.entrepot_repository.hosted", "cleanup_policies.0", "dsr-old-"+sfx),
					resource.TestCheckNoResourceAttr("data.entrepot_repository.hosted", "proxy"),
					resource.TestCheckNoResourceAttr("data.entrepot_repository.hosted", "group"),
					resource.TestCheckTypeSetElemAttr("data.entrepot_repository.hosted", "permissions.*", "admin"),
					resource.TestCheckTypeSetElemAttr("data.entrepot_repository.hosted", "permissions.*", "read"),
					// Proxy: no password, routing rule set.
					resource.TestCheckResourceAttr("data.entrepot_repository.proxy", "proxy.remote_url", "https://registry.npmjs.org"),
					resource.TestCheckResourceAttr("data.entrepot_repository.proxy", "proxy.username", "svc"),
					resource.TestCheckResourceAttr("data.entrepot_repository.proxy", "proxy.metadata_ttl_seconds", "600"),
					resource.TestCheckResourceAttr("data.entrepot_repository.proxy", "proxy.routing_rule", "dsr-block-"+sfx),
					resource.TestCheckNoResourceAttr("data.entrepot_repository.proxy", "proxy.password"),
					resource.TestCheckNoResourceAttr("data.entrepot_repository.proxy", "hosted"),
					resource.TestCheckNoResourceAttr("data.entrepot_repository.proxy", "cleanup_policies"),
					// Group.
					resource.TestCheckResourceAttr("data.entrepot_repository.group", "group.members.#", "2"),
					resource.TestCheckResourceAttr("data.entrepot_repository.group", "group.members.1", "dsr-npmjs-"+sfx),
					resource.TestCheckResourceAttr("data.entrepot_repository.group", "group.prefer_hosted", "true"),
					// Apt distributions.
					resource.TestCheckResourceAttr("data.entrepot_repository.apt", "hosted.apt.distributions.0.name", "bookworm"),
					resource.TestCheckResourceAttr("data.entrepot_repository.apt", "hosted.apt.distributions.0.architectures.0", "amd64"),
				),
			},
			{
				Config:      s.ProviderBlock() + `data "entrepot_repository" "missing" { name = "no-such-repo" }`,
				ExpectError: regexpMust(`Repository not found`),
			},
		},
	})
}
