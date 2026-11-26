# Scanners are imported by name. Secrets (trivy.server_token, webhook.secret,
# metadefender.api_key) are not imported.
terraform import entrepot_scanner.trivy trivy
