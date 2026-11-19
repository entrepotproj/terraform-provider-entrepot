# Publishing to the Terraform Registry and the OpenTofu registry

Both registries read GitHub releases of this repository, so a single tagged release serves
both. The release workflow (`.github/workflows/release.yml`) runs goreleaser on `v*` tags and
uploads everything the registries need:

* `terraform-provider-entrepot_<version>_<os>_<arch>.zip`, one per platform;
* `terraform-provider-entrepot_<version>_SHA256SUMS`, plus its detached GPG signature `.sig`;
* `terraform-provider-entrepot_<version>_manifest.json`, which declares protocol 6.0.

## One-time setup

### 1. Repository

The repository must be **public**, named `terraform-provider-entrepot`, and owned by the
namespace you publish under (`entrepotproj`). It also needs a `LICENSE` file, which both registries
require; the provider is licensed under Apache-2.0 (`LICENSE`).

### 2. GPG signing key

```shell
gpg --full-generate-key                      # RSA 4096, no expiry or a long one
gpg --list-secret-keys --keyid-format=long   # note the key ID
gpg --armor --export-secret-keys <KEY_ID> > private.asc
gpg --armor --export <KEY_ID> > public.asc
```

In GitHub, go to **Settings → Secrets and variables → Actions** and add:

* `GPG_PRIVATE_KEY`: the contents of `private.asc`;
* `PASSPHRASE`: the key passphrase.

Then delete `private.asc`.

### 3. Terraform Registry

1. Sign in at <https://registry.terraform.io> with a GitHub account that owns the `entrepotproj`
   organisation, and grant the registry access to the organisation when GitHub asks.
2. **User settings → Signing keys → Add a GPG key**, choose the `entrepotproj` namespace and
   paste `public.asc`.
3. **Publish → Provider**, then select `entrepotproj/terraform-provider-entrepot`. The registry
   installs a webhook and imports existing and future releases.

### 4. OpenTofu registry

The OpenTofu registry is a GitHub repository (`opentofu/registry`) fed by issue forms:

1. Open <https://github.com/opentofu/registry/issues/new/choose> and choose
   **Submit new Provider**. Enter `entrepotproj/terraform-provider-entrepot`.
2. Choose **Submit new Provider Signing Key**, enter the namespace `entrepotproj` and paste
   `public.asc`. OpenTofu verifies the `SHA256SUMS` signature with it.

The registry bot opens a PR; once it is merged, new GitHub releases are picked up automatically.

## Cutting a release

```shell
git tag v0.1.0
git push origin v0.1.0
```

Check the workflow run, then confirm the release is visible on each registry:

* <https://registry.terraform.io/providers/entrepotproj/entrepot>
* `tofu init` against a configuration that uses `source = "entrepotproj/entrepot"`

Before tagging:

* Update `CHANGELOG.md`.
* Run `make docs` and commit `docs/`; the registries render it.
* If the server API changed, run `make sync-spec`.

To try the release pipeline locally without signing:

```shell
goreleaser release --snapshot --clean --skip=sign
```

## Private distribution (optional)

Without the public registries, the release zips can be served from a
[provider network mirror](https://developer.hashicorp.com/terraform/internals/provider-network-mirror-protocol),
for example a raw repository in Entrepot itself.
