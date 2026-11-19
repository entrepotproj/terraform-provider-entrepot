package provider

import (
	"context"
	"net/http"
	"time"

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

	"github.com/entrepotproj/terraform-provider-entrepot/internal/client"
)

func init() { registerResource(NewWaiverResource) }

var (
	_ resource.Resource                   = (*waiverResource)(nil)
	_ resource.ResourceWithImportState    = (*waiverResource)(nil)
	_ resource.ResourceWithValidateConfig = (*waiverResource)(nil)
)

func NewWaiverResource() resource.Resource { return &waiverResource{} }

type waiverResource struct{ resourceBase }

type waiverModel struct {
	ID                types.String `tfsdk:"id"`
	VulnID            types.String `tfsdk:"vuln_id"`
	RepositoryPattern types.String `tfsdk:"repository_pattern"`
	ComponentPattern  types.String `tfsdk:"component_pattern"`
	Reason            types.String `tfsdk:"reason"`
	ExpiresAt         types.String `tfsdk:"expires_at"`
	CreatedBy         types.String `tfsdk:"created_by"`
	CreatedAt         types.String `tfsdk:"created_at"`
}

func (r *waiverResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_waiver"
}

func (r *waiverResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A vulnerability waiver: findings matching it are ignored when deciding whether to " +
			"quarantine a component. Repositories and components are matched with globs. Expired waivers stay " +
			"defined (and in state) but no longer apply.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, PlanModifiers: keep,
				MarkdownDescription: "Server-assigned waiver ID (used for import)."},
			"vuln_id": schema.StringAttribute{Required: true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
				MarkdownDescription: "Vulnerability ID, e.g. `CVE-2024-3094` or `GHSA-…`, or `*` for any vulnerability (then a repository or component pattern is required)."},
			"repository_pattern": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("*"),
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
				MarkdownDescription: "Glob over repository names. Default `*`."},
			"component_pattern": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("*"),
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
				MarkdownDescription: "Glob over `name` or `name@version` of components, e.g. `lodash@4.17.*`. Default `*`."},
			"reason": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString(""),
				MarkdownDescription: "Why the finding is accepted (shown in the UI and audit log)."},
			"expires_at": schema.StringAttribute{Optional: true, Validators: []validator.String{rfc3339Timestamp()},
				MarkdownDescription: "When the waiver stops applying, as an RFC 3339 timestamp (e.g. `2026-12-31T23:59:59Z`). Unset: never expires."},
			"created_by": schema.StringAttribute{Computed: true, PlanModifiers: keep,
				MarkdownDescription: "Principal that created the waiver."},
			"created_at": schema.StringAttribute{Computed: true, PlanModifiers: keep,
				MarkdownDescription: "Creation time (RFC 3339, UTC)."},
		},
	}
}

func (r *waiverResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m waiverModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	noSurroundingSpace(&resp.Diagnostics, path.Root("vuln_id"), m.VulnID)
	noSurroundingSpace(&resp.Diagnostics, path.Root("repository_pattern"), m.RepositoryPattern)
	noSurroundingSpace(&resp.Diagnostics, path.Root("component_pattern"), m.ComponentPattern)
	isAny := func(v types.String) bool { return !v.IsUnknown() && (v.IsNull() || v.ValueString() == "*") }
	if !m.VulnID.IsUnknown() && m.VulnID.ValueString() == "*" && isAny(m.RepositoryPattern) && isAny(m.ComponentPattern) {
		resp.Diagnostics.AddAttributeError(path.Root("vuln_id"), "Waiver too broad",
			`a waiver for every vulnerability (vuln_id = "*") must set repository_pattern or component_pattern`)
	}
}

func (r *waiverResource) input(m *waiverModel, diags *diag.Diagnostics) client.WaiverInput {
	in := client.WaiverInput{VulnId: m.VulnID.ValueString(), RepositoryPattern: strPtr(m.RepositoryPattern),
		ComponentPattern: strPtr(m.ComponentPattern), Reason: strPtr(m.Reason)}
	if s := strPtr(m.ExpiresAt); s != nil {
		t, err := time.Parse(time.RFC3339, *s)
		if err != nil {
			diags.AddAttributeError(path.Root("expires_at"), "Invalid timestamp", err.Error())
		}
		in.ExpiresAt = &t
	}
	return in
}

func (r *waiverResource) fromAPI(w *client.Waiver, m *waiverModel) {
	m.ID = types.StringValue(w.Id)
	m.VulnID = types.StringValue(w.VulnId)
	m.RepositoryPattern = types.StringValue(w.RepositoryPattern)
	m.ComponentPattern = types.StringValue(w.ComponentPattern)
	m.Reason = types.StringValue(w.Reason)
	m.ExpiresAt = timeKeepPrior(m.ExpiresAt, w.ExpiresAt)
	m.CreatedBy = types.StringValue(w.CreatedBy)
	m.CreatedAt = timeString(w.CreatedAt)
}

func (r *waiverResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan waiverModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	in := r.input(&plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.api.CreateWaiverWithResponse(ctx, in)
	if err = check(res, err, http.StatusCreated); err != nil {
		apiError(&resp.Diagnostics, "create waiver", err)
		return
	}
	r.fromAPI(res.JSON201, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *waiverResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state waiverModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.api.GetWaiverWithResponse(ctx, state.ID.ValueString())
	if err = check(res, err, http.StatusOK); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read waiver", err)
		return
	}
	r.fromAPI(res.JSON200, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *waiverResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state waiverModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	in := r.input(&plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.api.UpdateWaiverWithResponse(ctx, state.ID.ValueString(), in)
	if err = check(res, err, http.StatusOK); err != nil {
		apiError(&resp.Diagnostics, "update waiver", err)
		return
	}
	r.fromAPI(res.JSON200, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *waiverResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state waiverModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	res, err := r.api.DeleteWaiverWithResponse(ctx, state.ID.ValueString())
	if err = check(res, err, http.StatusNoContent, http.StatusNotFound); err != nil {
		apiError(&resp.Diagnostics, "delete waiver", err)
	}
}

func (r *waiverResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
