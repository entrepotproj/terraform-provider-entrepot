# Pull-through cache of the public npm registry.
resource "entrepot_repository" "npmjs" {
  name           = "npmjs"
  format         = "npm"
  kind           = "proxy"
  blob_store     = "default"
  anonymous_read = true
  proxy = {
    remote_url   = "https://registry.npmjs.org"
    routing_rule = "internal-names" # never fetch @acme/* from the public registry
  }
  scan = {
    scanners           = ["trivy"]
    mode               = "quarantine"
    severity_threshold = "high"
  }
}

# Internal packages, published by CI.
resource "entrepot_repository" "npm_internal" {
  name             = "npm-internal"
  format           = "npm"
  kind             = "hosted"
  blob_store       = "default"
  cleanup_policies = ["prune-prereleases"]

  lifecycle {
    prevent_destroy = true # replacing a repository deletes its content
  }
}

# One URL for clients: internal packages first, then npmjs.
resource "entrepot_repository" "npm" {
  name       = "npm"
  format     = "npm"
  kind       = "group"
  blob_store = "default"
  group = {
    members       = [entrepot_repository.npm_internal.name, entrepot_repository.npmjs.name]
    prefer_hosted = true # dependency-confusion protection
  }
}

# A signed Apt repository with two distributions.
resource "entrepot_repository" "apt" {
  name       = "apt-internal"
  format     = "apt"
  kind       = "hosted"
  blob_store = "default"
  hosted = {
    apt = {
      distributions = [
        { name = "bookworm", components = ["main"], architectures = ["amd64", "arm64", "all"] },
        { name = "noble", components = ["main"], architectures = ["amd64", "all"] },
      ]
    }
  }
}
