data "entrepot_role" "developer" {
  name = "developer"
}

output "developer_actions" {
  value = data.entrepot_role.developer.actions
}
