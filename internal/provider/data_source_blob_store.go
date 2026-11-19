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

func init() { registerDataSource(NewBlobStoreDataSource) }

var _ datasource.DataSource = (*blobStoreDataSource)(nil)

func NewBlobStoreDataSource() datasource.DataSource { return &blobStoreDataSource{} }

type blobStoreDataSource struct{ dataSourceBase }

type blobStoreDataModel struct {
	ID    types.String `tfsdk:"id"`
	Name  types.String `tfsdk:"name"`
	Type  types.String `tfsdk:"type"`
	File  types.Object `tfsdk:"file"`
	S3    types.Object `tfsdk:"s3"`
	Usage types.Object `tfsdk:"usage"`
}

type s3StoreDataModel struct {
	Bucket            types.String `tfsdk:"bucket"`
	Region            types.String `tfsdk:"region"`
	Prefix            types.String `tfsdk:"prefix"`
	Endpoint          types.String `tfsdk:"endpoint"`
	ForcePathStyle    types.Bool   `tfsdk:"force_path_style"`
	AccessKeyID       types.String `tfsdk:"access_key_id"`
	RedirectDownloads types.Bool   `tfsdk:"redirect_downloads"`
}

type blobStoreUsageModel struct {
	BlobCount    types.Int64 `tfsdk:"blob_count"`
	TotalBytes   types.Int64 `tfsdk:"total_bytes"`
	Repositories types.Int64 `tfsdk:"repositories"`
}

var (
	s3StoreDataAttrTypes = map[string]attr.Type{
		"bucket": types.StringType, "region": types.StringType, "prefix": types.StringType,
		"endpoint": types.StringType, "force_path_style": types.BoolType, "access_key_id": types.StringType,
		"redirect_downloads": types.BoolType,
	}
	blobStoreUsageAttrTypes = map[string]attr.Type{
		"blob_count": types.Int64Type, "total_bytes": types.Int64Type, "repositories": types.Int64Type,
	}
)

func (d *blobStoreDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_blob_store"
}

func (d *blobStoreDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	str := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Computed: true, MarkdownDescription: desc}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a blob store by name, including its current usage. The S3 secret access key is never returned.",
		Attributes: map[string]schema.Attribute{
			"id":   str("Same as `name`."),
			"name": schema.StringAttribute{Required: true, MarkdownDescription: "Blob store name."},
			"type": str("`file` or `s3`."),
			"file": schema.SingleNestedAttribute{Computed: true, MarkdownDescription: "Settings of a `file` store (null otherwise).",
				Attributes: map[string]schema.Attribute{"path": str("Directory holding the blobs.")}},
			"s3": schema.SingleNestedAttribute{Computed: true, MarkdownDescription: "Settings of an `s3` store (null otherwise).",
				Attributes: map[string]schema.Attribute{
					"bucket":             str("Bucket name."),
					"region":             str("Bucket region."),
					"prefix":             str("Key prefix inside the bucket."),
					"endpoint":           str("Custom S3 endpoint."),
					"force_path_style":   schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether path-style addressing is used."},
					"access_key_id":      str("Access key ID (null when the AWS default credential chain is used)."),
					"redirect_downloads": schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether large downloads are redirected to presigned URLs."},
				}},
			"usage": schema.SingleNestedAttribute{Computed: true, MarkdownDescription: "Current usage.",
				Attributes: map[string]schema.Attribute{
					"blob_count":   schema.Int64Attribute{Computed: true, MarkdownDescription: "Number of blobs stored."},
					"total_bytes":  schema.Int64Attribute{Computed: true, MarkdownDescription: "Total size of the blobs in bytes."},
					"repositories": schema.Int64Attribute{Computed: true, MarkdownDescription: "Number of repositories using the store."},
				}},
		},
	}
}

func (d *blobStoreDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m blobStoreDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := d.api.GetBlobStoreWithResponse(ctx, m.Name.ValueString())
	if err = check(res, err, http.StatusOK); err != nil {
		if client.IsNotFound(err) {
			resp.Diagnostics.AddError("Blob store not found", "No blob store named "+m.Name.ValueString()+" exists.")
			return
		}
		apiError(&resp.Diagnostics, "read blob store", err)
		return
	}
	b := res.JSON200
	diags := &resp.Diagnostics
	m.ID = types.StringValue(b.Name)
	m.Name = types.StringValue(b.Name)
	m.Type = types.StringValue(string(b.Type))
	m.File = types.ObjectNull(fileStoreAttrTypes)
	if b.File != nil {
		m.File = objectFrom(ctx, fileStoreAttrTypes, fileStoreModel{Path: types.StringValue(b.File.Path)}, diags)
	}
	m.S3 = types.ObjectNull(s3StoreDataAttrTypes)
	if s := b.S3; s != nil {
		m.S3 = objectFrom(ctx, s3StoreDataAttrTypes, s3StoreDataModel{
			Bucket: types.StringValue(s.Bucket), Region: strValOrEmpty(s.Region), Prefix: strValOrEmpty(s.Prefix),
			Endpoint: strValOrEmpty(s.Endpoint), ForcePathStyle: types.BoolValue(deref(s.ForcePathStyle)),
			AccessKeyID: strValOrEmpty(s.AccessKeyId), RedirectDownloads: types.BoolValue(deref(s.RedirectDownloads)),
		}, diags)
	}
	m.Usage = objectFrom(ctx, blobStoreUsageAttrTypes, blobStoreUsageModel{
		BlobCount: types.Int64Value(b.Usage.BlobCount), TotalBytes: types.Int64Value(b.Usage.TotalBytes),
		Repositories: types.Int64Value(b.Usage.Repositories),
	}, diags)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
