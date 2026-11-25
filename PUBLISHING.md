# Publishing to the Terraform Registry and the OpenTofu registry

Both registries read GitHub releases of this repository, so a single release serves both.
Releases are cut with [release-please](https://github.com/googleapis/release-please): versions and
`CHANGELOG.md` come from Conventional Commit messages on `main`. When a release PR is merged, the
release workflow (`.github/workflows/release.yml`) runs goreleaser, which uploads everything the
registries need to a draft release and then publishes it:

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

### 5. Repository settings for release-please

* **Settings → Actions → General → Workflow permissions:** enable *Allow GitHub Actions to create
  and approve pull requests*, so release-please can open its PR. If it is greyed out, enable it
  for the `entrepotproj` organisation first.
* **Settings → General → Pull Requests:** allow squash merging with *Default to pull request
  title*. Disable merge commits and rebase merging, so every change lands as one commit with a
  checked title.
* **Optional:** add a `RELEASE_PLEASE_TOKEN` secret (a GitHub App token or a fine-grained token
  with contents, issues and pull-requests write access). Without it, release-please uses
  `GITHUB_TOKEN`, and GitHub doesn't run workflows for PRs created with that token: the tests
  don't run on the release PR. If branch protection requires status checks, you need the token,
  or an admin has to merge the release PR.

## Commit messages

PRs are squash-merged, and the PR title becomes the commit message on `main`. The **PR title**
workflow checks that it is a [Conventional Commit](https://www.conventionalcommits.org):

| Title | Changelog section | Version bump |
|---|---|---|
| `feat: …`, `feat(scope): …` | Features | minor |
| `fix: …` | Bug Fixes | patch |
| `perf: …` | Performance | patch |
| `deps: …` | Dependencies | patch |
| `revert: …` | Reverts | patch |
| `feat!: …`, or a `BREAKING CHANGE:` footer | ⚠ BREAKING CHANGES | major (minor before 1.0) |
| `docs:`, `refactor:`, `test:`, `build:`, `ci:`, `chore:`, `style:` | not listed | none |

Only the first five types trigger a release. Before 1.0, a breaking change bumps the minor
version (0.3.0 → 0.4.0), and features bump the minor version too. A new resource or attribute is
a `feat:`; removing or renaming one, or a change that forces replacement, is breaking.

## Cutting a release

1. release-please keeps a PR called **chore(main): release X.Y.Z** open. Every push to `main`
   updates it with the next version and its `CHANGELOG.md` entry.
2. To change the release notes, edit `CHANGELOG.md` in that PR, and its description, which becomes
   the GitHub release text. release-please rewrites both whenever `main` changes, so edit just
   before merging.
3. Merge the PR. release-please creates the tag `vX.Y.Z` and a **draft** GitHub release. The
   release workflow runs goreleaser on the tag. goreleaser finds the draft by name, uploads the
   signed artifacts, keeps the release-please notes, and publishes the release. The registries
   never see a release without its files.
4. Confirm the release is visible on each registry:
   * <https://registry.terraform.io/providers/entrepotproj/entrepot>
   * `tofu init` against a configuration that uses `source = "entrepotproj/entrepot"`

Before merging the release PR:

* Make sure `docs/` is up to date (`make docs`); the registries render it.
* If the server API changed, run `make sync-spec` in a separate PR first.

To try the release pipeline locally without signing:

```shell
goreleaser release --snapshot --clean --skip=sign
```

### Fixing a bad release

* **The publish job failed** before the release was published (wrong `PASSPHRASE`, expired
  key): fix the secrets, then re-run the failed job of the **Release Please** run. Or run the
  **Release** workflow by hand with the tag: it picks up the draft release and carries on.
* **The tagged commit can't be released** (a build or signing problem in the code): leave the
  draft release and its tag in place. Merge a `fix:` PR, then merge the next release PR
  (X.Y.Z+1). Delete the broken draft once that release is out.
* **A published release is broken:** release a fix as a new version. Don't move or delete the tag
  of a published release: registries and caches may already have recorded it, and release-please
  uses the latest release's tag to find where the next changelog starts.

## Private distribution (optional)

Without the public registries, the release zips can be served from a
[provider network mirror](https://developer.hashicorp.com/terraform/internals/provider-network-mirror-protocol),
for example a raw repository in Entrepot itself.
