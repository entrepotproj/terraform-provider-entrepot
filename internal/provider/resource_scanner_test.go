package provider

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"testing"
	"time"

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

// testCertPEM is a self-signed certificate (the server checks that
// ca_certificate holds PEM certificates).
func testCertPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Test CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestAccScannerMetaDefender(t *testing.T) {
	s := testServer(t)
	requireScanFeatures(t, s)
	name := "md-" + randSuffix()
	cfg := func(body string) string {
		return s.ProviderBlock() + fmt.Sprintf(`
resource "entrepot_scanner" "test" {
  name = %q
  type = "metadefender"
  rescan_interval_hours = 168
  metadefender = {
%s
  }
}`, name, body)
	}
	// The server trims the URL's trailing slash and the PEM's final newline;
	// the configured spellings are kept, so there is no perpetual diff.
	full := cfg(fmt.Sprintf(`
    url                  = "https://md.example.internal:8008/"
    api_key              = "key-one"
    rule                 = "Packages"
    timeout_seconds      = 900
    concurrency          = 8
    hash_lookup          = false
    max_result_age_hours = 48
    web_url              = "https://md.example.internal:8008/#/public/process/dataId/{dataId}"
    ca_certificate       = <<-EOT
%sEOT`, testCertPEM(t)))
	updated := cfg(`
    url     = "https://md.example.internal:8008/"
    api_key = "key-one"
    rule    = "File process"`)
	minimal := cfg(`    url = "https://md.example.internal:8008"`)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				Config: full,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_scanner.test", "type", "metadefender"),
					resource.TestCheckResourceAttr("entrepot_scanner.test", "metadefender.url", "https://md.example.internal:8008/"),
					resource.TestCheckResourceAttr("entrepot_scanner.test", "metadefender.api_key", "key-one"),
					resource.TestCheckResourceAttr("entrepot_scanner.test", "metadefender.rule", "Packages"),
					resource.TestCheckResourceAttr("entrepot_scanner.test", "metadefender.timeout_seconds", "900"),
					resource.TestCheckResourceAttr("entrepot_scanner.test", "metadefender.concurrency", "8"),
					resource.TestCheckResourceAttr("entrepot_scanner.test", "metadefender.hash_lookup", "false"),
					resource.TestCheckResourceAttr("entrepot_scanner.test", "metadefender.max_result_age_hours", "48"),
					resource.TestCheckResourceAttrSet("entrepot_scanner.test", "metadefender.ca_certificate"),
					resource.TestCheckNoResourceAttr("entrepot_scanner.test", "trivy"),
					resource.TestCheckNoResourceAttr("entrepot_scanner.test", "webhook"),
				),
			},
			{Config: full, PlanOnly: true},
			// In-place update; unset settings go back to the server defaults.
			{
				Config: updated,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_scanner.test", plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_scanner.test", "metadefender.rule", "File process"),
					resource.TestCheckResourceAttr("entrepot_scanner.test", "metadefender.api_key", "key-one"),
					resource.TestCheckNoResourceAttr("entrepot_scanner.test", "metadefender.concurrency"),
					resource.TestCheckNoResourceAttr("entrepot_scanner.test", "metadefender.hash_lookup"),
					resource.TestCheckNoResourceAttr("entrepot_scanner.test", "metadefender.ca_certificate"),
				),
			},
			{Config: updated, PlanOnly: true},
			{
				Config: minimal,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_scanner.test", "metadefender.url", "https://md.example.internal:8008"),
					resource.TestCheckNoResourceAttr("entrepot_scanner.test", "metadefender.api_key"),
					resource.TestCheckNoResourceAttr("entrepot_scanner.test", "metadefender.rule"),
				),
			},
			{ResourceName: "entrepot_scanner.test", ImportState: true, ImportStateId: name,
				ImportStateVerify: true, ImportStateVerifyIgnore: []string{"metadefender.api_key"}},
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
			{
				Config: s.ProviderBlock() + `
resource "entrepot_scanner" "bad" {
  name = "md-missing"
  type = "metadefender"
}`,
				ExpectError: regexpMust(`requires metadefender`),
			},
			{
				Config: s.ProviderBlock() + `
resource "entrepot_scanner" "bad" {
  name         = "md-mismatch"
  type         = "trivy"
  metadefender = { url = "https://md.example.com" }
}`,
				ExpectError: regexpMust(`(?s)Settings do not match the scanner type.*only valid for type = "metadefender"`),
			},
			{
				Config: s.ProviderBlock() + `
resource "entrepot_scanner" "bad" {
  name         = "md-concurrency"
  type         = "metadefender"
  metadefender = { url = "https://md.example.com", concurrency = 99 }
}`,
				ExpectError: regexpMust(`(?s)concurrency.*between 1 and 32`),
			},
			{
				Config: s.ProviderBlock() + `
resource "entrepot_scanner" "bad" {
  name         = "md-weburl"
  type         = "metadefender"
  metadefender = { url = "https://md.example.com", web_url = "javascript:alert(1)" }
}`,
				ExpectError: regexpMust(`must be an http\(s\) URL`),
			},
		},
	})
}
