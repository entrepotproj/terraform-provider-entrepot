TF ?= terraform
SERVER ?= ../package-repository
PLUGIN_DIR := $(HOME)/.terraform.d/plugins/registry.terraform.io/geoffo-dev/entrepot/0.0.0-dev/$(shell go env GOOS)_$(shell go env GOARCH)

.PHONY: build install test testacc docs generate sync-spec lint

build:
	go build -o terraform-provider-entrepot

# Installs into the local plugin directory (filesystem mirror layout).
install: build
	mkdir -p $(PLUGIN_DIR)
	cp terraform-provider-entrepot $(PLUGIN_DIR)/

test:
	go test ./...

# Needs Postgres and an entrepot server binary (see internal/acctest):
#   make testacc ENTREPOT_BINARY=../package-repository/bin/entrepot
#   make testacc TF=tofu ENTREPOT_BINARY=...
testacc:
	TF_ACC=1 TF_ACC_TERRAFORM_PATH=$(shell which $(TF)) go test -v -count=1 -timeout 30m ./internal/provider/

generate:
	cd internal/client && go generate ./...
	go generate ./...

docs:
	go generate ./...

# Refresh the vendored API spec from a server checkout.
sync-spec:
	cp $(SERVER)/api/openapi.yaml internal/client/openapi.yaml
	git -C $(SERVER) describe --tags --always > internal/client/SPEC_VERSION
	cd internal/client && go generate ./...

lint:
	go vet ./...
	test -z "$(shell gofmt -l .)"
