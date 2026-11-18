resource "entrepot_service_account" "ci" {
  name        = "ci"
  description = "CI publisher"
}

# Use the username ("sa:ci") as a role binding subject.
output "ci_username" {
  value = entrepot_service_account.ci.username
}
