data "entrepot_blob_store" "default" {
  name = "default"
}

output "default_store_gib" {
  value = data.entrepot_blob_store.default.usage.total_bytes / 1073741824
}
