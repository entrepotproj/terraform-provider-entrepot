terraform {
  required_providers {
    entrepot = {
      source  = "entrepotproj/entrepot"
      version = "~> 0.1"
    }
  }
}

# url and token can also come from ENTREPOT_URL and ENTREPOT_TOKEN.
provider "entrepot" {
  url   = "https://entrepot.example.com"
  token = var.entrepot_token # a service-account token (ent_…) with the admin role
}

variable "entrepot_token" {
  type      = string
  sensitive = true
}
