data "entrepot_system_info" "this" {}

output "entrepot_version" {
  value = data.entrepot_system_info.this.version
}

output "supported_formats" {
  value = [for f in data.entrepot_system_info.this.formats : f.name]
}
