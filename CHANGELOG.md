# Changelog

Releases after 0.1.1 are cut automatically from the commit messages on `main` (see
[PUBLISHING.md](PUBLISHING.md#cutting-a-release)). Their notes are on the
[GitHub releases page](https://github.com/entrepotproj/terraform-provider-entrepot/releases);
this file covers the releases before that.

## 0.1.1

First published release. The provider is the same as 0.1.0. The 0.1.0 release was never published to the registries because its checksum file was signed with an ed25519 key.

* Release signing is non-interactive. gpg reads the passphrase from stdin, so a missing or wrong `PASSPHRASE` fails immediately instead of hanging the release job.
* The release job has a 30-minute timeout.

## 0.1.0

Initial version (not published to the registries).

* Resources:
  * `entrepot_blob_store`, `entrepot_repository`;
  * `entrepot_role`, `entrepot_role_binding`, `entrepot_gitlab_trust_policy`;
  * `entrepot_service_account`, `entrepot_service_account_token`;
  * `entrepot_scanner`, `entrepot_waiver`, `entrepot_cleanup_policy`, `entrepot_routing_rule`;
  * `entrepot_repository_signing_key`.
* Data sources: `entrepot_system_info`, `entrepot_repository`, `entrepot_repositories`,
  `entrepot_blob_store`, `entrepot_role`, `entrepot_signing_key`.
* Works with Terraform ≥ 1.5 and OpenTofu ≥ 1.8 (plugin protocol 6).
