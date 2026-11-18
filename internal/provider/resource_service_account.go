package provider

import (
	"context"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/geoffo-dev/terraform-provider-entrepot/internal/client"
)

func init() { registerResource(NewServiceAccountResource) }

var (
	_ resource.Resource                = (*serviceAccountResource)(nil)
	_ resource.ResourceWithImportState = (*serviceAccountResource)(nil)
)

func NewServiceAccountResource() resource.Resource { return &serviceAccountResource{} }

type serviceAccountResource struct{ resourceBase }

// serviceAccountPrefix is prepended by the server to service account usernames.
const serviceAccountPrefix = "sa:"

type serviceAccountModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Username    types.String `tfsdk:"username"`
	UserID      types.String `tfsdk:"user_id"`
}

func (r *serviceAccountResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_account"
}

func (r *serviceAccountResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A service account: a non-human principal for automation. It authenticates with " +
			"`entrepot_service_account_token` tokens and is granted access with `entrepot_role_binding` " +
			"(`subject_type = \"user\"`, `subject = entrepot_service_account.<x>.username`). " +
			"Deleting a service account revokes its tokens.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, PlanModifiers: keep, MarkdownDescription: "Same as `name`."},
			"name": schema.StringAttribute{
				Required: true, Validators: nameValidators(), PlanModifiers: replace,
				MarkdownDescription: "Service account name (without the `sa:` prefix). Changing it forces a new service account.",
			},
			"description": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString(""), PlanModifiers: replace,
				MarkdownDescription: "Free-form description (the account's display name). The API cannot update " +
					"service accounts, so changing it forces a new service account (and new tokens).",
			},
			"username": schema.StringAttribute{
				Computed: true, PlanModifiers: keep,
				MarkdownDescription: "Username to use as a role binding subject: `sa:<name>`.",
			},
			"user_id": schema.StringAttribute{
				Computed: true, PlanModifiers: keep, MarkdownDescription: "Server-assigned user ID.",
			},
		},
	}
}

func (r *serviceAccountResource) fromAPI(u *client.User, m *serviceAccountModel) {
	name := u.Subject
	if name == "" {
		name = strings.TrimPrefix(u.Username, serviceAccountPrefix)
	}
	m.ID = types.StringValue(name)
	m.Name = types.StringValue(name)
	m.Description = types.StringValue(u.DisplayName)
	m.Username = types.StringValue(u.Username)
	m.UserID = types.StringValue(u.Id)
}

func (r *serviceAccountResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan serviceAccountModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.api.CreateServiceAccountWithResponse(ctx, client.ServiceAccountInput{
		Name: plan.Name.ValueString(), Description: strPtr(plan.Description),
	})
	if err = check(res, err, http.StatusCreated); err != nil {
		apiError(&resp.Diagnostics, "create service account", err)
		return
	}
	r.fromAPI(res.JSON201, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *serviceAccountResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state serviceAccountModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.api.GetServiceAccountWithResponse(ctx, state.Name.ValueString())
	if err = check(res, err, http.StatusOK); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read service account", err)
		return
	}
	r.fromAPI(res.JSON200, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is never called: every configurable attribute forces replacement.
func (r *serviceAccountResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Service accounts cannot be updated",
		"Every service account attribute forces replacement; this is a provider bug.")
}

func (r *serviceAccountResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state serviceAccountModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	res, err := r.api.DeleteServiceAccountWithResponse(ctx, state.Name.ValueString())
	if err = check(res, err, http.StatusNoContent, http.StatusNotFound); err != nil {
		apiError(&resp.Diagnostics, "delete service account", err)
	}
}

// ImportState accepts the name with or without the "sa:" prefix.
func (r *serviceAccountResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	name := strings.TrimPrefix(req.ID, serviceAccountPrefix)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), name)...)
}
