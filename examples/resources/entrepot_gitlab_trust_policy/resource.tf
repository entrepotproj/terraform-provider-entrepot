# Trust id_tokens from protected branches of projects under platform/ on the
# GitLab instance configured as "gitlab" in the server's auth.gitlab list.
resource "entrepot_gitlab_trust_policy" "platform" {
  name        = "platform-protected"
  description = "Protected pipelines of the platform group"
  gitlab      = "gitlab"
  conditions = {
    project_path  = "platform/**"
    ref_protected = "true"
  }
}

# In .gitlab-ci.yml:
#   id_tokens:
#     ENTREPOT_TOKEN:
#       aud: https://entrepot.example.com
