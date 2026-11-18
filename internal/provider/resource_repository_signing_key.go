package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/geoffo-dev/terraform-provider-entrepot/internal/client"
)

func init() { registerResource(NewRepositorySigningKeyResource) }

var (
	_ resource.Resource                   = (*repositorySigningKeyResource)(nil)
	_ resource.ResourceWithImportState    = (*repositorySigningKeyResource)(nil)
	_ resource.ResourceWithValidateConfig = (*repositorySigningKeyResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*repositorySigningKeyResource)(nil)
)

func NewRepositorySigningKeyResource() resource.Resource { return &repositorySigningKeyResource{} }

type repositorySigningKeyResource struct{ resourceBase }

type repositorySigningKeyModel struct {
	ID          types.String `tfsdk:"id"`
	Repository  types.String `tfsdk:"repository"`
	PrivateKey  types.String `tfsdk:"private_key"`
	Passphrase  types.String `tfsdk:"passphrase"`
	Fingerprint types.String `tfsdk:"fingerprint"`
	Algorithm   types.String `tfsdk:"algorithm"`
	PublicKey   types.String `tfsdk:"public_key"`
	URL         types.String `tfsdk:"url"`
	CreatedAt   types.String `tfsdk:"created_at"`
}

func (r *repositorySigningKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_repository_signing_key"
}

func (r *repositorySigningKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Imports your own OpenPGP private key as the signing key of a hosted `apt` or `yum` " +
			"repository (by default the server generates one on first use). The repository metadata is " +
			"re-signed with the imported key.\n\n" +
			"**Drift:** the private key is write-only. The provider computes the key's fingerprint locally and " +
			"compares it with the server's active key on every refresh; if the key was rotated or replaced " +
			"outside Terraform, the plan shows a `fingerprint` change and applying it imports the configured " +
			"key again.\n\n" +
			"**Destroy** rotates the key: the server replaces the imported key with a freshly generated one " +
			"(a repository always has a signing key), so clients must fetch the new public key.\n\n" +
			"The private key (and passphrase) are stored in the Terraform state; protect it accordingly.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, MarkdownDescription: "Same as `repository`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"repository": schema.StringAttribute{
				Required: true, Validators: nameValidators(),
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				MarkdownDescription: "Name of a hosted `apt` or `yum` repository. Changing it forces a new resource.",
			},
			"private_key": schema.StringAttribute{
				Required: true, Sensitive: true,
				MarkdownDescription: "ASCII-armored OpenPGP private key (`gpg --armor --export-secret-keys <id>`) " +
					"containing exactly one key with a usable signing (sub)key. Write-only: changes on the server are " +
					"detected through `fingerprint`. Null after import until the next apply records it.",
			},
			"passphrase": schema.StringAttribute{
				Optional: true, Sensitive: true,
				MarkdownDescription: "Passphrase of a protected private key. The server stores the key decrypted " +
					"(sealed with its secrets key), so changing only the passphrase makes no API call.",
			},
			"fingerprint": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Fingerprint (upper-case hex) of the repository's active key.",
			},
			"algorithm": schema.StringAttribute{
				Computed: true, MarkdownDescription: "Key algorithm, e.g. `rsa4096` or `ed25519`.",
			},
			"public_key": schema.StringAttribute{
				Computed: true, MarkdownDescription: "ASCII-armored public key to install on clients.",
			},
			"url": schema.StringAttribute{
				Computed: true, MarkdownDescription: "URL from which clients download the public key.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created_at": schema.StringAttribute{
				Computed: true, MarkdownDescription: "When the active key was imported or generated (RFC 3339).",
			},
		},
	}
}

func (r *repositorySigningKeyResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var key types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("private_key"), &key)...)
	if key.IsNull() || key.IsUnknown() {
		return
	}
	if _, err := openpgpKeyInfo(key.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("private_key"), "Invalid OpenPGP private key", err.Error())
	}
}

// ModifyPlan predicts the fingerprint from the configured key, so drift
// (a different active key on the server) shows up as a planned change and
// an unchanged key plans no change at all.
func (r *repositorySigningKeyResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return // destroy
	}
	var plan repositorySigningKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || plan.PrivateKey.IsUnknown() {
		return
	}
	fp, err := openpgpKeyInfo(plan.PrivateKey.ValueString())
	if err != nil {
		return // reported by ValidateConfig
	}
	plan.Fingerprint = types.StringValue(fp)
	if !req.State.Raw.IsNull() {
		var state repositorySigningKeyModel
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
		if resp.Diagnostics.HasError() {
			return
		}
		if state.Repository.Equal(plan.Repository) {
			if state.Fingerprint.Equal(plan.Fingerprint) {
				// The configured key is the active key: nothing changes on the server.
				plan.Algorithm, plan.PublicKey, plan.CreatedAt = state.Algorithm, state.PublicKey, state.CreatedAt
			} else {
				plan.Algorithm, plan.PublicKey, plan.CreatedAt = types.StringUnknown(), types.StringUnknown(), types.StringUnknown()
			}
		}
	}
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}

func (r *repositorySigningKeyResource) fromAPI(k *client.SigningKey, m *repositorySigningKeyModel) {
	m.ID = m.Repository
	m.Fingerprint = types.StringValue(k.Fingerprint)
	m.Algorithm = types.StringValue(deref(k.Algorithm))
	m.PublicKey = types.StringValue(k.PublicKey)
	m.URL = types.StringValue(k.Url)
	m.CreatedAt = timeValue(k.CreatedAt)
}

func (r *repositorySigningKeyResource) put(ctx context.Context, m *repositorySigningKeyModel) (*client.SigningKey, error) {
	res, err := r.api.ImportSigningKeyWithResponse(ctx, m.Repository.ValueString(), client.SigningKeyImport{
		PrivateKey: strPtr(m.PrivateKey), Passphrase: strPtr(m.Passphrase),
	})
	if err = check(res, err, http.StatusOK); err != nil {
		return nil, err
	}
	return res.JSON200, nil
}

func (r *repositorySigningKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan repositorySigningKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	k, err := r.put(ctx, &plan)
	if err != nil {
		apiError(&resp.Diagnostics, "import signing key", err)
		return
	}
	r.fromAPI(k, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *repositorySigningKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state repositorySigningKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.api.GetSigningKeyWithResponse(ctx, state.Repository.ValueString())
	if err = check(res, err, http.StatusOK); err != nil {
		if client.IsNotFound(err) { // the repository is gone
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read signing key", err)
		return
	}
	// private_key/passphrase are kept from state; a key rotated outside
	// Terraform shows up as a fingerprint that no longer matches the
	// configured key (see ModifyPlan).
	r.fromAPI(res.JSON200, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *repositorySigningKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan repositorySigningKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// ModifyPlan keeps created_at when the configured key is already active
	// (e.g. only the passphrase changed, or private_key is recorded after
	// import): then there is nothing to send.
	if plan.CreatedAt.IsUnknown() {
		k, err := r.put(ctx, &plan)
		if err != nil {
			apiError(&resp.Diagnostics, "import signing key", err)
			return
		}
		r.fromAPI(k, &plan)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete rotates the key: a signed repository always has an active key, so
// the imported key is replaced with a newly generated one.
func (r *repositorySigningKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state repositorySigningKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	res, err := r.api.RotateSigningKeyWithResponse(ctx, state.Repository.ValueString())
	if err = check(res, err, http.StatusOK, http.StatusNotFound); err != nil {
		apiError(&resp.Diagnostics, "rotate signing key", err)
	}
}

func (r *repositorySigningKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("repository"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
