# Signing keys are imported by repository name. The private key cannot be
# read back: the next apply records the configured key (no API call when it
# matches the active key's fingerprint).
terraform import entrepot_repository_signing_key.apt apt-internal
