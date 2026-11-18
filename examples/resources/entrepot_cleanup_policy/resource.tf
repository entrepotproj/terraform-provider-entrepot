# Delete npm pre-releases older than 30 days, keeping the 3 newest versions.
resource "entrepot_cleanup_policy" "npm_prereleases" {
  name        = "npm-prereleases"
  description = "Remove old release candidates"
  format      = "npm"
  criteria = {
    published_older_than_days = 30
    prerelease_only           = true
    keep_latest               = 3
  }
}

# Remove untagged Docker manifests after a week.
resource "entrepot_cleanup_policy" "docker_untagged" {
  name     = "docker-untagged"
  format   = "docker"
  criteria = { untagged_older_than_days = 7 }
}

resource "entrepot_repository" "npm" {
  name             = "npm-internal"
  format           = "npm"
  kind             = "hosted"
  blob_store       = "default"
  cleanup_policies = [entrepot_cleanup_policy.npm_prereleases.name]
}
