data "entrepot_repository" "npm" {
  name = "npm"
}

# e.g. for an .npmrc template
output "npm_registry_url" {
  value = data.entrepot_repository.npm.url
}
