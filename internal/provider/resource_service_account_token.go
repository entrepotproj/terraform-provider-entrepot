package provider

import (
	"context"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/entrepotproj/terraform-provider-entrepot/internal/client"
)

func init() { registerResource(NewServiceAccountTokenResource) }

var (
	_ resource.Resource                = (*serviceAccountTokenResource)(nil)
	_ resource.ResourceWithImportState = (*serviceAccountTokenResource)(nil)
)

func NewServiceAccountTokenResource() resource.Resource { return &serviceAccountTokenResource{} }

type serviceAccountTokenResource struct{ resourceBase }

type serviceAccountTokenModel struct {
	ID             types.String `tfsdk:"id"`
	ServiceAccount types.String `tfsdk:"service_account"`
	Name           types.String `tfsdk:"name"`
	Scopes         types.Set    `tfsdk:"scopes"`
	ExpiresInDays  types.Int64  `tfsdk:"expires_in_days"`
	Secret         types.String `tfsdk:"secret"`
	Hint           types.String `tfsdk:"hint"`
	ExpiresAt      types.String `tfsdk:"expires_at"`
	CreatedAt      types.String `tfsdk:"created_at"`
}

func (r *serviceAccountTokenResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_account_token"
}

func (r *serviceAccountTokenResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "An API token (`ent_…`) for a service account. Tokens cannot be changed: changing " +
			"any attribute revokes the token and issues a new one.\n\n" +
			"The token value (`secret`) is returned by the server only when the token is created and is stored " +
			"in the Terraform state — protect the state accordingly. After `terraform import` the secret is " +
			"unknown (null); replace the token (e.g. with `-replace`) to obtain a new secret.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, PlanModifiers: keep, MarkdownDescription: "Server-assigned token ID."},
			"service_account": schema.StringAttribute{
				Required: true, Validators: nameValidators(), PlanModifiers: replace,
				MarkdownDescription: "Name of the service account (without the `sa:` prefix), e.g. " +
					"`entrepot_service_account.ci.name`. Changing it forces a new token.",
			},
			"name": schema.StringAttribute{
				Required: true, PlanModifiers: replace,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
				MarkdownDescription: "Token name (shown in the UI and audit log). Changing it forces a new token.",
			},
			"scopes": schema.SetAttribute{
				Optional: true, Computed: true, ElementType: types.StringType, Validators: actionSetValidators(),
				PlanModifiers: []planmodifier.Set{setplanmodifier.UseStateForUnknown(), setplanmodifier.RequiresReplace()},
				MarkdownDescription: "Restrict the token to these actions (`read`, `write`, `delete`, `admin`) on top of " +
					"the service account's role bindings. Omit for an unrestricted token (the attribute is then empty), " +
					"unless the provider itself authenticates with a scoped token, in which case the server copies its " +
					"scopes. Changing it forces a new token.",
			},
			"expires_in_days": schema.Int64Attribute{
				Optional: true, Validators: []validator.Int64{int64validator.AtLeast(1)},
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplaceIf(
					func(_ context.Context, req planmodifier.Int64Request, resp *int64planmodifier.RequiresReplaceIfFuncResponse) {
						// Not returned by the API: after import the state is null, and
						// setting it then only records the configured value.
						resp.RequiresReplace = !req.StateValue.IsNull()
					},
					"Changing the value forces a new token (except after import).",
					"Changing the value forces a new token (except after import).")},
				MarkdownDescription: "Lifetime in days; omit for a token that never expires. Only used at creation " +
					"(not returned by the API, so null after import; setting it on an imported token just records " +
					"the value). Changing it forces a new token. " +
					"The token is not renewed automatically when it expires: replace it.",
			},
			"secret": schema.StringAttribute{
				Computed: true, Sensitive: true, PlanModifiers: keep,
				MarkdownDescription: "The token value, for `Authorization: Bearer` or as a package-client password. " +
					"Only known when Terraform created the token (null after import).",
			},
			"hint": schema.StringAttribute{
				Computed: true, PlanModifiers: keep, MarkdownDescription: "Non-secret abbreviation of the token, e.g. `ent_AbCd…wXyZ`.",
			},
			"expires_at": schema.StringAttribute{
				Computed: true, PlanModifiers: keep, MarkdownDescription: "Expiry time (RFC 3339); null for no expiry.",
			},
			"created_at": schema.StringAttribute{
				Computed: true, PlanModifiers: keep, MarkdownDescription: "Creation time (RFC 3339).",
			},
		},
	}
}

func (r *serviceAccountTokenResource) fromAPI(t *client.ApiToken, m *serviceAccountTokenModel) {
	m.ID = types.StringValue(t.Id)
	m.Name = types.StringValue(t.Name)
	m.Scopes = stringSetFrom(t.Scopes, false)
	m.Hint = types.StringValue(t.Hint)
	m.ExpiresAt = timePtrValue(t.ExpiresAt)
	m.CreatedAt = timeValue(t.CreatedAt)
}

func (r *serviceAccountTokenResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan serviceAccountTokenModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := client.ApiTokenInput{Name: plan.Name.ValueString(), ExpiresInDays: intPtr(plan.ExpiresInDays)}
	if scopes := stringsFromSet(ctx, plan.Scopes, &resp.Diagnostics); scopes != nil {
		s := make([]client.ApiTokenInputScopes, 0, len(scopes))
		for _, sc := range scopes {
			s = append(s, client.ApiTokenInputScopes(sc))
		}
		in.Scopes = &s
	}
	res, err := r.api.CreateServiceAccountTokenWithResponse(ctx, plan.ServiceAccount.ValueString(), in)
	if err = check(res, err, http.StatusCreated); err != nil {
		apiError(&resp.Diagnostics, "create service account token", err)
		return
	}
	r.fromAPI(&res.JSON201.Token, &plan)
	plan.Secret = types.StringValue(res.JSON201.Secret)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *serviceAccountTokenResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state serviceAccountTokenModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.api.ListServiceAccountTokensWithResponse(ctx, state.ServiceAccount.ValueString())
	if err = check(res, err, http.StatusOK); err != nil {
		if client.IsNotFound(err) { // the service account is gone, and its tokens with it
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "list service account tokens", err)
		return
	}
	for i := range *res.JSON200 {
		if t := &(*res.JSON200)[i]; t.Id == state.ID.ValueString() {
			r.fromAPI(t, &state) // secret and expires_in_days are kept from state
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

// Update only happens when expires_in_days is set on an imported token
// (everything else forces replacement): it records the planned value.
func (r *serviceAccountTokenResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan serviceAccountTokenModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *serviceAccountTokenResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state serviceAccountTokenModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	res, err := r.api.DeleteServiceAccountTokenWithResponse(ctx, state.ServiceAccount.ValueString(), state.ID.ValueString())
	if err = check(res, err, http.StatusNoContent, http.StatusNotFound); err != nil {
		apiError(&resp.Diagnostics, "revoke service account token", err)
	}
}

// ImportState takes "<service_account>/<token_id>".
func (r *serviceAccountTokenResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	sa, id, err := splitImportID(req.ID, "<service_account>/<token_id>")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("service_account"), strings.TrimPrefix(sa, serviceAccountPrefix))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}
