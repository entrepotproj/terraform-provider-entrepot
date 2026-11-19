package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/entrepotproj/terraform-provider-entrepot/internal/acctest"
	"github.com/entrepotproj/terraform-provider-entrepot/internal/client"
)

// serverFingerprint returns the repository's active key fingerprint on the server.
func serverFingerprint(s *acctest.Server, repo string) (string, error) {
	res, err := s.API.GetSigningKeyWithResponse(context.Background(), repo)
	if err = check(res, err, 200); err != nil {
		return "", err
	}
	return res.JSON200.Fingerprint, nil
}

func TestAccRepositorySigningKey(t *testing.T) {
	s := testServer(t)
	repo := "apt-sig-" + randSuffix()
	keyA, _, fpA := testOpenPGPKey(t, "")
	keyB, _, fpB := testOpenPGPKey(t, "correct horse")
	repoBlock := fmt.Sprintf(`
resource "entrepot_repository" "apt" {
  name       = %q
  format     = "apt"
  kind       = "hosted"
  blob_store = "default"
}
`, repo)
	cfg := func(key, passphrase string) string {
		pass := "null"
		if passphrase != "" {
			pass = fmt.Sprintf("%q", passphrase)
		}
		return s.ProviderBlock() + repoBlock + fmt.Sprintf(`
resource "entrepot_repository_signing_key" "test" {
  repository  = entrepot_repository.apt.name
  private_key = %q
  passphrase  = %s
}

data "entrepot_signing_key" "test" {
  repository = entrepot_repository_signing_key.test.repository
  depends_on = [entrepot_repository_signing_key.test]
}
`, key, pass)
	}
	onServer := func(want string) resource.TestCheckFunc {
		return func(*terraform.State) error {
			got, err := serverFingerprint(s, repo)
			if err != nil {
				return err
			}
			if got != want {
				return fmt.Errorf("server fingerprint %s, want %s", got, want)
			}
			return nil
		}
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				Config: cfg(keyA, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					// The fingerprint is predicted from the configured key.
					plancheck.ExpectKnownValue("entrepot_repository_signing_key.test", tfjsonpath.New("fingerprint"), knownvalue.StringExact(fpA)),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_repository_signing_key.test", "fingerprint", fpA),
					resource.TestCheckResourceAttr("entrepot_repository_signing_key.test", "algorithm", "rsa2048"),
					resource.TestCheckResourceAttr("entrepot_repository_signing_key.test", "url", s.URL+"/repository/"+repo+"/public.key"),
					resource.TestMatchResourceAttr("entrepot_repository_signing_key.test", "public_key", regexpMust(`^-----BEGIN PGP PUBLIC KEY BLOCK-----`)),
					resource.TestCheckResourceAttrSet("entrepot_repository_signing_key.test", "created_at"),
					resource.TestCheckResourceAttr("data.entrepot_signing_key.test", "fingerprint", fpA),
					resource.TestCheckResourceAttrPair("data.entrepot_signing_key.test", "public_key", "entrepot_repository_signing_key.test", "public_key"),
					resource.TestCheckResourceAttrPair("data.entrepot_signing_key.test", "url", "entrepot_repository_signing_key.test", "url"),
					onServer(fpA),
				),
			},
			// A new (passphrase-protected) key is imported in place.
			{
				Config: cfg(keyB, "correct horse"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_repository_signing_key.test", plancheck.ResourceActionUpdate),
					plancheck.ExpectKnownValue("entrepot_repository_signing_key.test", tfjsonpath.New("fingerprint"), knownvalue.StringExact(fpB)),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_repository_signing_key.test", "fingerprint", fpB),
					resource.TestCheckResourceAttr("data.entrepot_signing_key.test", "fingerprint", fpB),
					onServer(fpB),
				),
			},
			{
				ResourceName: "entrepot_repository_signing_key.test", ImportState: true, ImportStateId: repo,
				ImportStateVerify: true, ImportStateVerifyIgnore: []string{"private_key", "passphrase"},
			},
			// Rotated outside Terraform → the plan shows the drift and re-imports the key.
			{
				PreConfig: func() {
					res, err := s.API.RotateSigningKeyWithResponse(context.Background(), repo)
					if err = check(res, err, 200); err != nil {
						t.Fatal(err)
					}
				},
				Config: cfg(keyB, "correct horse"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_repository_signing_key.test", plancheck.ResourceActionUpdate),
					plancheck.ExpectKnownValue("entrepot_repository_signing_key.test", tfjsonpath.New("fingerprint"), knownvalue.StringExact(fpB)),
				}},
				Check: onServer(fpB),
			},
			// Removing the resource rotates the key (the repository keeps a generated one).
			{
				Config: s.ProviderBlock() + repoBlock,
				Check: func(*terraform.State) error {
					got, err := serverFingerprint(s, repo)
					if err != nil {
						return err
					}
					if got == fpB || got == fpA {
						return fmt.Errorf("key was not rotated on destroy (still %s)", got)
					}
					return nil
				},
			},
		},
	})
}

// Recording the private key after import (or changing only the passphrase)
// makes no API call: the active key is kept.
func TestAccRepositorySigningKeyImportNoop(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()
	repo := "yum-sig-" + randSuffix()
	key, _, fp := testOpenPGPKey(t, "")
	rr, err := s.API.CreateRepositoryWithResponse(ctx, client.RepositoryInput{
		Name: repo, Format: "yum", Kind: "hosted", BlobStore: "default",
	})
	if err = check(rr, err, 201); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = s.API.DeleteRepositoryWithResponse(ctx, repo) })
	kr, err := s.API.ImportSigningKeyWithResponse(ctx, repo, client.SigningKeyImport{PrivateKey: &key})
	if err = check(kr, err, 200); err != nil {
		t.Fatal(err)
	}
	createdAt := timeValue(kr.JSON200.CreatedAt).ValueString()
	cfg := func(passphrase string) string {
		return s.ProviderBlock() + fmt.Sprintf(`
import {
  to = entrepot_repository_signing_key.test
  id = %[1]q
}

resource "entrepot_repository_signing_key" "test" {
  repository  = %[1]q
  private_key = %[2]q
  passphrase  = %[3]s
}
`, repo, key, passphrase)
	}
	unchanged := resource.ComposeAggregateTestCheckFunc(
		resource.TestCheckResourceAttr("entrepot_repository_signing_key.test", "fingerprint", fp),
		resource.TestCheckResourceAttr("entrepot_repository_signing_key.test", "created_at", createdAt),
	)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				Config: cfg("null"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_repository_signing_key.test", plancheck.ResourceActionUpdate),
					plancheck.ExpectKnownValue("entrepot_repository_signing_key.test", tfjsonpath.New("created_at"), knownvalue.StringExact(createdAt)),
				}},
				Check: unchanged,
			},
			{
				Config: cfg(`"unused"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_repository_signing_key.test", plancheck.ResourceActionUpdate),
				}},
				Check: unchanged,
			},
		},
	})
}

func TestAccRepositorySigningKeyValidation(t *testing.T) {
	s := testServer(t)
	key, pub, _ := testOpenPGPKey(t, "")
	sfx := randSuffix()
	sig := func(repo, key string) string {
		return s.ProviderBlock() + fmt.Sprintf(`
resource "entrepot_repository" "raw" {
  name       = "raw-sig-%s"
  format     = "raw"
  kind       = "hosted"
  blob_store = "default"
}

resource "entrepot_repository_signing_key" "bad" {
  repository  = %s
  private_key = %q
}`, sfx, repo, key)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{Config: sig(`"x"`, pub), ExpectError: regexpMust(`not a private key`)},
			{Config: sig(`"x"`, "garbage"), ExpectError: regexpMust(`Invalid OpenPGP private key`)},
			{Config: sig(`entrepot_repository.raw.name`, key), ExpectError: regexpMust(`only available for hosted apt and yum`)},
		},
	})
}
