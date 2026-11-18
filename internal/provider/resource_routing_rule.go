package provider

import (
	"context"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/geoffo-dev/terraform-provider-entrepot/internal/client"
)

func init() { registerResource(NewRoutingRuleResource) }

var (
	_ resource.Resource                   = (*routingRuleResource)(nil)
	_ resource.ResourceWithImportState    = (*routingRuleResource)(nil)
	_ resource.ResourceWithValidateConfig = (*routingRuleResource)(nil)
)

func NewRoutingRuleResource() resource.Resource { return &routingRuleResource{} }

type routingRuleResource struct{ resourceBase }

type routingRuleModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Description  types.String `tfsdk:"description"`
	Mode         types.String `tfsdk:"mode"`
	Matchers     types.List   `tfsdk:"matchers"`
	Repositories types.List   `tfsdk:"repositories"`
}

func (r *routingRuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_routing_rule"
}

func (r *routingRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A routing rule limits which requests a proxy repository forwards upstream (set it with " +
			"`proxy.routing_rule` on an `entrepot_repository`). Use it to stop internal package names from ever " +
			"being fetched from a public registry (dependency confusion), or to allow only an approved set.\n\n" +
			"A rule cannot be deleted while proxy repositories use it; when repositories reference the rule " +
			"resource, Terraform updates them first.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, MarkdownDescription: "Same as `name`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name": schema.StringAttribute{Required: true, Validators: nameValidators(),
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				MarkdownDescription: "Rule name. Changing it forces a new rule."},
			"description": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString(""),
				MarkdownDescription: "Free-form description."},
			"mode": schema.StringAttribute{Required: true,
				Validators: []validator.String{stringvalidator.OneOf("allow", "block")},
				MarkdownDescription: "`allow`: only requests matching a matcher go upstream; " +
					"`block`: matching requests never go upstream."},
			"matchers": schema.ListAttribute{ElementType: types.StringType, Required: true,
				Validators: []validator.List{listvalidator.SizeAtLeast(1)},
				MarkdownDescription: "Go regular expressions over the package / image name or request path, " +
					"e.g. `^@acme/` or `^acme-`. The first matching expression decides."},
			"repositories": schema.ListAttribute{ElementType: types.StringType, Computed: true,
				PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
				MarkdownDescription: "Proxy repositories that currently use the rule (informational). It changes when " +
					"repositories are configured and is refreshed on every read; it never causes a diff."},
		},
	}
}

func (r *routingRuleResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m routingRuleModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || m.Matchers.IsNull() || m.Matchers.IsUnknown() {
		return
	}
	for i, v := range m.Matchers.Elements() {
		s, ok := v.(types.String)
		if !ok || s.IsNull() || s.IsUnknown() {
			continue
		}
		p := path.Root("matchers").AtListIndex(i)
		if strings.TrimSpace(s.ValueString()) == "" {
			resp.Diagnostics.AddAttributeError(p, "Empty matcher", "matchers must not be empty")
			continue
		}
		validateRegexp(&resp.Diagnostics, p, s.ValueString())
	}
}

func (r *routingRuleResource) input(ctx context.Context, m *routingRuleModel, diags *diag.Diagnostics) client.RoutingRuleInput {
	return client.RoutingRuleInput{
		Name: m.Name.ValueString(), Description: strPtr(m.Description),
		Mode: client.RoutingRuleInputMode(m.Mode.ValueString()), Matchers: stringList(ctx, m.Matchers, diags),
	}
}

func (r *routingRuleResource) fromAPI(rr *client.RoutingRule, m *routingRuleModel) {
	m.ID = types.StringValue(rr.Name)
	m.Name = types.StringValue(rr.Name)
	m.Description = types.StringValue(rr.Description)
	m.Mode = types.StringValue(string(rr.Mode))
	m.Matchers = listValue(rr.Matchers, false)
}

func (r *routingRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan routingRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	in := r.input(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.api.CreateRoutingRuleWithResponse(ctx, in)
	if err = check(res, err, http.StatusCreated); err != nil {
		apiError(&resp.Diagnostics, "create routing rule", err)
		return
	}
	r.fromAPI(res.JSON201, &plan)
	plan.Repositories = usersList(plan.Repositories, res.JSON201.Repositories)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *routingRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state routingRuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.api.GetRoutingRuleWithResponse(ctx, state.Name.ValueString())
	if err = check(res, err, http.StatusOK); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read routing rule", err)
		return
	}
	r.fromAPI(res.JSON200, &state)
	state.Repositories = listValue(deref(res.JSON200.Repositories), false)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *routingRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan routingRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	in := r.input(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := r.api.UpdateRoutingRuleWithResponse(ctx, plan.Name.ValueString(), in)
	if err = check(res, err, http.StatusOK); err != nil {
		apiError(&resp.Diagnostics, "update routing rule", err)
		return
	}
	r.fromAPI(res.JSON200, &plan)
	plan.Repositories = usersList(plan.Repositories, res.JSON200.Repositories)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *routingRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state routingRuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	res, err := r.api.DeleteRoutingRuleWithResponse(ctx, state.Name.ValueString())
	if err = check(res, err, http.StatusNoContent, http.StatusNotFound); err != nil {
		// 409 while proxy repositories still use the rule; the message names them.
		apiError(&resp.Diagnostics, "delete routing rule", err)
	}
}

func (r *routingRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
