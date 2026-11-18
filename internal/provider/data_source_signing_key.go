package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func init() { registerDataSource(NewSigningKeyDataSource) }

var _ datasource.DataSource = (*signingKeyDataSource)(nil)

func NewSigningKeyDataSource() datasource.DataSource { return &signingKeyDataSource{} }

type signingKeyDataSource struct{ dataSourceBase }

type signingKeyDataModel struct {
	ID          types.String `tfsdk:"id"`
	Repository  types.String `tfsdk:"repository"`
	Fingerprint types.String `tfsdk:"fingerprint"`
	Algorithm   types.String `tfsdk:"algorithm"`
	PublicKey   types.String `tfsdk:"public_key"`
	URL         types.String `tfsdk:"url"`
	CreatedAt   types.String `tfsdk:"created_at"`
}

func (d *signingKeyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_signing_key"
}

func (d *signingKeyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	str := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Computed: true, MarkdownDescription: desc}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "The public signing key of a hosted `apt` or `yum` repository, e.g. to install it on " +
			"hosts (`/etc/apt/keyrings/…`, `rpm --import`). The server generates a key the first time one is needed, " +
			"so reading this data source may create it.",
		Attributes: map[string]schema.Attribute{
			"id":          str("Same as `repository`."),
			"repository":  schema.StringAttribute{Required: true, Validators: nameValidators(), MarkdownDescription: "Name of a hosted `apt` or `yum` repository."},
			"fingerprint": str("Fingerprint (upper-case hex) of the active key."),
			"algorithm":   str("Key algorithm, e.g. `rsa4096` or `ed25519`."),
			"public_key":  str("ASCII-armored OpenPGP public key."),
			"url":         str("URL from which clients download the public key."),
			"created_at":  str("When the active key was imported or generated (RFC 3339)."),
		},
	}
}

func (d *signingKeyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m signingKeyDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := d.api.GetSigningKeyWithResponse(ctx, m.Repository.ValueString())
	if err = check(res, err, http.StatusOK); err != nil {
		apiError(&resp.Diagnostics, "read signing key of repository "+m.Repository.ValueString(), err)
		return
	}
	k := res.JSON200
	m.ID = m.Repository
	m.Fingerprint = types.StringValue(k.Fingerprint)
	m.Algorithm = types.StringValue(deref(k.Algorithm))
	m.PublicKey = types.StringValue(k.PublicKey)
	m.URL = types.StringValue(k.Url)
	m.CreatedAt = timeValue(k.CreatedAt)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
