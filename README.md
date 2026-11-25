# Terraform / OpenTofu provider for Entrepot

Manage an [Entrepot](https://github.com/entrepotproj/entrepot) package repository
manager as code. It covers blob stores, repositories, access control, GitLab CI trust, scanning,
cleanup, routing and signing keys.

```hcl
terraform {
  required_providers {
    entrepot = {
      source  = "entrepotproj/entrepot"   # registry.terraform.io or registry.opentofu.org
      version = "~> 0.1"
    }
  }
}

provider "entrepot" {
  url   = "https://entrepot.example.com"   # or ENTREPOT_URL
  token = var.entrepot_token               # or ENTREPOT_TOKEN (service-account token, admin role)
}

resource "entrepot_repository" "npmjs" {
  name           = "npmjs"
  format         = "npm"
  kind           = "proxy"
  blob_store     = "default"
  anonymous_read = true
  proxy          = { remote_url = "https://registry.npmjs.org" }
}
```

* Guides (installation, a platform walkthrough, tips) and the Entrepot documentation:
  <https://docs.entrepotproj.io>.
* Resource and data source documentation: [`docs/`](docs/), also rendered on the registries.
* A complete platform setup: [`examples/platform`](examples/platform).

## Resources and data sources

| Resources | Data sources |
|---|---|
| `entrepot_blob_store`, `entrepot_repository` | `entrepot_system_info` |
| `entrepot_role`, `entrepot_role_binding` | `entrepot_repository`, `entrepot_repositories` |
| `entrepot_gitlab_trust_policy` | `entrepot_blob_store` |
| `entrepot_service_account`, `entrepot_service_account_token` | `entrepot_role` |
| `entrepot_scanner`, `entrepot_waiver` | `entrepot_signing_key` |
| `entrepot_cleanup_policy`, `entrepot_routing_rule` | |
| `entrepot_repository_signing_key` | |

## Behaviour notes

* **Immutable fields.** Changing a repository's `name`, `format`, `kind` or `blob_store`, or a
  blob store's `name`/`type`, forces replacement. Replacing a repository **deletes its
  content**; use `lifecycle { prevent_destroy = true }` on important repositories.
* **Write-only secrets.** Proxy passwords, S3 secret keys, scanner tokens and secrets, and
  signing private keys are never returned by the API. The provider keeps your configured value,
  so changes made outside Terraform to these values are not detected.
* **Out-of-band deletion.** Objects deleted outside Terraform are recreated on the next apply.

## Development

Requirements:
* Go 1.26+;
* Terraform ≥ 1.5 or OpenTofu ≥ 1.8;
* Postgres plus an `entrepot` server binary, for the acceptance tests.

```shell
make build                     # ./terraform-provider-entrepot
make test                      # unit tests
make testacc ENTREPOT_BINARY=/path/to/entrepot \
  ENTREPOT_TEST_DATABASE_URL=postgres://postgres@localhost:5432/postgres?sslmode=disable
make testacc TF=tofu ENTREPOT_BINARY=…   # same suite against OpenTofu
make docs                      # regenerate docs/ with tfplugindocs
make sync-spec SERVER=../entrepot   # refresh the vendored OpenAPI spec + client
```

The API client in `internal/client` is generated from the server's OpenAPI document, vendored
in `internal/client/openapi.yaml`. `SPEC_VERSION` records which server version it came from.

To use a local build, add this to `~/.terraformrc` (OpenTofu: `~/.tofurc`):

```hcl
provider_installation {
  dev_overrides { "entrepotproj/entrepot" = "/path/to/terraform-provider-entrepot" }
  direct {}
}
```

PR titles must be [Conventional Commits](https://www.conventionalcommits.org) (`feat: …`,
`fix: …`): release-please builds the version and [CHANGELOG.md](CHANGELOG.md) from them.
Releasing and publishing to the registries is described in [PUBLISHING.md](PUBLISHING.md).

## License

Apache License 2.0. See [LICENSE](LICENSE).
