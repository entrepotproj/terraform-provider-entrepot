package provider

import (
	"context"
	"net/http"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/entrepotproj/terraform-provider-entrepot/internal/acctest"
	"github.com/entrepotproj/terraform-provider-entrepot/internal/client"
)

func TestMain(m *testing.M) { acctest.Main(m) }

// protoV6Factories is used by every acceptance test.
var protoV6Factories = map[string]func() (tfprotov6.ProviderServer, error){
	"entrepot": providerserver.NewProtocol6WithError(New("test")()),
}

// testServer returns the acceptance test server (skipping when TF_ACC is unset).
func testServer(t *testing.T) *acctest.Server { return acctest.Get(t) }

func randSuffix() string { return acctest.RandName("x")[2:] }

func regexpMust(s string) *regexp.Regexp { return regexp.MustCompile(s) }

// requireScanFeatures skips tests of features added in Entrepot 1.2.0
// (MetaDefender scanners, scan holds, block on error) when the test server
// is older: it rejects the unknown settings with 400.
func requireScanFeatures(t *testing.T, s *acctest.Server) {
	t.Helper()
	ctx := context.Background()
	name := "probe-" + randSuffix()
	res, err := s.API.CreateScannerWithResponse(ctx, client.ScannerInput{Name: name, Type: client.ScannerType("metadefender"),
		Metadefender: &client.MetaDefenderSettings{Url: "https://metadefender.invalid"}})
	if err != nil {
		t.Fatalf("probe the server for MetaDefender support: %v", err)
	}
	switch res.StatusCode() {
	case http.StatusCreated:
		if _, err := s.API.DeleteScannerWithResponse(ctx, name); err != nil {
			t.Fatalf("delete probe scanner: %v", err)
		}
	case http.StatusBadRequest:
		t.Skip("requires Entrepot 1.2.0 or later (MetaDefender scanners, scan holds and block on error)")
	default:
		t.Fatalf("probe the server for MetaDefender support: HTTP %d: %s", res.StatusCode(), res.Body)
	}
}

// requireDockerHost skips tests of Docker repository hostnames (Entrepot
// 1.3.1) when the test server is older: it rejects the unknown setting.
func requireDockerHost(t *testing.T, s *acctest.Server) {
	t.Helper()
	ctx := context.Background()
	name := "probe-" + randSuffix()
	host := name + ".probe.invalid"
	res, err := s.API.CreateRepositoryWithResponse(ctx, client.RepositoryInput{Name: name, Format: client.RepositoryInputFormat("docker"),
		Kind: client.RepositoryKind("hosted"), BlobStore: "default", Docker: &client.DockerSettings{Host: &host}})
	if err != nil {
		t.Fatalf("probe the server for Docker hostnames: %v", err)
	}
	switch res.StatusCode() {
	case http.StatusCreated:
		if _, err := s.API.DeleteRepositoryWithResponse(ctx, name); err != nil {
			t.Fatalf("delete probe repository: %v", err)
		}
	case http.StatusBadRequest:
		t.Skip("requires Entrepot 1.3.1 or later (Docker repository hostnames)")
	default:
		t.Fatalf("probe the server for Docker hostnames: HTTP %d: %s", res.StatusCode(), res.Body)
	}
}
