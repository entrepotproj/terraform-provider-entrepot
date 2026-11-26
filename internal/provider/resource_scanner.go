package provider

import (
	"context"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/entrepotproj/terraform-provider-entrepot/internal/client"
)

func init() { registerResource(NewScannerResource) }

var (
	_ resource.Resource                   = (*scannerResource)(nil)
	_ resource.ResourceWithImportState    = (*scannerResource)(nil)
	_ resource.ResourceWithValidateConfig = (*scannerResource)(nil)
)

func NewScannerResource() resource.Resource { return &scannerResource{} }

type scannerResource struct{ resourceBase }

type scannerModel struct {
	ID                  types.String `tfsdk:"id"`
	Name                types.String `tfsdk:"name"`
	Type                types.String `tfsdk:"type"`
	Enabled             types.Bool   `tfsdk:"enabled"`
	RescanIntervalHours types.Int64  `tfsdk:"rescan_interval_hours"`
	Trivy               types.Object `tfsdk:"trivy"`
	Grype               types.Object `tfsdk:"grype"`
	Webhook             types.Object `tfsdk:"webhook"`
	MetaDefender        types.Object `tfsdk:"metadefender"`
}

type trivyModel struct {
	Binary         types.String `tfsdk:"binary"`
	ServerURL      types.String `tfsdk:"server_url"`
	ServerToken    types.String `tfsdk:"server_token"`
	TimeoutSeconds types.Int64  `tfsdk:"timeout_seconds"`
	ExtraArgs      types.List   `tfsdk:"extra_args"`
}

type grypeModel struct {
	Binary         types.String `tfsdk:"binary"`
	TimeoutSeconds types.Int64  `tfsdk:"timeout_seconds"`
	ExtraArgs      types.List   `tfsdk:"extra_args"`
}

type webhookScannerModel struct {
	URL            types.String `tfsdk:"url"`
	Secret         types.String `tfsdk:"secret"`
	TimeoutSeconds types.Int64  `tfsdk:"timeout_seconds"`
}

type metadefenderModel struct {
	URL               types.String `tfsdk:"url"`
	APIKey            types.String `tfsdk:"api_key"`
	Rule              types.String `tfsdk:"rule"`
	TimeoutSeconds    types.Int64  `tfsdk:"timeout_seconds"`
	Concurrency       types.Int64  `tfsdk:"concurrency"`
	HashLookup        types.Bool   `tfsdk:"hash_lookup"`
	MaxResultAgeHours types.Int64  `tfsdk:"max_result_age_hours"`
	WebURL            types.String `tfsdk:"web_url"`
	CACertificate     types.String `tfsdk:"ca_certificate"`
}

var (
	trivyAttrTypes = map[string]attr.Type{
		"binary": types.StringType, "server_url": types.StringType, "server_token": types.StringType,
		"timeout_seconds": types.Int64Type, "extra_args": types.ListType{ElemType: types.StringType},
	}
	grypeAttrTypes = map[string]attr.Type{
		"binary": types.StringType, "timeout_seconds": types.Int64Type, "extra_args": types.ListType{ElemType: types.StringType},
	}
	webhookScannerAttrTypes = map[string]attr.Type{
		"url": types.StringType, "secret": types.StringType, "timeout_seconds": types.Int64Type,
	}
	metadefenderAttrTypes = map[string]attr.Type{
		"url": types.StringType, "api_key": types.StringType, "rule": types.StringType,
		"timeout_seconds": types.Int64Type, "concurrency": types.Int64Type, "hash_lookup": types.BoolType,
		"max_result_age_hours": types.Int64Type, "web_url": types.StringType, "ca_certificate": types.StringType,
	}
)

func (r *scannerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_scanner"
}

func (r *scannerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	writeOnly := "Write-only: never returned by the API, so changes made outside Terraform are not detected. " +
		"Removing it from the configuration keeps the value stored on the server."
	timeout := func(desc string) schema.Int64Attribute {
		return schema.Int64Attribute{Optional: true, MarkdownDescription: desc,
			Validators: []validator.Int64{int64validator.Between(1, 86400)}}
	}
	binary := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Optional: true, MarkdownDescription: desc,
			Validators: []validator.String{stringvalidator.LengthAtLeast(1)}}
	}
	extraArgs := schema.ListAttribute{ElementType: types.StringType, Optional: true,
		Validators:          []validator.List{listvalidator.SizeAtLeast(1)},
		MarkdownDescription: "Extra command-line arguments passed to the scanner."}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A vulnerability scanner that repositories can use through their `scan.scanners` setting. " +
			"`trivy` and `grype` run the CLI on the Entrepot server (Trivy optionally in client/server mode); " +
			"`webhook` delegates scans to an external service that calls back with results; " +
			"`metadefender` sends the files of each package (and the layers of Docker images) to OPSWAT MetaDefender Core " +
			"for malware analysis (Entrepot 1.2.0 or later).\n\n" +
			"A scanner cannot be deleted while a repository uses it: remove it from the repositories' `scan.scanners` " +
			"first (Terraform does this in the right order when the repository references the scanner resource).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, MarkdownDescription: "Same as `name`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name": schema.StringAttribute{Required: true, Validators: nameValidators(),
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				MarkdownDescription: "Scanner name. Changing it forces a new scanner."},
			"type": schema.StringAttribute{Required: true,
				Validators:          []validator.String{stringvalidator.OneOf("trivy", "grype", "webhook", "metadefender")},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				MarkdownDescription: "`trivy`, `grype`, `webhook` or `metadefender`. Changing it forces a new scanner."},
			"enabled": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				MarkdownDescription: "Disabled scanners are skipped. Default `true`."},
			"rescan_interval_hours": schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(0),
				Validators:          []validator.Int64{int64validator.Between(0, 8760)},
				MarkdownDescription: "Re-scan components periodically, every this many hours (`0`, the default, never re-scans)."},
			"trivy": schema.SingleNestedAttribute{Optional: true,
				MarkdownDescription: "Settings for `type = \"trivy\"`. May be omitted to use the defaults.",
				Attributes: map[string]schema.Attribute{
					"binary": binary("Path to the `trivy` CLI (server default `trivy`)."),
					"server_url": schema.StringAttribute{Optional: true,
						MarkdownDescription: "Trivy server for client/server mode, e.g. `http://trivy:4954` (recommended: keeps one vulnerability DB)."},
					"server_token": schema.StringAttribute{Optional: true, Sensitive: true,
						MarkdownDescription: "Token for the Trivy server. " + writeOnly + " The stored token is dropped when `server_url` changes."},
					"timeout_seconds": timeout("Scan timeout in seconds."),
					"extra_args":      extraArgs,
				},
			},
			"grype": schema.SingleNestedAttribute{Optional: true,
				MarkdownDescription: "Settings for `type = \"grype\"`. May be omitted to use the defaults.",
				Attributes: map[string]schema.Attribute{
					"binary":          binary("Path to the `grype` CLI (server default `grype`)."),
					"timeout_seconds": timeout("Scan timeout in seconds."),
					"extra_args":      extraArgs,
				},
			},
			"webhook": schema.SingleNestedAttribute{Optional: true,
				MarkdownDescription: "Settings for `type = \"webhook\"` (required for that type).",
				Attributes: map[string]schema.Attribute{
					"url": schema.StringAttribute{Required: true,
						Validators:          []validator.String{stringvalidator.RegexMatches(httpURLRE, "must be an http(s) URL")},
						MarkdownDescription: "Endpoint that receives scan requests (http or https)."},
					"secret": schema.StringAttribute{Optional: true, Sensitive: true,
						MarkdownDescription: "HMAC-SHA256 key for the `X-Entrepot-Signature` header. " + writeOnly},
					"timeout_seconds": timeout("Time allowed for the callback in seconds (server default 900)."),
				},
			},
			"metadefender": schema.SingleNestedAttribute{Optional: true,
				MarkdownDescription: "Settings for `type = \"metadefender\"` (required for that type): an OPSWAT MetaDefender Core " +
					"server (REST API v5). Requires Entrepot 1.2.0 or later.",
				Attributes: map[string]schema.Attribute{
					"url": schema.StringAttribute{Required: true,
						Validators:          []validator.String{stringvalidator.RegexMatches(httpURLRE, "must be an http(s) URL")},
						MarkdownDescription: "MetaDefender Core REST API, e.g. `https://metadefender.example.com:8008`."},
					"api_key": schema.StringAttribute{Optional: true, Sensitive: true,
						MarkdownDescription: "API key, sent in the `apikey` header. " + writeOnly + " The stored key is dropped when `url` changes."},
					"rule": schema.StringAttribute{Optional: true,
						MarkdownDescription: "Workflow rule to process files with (server default rule when unset)."},
					"timeout_seconds": timeout("Time allowed to analyse all files of a component, in seconds (server default 600)."),
					"concurrency": schema.Int64Attribute{Optional: true,
						Validators:          []validator.Int64{int64validator.Between(1, 32)},
						MarkdownDescription: "Files of a component analysed at once (server default 4, at most 32)."},
					"hash_lookup": schema.BoolAttribute{Optional: true,
						MarkdownDescription: "Reuse earlier analyses of the same content by the same rule, matched by sha256 " +
							"(server default `true`)."},
					"max_result_age_hours": schema.Int64Attribute{Optional: true,
						Validators:          []validator.Int64{int64validator.Between(1, 8760)},
						MarkdownDescription: "Earlier analyses older than this are not reused (server default 24)."},
					"web_url": schema.StringAttribute{Optional: true,
						Validators: []validator.String{stringvalidator.RegexMatches(httpURLRE, "must be an http(s) URL")},
						MarkdownDescription: "Link from findings to the result in the MetaDefender console; `{dataId}` is replaced, " +
							"e.g. `https://metadefender.example.com:8008/#/public/process/dataId/{dataId}`."},
					"ca_certificate": schema.StringAttribute{Optional: true,
						MarkdownDescription: "PEM certificates to trust for `url`, when it uses an internal CA."},
				},
			},
		},
	}
}

func (r *scannerResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m scannerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || m.Type.IsUnknown() || m.Type.IsNull() {
		return
	}
	typ := m.Type.ValueString()
	for name, o := range map[string]types.Object{"trivy": m.Trivy, "grype": m.Grype, "webhook": m.Webhook, "metadefender": m.MetaDefender} {
		if name != typ && !o.IsNull() {
			resp.Diagnostics.AddAttributeError(path.Root(name), "Settings do not match the scanner type",
				name+" settings are only valid for type = \""+name+"\" (this scanner has type = \""+typ+"\")")
		}
	}
	if typ == "webhook" && m.Webhook.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("webhook"), "Missing webhook settings", `type = "webhook" requires webhook`)
	}
	if typ == "metadefender" && m.MetaDefender.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("metadefender"), "Missing MetaDefender settings",
			`type = "metadefender" requires metadefender`)
	}
}

func (r *scannerResource) input(ctx context.Context, m *scannerModel, diags *diag.Diagnostics) client.ScannerInput {
	in := client.ScannerInput{
		Name: m.Name.ValueString(), Type: client.ScannerType(m.Type.ValueString()),
		Enabled: boolPtr(m.Enabled), RescanIntervalHours: intPtr(m.RescanIntervalHours),
	}
	optList := func(l types.List) *[]string {
		if v := stringList(ctx, l, diags); v != nil {
			return &v
		}
		return nil
	}
	var t trivyModel
	if objectAs(ctx, m.Trivy, &t, diags) {
		in.Trivy = &client.TrivySettings{Binary: strPtr(t.Binary), ServerUrl: strPtr(t.ServerURL), ServerToken: strPtr(t.ServerToken),
			TimeoutSeconds: intPtr(t.TimeoutSeconds), ExtraArgs: optList(t.ExtraArgs)}
	}
	var g grypeModel
	if objectAs(ctx, m.Grype, &g, diags) {
		in.Grype = &client.GrypeSettings{Binary: strPtr(g.Binary), TimeoutSeconds: intPtr(g.TimeoutSeconds), ExtraArgs: optList(g.ExtraArgs)}
	}
	var w webhookScannerModel
	if objectAs(ctx, m.Webhook, &w, diags) {
		in.Webhook = &client.WebhookScannerSettings{Url: w.URL.ValueString(), Secret: strPtr(w.Secret), TimeoutSeconds: intPtr(w.TimeoutSeconds)}
	}
	var md metadefenderModel
	if objectAs(ctx, m.MetaDefender, &md, diags) {
		in.Metadefender = &client.MetaDefenderSettings{Url: md.URL.ValueString(), ApiKey: strPtr(md.APIKey), Rule: strPtr(md.Rule),
			TimeoutSeconds: intPtr(md.TimeoutSeconds), Concurrency: intPtr(md.Concurrency), HashLookup: boolPtr(md.HashLookup),
			MaxResultAgeHours: intPtr(md.MaxResultAgeHours), WebUrl: strPtr(md.WebURL), CaCertificate: strPtr(md.CACertificate)}
	}
	return in
}

// fromAPI updates m from the API object, keeping write-only secrets (and
// equivalent spellings of normalised values) from m.
func (r *scannerResource) fromAPI(ctx context.Context, sc *client.Scanner, m *scannerModel, diags *diag.Diagnostics) {
	m.ID = types.StringValue(sc.Name)
	m.Name = types.StringValue(sc.Name)
	m.Type = types.StringValue(string(sc.Type))
	m.Enabled = types.BoolValue(sc.Enabled)
	m.RescanIntervalHours = types.Int64Value(int64(sc.RescanIntervalHours))

	// The server always returns an (empty) settings object for trivy and
	// grype scanners; keep the attribute null unless it was configured or
	// has values.
	var oldT trivyModel
	hadT := objectAs(ctx, m.Trivy, &oldT, diags)
	m.Trivy = types.ObjectNull(trivyAttrTypes)
	if t := sc.Trivy; t != nil {
		nt := trivyModel{Binary: strValOrEmpty(t.Binary), ServerURL: strValOrEmpty(t.ServerUrl), ServerToken: types.StringNull(),
			TimeoutSeconds: intValNonZero(t.TimeoutSeconds), ExtraArgs: stringListOrNull(t.ExtraArgs)}
		if hadT {
			nt.ServerToken = oldT.ServerToken
			// The server trims trailing slashes from serverUrl.
			if !oldT.ServerURL.IsNull() && strings.TrimRight(oldT.ServerURL.ValueString(), "/") == nt.ServerURL.ValueString() {
				nt.ServerURL = oldT.ServerURL
			}
		}
		if hadT || !nt.Binary.IsNull() || !nt.ServerURL.IsNull() || !nt.TimeoutSeconds.IsNull() || !nt.ExtraArgs.IsNull() {
			m.Trivy = objectFrom(ctx, trivyAttrTypes, nt, diags)
		}
	}

	var oldG grypeModel
	hadG := objectAs(ctx, m.Grype, &oldG, diags)
	m.Grype = types.ObjectNull(grypeAttrTypes)
	if g := sc.Grype; g != nil {
		ng := grypeModel{Binary: strValOrEmpty(g.Binary), TimeoutSeconds: intValNonZero(g.TimeoutSeconds), ExtraArgs: stringListOrNull(g.ExtraArgs)}
		if hadG || !ng.Binary.IsNull() || !ng.TimeoutSeconds.IsNull() || !ng.ExtraArgs.IsNull() {
			m.Grype = objectFrom(ctx, grypeAttrTypes, ng, diags)
		}
	}

	var oldW webhookScannerModel
	secret := types.StringNull()
	if objectAs(ctx, m.Webhook, &oldW, diags) {
		secret = oldW.Secret
	}
	m.Webhook = types.ObjectNull(webhookScannerAttrTypes)
	if w := sc.Webhook; w != nil {
		m.Webhook = objectFrom(ctx, webhookScannerAttrTypes, webhookScannerModel{
			URL: types.StringValue(w.Url), Secret: secret, TimeoutSeconds: intValNonZero(w.TimeoutSeconds),
		}, diags)
	}

	var oldM metadefenderModel
	hadM := objectAs(ctx, m.MetaDefender, &oldM, diags)
	m.MetaDefender = types.ObjectNull(metadefenderAttrTypes)
	if md := sc.Metadefender; md != nil {
		nm := metadefenderModel{URL: types.StringValue(md.Url), APIKey: types.StringNull(), Rule: strValOrEmpty(md.Rule),
			TimeoutSeconds: intValNonZero(md.TimeoutSeconds), Concurrency: intValNonZero(md.Concurrency),
			HashLookup: boolVal(md.HashLookup), MaxResultAgeHours: intValNonZero(md.MaxResultAgeHours),
			WebURL: strValOrEmpty(md.WebUrl), CACertificate: strValOrEmpty(md.CaCertificate)}
		if hadM {
			nm.APIKey = oldM.APIKey
			// The server trims whitespace (and trailing slashes from url).
			trimURL := func(s string) string { return strings.TrimRight(strings.TrimSpace(s), "/") }
			nm.URL = sameSpelling(oldM.URL, nm.URL, trimURL)
			nm.Rule = sameSpelling(oldM.Rule, nm.Rule, strings.TrimSpace)
			nm.WebURL = sameSpelling(oldM.WebURL, nm.WebURL, strings.TrimSpace)
			nm.CACertificate = sameSpelling(oldM.CACertificate, nm.CACertificate, strings.TrimSpace)
		}
		m.MetaDefender = objectFrom(ctx, metadefenderAttrTypes, nm, diags)
	}
}

// sameSpelling keeps the configured value when the server's differs only by
// normalisation (norm), so normalised values don't show a perpetual diff.
func sameSpelling(configured, got types.String, norm func(string) string) types.String {
	if configured.IsNull() || configured.IsUnknown() {
		return got
	}
	want := norm(configured.ValueString())
	if (got.IsNull() && want == "") || (!got.IsNull() && got.ValueString() == want) {
		return configured
	}
	return got
}

func (r *scannerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan scannerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	in := r.input(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.api.CreateScannerWithResponse(ctx, in)
	if err = check(res, err, http.StatusCreated); err != nil {
		apiError(&resp.Diagnostics, "create scanner", err)
		return
	}
	r.fromAPI(ctx, res.JSON201, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *scannerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state scannerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.api.GetScannerWithResponse(ctx, state.Name.ValueString())
	if err = check(res, err, http.StatusOK); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read scanner", err)
		return
	}
	r.fromAPI(ctx, res.JSON200, &state, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *scannerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan scannerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	in := r.input(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.api.UpdateScannerWithResponse(ctx, plan.Name.ValueString(), in)
	if err = check(res, err, http.StatusOK); err != nil {
		apiError(&resp.Diagnostics, "update scanner", err)
		return
	}
	r.fromAPI(ctx, res.JSON200, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *scannerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state scannerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	res, err := r.api.DeleteScannerWithResponse(ctx, state.Name.ValueString())
	if err = check(res, err, http.StatusNoContent, http.StatusNotFound); err != nil {
		// 409 when repositories still use the scanner; the server's message names them.
		apiError(&resp.Diagnostics, "delete scanner", err)
	}
}

func (r *scannerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
