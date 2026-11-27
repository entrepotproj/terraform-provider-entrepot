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

# Raw files checked by MetaDefender before anyone can download them. A
# download waits up to a minute for the verdict, and stays blocked while
# MetaDefender is unavailable. hold_seconds and block_on_error require
# Entrepot 1.2.0 or later.
resource "entrepot_repository" "downloads" {
  name       = "downloads"
  format     = "raw"
  kind       = "hosted"
  blob_store = "default"
  scan = {
    scanners            = ["metadefender"]
    mode                = "quarantine"
    block_until_scanned = true
    hold_seconds        = 60
    block_on_error      = true
  }
}

# A Docker Hub proxy on its own hostname: `docker pull hub.entrepot.example.com/library/alpine`,
# and usable as Docker Engine's registry mirror ({"registry-mirrors": ["https://hub.entrepot.example.com"]}).
# Point DNS and TLS for the name at Entrepot. docker.host requires Entrepot 1.3.1 or later.
resource "entrepot_repository" "docker_hub" {
  name           = "docker-hub"
  format         = "docker"
  kind           = "proxy"
  blob_store     = "default"
  anonymous_read = true # Docker Engine doesn't send Entrepot credentials to a mirror
  proxy = {
    remote_url = "https://registry-1.docker.io"
  }
  docker = {
    host = "hub.entrepot.example.com"
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
