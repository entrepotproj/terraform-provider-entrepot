package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/geoffo-dev/terraform-provider-entrepot/internal/client"
)

func init() { registerResource(NewCleanupPolicyResource) }

var (
	_ resource.Resource                   = (*cleanupPolicyResource)(nil)
	_ resource.ResourceWithImportState    = (*cleanupPolicyResource)(nil)
	_ resource.ResourceWithValidateConfig = (*cleanupPolicyResource)(nil)
)

func NewCleanupPolicyResource() resource.Resource { return &cleanupPolicyResource{} }

type cleanupPolicyResource struct{ resourceBase }

type cleanupPolicyModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Description  types.String `tfsdk:"description"`
	Format       types.String `tfsdk:"format"`
	Criteria     types.Object `tfsdk:"criteria"`
	Repositories types.List   `tfsdk:"repositories"`
}

type cleanupCriteriaModel struct {
	PublishedOlderThanDays      types.Int64  `tfsdk:"published_older_than_days"`
	LastDownloadedOlderThanDays types.Int64  `tfsdk:"last_downloaded_older_than_days"`
	NameRegex                   types.String `tfsdk:"name_regex"`
	VersionRegex                types.String `tfsdk:"version_regex"`
	PrereleaseOnly              types.Bool   `tfsdk:"prerelease_only"`
	KeepLatest                  types.Int64  `tfsdk:"keep_latest"`
	UntaggedOlderThanDays       types.Int64  `tfsdk:"untagged_older_than_days"`
}

var cleanupCriteriaAttrTypes = map[string]attr.Type{
	"published_older_than_days": types.Int64Type, "last_downloaded_older_than_days": types.Int64Type,
	"name_regex": types.StringType, "version_regex": types.StringType, "prerelease_only": types.BoolType,
	"keep_latest": types.Int64Type, "untagged_older_than_days": types.Int64Type,
}

func (r *cleanupPolicyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cleanup_policy"
}

func (r *cleanupPolicyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	days := func(desc string) schema.Int64Attribute {
		return schema.Int64Attribute{Optional: true, MarkdownDescription: desc,
			Validators: []validator.Int64{int64validator.AtLeast(1)}}
	}
	re := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Optional: true, MarkdownDescription: desc,
			Validators: []validator.String{stringvalidator.LengthAtLeast(1), goRegexp()}}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A cleanup policy deletes components matching **all** of its set criteria from the " +
			"repositories that list it in `cleanup_policies`. Use the server's preview endpoint (or the UI) for a " +
			"dry run before attaching a policy.\n\n" +
			"A policy cannot be deleted while repositories use it; when repositories reference the policy resource, " +
			"Terraform updates them first.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, MarkdownDescription: "Same as `name`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name": schema.StringAttribute{Required: true, Validators: nameValidators(),
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				MarkdownDescription: "Policy name. Changing it forces a new policy."},
			"description": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString(""),
				MarkdownDescription: "Free-form description."},
			"format": schema.StringAttribute{Optional: true,
				Validators:          []validator.String{stringvalidator.OneOf(repositoryFormats...)},
				MarkdownDescription: "Restrict the policy to repositories of one format (`raw`, `pypi`, `npm`, `docker`, `apt`, `yum`, `helm`). Unset: any format."},
			"criteria": schema.SingleNestedAttribute{Required: true,
				MarkdownDescription: "What to delete. At least one criterion is required; all set criteria must match.",
				Attributes: map[string]schema.Attribute{
					"published_older_than_days":       days("Components published more than this many days ago."),
					"last_downloaded_older_than_days": days("Components last downloaded more than this many days ago (never-downloaded components count from their publish date)."),
					"name_regex":                      re("Go regular expression the component name must match."),
					"version_regex":                   re("Go regular expression the version must match."),
					"prerelease_only": schema.BoolAttribute{Optional: true,
						MarkdownDescription: "Only versions with a pre-release part (`1.2.0-rc.1`, `1.0.0.dev3`)."},
					"keep_latest": schema.Int64Attribute{Optional: true, Validators: []validator.Int64{int64validator.AtLeast(0)},
						MarkdownDescription: "Always keep this many newest versions of each component."},
					"untagged_older_than_days": days("Docker only: delete untagged manifests (and unreferenced layers) older than this many days."),
				},
			},
			"repositories": schema.ListAttribute{ElementType: types.StringType, Computed: true,
				PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
				MarkdownDescription: "Repositories that currently use the policy (informational). It changes when " +
					"repositories are configured and is refreshed on every read; it never causes a diff."},
		},
	}
}

func (r *cleanupPolicyResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m cleanupPolicyModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || m.Criteria.IsUnknown() || m.Criteria.IsNull() {
		return
	}
	var c cleanupCriteriaModel
	if !objectAs(ctx, m.Criteria, &c, &resp.Diagnostics) {
		return
	}
	set := func(v attr.Value) bool { return !v.IsNull() }
	if !set(c.PublishedOlderThanDays) && !set(c.LastDownloadedOlderThanDays) && !set(c.NameRegex) && !set(c.VersionRegex) &&
		!set(c.PrereleaseOnly) && !set(c.KeepLatest) && !set(c.UntaggedOlderThanDays) {
		resp.Diagnostics.AddAttributeError(path.Root("criteria"), "No cleanup criteria", "set at least one criterion")
	}
	if set(c.UntaggedOlderThanDays) && !m.Format.IsUnknown() && !m.Format.IsNull() && m.Format.ValueString() != "docker" {
		resp.Diagnostics.AddAttributeError(path.Root("criteria").AtName("untagged_older_than_days"), "Docker-only criterion",
			`untagged_older_than_days only applies to docker repositories (format = "docker" or unset)`)
	}
}

func (r *cleanupPolicyResource) input(ctx context.Context, m *cleanupPolicyModel, diags *diag.Diagnostics) client.CleanupPolicyInput {
	in := client.CleanupPolicyInput{Name: m.Name.ValueString(), Description: strPtr(m.Description), Format: strPtr(m.Format)}
	var c cleanupCriteriaModel
	if objectAs(ctx, m.Criteria, &c, diags) {
		in.Criteria = client.CleanupCriteria{
			PublishedOlderThanDays: intPtr(c.PublishedOlderThanDays), LastDownloadedOlderThanDays: intPtr(c.LastDownloadedOlderThanDays),
			NameRegex: strPtr(c.NameRegex), VersionRegex: strPtr(c.VersionRegex), PrereleaseOnly: boolPtr(c.PrereleaseOnly),
			KeepLatest: intPtr(c.KeepLatest), UntaggedOlderThanDays: intPtr(c.UntaggedOlderThanDays),
		}
	}
	return in
}

func (r *cleanupPolicyResource) fromAPI(ctx context.Context, p *client.CleanupPolicy, m *cleanupPolicyModel, diags *diag.Diagnostics) {
	m.ID = types.StringValue(p.Name)
	m.Name = types.StringValue(p.Name)
	m.Description = types.StringValue(p.Description)
	m.Format = strValOrEmpty(&p.Format)
	c := p.Criteria
	m.Criteria = objectFrom(ctx, cleanupCriteriaAttrTypes, cleanupCriteriaModel{
		PublishedOlderThanDays: intVal(c.PublishedOlderThanDays), LastDownloadedOlderThanDays: intVal(c.LastDownloadedOlderThanDays),
		NameRegex: strVal(c.NameRegex), VersionRegex: strVal(c.VersionRegex), PrereleaseOnly: boolVal(c.PrereleaseOnly),
		KeepLatest: intVal(c.KeepLatest), UntaggedOlderThanDays: intVal(c.UntaggedOlderThanDays),
	}, diags)
}

func (r *cleanupPolicyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan cleanupPolicyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	in := r.input(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.api.CreateCleanupPolicyWithResponse(ctx, in)
	if err = check(res, err, http.StatusCreated); err != nil {
		apiError(&resp.Diagnostics, "create cleanup policy", err)
		return
	}
	r.fromAPI(ctx, res.JSON201, &plan, &resp.Diagnostics)
	plan.Repositories = usersList(plan.Repositories, res.JSON201.Repositories)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *cleanupPolicyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state cleanupPolicyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.api.GetCleanupPolicyWithResponse(ctx, state.Name.ValueString())
	if err = check(res, err, http.StatusOK); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read cleanup policy", err)
		return
	}
	r.fromAPI(ctx, res.JSON200, &state, &resp.Diagnostics)
	state.Repositories = listValue(deref(res.JSON200.Repositories), false)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *cleanupPolicyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan cleanupPolicyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	in := r.input(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.api.UpdateCleanupPolicyWithResponse(ctx, plan.Name.ValueString(), in)
	if err = check(res, err, http.StatusOK); err != nil {
		apiError(&resp.Diagnostics, "update cleanup policy", err)
		return
	}
	r.fromAPI(ctx, res.JSON200, &plan, &resp.Diagnostics)
	plan.Repositories = usersList(plan.Repositories, res.JSON200.Repositories)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *cleanupPolicyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state cleanupPolicyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	res, err := r.api.DeleteCleanupPolicyWithResponse(ctx, state.Name.ValueString())
	if err = check(res, err, http.StatusNoContent, http.StatusNotFound); err != nil {
		// 409 while repositories still list the policy; the message names them.
		apiError(&resp.Diagnostics, "delete cleanup policy", err)
	}
}

func (r *cleanupPolicyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
