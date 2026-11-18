package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/geoffo-dev/terraform-provider-entrepot/internal/client"
)

func init() { registerResource(NewGitLabTrustPolicyResource) }

var (
	_ resource.Resource                = (*gitlabTrustPolicyResource)(nil)
	_ resource.ResourceWithImportState = (*gitlabTrustPolicyResource)(nil)
)

func NewGitLabTrustPolicyResource() resource.Resource { return &gitlabTrustPolicyResource{} }

type gitlabTrustPolicyResource struct{ resourceBase }

type gitlabTrustPolicyModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	GitLab      types.String `tfsdk:"gitlab"`
	Conditions  types.Map    `tfsdk:"conditions"`
}

func (r *gitlabTrustPolicyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_gitlab_trust_policy"
}

func (r *gitlabTrustPolicyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A GitLab trust policy lets GitLab CI/CD jobs authenticate with their `id_token` " +
			"(no stored secrets). A job is matched to the policy when its token was issued by the named GitLab " +
			"instance and every condition matches; bind roles to the policy with `entrepot_role_binding` " +
			"(`subject_type = \"gitlab_policy\"`). Deleting a policy also deletes the role bindings that reference it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, MarkdownDescription: "Same as `name`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required: true, Validators: nameValidators(),
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				MarkdownDescription: "Policy name, used as the role binding subject. Changing it forces a new policy.",
			},
			"description": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString(""),
				MarkdownDescription: "Free-form description.",
			},
			"gitlab": schema.StringAttribute{
				Required: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
				MarkdownDescription: "Name of a trusted GitLab instance from the server's `auth.gitlab` configuration " +
					"(listed in `GET /api/v1/system/info` as `gitlabInstances`).",
			},
			"conditions": schema.MapAttribute{
				Required: true, ElementType: types.StringType,
				Validators: []validator.Map{
					mapvalidator.SizeAtLeast(1),
					mapvalidator.KeysAre(stringvalidator.LengthAtLeast(1)),
					mapvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)),
				},
				MarkdownDescription: "`id_token` claim → glob pattern; all must match. Common claims: `project_path`, " +
					"`namespace_path`, `ref`, `ref_type`, `ref_protected`, `environment`, `pipeline_source`. " +
					"`*` matches within a path segment, `**` across segments and `a|b` either alternative, e.g. `{ project_path = \"platform/**\", ref_protected = \"true\" }`.",
			},
		},
	}
}

func (r *gitlabTrustPolicyResource) input(ctx context.Context, m *gitlabTrustPolicyModel, diags *diag.Diagnostics) client.GitLabTrustPolicyInput {
	return client.GitLabTrustPolicyInput{
		Name: m.Name.ValueString(), Description: strPtr(m.Description), Gitlab: m.GitLab.ValueString(),
		Conditions: stringMap(ctx, m.Conditions, diags),
	}
}

func (r *gitlabTrustPolicyResource) fromAPI(p *client.GitLabTrustPolicy, m *gitlabTrustPolicyModel) {
	m.ID = types.StringValue(p.Name)
	m.Name = types.StringValue(p.Name)
	m.Description = types.StringValue(p.Description)
	m.GitLab = types.StringValue(p.Gitlab)
	m.Conditions = mapValue(p.Conditions)
}

func (r *gitlabTrustPolicyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan gitlabTrustPolicyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	in := r.input(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.api.CreateGitLabTrustPolicyWithResponse(ctx, in)
	if err = check(res, err, http.StatusCreated); err != nil {
		apiError(&resp.Diagnostics, "create GitLab trust policy", err)
		return
	}
	r.fromAPI(res.JSON201, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *gitlabTrustPolicyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state gitlabTrustPolicyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.api.GetGitLabTrustPolicyWithResponse(ctx, state.Name.ValueString())
	if err = check(res, err, http.StatusOK); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read GitLab trust policy", err)
		return
	}
	r.fromAPI(res.JSON200, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *gitlabTrustPolicyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan gitlabTrustPolicyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	in := r.input(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.api.UpdateGitLabTrustPolicyWithResponse(ctx, in.Name, in)
	if err = check(res, err, http.StatusOK); err != nil {
		apiError(&resp.Diagnostics, "update GitLab trust policy", err)
		return
	}
	r.fromAPI(res.JSON200, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *gitlabTrustPolicyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state gitlabTrustPolicyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	res, err := r.api.DeleteGitLabTrustPolicyWithResponse(ctx, state.Name.ValueString())
	if err = check(res, err, http.StatusNoContent, http.StatusNotFound); err != nil {
		apiError(&resp.Diagnostics, "delete GitLab trust policy", err)
	}
}

func (r *gitlabTrustPolicyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
