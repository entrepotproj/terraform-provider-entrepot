# Publishing to the Terraform Registry and the OpenTofu registry

Both registries read GitHub releases of this repository, so a single tagged release serves
both. Releases are cut automatically from the commit messages on `main` (see
[Cutting a release](#cutting-a-release)), and goreleaser uploads everything the registries need:

* `terraform-provider-entrepot_<version>_<os>_<arch>.zip`, one per platform;
* `terraform-provider-entrepot_<version>_SHA256SUMS`, plus its detached GPG signature `.sig`;
* `terraform-provider-entrepot_<version>_manifest.json`, which declares protocol 6.0.

## One-time setup

### 1. Repository

The repository must be **public**, named `terraform-provider-entrepot`, and owned by the
namespace you publish under (`entrepotproj`). Neither registry can see a private repository. It also needs a `LICENSE` file, which both registries
require; the provider is licensed under Apache-2.0 (`LICENSE`).

### 2. GPG signing key

Use an **RSA** key. The Terraform Registry cannot verify ed25519/EdDSA signatures, and reports
them as "SHASUM signature failed verification: Invalid signature".

```shell
gpg --quick-generate-key "Entrepot releases <you@example.com>" rsa4096 sign 3y
gpg --list-secret-keys --keyid-format=long   # note the key ID after rsa4096/
gpg --armor --export-secret-keys <KEY_ID> > private.asc
gpg --armor --export <KEY_ID> > public.asc
```

In GitHub, go to **Settings → Secrets and variables → Actions** and add:

* `GPG_PRIVATE_KEY`: the contents of `private.asc`;
* `PASSPHRASE`: the key passphrase.

Then delete `private.asc`. goreleaser hands `PASSPHRASE` to gpg on stdin
(`--pinentry-mode loopback --passphrase-fd 0`). If it is wrong, the release fails at the
signing step with "Bad passphrase".

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

Nobody tags by hand. Every push to `main` runs the `Release` workflow
(`.github/workflows/release.yml`):

1. the unit and acceptance tests (`.github/workflows/test.yml`) run against the pushed commit.
   The acceptance tests pull the server image `ghcr.io/entrepotproj/entrepot:latest` (or the
   tag in the `ENTREPOT_VERSION` repository variable), so that package must be public;
2. [semantic-release](https://semantic-release.gitbook.io/) looks at the commits since the last
   `v*` tag. If one of them warrants a release, it works out the next version and pushes the
   tag `vX.Y.Z`;
3. goreleaser checks out that tag, builds the zips, signs `SHA256SUMS` and publishes the GitHub
   release, with notes generated from the commits.

The registries then import the release. Check the workflow run, then confirm the version is
visible on each registry:

* <https://registry.terraform.io/providers/entrepotproj/entrepot>
* `tofu init` against a configuration that uses `source = "entrepotproj/entrepot"`

### Commit messages

Commits follow [Conventional Commits](https://www.conventionalcommits.org/). The type decides
the release:

| Commit | Example | Release |
|---|---|---|
| `fix:` or `perf:` | `fix(repository): keep the blob store on import` | patch (`0.1.1` → `0.1.2`) |
| `feat:` | `feat: add the entrepot_webhook resource` | minor (`0.1.1` → `0.2.0`) |
| `!` after the type, or a `BREAKING CHANGE:` footer | `feat(role)!: rename permissions to grants` | major (`0.1.1` → `1.0.0`) |
| anything else (`docs:`, `chore:`, `ci:`, `test:`, `refactor:`, …) | `docs: regenerate the resource docs` | none |

The highest type among the new commits wins, and a push with no releasing commit publishes
nothing. When a pull request is squash-merged, its title becomes the commit message, so the
title must follow the convention; with a merge commit, the commits on the branch are what count.
semantic-release has no special handling of `0.x`: a breaking change bumps to `1.0.0`.

The configuration is `.releaserc.json`; the tool versions are pinned in the workflow.

### Dependency updates

[Renovate](https://docs.renovatebot.com/) opens dependency PRs every Monday morning (UTC),
configured in `.github/renovate.json`: one PR for minor and patch Go module updates, one for CI,
and one per major update. Go module updates are `fix(deps):`, so merging one releases a patch;
workflow actions and the semantic-release tooling are `ci(deps):` and release nothing. The
examples, which pin this provider, are left alone. If an update changes the generated client or
docs, the PR's CI says so: run `make generate` on the branch. Renovate runs as the
[Renovate GitHub App](https://github.com/apps/renovate), which must be installed on the
repository.

### Before merging

* Run `make docs` and commit `docs/`; the registries render it.
* If the server API changed, run `make sync-spec`.

To try the release pipeline locally without signing:

```shell
goreleaser release --snapshot --clean --skip=sign
```

### Fixing a bad release

Re-running the failed jobs of a `Release` run builds from the same tag, which is enough when a
secret was wrong or a step flaked. goreleaser can't overwrite assets that are already uploaded,
so delete the GitHub release first if it exists.

If the code itself was wrong, merge the fix as a `fix:` commit: the next run releases a new
patch version. Don't move an existing tag: registries and caches may already have recorded it.

## Private distribution (optional)

Without the public registries, the release zips can be served from a
[provider network mirror](https://developer.hashicorp.com/terraform/internals/provider-network-mirror-protocol),
for example a raw repository in Entrepot itself.
