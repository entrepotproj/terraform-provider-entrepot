# A complete Entrepot platform as code:
#   * blob stores;
#   * proxies of public registries, hosted repositories and groups for every format;
#   * Keycloak group permissions and a GitLab CI trust policy;
#   * a Terraform service account;
#   * a vulnerability scanner with quarantine, a waiver;
#   * cleanup and routing rules.
#
#   terraform init && terraform apply -var url=https://entrepot.example.com -var token=$ENTREPOT_TOKEN

terraform {
  required_providers {
    entrepot = {
      source  = "entrepotproj/entrepot"
      version = "~> 0.1"
    }
  }
}

variable "url" {
  type = string
}

variable "token" {
  type      = string
  sensitive = true
}

variable "data_root" {
  description = "Mount point of file blob stores on the server (storage.allowedRoots)."
  type        = string
  default     = "/data"
}

variable "gitlab_instance" {
  description = "Name of the GitLab instance configured on the server (auth.gitlab[].name)."
  type        = string
  default     = "gitlab"
}

variable "trivy_binary" {
  description = "Path to the trivy CLI on the scan workers."
  type        = string
  default     = "trivy"
}

provider "entrepot" {
  url   = var.url
  token = var.token
}

# ---------------------------------------------------------------- storage

resource "entrepot_blob_store" "shared" {
  name = "shared"
  type = "file"
  file = { path = "${var.data_root}/shared" }
}

# ------------------------------------------------- scanning & hygiene

resource "entrepot_scanner" "trivy" {
  name                  = "trivy"
  type                  = "trivy"
  rescan_interval_hours = 24
  trivy                 = { binary = var.trivy_binary }
}

resource "entrepot_routing_rule" "internal_names" {
  name        = "internal-names"
  description = "Internal package names must never be fetched from public registries."
  mode        = "block"
  matchers    = ["^@acme/", "^acme-"]
}

resource "entrepot_cleanup_policy" "prune_prereleases" {
  name        = "prune-prereleases"
  description = "Remove pre-releases older than 30 days, keeping the 5 newest."
  criteria = {
    prerelease_only           = true
    published_older_than_days = 30
    keep_latest               = 5
  }
}

resource "entrepot_cleanup_policy" "stale_cache" {
  name        = "stale-cache"
  description = "Evict cached proxy content nobody downloaded for 90 days."
  criteria    = { last_downloaded_older_than_days = 90 }
}

resource "entrepot_waiver" "lodash_template" {
  vuln_id            = "CVE-2021-23337"
  repository_pattern = "npm*"
  component_pattern  = "lodash@4.17.*"
  reason             = "template() is never called with untrusted input"
  expires_at         = "2027-06-30T00:00:00Z"
}

# ----------------------------------------------------------- repositories

locals {
  proxies = {
    npmjs      = { format = "npm", url = "https://registry.npmjs.org" }
    pypi-proxy = { format = "pypi", url = "https://pypi.org" }
    docker-hub = { format = "docker", url = "https://registry-1.docker.io" }
    ubuntu     = { format = "apt", url = "http://archive.ubuntu.com/ubuntu" }
    prometheus = { format = "helm", url = "https://prometheus-community.github.io/helm-charts" }
  }
  hosted = {
    npm-internal    = "npm"
    pypi-internal   = "pypi"
    docker-internal = "docker"
    helm-internal   = "helm"
    files           = "raw"
  }
}

resource "entrepot_repository" "proxy" {
  for_each       = local.proxies
  name           = each.key
  format         = each.value.format
  kind           = "proxy"
  blob_store     = entrepot_blob_store.shared.name
  anonymous_read = true
  proxy = {
    remote_url   = each.value.url
    routing_rule = contains(["npm", "pypi"], each.value.format) ? entrepot_routing_rule.internal_names.name : null
  }
  scan = contains(["npm", "pypi", "docker"], each.value.format) ? {
    scanners           = [entrepot_scanner.trivy.name]
    mode               = "quarantine"
    severity_threshold = "critical"
  } : null
  cleanup_policies = [entrepot_cleanup_policy.stale_cache.name]
}

resource "entrepot_repository" "hosted" {
  for_each   = local.hosted
  name       = each.key
  format     = each.value
  kind       = "hosted"
  blob_store = entrepot_blob_store.shared.name
  scan = each.value != "raw" && each.value != "helm" ? {
    scanners = [entrepot_scanner.trivy.name]
    mode     = "warn"
  } : null
  cleanup_policies = contains(["npm", "pypi"], each.value) ? [entrepot_cleanup_policy.prune_prereleases.name] : null
}

resource "entrepot_repository" "apt_internal" {
  name       = "apt-internal"
  format     = "apt"
  kind       = "hosted"
  blob_store = entrepot_blob_store.shared.name
  hosted = {
    apt = { distributions = [{ name = "stable", components = ["main"], architectures = ["amd64", "arm64", "all"] }] }
  }
}

resource "entrepot_repository" "yum_internal" {
  name       = "yum-internal"
  format     = "yum"
  kind       = "hosted"
  blob_store = entrepot_blob_store.shared.name
  hosted     = { yum = { repodata_depth = 0 } }
}

resource "entrepot_repository" "npm" {
  name           = "npm"
  format         = "npm"
  kind           = "group"
  blob_store     = entrepot_blob_store.shared.name
  anonymous_read = true
  group = {
    members       = [entrepot_repository.hosted["npm-internal"].name, entrepot_repository.proxy["npmjs"].name]
    prefer_hosted = true
  }
}

resource "entrepot_repository" "pypi" {
  name           = "pypi"
  format         = "pypi"
  kind           = "group"
  blob_store     = entrepot_blob_store.shared.name
  anonymous_read = true
  group = {
    members = [entrepot_repository.hosted["pypi-internal"].name, entrepot_repository.proxy["pypi-proxy"].name]
  }
}

data "entrepot_signing_key" "apt" {
  repository = entrepot_repository.apt_internal.name
}

# --------------------------------------------------------- access control

resource "entrepot_role" "publisher" {
  name        = "publisher"
  description = "Publish and overwrite, but never delete."
  actions     = ["read", "write"]
}

# Keycloak groups (groups claim).
resource "entrepot_role_binding" "developers_read_all" {
  role               = "viewer"
  subject_type       = "group"
  subject            = "developers"
  repository_pattern = "*"
}

resource "entrepot_role_binding" "developers_publish_internal" {
  role               = entrepot_role.publisher.name
  subject_type       = "group"
  subject            = "developers"
  repository_pattern = "*-internal"
}

resource "entrepot_role_binding" "platform_admins" {
  role               = "admin"
  subject_type       = "group"
  subject            = "platform-admins"
  repository_pattern = "*"
}

# GitLab CI: protected branches of the platform group may publish internal packages.
resource "entrepot_gitlab_trust_policy" "platform_releases" {
  name        = "platform-releases"
  description = "Protected branches of platform/** publish without stored secrets."
  gitlab      = var.gitlab_instance
  conditions = {
    project_path  = "platform/**"
    ref_protected = "true"
  }
}

resource "entrepot_role_binding" "ci_publish" {
  role               = entrepot_role.publisher.name
  subject_type       = "gitlab_policy"
  subject            = entrepot_gitlab_trust_policy.platform_releases.name
  repository_pattern = "*-internal"
}

# A service account for Jenkins with a 90-day, read/write token.
resource "entrepot_service_account" "jenkins" {
  name        = "jenkins"
  description = "Legacy Jenkins pipelines"
}

resource "entrepot_role_binding" "jenkins" {
  role               = entrepot_role.publisher.name
  subject_type       = "user"
  subject            = entrepot_service_account.jenkins.username
  repository_pattern = "*-internal"
}

resource "entrepot_service_account_token" "jenkins" {
  service_account = entrepot_service_account.jenkins.name
  name            = "jenkins-ci"
  scopes          = ["read", "write"]
  expires_in_days = 90
}

# ---------------------------------------------------------------- outputs

data "entrepot_repositories" "all" {
  depends_on = [entrepot_repository.proxy, entrepot_repository.hosted, entrepot_repository.npm, entrepot_repository.pypi,
  entrepot_repository.apt_internal, entrepot_repository.yum_internal]
}

output "npm_registry" {
  value = entrepot_repository.npm.url
}

output "pypi_index" {
  value = "${entrepot_repository.pypi.url}simple/"
}

output "docker_hub_mirror" {
  value = entrepot_repository.proxy["docker-hub"].url
}

output "apt_signing_key_fingerprint" {
  value = data.entrepot_signing_key.apt.fingerprint
}

output "jenkins_token" {
  value     = entrepot_service_account_token.jenkins.secret
  sensitive = true
}

output "repository_count" {
  value = length(data.entrepot_repositories.all.names)
}
