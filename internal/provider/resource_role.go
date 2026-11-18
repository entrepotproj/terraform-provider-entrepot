package provider

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/geoffo-dev/terraform-provider-entrepot/internal/client"
)

func init() { registerResource(NewRoleResource) }

var (
	_ resource.Resource                   = (*roleResource)(nil)
	_ resource.ResourceWithImportState    = (*roleResource)(nil)
	_ resource.ResourceWithValidateConfig = (*roleResource)(nil)
)

func NewRoleResource() resource.Resource { return &roleResource{} }

type roleResource struct{ resourceBase }

type roleModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Actions     types.Set    `tfsdk:"actions"`
}

func (r *roleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

func (r *roleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A custom role: a named set of actions that `entrepot_role_binding` grants to users, " +
			"groups, GitLab trust policies or all authenticated principals on matching repositories.\n\n" +
			"The built-in roles (`viewer`, `developer`, `maintainer`, `admin`) are read-only and cannot be " +
			"managed with this resource; reference them by name in role bindings (or read them with the " +
			"`entrepot_role` data source).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, MarkdownDescription: "Same as `name`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required: true, Validators: nameValidators(),
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				MarkdownDescription: "Role name. Must not be a built-in role name. Changing it forces a new role.",
			},
			"description": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString(""),
				MarkdownDescription: "Free-form description.",
			},
			"actions": schema.SetAttribute{
				Required: true, ElementType: types.StringType, Validators: actionSetValidators(),
				MarkdownDescription: "Actions the role grants: any of `read`, `write` (upload/overwrite), `delete` " +
					"and `admin` (manage the repository; on repository pattern `*` it grants system administration).",
			},
		},
	}
}

func (r *roleResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var name types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("name"), &name)...)
	if !name.IsNull() && !name.IsUnknown() && slices.Contains(builtinRoles, name.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("name"), "Built-in role",
			fmt.Sprintf("%q is a built-in role; built-in roles are read-only on the server and cannot be managed. "+
				"Reference it by name in entrepot_role_binding instead, or choose another name.", name.ValueString()))
	}
}

func (r *roleResource) input(ctx context.Context, m *roleModel, diags *diag.Diagnostics) client.RoleInput {
	in := client.RoleInput{Name: m.Name.ValueString(), Description: strPtr(m.Description), Actions: []client.Action{}}
	for _, a := range stringsFromSet(ctx, m.Actions, diags) {
		in.Actions = append(in.Actions, client.Action(a))
	}
	return in
}

func (r *roleResource) fromAPI(role *client.Role, m *roleModel) {
	m.ID = types.StringValue(role.Name)
	m.Name = types.StringValue(role.Name)
	m.Description = types.StringValue(role.Description)
	actions := make([]string, 0, len(role.Actions))
	for _, a := range role.Actions {
		actions = append(actions, string(a))
	}
	m.Actions = stringSetFrom(actions, false)
}

// isBuiltin reports whether the server has a built-in role with this name.
func (r *roleResource) isBuiltin(ctx context.Context, name string) bool {
	res, err := r.api.GetRoleWithResponse(ctx, name)
	return err == nil && res.JSON200 != nil && res.JSON200.Builtin
}

// apiErr reports a failed call, explaining failures caused by targeting a
// built-in role (the server answers those with 404/409).
func (r *roleResource) apiErr(ctx context.Context, name, action string, err error, diags *diag.Diagnostics) {
	if r.isBuiltin(ctx, name) {
		diags.AddError("Built-in role", fmt.Sprintf("Unable to %s: %q is a built-in role, which is read-only on the server. "+
			"Remove it from the configuration (and from state with `terraform state rm`) and reference it by name instead.", action, name))
		return
	}
	apiError(diags, action, err)
}

func (r *roleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan roleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	in := r.input(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.api.CreateRoleWithResponse(ctx, in)
	if err = check(res, err, http.StatusCreated); err != nil {
		r.apiErr(ctx, in.Name, "create role", err, &resp.Diagnostics)
		return
	}
	r.fromAPI(res.JSON201, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *roleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state roleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.api.GetRoleWithResponse(ctx, state.Name.ValueString())
	if err = check(res, err, http.StatusOK); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read role", err)
		return
	}
	r.fromAPI(res.JSON200, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *roleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan roleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	in := r.input(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.api.UpdateRoleWithResponse(ctx, in.Name, in)
	if err = check(res, err, http.StatusOK); err != nil {
		r.apiErr(ctx, in.Name, "update role", err, &resp.Diagnostics)
		return
	}
	r.fromAPI(res.JSON200, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *roleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state roleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	name := state.Name.ValueString()
	res, err := r.api.DeleteRoleWithResponse(ctx, name)
	if err = check(res, err, http.StatusNoContent, http.StatusNotFound); err != nil {
		apiError(&resp.Diagnostics, "delete role", err)
		return
	}
	// The server also answers 404 for built-in roles; never report those as deleted.
	if res.StatusCode() == http.StatusNotFound && r.isBuiltin(ctx, name) {
		r.apiErr(ctx, name, "delete role", err, &resp.Diagnostics)
	}
}

func (r *roleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
