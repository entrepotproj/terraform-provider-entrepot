# Never fetch internal package names from the public registry
# (dependency-confusion protection).
resource "entrepot_routing_rule" "no_internal_upstream" {
  name        = "no-internal-upstream"
  description = "Internal scopes are only served from hosted repositories"
  mode        = "block"
  matchers    = ["^@acme/", "^acme-"]
}

resource "entrepot_repository" "npmjs" {
  name       = "npmjs"
  format     = "npm"
  kind       = "proxy"
  blob_store = "default"
  proxy = {
    remote_url   = "https://registry.npmjs.org"
    routing_rule = entrepot_routing_rule.no_internal_upstream.name
  }
}
