package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/entrepotproj/terraform-provider-entrepot/internal/client"
)

func init() { registerResource(NewRepositoryResource) }

var (
	_ resource.Resource                   = (*repositoryResource)(nil)
	_ resource.ResourceWithImportState    = (*repositoryResource)(nil)
	_ resource.ResourceWithValidateConfig = (*repositoryResource)(nil)
)

func NewRepositoryResource() resource.Resource { return &repositoryResource{} }

type repositoryResource struct{ resourceBase }

var repositoryFormats = []string{"raw", "pypi", "npm", "docker", "apt", "yum", "helm"}

type repositoryModel struct {
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
	URL             types.String `tfsdk:"url"`
}

type proxyModel struct {
	RemoteURL               types.String `tfsdk:"remote_url"`
	Username                types.String `tfsdk:"username"`
	Password                types.String `tfsdk:"password"`
	MetadataTTLSeconds      types.Int64  `tfsdk:"metadata_ttl_seconds"`
	ContentTTLSeconds       types.Int64  `tfsdk:"content_ttl_seconds"`
	NegativeCacheTTLSeconds types.Int64  `tfsdk:"negative_cache_ttl_seconds"`
	RoutingRule             types.String `tfsdk:"routing_rule"`
}

type hostedModel struct {
	AllowRedeploy types.Bool   `tfsdk:"allow_redeploy"`
	Apt           types.Object `tfsdk:"apt"`
	Yum           types.Object `tfsdk:"yum"`
}

type aptModel struct {
	Distributions types.List `tfsdk:"distributions"`
}

type aptDistributionModel struct {
	Name          types.String `tfsdk:"name"`
	Components    types.List   `tfsdk:"components"`
	Architectures types.List   `tfsdk:"architectures"`
	Origin        types.String `tfsdk:"origin"`
	Label         types.String `tfsdk:"label"`
}

type yumModel struct {
	RepodataDepth types.Int64 `tfsdk:"repodata_depth"`
}

type groupModel struct {
	Members      types.List `tfsdk:"members"`
	PreferHosted types.Bool `tfsdk:"prefer_hosted"`
}

type scanModel struct {
	Scanners          types.List   `tfsdk:"scanners"`
	Mode              types.String `tfsdk:"mode"`
	SeverityThreshold types.String `tfsdk:"severity_threshold"`
	OnUpload          types.Bool   `tfsdk:"on_upload"`
	OnProxyFetch      types.Bool   `tfsdk:"on_proxy_fetch"`
	BlockUntilScanned types.Bool   `tfsdk:"block_until_scanned"`
	Distro            types.String `tfsdk:"distro"`
}

var (
	proxyAttrTypes = map[string]attr.Type{
		"remote_url": types.StringType, "username": types.StringType, "password": types.StringType,
		"metadata_ttl_seconds": types.Int64Type, "content_ttl_seconds": types.Int64Type,
		"negative_cache_ttl_seconds": types.Int64Type, "routing_rule": types.StringType,
	}
	aptDistributionAttrTypes = map[string]attr.Type{
		"name": types.StringType, "components": types.ListType{ElemType: types.StringType},
		"architectures": types.ListType{ElemType: types.StringType}, "origin": types.StringType, "label": types.StringType,
	}
	aptAttrTypes = map[string]attr.Type{
		"distributions": types.ListType{ElemType: types.ObjectType{AttrTypes: aptDistributionAttrTypes}},
	}
	yumAttrTypes    = map[string]attr.Type{"repodata_depth": types.Int64Type}
	hostedAttrTypes = map[string]attr.Type{
		"allow_redeploy": types.BoolType,
		"apt":            types.ObjectType{AttrTypes: aptAttrTypes},
		"yum":            types.ObjectType{AttrTypes: yumAttrTypes},
	}
	groupAttrTypes = map[string]attr.Type{"members": types.ListType{ElemType: types.StringType}, "prefer_hosted": types.BoolType}
	scanAttrTypes  = map[string]attr.Type{
		"scanners": types.ListType{ElemType: types.StringType}, "mode": types.StringType,
		"severity_threshold": types.StringType, "on_upload": types.BoolType, "on_proxy_fetch": types.BoolType,
		"block_until_scanned": types.BoolType, "distro": types.StringType,
	}
)

func (r *repositoryResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_repository"
}

func (r *repositoryResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	stringList := func(desc string, optionalComputed bool) schema.ListAttribute {
		a := schema.ListAttribute{ElementType: types.StringType, Optional: true, MarkdownDescription: desc}
		a.Computed = optionalComputed
		return a
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A repository: `hosted` (you publish to it), `proxy` (pull-through cache of an upstream) " +
			"or `group` (an ordered union of repositories of the same format).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, MarkdownDescription: "Same as `name`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name": schema.StringAttribute{Required: true, Validators: nameValidators(), PlanModifiers: replace,
				MarkdownDescription: "Repository name (part of client URLs). Changing it forces a new repository."},
			"format": schema.StringAttribute{Required: true, PlanModifiers: replace,
				Validators:          []validator.String{stringvalidator.OneOf(repositoryFormats...)},
				MarkdownDescription: "One of `raw`, `pypi`, `npm`, `docker`, `apt`, `yum`, `helm`. Changing it forces a new repository."},
			"kind": schema.StringAttribute{Required: true, PlanModifiers: replace,
				Validators:          []validator.String{stringvalidator.OneOf("hosted", "proxy", "group")},
				MarkdownDescription: "`hosted`, `proxy` or `group`. Changing it forces a new repository."},
			"blob_store": schema.StringAttribute{Required: true, PlanModifiers: replace,
				MarkdownDescription: "Blob store for the repository's content. Changing it forces a new repository (content is not migrated)."},
			"description": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("")},
			"online": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				MarkdownDescription: "Offline proxies serve only cached content."},
			"anonymous_read": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				MarkdownDescription: "Allow unauthenticated reads."},
			"url": schema.StringAttribute{Computed: true, MarkdownDescription: "URL to configure package clients with.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"proxy": schema.SingleNestedAttribute{
				Optional: true, MarkdownDescription: "Required for `kind = \"proxy\"`.",
				Attributes: map[string]schema.Attribute{
					"remote_url": schema.StringAttribute{Required: true, MarkdownDescription: "Upstream URL, e.g. `https://registry.npmjs.org`."},
					"username":   schema.StringAttribute{Optional: true},
					"password": schema.StringAttribute{Optional: true, Sensitive: true,
						MarkdownDescription: "Write-only: never returned by the API, so changes made outside Terraform are not detected."},
					"metadata_ttl_seconds":       schema.Int64Attribute{Optional: true, MarkdownDescription: "Cache time for indexes/metadata (server default 3600)."},
					"content_ttl_seconds":        schema.Int64Attribute{Optional: true, MarkdownDescription: "Cache time for artifacts (default: never expire)."},
					"negative_cache_ttl_seconds": schema.Int64Attribute{Optional: true, MarkdownDescription: "Cache time for upstream 404s (server default 300, -1 disables)."},
					"routing_rule":               schema.StringAttribute{Optional: true, MarkdownDescription: "Name of an `entrepot_routing_rule` restricting what is fetched upstream."},
				},
			},
			"hosted": schema.SingleNestedAttribute{
				Optional: true, Computed: true, MarkdownDescription: "Settings for `kind = \"hosted\"`.",
				PlanModifiers: []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
				Attributes: map[string]schema.Attribute{
					"allow_redeploy": schema.BoolAttribute{Optional: true, Computed: true,
						MarkdownDescription: "Allow overwriting existing versions (for Docker: moving tags). Server default: false, true for Docker."},
					"apt": schema.SingleNestedAttribute{Optional: true, MarkdownDescription: "Apt distributions to publish.",
						Attributes: map[string]schema.Attribute{
							"distributions": schema.ListNestedAttribute{Optional: true,
								NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
									"name":          schema.StringAttribute{Required: true, MarkdownDescription: "e.g. `stable`, `bookworm`."},
									"components":    stringList("Components (default `main`).", true),
									"architectures": stringList("Architectures, e.g. `amd64`, `arm64`, `all`.", true),
									"origin":        schema.StringAttribute{Optional: true},
									"label":         schema.StringAttribute{Optional: true},
								}},
							},
						},
					},
					"yum": schema.SingleNestedAttribute{Optional: true, MarkdownDescription: "Yum repodata settings.",
						Attributes: map[string]schema.Attribute{
							"repodata_depth": schema.Int64Attribute{Optional: true, Computed: true,
								Validators:          []validator.Int64{int64validator.Between(0, 5)},
								MarkdownDescription: "Directory depth at which repodata is generated (0 = root)."},
						},
					},
				},
			},
			"group": schema.SingleNestedAttribute{
				Optional: true, MarkdownDescription: "Required for `kind = \"group\"`.",
				Attributes: map[string]schema.Attribute{
					"members": schema.ListAttribute{ElementType: types.StringType, Required: true,
						Validators:          []validator.List{listvalidator.SizeAtLeast(1)},
						MarkdownDescription: "Member repositories, searched in order."},
					"prefer_hosted": schema.BoolAttribute{Optional: true, Computed: true,
						MarkdownDescription: "npm/PyPI/Helm: never serve a package name from proxy members when a hosted member has it (dependency-confusion protection). Server default: true."},
				},
			},
			"scan": schema.SingleNestedAttribute{
				Optional: true, MarkdownDescription: "Vulnerability scanning policy.",
				Attributes: map[string]schema.Attribute{
					"scanners": schema.ListAttribute{ElementType: types.StringType, Optional: true, Computed: true,
						MarkdownDescription: "Names of `entrepot_scanner`s to run."},
					"mode": schema.StringAttribute{Optional: true, Computed: true,
						Validators:          []validator.String{stringvalidator.OneOf("off", "warn", "quarantine")},
						MarkdownDescription: "`off`, `warn` (record findings) or `quarantine` (block downloads). Server default: warn."},
					"severity_threshold": schema.StringAttribute{Optional: true, Computed: true,
						Validators:          []validator.String{stringvalidator.OneOf("critical", "high", "medium", "low")},
						MarkdownDescription: "Lowest severity that quarantines. Server default: high."},
					"on_upload":           schema.BoolAttribute{Optional: true, Computed: true},
					"on_proxy_fetch":      schema.BoolAttribute{Optional: true, Computed: true},
					"block_until_scanned": schema.BoolAttribute{Optional: true, Computed: true},
					"distro": schema.StringAttribute{Optional: true,
						MarkdownDescription: "Apt/Yum: distribution for vulnerability matching, e.g. `debian-12`, `rocky-9`."},
				},
			},
			"cleanup_policies": schema.ListAttribute{ElementType: types.StringType, Optional: true,
				Validators:          []validator.List{listvalidator.SizeAtLeast(1)},
				MarkdownDescription: "Names of `entrepot_cleanup_policy`s applied to the repository."},
		},
	}
}

func (r *repositoryResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m repositoryModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || m.Kind.IsUnknown() {
		return
	}
	kind := m.Kind.ValueString()
	if kind == "proxy" && m.Proxy.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("proxy"), "Missing proxy settings", `kind = "proxy" requires proxy`)
	}
	if kind == "group" && m.Group.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("group"), "Missing group settings", `kind = "group" requires group`)
	}
	if kind != "proxy" && !m.Proxy.IsNull() && !m.Proxy.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("proxy"), "Unexpected proxy settings", "proxy is only valid for kind = \"proxy\"")
	}
	if kind != "group" && !m.Group.IsNull() && !m.Group.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("group"), "Unexpected group settings", "group is only valid for kind = \"group\"")
	}
	if kind != "hosted" && !m.Hosted.IsNull() && !m.Hosted.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("hosted"), "Unexpected hosted settings", "hosted is only valid for kind = \"hosted\"")
	}
}

func (r *repositoryResource) input(ctx context.Context, m *repositoryModel, diags *diag.Diagnostics) client.RepositoryInput {
	in := client.RepositoryInput{
		Name: m.Name.ValueString(), Format: client.RepositoryInputFormat(m.Format.ValueString()),
		Kind: client.RepositoryKind(m.Kind.ValueString()), BlobStore: m.BlobStore.ValueString(),
		Description: strPtr(m.Description), Online: boolPtr(m.Online), AnonymousRead: boolPtr(m.AnonymousRead),
	}
	var p proxyModel
	if objectAs(ctx, m.Proxy, &p, diags) {
		in.Proxy = &client.ProxySettings{
			RemoteUrl: p.RemoteURL.ValueString(), Username: strPtr(p.Username), Password: strPtr(p.Password),
			MetadataTtlSeconds: intPtr(p.MetadataTTLSeconds), ContentTtlSeconds: intPtr(p.ContentTTLSeconds),
			NegativeCacheTtlSeconds: intPtr(p.NegativeCacheTTLSeconds), RoutingRule: strPtr(p.RoutingRule),
		}
	}
	var h hostedModel
	if objectAs(ctx, m.Hosted, &h, diags) {
		hs := &client.HostedSettings{AllowRedeploy: boolPtr(h.AllowRedeploy)}
		var a aptModel
		if objectAs(ctx, h.Apt, &a, diags) {
			var dists []aptDistributionModel
			if !a.Distributions.IsNull() && !a.Distributions.IsUnknown() {
				diags.Append(a.Distributions.ElementsAs(ctx, &dists, false)...)
			}
			out := []client.AptDistribution{}
			for _, d := range dists {
				ad := client.AptDistribution{Name: d.Name.ValueString(), Origin: strPtr(d.Origin), Label: strPtr(d.Label)}
				if c := stringList(ctx, d.Components, diags); c != nil {
					ad.Components = &c
				}
				if c := stringList(ctx, d.Architectures, diags); c != nil {
					ad.Architectures = &c
				}
				out = append(out, ad)
			}
			hs.Apt = &client.AptHostedSettings{Distributions: &out}
		}
		var y yumModel
		if objectAs(ctx, h.Yum, &y, diags) {
			hs.Yum = &client.YumHostedSettings{RepodataDepth: intPtr(y.RepodataDepth)}
		}
		in.Hosted = hs
	}
	var g groupModel
	if objectAs(ctx, m.Group, &g, diags) {
		in.Group = &client.GroupSettings{Members: stringList(ctx, g.Members, diags), PreferHosted: boolPtr(g.PreferHosted)}
	}
	var s scanModel
	if objectAs(ctx, m.Scan, &s, diags) {
		ss := &client.ScanSettings{
			OnUpload: boolPtr(s.OnUpload), OnProxyFetch: boolPtr(s.OnProxyFetch),
			BlockUntilScanned: boolPtr(s.BlockUntilScanned), Distro: strPtr(s.Distro),
		}
		if l := stringList(ctx, s.Scanners, diags); l != nil {
			ss.Scanners = &l
		}
		if v := strPtr(s.Mode); v != nil {
			ss.Mode = ptr(client.ScanSettingsMode(*v))
		}
		if v := strPtr(s.SeverityThreshold); v != nil {
			ss.SeverityThreshold = ptr(client.ScanSettingsSeverityThreshold(*v))
		}
		in.Scan = ss
	}
	if l := stringList(ctx, m.CleanupPolicies, diags); l != nil {
		in.Cleanup = &client.CleanupSettings{Policies: &l}
	}
	return in
}

func (r *repositoryResource) fromAPI(ctx context.Context, rp *client.Repository, m *repositoryModel, diags *diag.Diagnostics) {
	m.ID = types.StringValue(rp.Name)
	m.Name = types.StringValue(rp.Name)
	m.Format = types.StringValue(rp.Format)
	m.Kind = types.StringValue(string(rp.Kind))
	m.BlobStore = types.StringValue(rp.BlobStore)
	m.Description = types.StringValue(rp.Description)
	m.Online = types.BoolValue(rp.Online)
	m.AnonymousRead = types.BoolValue(rp.AnonymousRead)
	m.URL = types.StringValue(rp.Url)

	// Proxy: keep the write-only password from prior state/plan.
	var oldProxy proxyModel
	password := types.StringNull()
	if objectAs(ctx, m.Proxy, &oldProxy, diags) {
		password = oldProxy.Password
	}
	m.Proxy = types.ObjectNull(proxyAttrTypes)
	if p := rp.Proxy; p != nil {
		m.Proxy = objectFrom(ctx, proxyAttrTypes, proxyModel{
			RemoteURL: types.StringValue(p.RemoteUrl), Username: strValOrEmpty(p.Username), Password: password,
			MetadataTTLSeconds: intValNonZero(p.MetadataTtlSeconds), ContentTTLSeconds: intValNonZero(p.ContentTtlSeconds),
			NegativeCacheTTLSeconds: intValNonZero(p.NegativeCacheTtlSeconds), RoutingRule: strValOrEmpty(p.RoutingRule),
		}, diags)
	}

	m.Hosted = types.ObjectNull(hostedAttrTypes)
	if h := rp.Hosted; h != nil {
		hm := hostedModel{
			AllowRedeploy: types.BoolValue(deref(h.AllowRedeploy)),
			Apt:           types.ObjectNull(aptAttrTypes),
			Yum:           types.ObjectNull(yumAttrTypes),
		}
		if h.Apt != nil {
			var dists []aptDistributionModel
			for _, d := range deref(h.Apt.Distributions) {
				dists = append(dists, aptDistributionModel{
					Name: types.StringValue(d.Name), Components: listValue(deref(d.Components), false),
					Architectures: listValue(deref(d.Architectures), false),
					Origin:        strValOrEmpty(d.Origin), Label: strValOrEmpty(d.Label),
				})
			}
			distList, d := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: aptDistributionAttrTypes}, dists)
			diags.Append(d...)
			if len(dists) == 0 {
				distList = types.ListNull(types.ObjectType{AttrTypes: aptDistributionAttrTypes})
			}
			hm.Apt = objectFrom(ctx, aptAttrTypes, aptModel{Distributions: distList}, diags)
		}
		if h.Yum != nil {
			hm.Yum = objectFrom(ctx, yumAttrTypes, yumModel{RepodataDepth: intVal(h.Yum.RepodataDepth)}, diags)
		}
		m.Hosted = objectFrom(ctx, hostedAttrTypes, hm, diags)
	}

	m.Group = types.ObjectNull(groupAttrTypes)
	if g := rp.Group; g != nil {
		m.Group = objectFrom(ctx, groupAttrTypes, groupModel{
			Members: listValue(g.Members, false), PreferHosted: types.BoolValue(g.PreferHosted == nil || *g.PreferHosted),
		}, diags)
	}

	m.Scan = types.ObjectNull(scanAttrTypes)
	if s := rp.Scan; s != nil {
		sm := scanModel{
			Scanners:          listValue(deref(s.Scanners), false),
			Mode:              types.StringValue("warn"),
			SeverityThreshold: types.StringValue("high"),
			OnUpload:          types.BoolValue(s.OnUpload == nil || *s.OnUpload),
			OnProxyFetch:      types.BoolValue(s.OnProxyFetch == nil || *s.OnProxyFetch),
			BlockUntilScanned: types.BoolValue(deref(s.BlockUntilScanned)),
			Distro:            strValOrEmpty(s.Distro),
		}
		if s.Mode != nil {
			sm.Mode = types.StringValue(string(*s.Mode))
		}
		if s.SeverityThreshold != nil {
			sm.SeverityThreshold = types.StringValue(string(*s.SeverityThreshold))
		}
		m.Scan = objectFrom(ctx, scanAttrTypes, sm, diags)
	}

	m.CleanupPolicies = types.ListNull(types.StringType)
	if c := rp.Cleanup; c != nil && len(deref(c.Policies)) > 0 {
		m.CleanupPolicies = listValue(deref(c.Policies), true)
	}
}

func (r *repositoryResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan repositoryModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	in := r.input(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.api.CreateRepositoryWithResponse(ctx, in)
	if err = check(res, err, http.StatusCreated); err != nil {
		apiError(&resp.Diagnostics, "create repository", err)
		return
	}
	r.fromAPI(ctx, res.JSON201, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *repositoryResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state repositoryModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.api.GetRepositoryWithResponse(ctx, state.Name.ValueString())
	if err = check(res, err, http.StatusOK); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read repository", err)
		return
	}
	r.fromAPI(ctx, res.JSON200, &state, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *repositoryResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan repositoryModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	in := r.input(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.api.UpdateRepositoryWithResponse(ctx, plan.Name.ValueString(), in)
	if err = check(res, err, http.StatusOK); err != nil {
		apiError(&resp.Diagnostics, "update repository", err)
		return
	}
	r.fromAPI(ctx, res.JSON200, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *repositoryResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state repositoryModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	res, err := r.api.DeleteRepositoryWithResponse(ctx, state.Name.ValueString())
	if err = check(res, err, http.StatusNoContent, http.StatusNotFound); err != nil {
		apiError(&resp.Diagnostics, "delete repository", err)
	}
}

func (r *repositoryResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
