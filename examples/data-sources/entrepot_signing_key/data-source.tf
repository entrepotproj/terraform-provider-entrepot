# Distribute a repository's public key to hosts, e.g. with cloud-init.
data "entrepot_signing_key" "apt" {
  repository = "apt-internal"
}

locals {
  cloud_init_apt = {
    apt = {
      sources = {
        entrepot = {
          source = "deb [signed-by=$KEY_FILE] https://entrepot.example.com/repository/apt-internal/ stable main"
          key    = data.entrepot_signing_key.apt.public_key
        }
      }
    }
  }
}

output "apt_key_url" {
  value = data.entrepot_signing_key.apt.url
}
