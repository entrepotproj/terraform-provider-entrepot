data "entrepot_repositories" "docker" {
  format = "docker"
}

output "docker_repository_urls" {
  value = { for r in data.entrepot_repositories.docker.repositories : r.name => r.url }
}
