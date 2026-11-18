package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/geoffo-dev/terraform-provider-entrepot/internal/acctest"
)

func TestAccGitLabTrustPolicy(t *testing.T) {
	s := testServer(t)
	name := "gl-" + randSuffix()
	cfg := func(policyName, desc, conditions string) string {
		return s.ProviderBlock() + fmt.Sprintf(`
resource "entrepot_gitlab_trust_policy" "test" {
  name        = %q
  description = %q
  gitlab      = %q
  conditions  = %s
}

resource "entrepot_role_binding" "ci" {
  role         = "developer"
  subject_type = "gitlab_policy"
  subject      = entrepot_gitlab_trust_policy.test.name
}
`, policyName, desc, acctest.GitLabInstance, conditions)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{
				Config: cfg(name, "platform pipelines", `{ project_path = "platform/**" }`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_gitlab_trust_policy.test", "id", name),
					resource.TestCheckResourceAttr("entrepot_gitlab_trust_policy.test", "gitlab", acctest.GitLabInstance),
					resource.TestCheckResourceAttr("entrepot_gitlab_trust_policy.test", "conditions.%", "1"),
					resource.TestCheckResourceAttr("entrepot_role_binding.ci", "subject", name),
				),
			},
			// Description and conditions update in place (the binding is kept).
			{
				Config: cfg(name, "protected platform pipelines", `{ project_path = "platform/**", ref_protected = "true" }`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_gitlab_trust_policy.test", plancheck.ResourceActionUpdate),
					plancheck.ExpectResourceAction("entrepot_role_binding.ci", plancheck.ResourceActionNoop),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("entrepot_gitlab_trust_policy.test", "description", "protected platform pipelines"),
					resource.TestCheckResourceAttr("entrepot_gitlab_trust_policy.test", "conditions.ref_protected", "true"),
				),
			},
			{ResourceName: "entrepot_gitlab_trust_policy.test", ImportState: true, ImportStateId: name, ImportStateVerify: true},
			// Deleted outside Terraform (which also deletes its bindings) → both recreated.
			{
				PreConfig: func() {
					if _, err := s.API.DeleteGitLabTrustPolicyWithResponse(context.Background(), name); err != nil {
						t.Fatal(err)
					}
				},
				Config: cfg(name, "protected platform pipelines", `{ project_path = "platform/**", ref_protected = "true" }`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_gitlab_trust_policy.test", plancheck.ResourceActionCreate),
					plancheck.ExpectResourceAction("entrepot_role_binding.ci", plancheck.ResourceActionCreate),
				}},
			},
			// Renaming replaces the policy and, through the reference, the binding.
			{
				Config: cfg(name+"-new", "protected platform pipelines", `{ project_path = "platform/**" }`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("entrepot_gitlab_trust_policy.test", plancheck.ResourceActionDestroyBeforeCreate),
					plancheck.ExpectResourceAction("entrepot_role_binding.ci", plancheck.ResourceActionDestroyBeforeCreate),
				}},
				Check: resource.TestCheckResourceAttr("entrepot_role_binding.ci", "subject", name+"-new"),
			},
		},
	})
}

func TestAccGitLabTrustPolicyValidation(t *testing.T) {
	s := testServer(t)
	policy := func(gitlab, conditions string) string {
		return s.ProviderBlock() + fmt.Sprintf(`
resource "entrepot_gitlab_trust_policy" "bad" {
  name       = "bad-%s"
  gitlab     = %q
  conditions = %s
}`, randSuffix(), gitlab, conditions)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{
			{Config: policy("no-such-gitlab", `{ project_path = "a/b" }`), ExpectError: regexpMust(`unknown\s+GitLab\s+instance`)},
			{Config: policy(acctest.GitLabInstance, `{}`), ExpectError: regexpMust(`at least 1`)},
			{Config: policy(acctest.GitLabInstance, `{ project_path = "" }`), ExpectError: regexpMust(`at least 1`)},
		},
	})
}
