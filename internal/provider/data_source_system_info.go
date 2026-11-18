package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func init() { registerDataSource(NewSystemInfoDataSource) }

var _ datasource.DataSource = (*systemInfoDataSource)(nil)

func NewSystemInfoDataSource() datasource.DataSource { return &systemInfoDataSource{} }

type systemInfoDataSource struct{ dataSourceBase }

type systemInfoModel struct {
	Version         types.String `tfsdk:"version"`
	BaseURL         types.String `tfsdk:"base_url"`
	Formats         types.List   `tfsdk:"formats"`
	OIDCEnabled     types.Bool   `tfsdk:"oidc_enabled"`
	OIDCDisplayName types.String `tfsdk:"oidc_display_name"`
	GitLabInstances types.List   `tfsdk:"gitlab_instances"`
	AnonymousAccess types.Bool   `tfsdk:"anonymous_access"`
}

type formatInfoModel struct {
	Name  types.String `tfsdk:"name"`
	Kinds types.List   `tfsdk:"kinds"`
}

var formatInfoAttrTypes = map[string]attr.Type{"name": types.StringType, "kinds": types.ListType{ElemType: types.StringType}}

func (d *systemInfoDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_system_info"
}

func (d *systemInfoDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Information about the Entrepot server: version, public URL, supported formats and enabled authentication features.",
		Attributes: map[string]schema.Attribute{
			"version":  schema.StringAttribute{Computed: true, MarkdownDescription: "Server version."},
			"base_url": schema.StringAttribute{Computed: true, MarkdownDescription: "Public base URL of the server (used in repository URLs)."},
			"formats": schema.ListNestedAttribute{Computed: true, MarkdownDescription: "Supported repository formats.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"name": schema.StringAttribute{Computed: true, MarkdownDescription: "Format name, e.g. `npm`."},
					"kinds": schema.ListAttribute{ElementType: types.StringType, Computed: true,
						MarkdownDescription: "Repository kinds the format supports (`hosted`, `proxy`, `group`)."},
				}}},
			"oidc_enabled":      schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether OIDC single sign-on is enabled."},
			"oidc_display_name": schema.StringAttribute{Computed: true, MarkdownDescription: "Display name of the OIDC provider (empty when disabled)."},
			"gitlab_instances": schema.ListAttribute{ElementType: types.StringType, Computed: true,
				MarkdownDescription: "GitLab instances whose CI job tokens (`id_token`) are accepted."},
			"anonymous_access": schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether anonymous access is enabled server-wide."},
		},
	}
}

func (d *systemInfoDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	res, err := d.api.GetSystemInfoWithResponse(ctx)
	if err = check(res, err, http.StatusOK); err != nil {
		apiError(&resp.Diagnostics, "read system info", err)
		return
	}
	info := res.JSON200
	formats := make([]formatInfoModel, 0, len(info.Formats))
	for _, f := range info.Formats {
		kinds := make([]string, 0, len(f.Kinds))
		for _, k := range f.Kinds {
			kinds = append(kinds, string(k))
		}
		formats = append(formats, formatInfoModel{Name: types.StringValue(f.Name), Kinds: listValue(kinds, false)})
	}
	m := systemInfoModel{
		Version: types.StringValue(info.Version), BaseURL: types.StringValue(info.BaseUrl),
		Formats:     listOfObjects(ctx, formatInfoAttrTypes, formats, &resp.Diagnostics),
		OIDCEnabled: types.BoolValue(info.Oidc.Enabled), OIDCDisplayName: types.StringValue(info.Oidc.DisplayName),
		GitLabInstances: listValue(info.GitlabInstances, false), AnonymousAccess: types.BoolValue(info.AnonymousAccess),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
