package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/entrepotproj/terraform-provider-entrepot/internal/client"
)

func init() { registerDataSource(NewRepositoryDataSource) }

var _ datasource.DataSource = (*repositoryDataSource)(nil)

func NewRepositoryDataSource() datasource.DataSource { return &repositoryDataSource{} }

type repositoryDataSource struct{ dataSourceBase }

// repositoryDataModel mirrors repositoryModel (the resource) without the
// write-only proxy password, plus the caller's permissions.
type repositoryDataModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Format          types.String `tfsdk:"format"`
	Kind            types.String `tfsdk:"kind"`
	BlobStore       types.String `tfsdk:"blob_store"`
	Description     types.String `tfsdk:"description"`
	Online          types.Bool   `tfsdk:"online"`
	AnonymousRead   types.Bool   `tfsdk:"anonymous_read"`
	Proxy           types.Object `tfsdk:"proxy"`
	Hosted          types.Object `tfsdk:"hosted"`
	Group           types.Object `tfsdk:"group"`
	Scan            types.Object `tfsdk:"scan"`
	CleanupPolicies types.List   `tfsdk:"cleanup_policies"`
	Docker          types.Object `tfsdk:"docker"`
	URL             types.String `tfsdk:"url"`
	Permissions     types.List   `tfsdk:"permissions"`
}

type proxyDataModel struct {
	RemoteURL               types.String `tfsdk:"remote_url"`
	Username                types.String `tfsdk:"username"`
	MetadataTTLSeconds      types.Int64  `tfsdk:"metadata_ttl_seconds"`
	ContentTTLSeconds       types.Int64  `tfsdk:"content_ttl_seconds"`
	NegativeCacheTTLSeconds types.Int64  `tfsdk:"negative_cache_ttl_seconds"`
	RoutingRule             types.String `tfsdk:"routing_rule"`
}

var proxyDataAttrTypes = map[string]attr.Type{
	"remote_url": types.StringType, "username": types.StringType,
	"metadata_ttl_seconds": types.Int64Type, "content_ttl_seconds": types.Int64Type,
	"negative_cache_ttl_seconds": types.Int64Type, "routing_rule": types.StringType,
}

func (d *repositoryDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_repository"
}

func (d *repositoryDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	str := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Computed: true, MarkdownDescription: desc}
	}
	boolean := func(desc string) schema.BoolAttribute {
		return schema.BoolAttribute{Computed: true, MarkdownDescription: desc}
	}
	num := func(desc string) schema.Int64Attribute {
		return schema.Int64Attribute{Computed: true, MarkdownDescription: desc}
	}
	strs := func(desc string) schema.ListAttribute {
		return schema.ListAttribute{ElementType: types.StringType, Computed: true, MarkdownDescription: desc}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a repository by name. The attributes mirror the `entrepot_repository` resource " +
			"(without the write-only proxy password), plus the actions the provider's credentials may perform on it.",
		Attributes: map[string]schema.Attribute{
			"id":             str("Same as `name`."),
			"name":           schema.StringAttribute{Required: true, MarkdownDescription: "Repository name."},
			"format":         str("Repository format (`raw`, `pypi`, `npm`, `docker`, `apt`, `yum`, `helm`)."),
			"kind":           str("`hosted`, `proxy` or `group`."),
			"blob_store":     str("Blob store holding the repository's content."),
			"description":    str("Description."),
			"online":         boolean("Whether the repository is online (offline proxies serve only cached content)."),
			"anonymous_read": boolean("Whether unauthenticated reads are allowed."),
			"url":            str("URL to configure package clients with."),
			"proxy": schema.SingleNestedAttribute{Computed: true, MarkdownDescription: "Proxy settings (null unless `kind = \"proxy\"`).",
				Attributes: map[string]schema.Attribute{
					"remote_url":                 str("Upstream URL."),
					"username":                   str("Upstream username."),
					"metadata_ttl_seconds":       num("Cache time for indexes/metadata (null: server default)."),
					"content_ttl_seconds":        num("Cache time for artifacts (null: never expire)."),
					"negative_cache_ttl_seconds": num("Cache time for upstream 404s (null: server default)."),
					"routing_rule":               str("Name of the routing rule restricting what is fetched upstream."),
				}},
			"hosted": schema.SingleNestedAttribute{Computed: true, MarkdownDescription: "Hosted settings (null unless `kind = \"hosted\"`).",
				Attributes: map[string]schema.Attribute{
					"allow_redeploy": boolean("Whether existing versions (Docker: tags) may be overwritten."),
					"apt": schema.SingleNestedAttribute{Computed: true, MarkdownDescription: "Apt distributions published.",
						Attributes: map[string]schema.Attribute{
							"distributions": schema.ListNestedAttribute{Computed: true, MarkdownDescription: "Distributions.",
								NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
									"name":          str("Distribution name, e.g. `bookworm`."),
									"components":    strs("Components."),
									"architectures": strs("Architectures."),
									"origin":        str("Release file Origin."),
									"label":         str("Release file Label."),
								}}},
						}},
					"yum": schema.SingleNestedAttribute{Computed: true, MarkdownDescription: "Yum repodata settings.",
						Attributes: map[string]schema.Attribute{
							"repodata_depth": num("Directory depth at which repodata is generated."),
						}},
				}},
			"group": schema.SingleNestedAttribute{Computed: true, MarkdownDescription: "Group settings (null unless `kind = \"group\"`).",
				Attributes: map[string]schema.Attribute{
					"members":       strs("Member repositories, searched in order."),
					"prefer_hosted": boolean("Whether hosted members win over proxy members for a package name."),
				}},
			"scan": schema.SingleNestedAttribute{Computed: true, MarkdownDescription: "Vulnerability scanning policy (null when not configured).",
				Attributes: map[string]schema.Attribute{
					"scanners":            strs("Names of the scanners run."),
					"mode":                str("`off`, `warn` or `quarantine`."),
					"severity_threshold":  str("Lowest severity that quarantines."),
					"on_upload":           boolean("Whether uploads are scanned."),
					"on_proxy_fetch":      boolean("Whether components cached by a proxy are scanned."),
					"block_until_scanned": boolean("Whether downloads are blocked until the first scan completes."),
					"hold_seconds":        num("How long a download of content still being scanned waits for the verdict (0: refused at once)."),
					"block_on_error":      boolean("Whether downloads of components whose latest scan failed are blocked (fail closed)."),
					"distro":              str("Apt/Yum distribution used for vulnerability matching."),
				}},
			"cleanup_policies": strs("Names of the cleanup policies applied (null when none)."),
			"docker": schema.SingleNestedAttribute{Computed: true, MarkdownDescription: "Docker settings (null when not set).",
				Attributes: map[string]schema.Attribute{
					"host": str("Hostname on which the registry API is this repository (images without the repository prefix)."),
				}},
			"permissions": strs("Actions the provider's credentials may perform on the repository (`read`, `write`, `delete`, `admin`)."),
		},
	}
}

func (d *repositoryDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg repositoryDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := d.api.GetRepositoryWithResponse(ctx, cfg.Name.ValueString())
	if err = check(res, err, http.StatusOK); err != nil {
		if client.IsNotFound(err) {
			resp.Diagnostics.AddError("Repository not found", "No repository named "+cfg.Name.ValueString()+
				" exists, or the provider's credentials cannot read it.")
			return
		}
		apiError(&resp.Diagnostics, "read repository", err)
		return
	}
	rp := res.JSON200
	diags := &resp.Diagnostics

	// Reuse the resource's conversion so both stay identical.
	rm := repositoryModel{
		Proxy: types.ObjectNull(proxyAttrTypes), Hosted: types.ObjectNull(hostedAttrTypes),
		Group: types.ObjectNull(groupAttrTypes), Scan: types.ObjectNull(scanAttrTypes),
		Docker: types.ObjectNull(dockerAttrTypes),
	}
	(&repositoryResource{}).fromAPI(ctx, rp, &rm, diags)

	m := repositoryDataModel{
		ID: rm.ID, Name: rm.Name, Format: rm.Format, Kind: rm.Kind, BlobStore: rm.BlobStore, Description: rm.Description,
		Online: rm.Online, AnonymousRead: rm.AnonymousRead, Hosted: rm.Hosted, Group: rm.Group, Scan: rm.Scan,
		CleanupPolicies: rm.CleanupPolicies, Docker: rm.Docker, URL: rm.URL, Permissions: listValue(rp.Permissions, false),
		Proxy: types.ObjectNull(proxyDataAttrTypes),
	}
	var p proxyModel
	if objectAs(ctx, rm.Proxy, &p, diags) {
		m.Proxy = objectFrom(ctx, proxyDataAttrTypes, proxyDataModel{
			RemoteURL: p.RemoteURL, Username: p.Username, MetadataTTLSeconds: p.MetadataTTLSeconds,
			ContentTTLSeconds: p.ContentTTLSeconds, NegativeCacheTTLSeconds: p.NegativeCacheTTLSeconds, RoutingRule: p.RoutingRule,
		}, diags)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
