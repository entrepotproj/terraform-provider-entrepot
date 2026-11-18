# A custom role for CI jobs that publish packages but never delete them.
resource "entrepot_role" "publisher" {
  name        = "publisher"
  description = "Read and upload packages"
  actions     = ["read", "write"]
}
