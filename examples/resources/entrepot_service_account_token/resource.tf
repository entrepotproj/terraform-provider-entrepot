resource "entrepot_service_account_token" "ci" {
  service_account = entrepot_service_account.ci.name
  name            = "github-actions"
  scopes          = ["read", "write"]
  expires_in_days = 90
}

# The token value is only known when Terraform creates the token, and is
# stored (sensitive) in the state.
output "ci_token" {
  value     = entrepot_service_account_token.ci.secret
  sensitive = true
}
