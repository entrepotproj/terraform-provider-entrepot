package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/objectvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/entrepotproj/terraform-provider-entrepot/internal/client"
)

func init() { registerResource(NewBlobStoreResource) }

var (
	_ resource.Resource                = (*blobStoreResource)(nil)
	_ resource.ResourceWithImportState = (*blobStoreResource)(nil)
)

func NewBlobStoreResource() resource.Resource { return &blobStoreResource{} }

type blobStoreResource struct{ resourceBase }

type blobStoreModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
	Type types.String `tfsdk:"type"`
	File types.Object `tfsdk:"file"`
	S3   types.Object `tfsdk:"s3"`
}

type fileStoreModel struct {
	Path types.String `tfsdk:"path"`
}

type s3StoreModel struct {
	Bucket            types.String `tfsdk:"bucket"`
	Region            types.String `tfsdk:"region"`
	Prefix            types.String `tfsdk:"prefix"`
	Endpoint          types.String `tfsdk:"endpoint"`
	ForcePathStyle    types.Bool   `tfsdk:"force_path_style"`
	AccessKeyID       types.String `tfsdk:"access_key_id"`
	SecretAccessKey   types.String `tfsdk:"secret_access_key"`
	RedirectDownloads types.Bool   `tfsdk:"redirect_downloads"`
}

var fileStoreAttrTypes = map[string]attr.Type{"path": types.StringType}

var s3StoreAttrTypes = map[string]attr.Type{
	"bucket": types.StringType, "region": types.StringType, "prefix": types.StringType,
	"endpoint": types.StringType, "force_path_style": types.BoolType, "access_key_id": types.StringType,
	"secret_access_key": types.StringType, "redirect_downloads": types.BoolType,
}

func (r *blobStoreResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_blob_store"
}

func (r *blobStoreResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A blob store holds repository content. `file` stores use a directory on a mounted " +
			"volume (PVC, EBS, EFS) under the server's `storage.allowedRoots`; `s3` stores use an S3-compatible bucket. " +
			"Repositories choose their blob store at creation.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, MarkdownDescription: "Same as `name`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required: true, MarkdownDescription: "Blob store name. Changing it forces a new store.",
				Validators:    nameValidators(),
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"type": schema.StringAttribute{
				Required: true, MarkdownDescription: "`file` or `s3`. Changing it forces a new store.",
				Validators:    []validator.String{stringvalidator.OneOf("file", "s3")},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"file": schema.SingleNestedAttribute{
				Optional: true, MarkdownDescription: "Settings for `type = \"file\"`.",
				Attributes: map[string]schema.Attribute{
					"path": schema.StringAttribute{Required: true, MarkdownDescription: "Directory, e.g. `/data/default`."},
				},
				Validators: []validator.Object{objectvalidator.ConflictsWith(path.MatchRoot("s3"))},
			},
			"s3": schema.SingleNestedAttribute{
				Optional: true, MarkdownDescription: "Settings for `type = \"s3\"`.",
				Attributes: map[string]schema.Attribute{
					"bucket":   schema.StringAttribute{Required: true},
					"region":   schema.StringAttribute{Optional: true},
					"prefix":   schema.StringAttribute{Optional: true, MarkdownDescription: "Key prefix inside the bucket."},
					"endpoint": schema.StringAttribute{Optional: true, MarkdownDescription: "Custom endpoint for MinIO, Ceph RGW, R2, …"},
					"force_path_style": schema.BoolAttribute{
						Optional: true, Computed: true, Default: booldefault.StaticBool(false),
						MarkdownDescription: "Use path-style addressing (MinIO).",
					},
					"access_key_id": schema.StringAttribute{
						Optional: true, MarkdownDescription: "Leave unset to use the AWS default credential chain (IRSA).",
					},
					"secret_access_key": schema.StringAttribute{
						Optional: true, Sensitive: true,
						MarkdownDescription: "Write-only: never returned by the API, so changes made outside Terraform are not detected.",
					},
					"redirect_downloads": schema.BoolAttribute{
						Optional: true, Computed: true, Default: booldefault.StaticBool(false),
						MarkdownDescription: "Redirect large downloads to presigned S3 URLs.",
					},
				},
				Validators: []validator.Object{objectvalidator.ConflictsWith(path.MatchRoot("file"))},
			},
		},
	}
}

func (r *blobStoreResource) input(ctx context.Context, m *blobStoreModel, diags *diag.Diagnostics) client.BlobStoreInput {
	in := client.BlobStoreInput{Name: m.Name.ValueString(), Type: client.BlobStoreType(m.Type.ValueString())}
	var f fileStoreModel
	if objectAs(ctx, m.File, &f, diags) {
		in.File = &client.FileBlobStoreConfig{Path: f.Path.ValueString()}
	}
	var s s3StoreModel
	if objectAs(ctx, m.S3, &s, diags) {
		in.S3 = &client.S3BlobStoreConfig{
			Bucket: s.Bucket.ValueString(), Region: strPtr(s.Region), Prefix: strPtr(s.Prefix), Endpoint: strPtr(s.Endpoint),
			ForcePathStyle: boolPtr(s.ForcePathStyle), AccessKeyId: strPtr(s.AccessKeyID),
			SecretAccessKey: strPtr(s.SecretAccessKey), RedirectDownloads: boolPtr(s.RedirectDownloads),
		}
	}
	switch {
	case in.Type == "file" && in.File == nil:
		diags.AddAttributeError(path.Root("file"), "Missing file settings", `type = "file" requires the file attribute`)
	case in.Type == "s3" && in.S3 == nil:
		diags.AddAttributeError(path.Root("s3"), "Missing s3 settings", `type = "s3" requires the s3 attribute`)
	}
	return in
}

// fromAPI updates m from the API object, keeping write-only secrets from prior.
func (r *blobStoreResource) fromAPI(ctx context.Context, b *client.BlobStore, prior *blobStoreModel, diags *diag.Diagnostics) {
	prior.ID = types.StringValue(b.Name)
	prior.Name = types.StringValue(b.Name)
	prior.Type = types.StringValue(string(b.Type))
	prior.File = types.ObjectNull(fileStoreAttrTypes)
	prior.S3 = types.ObjectNull(s3StoreAttrTypes)
	if b.File != nil {
		prior.File = objectFrom(ctx, fileStoreAttrTypes, fileStoreModel{Path: types.StringValue(b.File.Path)}, diags)
	}
	if b.S3 != nil {
		var old s3StoreModel
		secret := types.StringNull()
		if objectAs(ctx, prior.S3, &old, diags) {
			secret = old.SecretAccessKey
		}
		prior.S3 = objectFrom(ctx, s3StoreAttrTypes, s3StoreModel{
			Bucket: types.StringValue(b.S3.Bucket), Region: strValOrEmpty(b.S3.Region), Prefix: strValOrEmpty(b.S3.Prefix),
			Endpoint: strValOrEmpty(b.S3.Endpoint), ForcePathStyle: types.BoolValue(deref(b.S3.ForcePathStyle)),
			AccessKeyID: strValOrEmpty(b.S3.AccessKeyId), SecretAccessKey: secret,
			RedirectDownloads: types.BoolValue(deref(b.S3.RedirectDownloads)),
		}, diags)
	}
}

func (r *blobStoreResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan blobStoreModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	in := r.input(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.api.CreateBlobStoreWithResponse(ctx, in)
	if err = check(res, err, http.StatusCreated); err != nil {
		apiError(&resp.Diagnostics, "create blob store", err)
		return
	}
	// Keep the planned S3 secret (write-only).
	r.fromAPI(ctx, res.JSON201, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *blobStoreResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state blobStoreModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.api.GetBlobStoreWithResponse(ctx, state.Name.ValueString())
	if err = check(res, err, http.StatusOK); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read blob store", err)
		return
	}
	r.fromAPI(ctx, res.JSON200, &state, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *blobStoreResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan blobStoreModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	in := r.input(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.api.UpdateBlobStoreWithResponse(ctx, plan.Name.ValueString(), in)
	if err = check(res, err, http.StatusOK); err != nil {
		apiError(&resp.Diagnostics, "update blob store", err)
		return
	}
	r.fromAPI(ctx, res.JSON200, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *blobStoreResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state blobStoreModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	res, err := r.api.DeleteBlobStoreWithResponse(ctx, state.Name.ValueString())
	if err = check(res, err, http.StatusNoContent, http.StatusNotFound); err != nil {
		apiError(&resp.Diagnostics, "delete blob store", err)
	}
}

func (r *blobStoreResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
