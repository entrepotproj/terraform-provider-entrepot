package provider

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/entrepotproj/terraform-provider-entrepot/internal/acctest"
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
