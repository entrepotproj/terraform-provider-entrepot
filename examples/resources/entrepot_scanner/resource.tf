# Trivy in client/server mode (one shared vulnerability DB).
resource "entrepot_scanner" "trivy" {
  name                  = "trivy"
  type                  = "trivy"
  rescan_interval_hours = 24
  trivy = {
    server_url      = "http://trivy.security.svc:4954"
    server_token    = var.trivy_token
    timeout_seconds = 300
  }
}

# Grype with the server defaults.
resource "entrepot_scanner" "grype" {
  name = "grype"
  type = "grype"
}

# An external scanning service that calls back with results.
resource "entrepot_scanner" "webhook" {
  name = "acme-scan"
  type = "webhook"
  webhook = {
    url             = "https://scan.acme.example/entrepot"
    secret          = var.scan_webhook_secret
    timeout_seconds = 1800
  }
}

# OPSWAT MetaDefender Core: malware analysis of every package file (and of
# Docker image layers). Requires Entrepot 1.2.0 or later.
resource "entrepot_scanner" "metadefender" {
  name                  = "metadefender"
  type                  = "metadefender"
  rescan_interval_hours = 168
  metadefender = {
    url     = "https://metadefender.security.svc:8008"
    api_key = var.metadefender_api_key
    rule    = "Packages"
    web_url = "https://metadefender.example.com/#/public/process/dataId/{dataId}"
  }
}

variable "trivy_token" {
  type      = string
  sensitive = true
}

variable "scan_webhook_secret" {
  type      = string
  sensitive = true
}

variable "metadefender_api_key" {
  type      = string
  sensitive = true
}
