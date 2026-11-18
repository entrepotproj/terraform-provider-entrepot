# Accept one vulnerability in one component until the upgrade lands.
resource "entrepot_waiver" "xz" {
  vuln_id           = "CVE-2024-3094"
  component_pattern = "xz-utils@5.6.*"
  reason            = "Not shipped in production images; tracked in SEC-123"
  expires_at        = "2026-12-31T23:59:59Z"
}

# Ignore every finding in sandbox repositories.
resource "entrepot_waiver" "sandbox" {
  vuln_id            = "*"
  repository_pattern = "sandbox-*"
  reason             = "Experimental repositories are not scanned for policy"
}
