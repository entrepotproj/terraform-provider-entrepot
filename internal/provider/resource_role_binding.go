package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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

func init() { registerResource(NewRoleBindingResource) }

var (
	_ resource.Resource                   = (*roleBindingResource)(nil)
	_ resource.ResourceWithImportState    = (*roleBindingResource)(nil)
	_ resource.ResourceWithValidateConfig = (*roleBindingResource)(nil)
)

func NewRoleBindingResource() resource.Resource { return &roleBindingResource{} }

type roleBindingResource struct{ resourceBase }

type roleBindingModel struct {
	ID                types.String `tfsdk:"id"`
	Role              types.String `tfsdk:"role"`
	SubjectType       types.String `tfsdk:"subject_type"`
	Subject           types.String `tfsdk:"subject"`
	RepositoryPattern types.String `tfsdk:"repository_pattern"`
	CreatedAt         types.String `tfsdk:"created_at"`
}

var subjectTypes = []string{"user", "group", "gitlab_policy", "authenticated"}

func (r *roleBindingResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role_binding"
}

func (r *roleBindingResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Grants a role to a subject on the repositories matching a pattern. Bindings are " +
			"immutable: changing any attribute replaces the binding.\n\n" +
			"Subjects are users (OIDC usernames, or `sa:<name>` for service accounts — use " +
			"`entrepot_service_account.<x>.username`), OIDC groups, GitLab trust policies, or every " +
			"authenticated principal.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, MarkdownDescription: "Server-assigned binding ID (used for import).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"role": schema.StringAttribute{
				Required: true, PlanModifiers: replace,
				MarkdownDescription: "Role name: a built-in role (`viewer`, `developer`, `maintainer`, `admin`) or an " +
					"`entrepot_role`. Changing it forces a new binding.",
			},
			"subject_type": schema.StringAttribute{
				Required: true, PlanModifiers: replace,
				Validators: []validator.String{stringvalidator.OneOf(subjectTypes...)},
				MarkdownDescription: "`user`, `group` (OIDC group), `gitlab_policy` (an `entrepot_gitlab_trust_policy`) " +
					"or `authenticated` (anyone who is signed in). Changing it forces a new binding.",
			},
			"subject": schema.StringAttribute{
				Optional: true, PlanModifiers: replace,
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
				MarkdownDescription: "Username (`sa:<name>` for service accounts), group name or trust policy name. " +
					"Required unless `subject_type = \"authenticated\"`, where it must be omitted. Changing it forces a new binding.",
			},
			"repository_pattern": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("*"), PlanModifiers: replace,
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
				MarkdownDescription: "Glob over repository names (default `*`, all repositories). A role with `admin` " +
					"on `*` grants system administration. Changing it forces a new binding.",
			},
			"created_at": schema.StringAttribute{
				Computed: true, MarkdownDescription: "Creation time (RFC 3339).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *roleBindingResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m roleBindingModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || m.SubjectType.IsUnknown() || m.SubjectType.IsNull() || m.Subject.IsUnknown() {
		return
	}
	if m.SubjectType.ValueString() == "authenticated" {
		if !m.Subject.IsNull() {
			resp.Diagnostics.AddAttributeError(path.Root("subject"), "Unexpected subject",
				`subject must be omitted when subject_type = "authenticated"`)
		}
	} else if m.Subject.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("subject"), "Missing subject",
			`subject is required unless subject_type = "authenticated"`)
	}
}

func (r *roleBindingResource) fromAPI(b *client.RoleBinding, m *roleBindingModel) {
	m.ID = types.StringValue(b.Id)
	m.Role = types.StringValue(b.Role)
	m.SubjectType = types.StringValue(string(b.SubjectType))
	m.Subject = strValOrEmpty(&b.Subject)
	m.RepositoryPattern = types.StringValue(b.RepositoryPattern)
	m.CreatedAt = timeValue(b.CreatedAt)
}

func (r *roleBindingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan roleBindingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := client.RoleBindingInput{
		Role: plan.Role.ValueString(), SubjectType: client.SubjectType(plan.SubjectType.ValueString()),
		Subject: strPtr(plan.Subject), RepositoryPattern: strPtr(plan.RepositoryPattern),
	}
	res, err := r.api.CreateRoleBindingWithResponse(ctx, in)
	if err = check(res, err, http.StatusCreated); err != nil {
		apiError(&resp.Diagnostics, "create role binding", err)
		return
	}
	r.fromAPI(res.JSON201, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *roleBindingResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state roleBindingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.api.GetRoleBindingWithResponse(ctx, state.ID.ValueString())
	if err = check(res, err, http.StatusOK); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read role binding", err)
		return
	}
	r.fromAPI(res.JSON200, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is never called: every attribute forces replacement.
func (r *roleBindingResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Role bindings cannot be updated", "Every role binding attribute forces replacement; this is a provider bug.")
}

func (r *roleBindingResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state roleBindingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	res, err := r.api.DeleteRoleBindingWithResponse(ctx, state.ID.ValueString())
	if err = check(res, err, http.StatusNoContent, http.StatusNotFound); err != nil {
		apiError(&resp.Diagnostics, "delete role binding", err)
	}
}

func (r *roleBindingResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
