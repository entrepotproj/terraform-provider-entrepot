package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

func TestAccScannerTrivy(t *testing.T) {
	s := testServer(t)
	name := "scn-" + randSuffix()
	cfg := func(typ, body string) string {
		return s.ProviderBlock() + fmt.Sprintf(`
resource "entrepot_scanner" "test" {
  name = %q
  type = %q
%s
}`, name, typ, body)
	}
	// The trailing slash is trimmed by the server; the configured spelling is kept.
	trivy1 := cfg("trivy", `
  rescan_interval_hours = 24
  trivy = {
    server_url      = "http://trivy.example.internal:4954/"
    server_token    = "token-one"
    timeout_seconds = 120
    extra_args      = ["--skip-db-update"]
  }`)
	trivy2 := cfg("trivy", `
  enabled = false
  trivy = {
    binary          = "/usr/local/bin/trivy"
    server_url      = "http://trivy.example.internal:4954"
    server_token    = "token-two"
    timeout_seconds = 300
  }`)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				Config: trivy1,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_scanner.test", "id", name),
					resource.TestCheckResourceAttr("entrepot_scanner.test", "enabled", "true"),
					resource.TestCheckResourceAttr("entrepot_scanner.test", "rescan_interval_hours", "24"),
					resource.TestCheckResourceAttr("entrepot_scanner.test", "trivy.server_url", "http://trivy.example.internal:4954/"),
					resource.TestCheckResourceAttr("entrepot_scanner.test", "trivy.server_token", "token-one"),
					resource.TestCheckResourceAttr("entrepot_scanner.test", "trivy.timeout_seconds", "120"),
					resource.TestCheckResourceAttr("entrepot_scanner.test", "trivy.extra_args.0", "--skip-db-update"),
					resource.TestCheckNoResourceAttr("entrepot_scanner.test", "trivy.binary"),
					resource.TestCheckNoResourceAttr("entrepot_scanner.test", "grype"),
				),
			},
			// In-place update.
			{
				Config: trivy2,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_scanner.test", plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_scanner.test", "enabled", "false"),
					resource.TestCheckResourceAttr("entrepot_scanner.test", "rescan_interval_hours", "0"),
					resource.TestCheckResourceAttr("entrepot_scanner.test", "trivy.binary", "/usr/local/bin/trivy"),
					resource.TestCheckResourceAttr("entrepot_scanner.test", "trivy.timeout_seconds", "300"),
					resource.TestCheckNoResourceAttr("entrepot_scanner.test", "trivy.extra_args"),
				),
			},
			{ResourceName: "entrepot_scanner.test", ImportState: true, ImportStateId: name,
				ImportStateVerify: true, ImportStateVerifyIgnore: []string{"trivy.server_token"}},
			// Deleted outside Terraform → recreated.
			{
				PreConfig: func() {
					if _, err := s.API.DeleteScannerWithResponse(context.Background(), name); err != nil {
						t.Fatal(err)
					}
				},
				Config: trivy2,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_scanner.test", plancheck.ResourceActionCreate),
				}},
			},
			// Changing the type forces a new scanner.
			{
				Config: cfg("grype", `  grype = { timeout_seconds = 60 }`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_scanner.test", plancheck.ResourceActionDestroyBeforeCreate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_scanner.test", "type", "grype"),
					resource.TestCheckResourceAttr("entrepot_scanner.test", "grype.timeout_seconds", "60"),
					resource.TestCheckNoResourceAttr("entrepot_scanner.test", "trivy"),
				),
			},
			// So does renaming it.
			{
				Config: s.ProviderBlock() + fmt.Sprintf(`
resource "entrepot_scanner" "test" {
  name  = "%s-b"
  type  = "grype"
  grype = { timeout_seconds = 60 }
}`, name),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_scanner.test", plancheck.ResourceActionDestroyBeforeCreate),
				}},
			},
		},
	})
}

func TestAccScannerWebhookAndDefaults(t *testing.T) {
	s := testServer(t)
	sfx := randSuffix()
	cfg := func(url, secret string) string {
		return s.ProviderBlock() + fmt.Sprintf(`
resource "entrepot_scanner" "webhook" {
  name = "hook-%[1]s"
  type = "webhook"
  webhook = {
    url             = %[2]q
    secret          = %[3]q
    timeout_seconds = 600
  }
}

# No settings block: server defaults.
resource "entrepot_scanner" "grype" {
  name = "grype-%[1]s"
  type = "grype"
}

resource "entrepot_scanner" "trivy" {
  name = "trivy-%[1]s"
  type = "trivy"
}
`, sfx, url, secret)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				Config: cfg("https://scanner.example.com/scan", "hmac-one"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_scanner.webhook", "webhook.url", "https://scanner.example.com/scan"),
					resource.TestCheckResourceAttr("entrepot_scanner.webhook", "webhook.secret", "hmac-one"),
					resource.TestCheckResourceAttr("entrepot_scanner.webhook", "webhook.timeout_seconds", "600"),
					resource.TestCheckNoResourceAttr("entrepot_scanner.grype", "grype"),
					resource.TestCheckNoResourceAttr("entrepot_scanner.trivy", "trivy"),
					resource.TestCheckResourceAttr("entrepot_scanner.grype", "enabled", "true"),
				),
			},
			{
				Config: cfg("https://scanner2.example.com/scan", "hmac-two"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_scanner.webhook", plancheck.ResourceActionUpdate),
					plancheck.ExpectResourceAction("entrepot_scanner.grype", plancheck.ResourceActionNoop),
					plancheck.ExpectResourceAction("entrepot_scanner.trivy", plancheck.ResourceActionNoop),
				}},
				Check: resource.TestCheckResourceAttr("entrepot_scanner.webhook", "webhook.url", "https://scanner2.example.com/scan"),
			},
			{ResourceName: "entrepot_scanner.webhook", ImportState: true, ImportStateId: "hook-" + sfx,
				ImportStateVerify: true, ImportStateVerifyIgnore: []string{"webhook.secret"}},
			{ResourceName: "entrepot_scanner.grype", ImportState: true, ImportStateId: "grype-" + sfx, ImportStateVerify: true},
		},
	})
}

func TestAccScannerValidation(t *testing.T) {
	s := testServer(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				Config: s.ProviderBlock() + `
resource "entrepot_scanner" "bad" {
  name    = "mismatch"
  type    = "trivy"
  webhook = { url = "https://scanner.example.com" }
}`,
				ExpectError: regexpMust(`(?s)Settings do not match the scanner type.*only valid for type = "webhook"`),
			},
			{
				Config: s.ProviderBlock() + `
resource "entrepot_scanner" "bad" {
  name = "missing"
  type = "webhook"
}`,
				ExpectError: regexpMust(`requires webhook`),
			},
			{
				Config: s.ProviderBlock() + `
resource "entrepot_scanner" "bad" {
  name    = "badurl"
  type    = "webhook"
  webhook = { url = "ftp://scanner.example.com" }
}`,
				ExpectError: regexpMust(`must be an http\(s\) URL`),
			},
		},
	})
}
