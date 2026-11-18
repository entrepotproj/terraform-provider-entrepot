resource "entrepot_repository" "apt" {
  name       = "apt-internal"
  format     = "apt"
  kind       = "hosted"
  blob_store = "default"
}

# Sign the repository with your own key instead of a server-generated one:
#   gpg --armor --export-secret-keys <key-id> > signing-key.asc
resource "entrepot_repository_signing_key" "apt" {
  repository  = entrepot_repository.apt.name
  private_key = file("${path.module}/signing-key.asc")
  passphrase  = var.signing_key_passphrase
}

variable "signing_key_passphrase" {
  type      = string
  sensitive = true
  default   = null
}

output "apt_key_fingerprint" {
  value = entrepot_repository_signing_key.apt.fingerprint
}
