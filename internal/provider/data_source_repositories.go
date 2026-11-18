package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/geoffo-dev/terraform-provider-entrepot/internal/client"
)

func init() { registerDataSource(NewRepositoriesDataSource) }

var _ datasource.DataSource = (*repositoriesDataSource)(nil)

func NewRepositoriesDataSource() datasource.DataSource { return &repositoriesDataSource{} }

type repositoriesDataSource struct{ dataSourceBase }

type repositoriesDataModel struct {
	Format       types.String `tfsdk:"format"`
	Names        types.List   `tfsdk:"names"`
	Repositories types.List   `tfsdk:"repositories"`
}

type repositorySummaryModel struct {
	Name          types.String `tfsdk:"name"`
	Format        types.String `tfsdk:"format"`
	Kind          types.String `tfsdk:"kind"`
	URL           types.String `tfsdk:"url"`
	BlobStore     types.String `tfsdk:"blob_store"`
	Online        types.Bool   `tfsdk:"online"`
	AnonymousRead types.Bool   `tfsdk:"anonymous_read"`
}

var repositorySummaryAttrTypes = map[string]attr.Type{
	"name": types.StringType, "format": types.StringType, "kind": types.StringType, "url": types.StringType,
	"blob_store": types.StringType, "online": types.BoolType, "anonymous_read": types.BoolType,
}

func (d *repositoriesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_repositories"
}

func (d *repositoriesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	str := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Computed: true, MarkdownDescription: desc}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the repositories the provider's credentials can read, optionally of one format.",
		Attributes: map[string]schema.Attribute{
			"format": schema.StringAttribute{Optional: true,
				Validators:          []validator.String{stringvalidator.OneOf(repositoryFormats...)},
				MarkdownDescription: "Only list repositories of this format (`raw`, `pypi`, `npm`, `docker`, `apt`, `yum`, `helm`)."},
			"names": schema.ListAttribute{ElementType: types.StringType, Computed: true,
				MarkdownDescription: "Names of the listed repositories, in the same order as `repositories`."},
			"repositories": schema.ListNestedAttribute{Computed: true, MarkdownDescription: "The repositories.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"name":           str("Repository name."),
					"format":         str("Repository format."),
					"kind":           str("`hosted`, `proxy` or `group`."),
					"url":            str("URL to configure package clients with."),
					"blob_store":     str("Blob store holding the repository's content."),
					"online":         schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the repository is online."},
					"anonymous_read": schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether unauthenticated reads are allowed."},
				}}},
		},
	}
}

func (d *repositoriesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m repositoriesDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := d.api.ListRepositoriesWithResponse(ctx, &client.ListRepositoriesParams{Format: strPtr(m.Format)})
	if err = check(res, err, http.StatusOK); err != nil {
		apiError(&resp.Diagnostics, "list repositories", err)
		return
	}
	names := make([]string, 0, len(*res.JSON200))
	items := make([]repositorySummaryModel, 0, len(*res.JSON200))
	for _, rp := range *res.JSON200 {
		names = append(names, rp.Name)
		items = append(items, repositorySummaryModel{
			Name: types.StringValue(rp.Name), Format: types.StringValue(rp.Format), Kind: types.StringValue(string(rp.Kind)),
			URL: types.StringValue(rp.Url), BlobStore: types.StringValue(rp.BlobStore),
			Online: types.BoolValue(rp.Online), AnonymousRead: types.BoolValue(rp.AnonymousRead),
		})
	}
	m.Names = listValue(names, false)
	m.Repositories = listOfObjects(ctx, repositorySummaryAttrTypes, items, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
