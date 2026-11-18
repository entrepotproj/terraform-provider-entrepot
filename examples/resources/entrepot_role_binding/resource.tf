# Let the CI service account publish to every npm-* repository.
resource "entrepot_role_binding" "ci_npm" {
  role               = entrepot_role.publisher.name
  subject_type       = "user"
  subject            = entrepot_service_account.ci.username # "sa:ci"
  repository_pattern = "npm-*"
}

# Members of the OIDC group "platform-admins" administer the whole server.
resource "entrepot_role_binding" "admins" {
  role         = "admin"
  subject_type = "group"
  subject      = "platform-admins"
}

# Every signed-in user can read the public-* repositories.
resource "entrepot_role_binding" "readers" {
  role               = "viewer"
  subject_type       = "authenticated"
  repository_pattern = "public-*"
}

# GitLab CI jobs matching a trust policy can upload to the internal repositories.
resource "entrepot_role_binding" "gitlab_ci" {
  role               = "developer"
  subject_type       = "gitlab_policy"
  subject            = entrepot_gitlab_trust_policy.platform.name
  repository_pattern = "internal-*"
}
