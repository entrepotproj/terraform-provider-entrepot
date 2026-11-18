package provider

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccCleanupPolicy(t *testing.T) {
	s := testServer(t)
	name := "cp-" + randSuffix()
	cfg := func(name, body string) string {
		return s.ProviderBlock() + fmt.Sprintf(`
resource "entrepot_cleanup_policy" "test" {
  name = %q
%s
}`, name, body)
	}
	first := cfg(name, `
  description = "drop old release candidates"
  format      = "npm"
  criteria = {
    published_older_than_days = 30
    version_regex             = "-rc\\."
    prerelease_only           = true
    keep_latest               = 3
  }`)
	second := cfg(name, `
  criteria = {
    last_downloaded_older_than_days = 90
    name_regex                      = "^@acme/"
    prerelease_only                 = false
    keep_latest                     = 0
  }`)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				Config: first,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_cleanup_policy.test", "id", name),
					resource.TestCheckResourceAttr("entrepot_cleanup_policy.test", "format", "npm"),
					resource.TestCheckResourceAttr("entrepot_cleanup_policy.test", "criteria.published_older_than_days", "30"),
					resource.TestCheckResourceAttr("entrepot_cleanup_policy.test", "criteria.version_regex", `-rc\.`),
					resource.TestCheckResourceAttr("entrepot_cleanup_policy.test", "criteria.prerelease_only", "true"),
					resource.TestCheckResourceAttr("entrepot_cleanup_policy.test", "criteria.keep_latest", "3"),
					resource.TestCheckNoResourceAttr("entrepot_cleanup_policy.test", "criteria.name_regex"),
					resource.TestCheckResourceAttr("entrepot_cleanup_policy.test", "repositories.#", "0"),
				),
			},
			// In place; false and 0 are kept distinct from unset.
			{
				Config: second,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_cleanup_policy.test", plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("entrepot_cleanup_policy.test", "format"),
					resource.TestCheckResourceAttr("entrepot_cleanup_policy.test", "description", ""),
					resource.TestCheckNoResourceAttr("entrepot_cleanup_policy.test", "criteria.published_older_than_days"),
					resource.TestCheckResourceAttr("entrepot_cleanup_policy.test", "criteria.last_downloaded_older_than_days", "90"),
					resource.TestCheckResourceAttr("entrepot_cleanup_policy.test", "criteria.prerelease_only", "false"),
					resource.TestCheckResourceAttr("entrepot_cleanup_policy.test", "criteria.keep_latest", "0"),
				),
			},
			{ResourceName: "entrepot_cleanup_policy.test", ImportState: true, ImportStateId: name, ImportStateVerify: true},
			{
				PreConfig: func() {
					if _, err := s.API.DeleteCleanupPolicyWithResponse(context.Background(), name); err != nil {
						t.Fatal(err)
					}
				},
				Config: second,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_cleanup_policy.test", plancheck.ResourceActionCreate),
				}},
			},
			// Docker-only criterion, and a rename forcing replacement.
			{
				Config: cfg(name+"-b", `
  format   = "docker"
  criteria = { untagged_older_than_days = 7 }`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_cleanup_policy.test", plancheck.ResourceActionDestroyBeforeCreate),
				}},
				Check: resource.TestCheckResourceAttr("entrepot_cleanup_policy.test", "criteria.untagged_older_than_days", "7"),
			},
		},
	})
}

func TestAccCleanupPolicyValidation(t *testing.T) {
	s := testServer(t)
	cfg := func(body string) string {
		return s.ProviderBlock() + fmt.Sprintf(`
resource "entrepot_cleanup_policy" "bad" {
  name = "bad"
%s
}`, body)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{Config: cfg(`  criteria = { name_regex = "(unclosed" }`), ExpectError: regexpMust(`(?s)Invalid regular expression.*unclosed`)},
			{Config: cfg(`  criteria = {}`), ExpectError: regexpMust(`set at least one criterion`)},
			{Config: cfg(`
  format   = "npm"
  criteria = { untagged_older_than_days = 7 }`), ExpectError: regexpMust(`only applies to docker`)},
			{Config: cfg(`
  format   = "maven"
  criteria = { keep_latest = 1 }`), ExpectError: regexpMust(`value must be one of`)},
			{Config: cfg(`  criteria = { published_older_than_days = 0 }`), ExpectError: regexpMust(`must be at least 1`)},
		},
	})
}

// TestAccHygieneWiredIntoRepository wires a scanner, a cleanup policy and a
// routing rule into a proxy repository: the computed `repositories` lists
// must not cause diffs, updates while attached must apply cleanly, deleting
// an in-use scanner surfaces the server's 409, and destroy runs repository
// first.
func TestAccHygieneWiredIntoRepository(t *testing.T) {
	s := testServer(t)
	sfx := randSuffix()
	repo := "npmjs-" + sfx
	objects := func(policyDesc string) string {
		return fmt.Sprintf(`
resource "entrepot_scanner" "s" {
  name  = "grype-%[1]s"
  type  = "grype"
  grype = { timeout_seconds = 120 }
}

resource "entrepot_cleanup_policy" "p" {
  name        = "old-%[1]s"
  description = %[2]q
  format      = "npm"
  criteria    = { published_older_than_days = 180, keep_latest = 5 }
}

resource "entrepot_routing_rule" "r" {
  name     = "internal-%[1]s"
  mode     = "block"
  matchers = ["^@acme/"]
}
`, sfx, policyDesc)
	}
	repository := func(scanner, policies string) string {
		return fmt.Sprintf(`
resource "entrepot_repository" "npm" {
  name       = %q
  format     = "npm"
  kind       = "proxy"
  blob_store = "default"
  proxy = {
    remote_url   = "https://registry.npmjs.org"
    routing_rule = entrepot_routing_rule.r.name
  }
  scan             = { scanners = [%s], mode = "quarantine" }
  cleanup_policies = %s
}
`, repo, scanner, policies)
	}
	wired := s.ProviderBlock() + objects("v1") + repository("entrepot_scanner.s.name", "[entrepot_cleanup_policy.p.name]")
	// Policy changed and detached from the repository in the same apply.
	detached := s.ProviderBlock() + objects("v2") + repository("entrepot_scanner.s.name", "null")
	// The scanner resource removed while the repository still names it
	// literally: Terraform tries to delete it first and the server refuses.
	scannerRemoved := s.ProviderBlock() + strings.Replace(objects("v2"), `resource "entrepot_scanner" "s"`, `resource "entrepot_scanner" "unused"`, 1) +
		repository(fmt.Sprintf("%q", "grype-"+sfx), "null")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		CheckDestroy: func(*terraform.State) error {
			ctx := context.Background()
			if r, err := s.API.GetRepositoryWithResponse(ctx, repo); err != nil || r.StatusCode() != 404 {
				return fmt.Errorf("repository %s still exists (%v)", repo, err)
			}
			for _, c := range []struct {
				what string
				get  func() (int, error)
			}{
				{"scanner", func() (int, error) {
					r, err := s.API.GetScannerWithResponse(ctx, "grype-"+sfx)
					return r.StatusCode(), err
				}},
				{"cleanup policy", func() (int, error) {
					r, err := s.API.GetCleanupPolicyWithResponse(ctx, "old-"+sfx)
					return r.StatusCode(), err
				}},
				{"routing rule", func() (int, error) {
					r, err := s.API.GetRoutingRuleWithResponse(ctx, "internal-"+sfx)
					return r.StatusCode(), err
				}},
			} {
				if code, err := c.get(); err != nil || code != 404 {
					return fmt.Errorf("%s still exists (status %d, %v)", c.what, code, err)
				}
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: wired,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_repository.npm", "proxy.routing_rule", "internal-"+sfx),
					resource.TestCheckResourceAttr("entrepot_repository.npm", "scan.scanners.0", "grype-"+sfx),
					resource.TestCheckResourceAttr("entrepot_repository.npm", "cleanup_policies.0", "old-"+sfx),
				),
			},
			// Re-plan: no changes, and the refreshed `repositories` lists name the repository.
			{
				Config: wired,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_cleanup_policy.p", "repositories.#", "1"),
					resource.TestCheckResourceAttr("entrepot_cleanup_policy.p", "repositories.0", repo),
					resource.TestCheckResourceAttr("entrepot_routing_rule.r", "repositories.#", "1"),
					resource.TestCheckResourceAttr("entrepot_routing_rule.r", "repositories.0", repo),
				),
			},
			{Config: wired, PlanOnly: true},
			// Update the policy and detach it in one apply.
			{
				Config: detached,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("entrepot_cleanup_policy.p", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("entrepot_repository.npm", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("entrepot_routing_rule.r", plancheck.ResourceActionNoop),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				Config:           detached,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_cleanup_policy.p", "repositories.#", "0"),
					resource.TestCheckResourceAttr("entrepot_cleanup_policy.p", "description", "v2"),
					resource.TestCheckNoResourceAttr("entrepot_repository.npm", "cleanup_policies"),
				),
			},
			// Deleting a scanner in use fails with the server's message.
			{
				Config:      scannerRemoved,
				ExpectError: regexpMust(`(?s)Unable to delete scanner.*409.*used by repositories`),
			},
			// The failed delete left the scanner in state; back to normal.
			{
				Config:           detached,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{ResourceName: "entrepot_cleanup_policy.p", ImportState: true, ImportStateId: "old-" + sfx, ImportStateVerify: true},
			{ResourceName: "entrepot_routing_rule.r", ImportState: true, ImportStateId: "internal-" + sfx, ImportStateVerify: true},
		},
	})
}
