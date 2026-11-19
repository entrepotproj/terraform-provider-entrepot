package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/entrepotproj/terraform-provider-entrepot/internal/client"
)

func init() { registerDataSource(NewRoleDataSource) }

var _ datasource.DataSource = (*roleDataSource)(nil)

func NewRoleDataSource() datasource.DataSource { return &roleDataSource{} }

type roleDataSource struct{ dataSourceBase }

type roleDataModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Actions     types.List   `tfsdk:"actions"`
	Builtin     types.Bool   `tfsdk:"builtin"`
}

func (d *roleDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

func (d *roleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a role (built-in such as `viewer`, `developer`, `maintainer`, `admin`, or custom) by name.",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Computed: true, MarkdownDescription: "Same as `name`."},
			"name":        schema.StringAttribute{Required: true, MarkdownDescription: "Role name."},
			"description": schema.StringAttribute{Computed: true, MarkdownDescription: "Role description."},
			"actions": schema.ListAttribute{ElementType: types.StringType, Computed: true,
				MarkdownDescription: "Actions the role grants (`read`, `write`, `delete`, `admin`)."},
			"builtin": schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the role is built in (and cannot be changed)."},
		},
	}
}

func (d *roleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m roleDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := d.api.GetRoleWithResponse(ctx, m.Name.ValueString())
	if err = check(res, err, http.StatusOK); err != nil {
		if client.IsNotFound(err) {
			resp.Diagnostics.AddError("Role not found", "No role named "+m.Name.ValueString()+" exists.")
			return
		}
		apiError(&resp.Diagnostics, "read role", err)
		return
	}
	role := res.JSON200
	actions := make([]string, 0, len(role.Actions))
	for _, a := range role.Actions {
		actions = append(actions, string(a))
	}
	m.ID = types.StringValue(role.Name)
	m.Name = types.StringValue(role.Name)
	m.Description = types.StringValue(role.Description)
	m.Actions = listValue(actions, false)
	m.Builtin = types.BoolValue(role.Builtin)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
