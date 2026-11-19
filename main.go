// Command terraform-provider-entrepot is the Terraform / OpenTofu provider
// for Entrepot.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/entrepotproj/terraform-provider-entrepot/internal/provider"
)

// version is set by goreleaser.
var version = "dev"

//go:generate go tool tfplugindocs generate --provider-name entrepot

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/entrepotproj/entrepot",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
