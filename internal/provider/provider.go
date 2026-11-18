// Package provider implements the Entrepot Terraform / OpenTofu provider.
package provider

import (
	"context"
	"os"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/geoffo-dev/terraform-provider-entrepot/internal/client"
)

var _ provider.Provider = (*entrepotProvider)(nil)

type entrepotProvider struct {
	version string
}

// New returns a provider factory.
func New(version string) func() provider.Provider {
	return func() provider.Provider { return &entrepotProvider{version: version} }
}

type providerModel struct {
	URL                types.String `tfsdk:"url"`
	Token              types.String `tfsdk:"token"`
	CACertPEM          types.String `tfsdk:"ca_cert_pem"`
	InsecureSkipVerify types.Bool   `tfsdk:"insecure_skip_verify"`
	TimeoutSeconds     types.Int64  `tfsdk:"timeout_seconds"`
}

func (p *entrepotProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "entrepot"
	resp.Version = p.version
}

func (p *entrepotProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Configure [Entrepot](https://github.com/geoffo-dev/package-repository) — repositories, " +
			"blob stores, access control, scanning, cleanup and routing — as code.",
		Attributes: map[string]schema.Attribute{
			"url": schema.StringAttribute{
				MarkdownDescription: "Base URL of the Entrepot server, e.g. `https://entrepot.example.com`. Can be set with `ENTREPOT_URL`.",
				Optional:            true,
			},
			"token": schema.StringAttribute{
				MarkdownDescription: "API token: a service-account token (`ent_…`, recommended), an OIDC access token or the bootstrap token. Can be set with `ENTREPOT_TOKEN`.",
				Optional:            true,
				Sensitive:           true,
			},
			"ca_cert_pem": schema.StringAttribute{
				MarkdownDescription: "PEM-encoded CA certificate(s) to trust in addition to the system roots.",
				Optional:            true,
			},
			"insecure_skip_verify": schema.BoolAttribute{
				MarkdownDescription: "Disable TLS certificate verification (testing only).",
				Optional:            true,
			},
			"timeout_seconds": schema.Int64Attribute{
				MarkdownDescription: "HTTP request timeout (default 60).",
				Optional:            true,
			},
		},
	}
}

func (p *entrepotProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if cfg.URL.IsUnknown() || cfg.Token.IsUnknown() {
		resp.Diagnostics.AddWarning("Unknown provider configuration",
			"url or token is not known yet; the provider cannot connect until they are.")
		return
	}
	url := os.Getenv("ENTREPOT_URL")
	if !cfg.URL.IsNull() {
		url = cfg.URL.ValueString()
	}
	token := os.Getenv("ENTREPOT_TOKEN")
	if !cfg.Token.IsNull() {
		token = cfg.Token.ValueString()
	}
	if url == "" {
		resp.Diagnostics.AddAttributeError(path.Root("url"), "Missing Entrepot URL",
			"Set the url attribute or the ENTREPOT_URL environment variable.")
	}
	if token == "" {
		resp.Diagnostics.AddAttributeError(path.Root("token"), "Missing Entrepot token",
			"Set the token attribute or the ENTREPOT_TOKEN environment variable.")
	}
	if resp.Diagnostics.HasError() {
		return
	}
	var timeout time.Duration
	if !cfg.TimeoutSeconds.IsNull() {
		timeout = time.Duration(cfg.TimeoutSeconds.ValueInt64()) * time.Second
	}
	c, err := client.New(client.Options{
		URL:                url,
		Token:              token,
		CACertPEM:          cfg.CACertPEM.ValueString(),
		InsecureSkipVerify: cfg.InsecureSkipVerify.ValueBool(),
		Timeout:            timeout,
		UserAgent:          "terraform-provider-entrepot/" + p.version,
	})
	if err != nil {
		resp.Diagnostics.AddError("Invalid provider configuration", err.Error())
		return
	}
	resp.ResourceData = c
	resp.DataSourceData = c
}

// Resources lists all managed resource types. Each resource lives in its
// own file (resource_<name>.go).
func (p *entrepotProvider) Resources(_ context.Context) []func() resource.Resource {
	return allResources
}

func (p *entrepotProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return allDataSources
}

// allResources / allDataSources are appended to by each file's init(), so
// resources can be developed in parallel without editing this file.
var (
	allResources   []func() resource.Resource
	allDataSources []func() datasource.DataSource
)

func registerResource(f func() resource.Resource)       { allResources = append(allResources, f) }
func registerDataSource(f func() datasource.DataSource) { allDataSources = append(allDataSources, f) }
