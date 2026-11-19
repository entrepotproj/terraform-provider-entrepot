package provider

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/entrepotproj/terraform-provider-entrepot/internal/client"
)

// base holds the configured API client for resources and data sources.
type base struct {
	api *client.API
}

func (b *base) configure(providerData any, diags *diag.Diagnostics) {
	if providerData == nil {
		return // provider not configured yet (validate / plan with unknowns)
	}
	api, ok := providerData.(*client.API)
	if !ok {
		diags.AddError("Unexpected provider data", fmt.Sprintf("expected *client.API, got %T", providerData))
		return
	}
	b.api = api
}

// resourceBase / dataSourceBase implement Configure for embedding types.
type resourceBase struct{ base }

func (r *resourceBase) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.configure(req.ProviderData, &resp.Diagnostics)
}

type dataSourceBase struct{ base }

func (d *dataSourceBase) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.configure(req.ProviderData, &resp.Diagnostics)
}

// apiError adds an error diagnostic for a failed API call.
func apiError(diags *diag.Diagnostics, action string, err error) {
	diags.AddError("Entrepot API error", fmt.Sprintf("Unable to %s: %s", action, err))
}

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

// nameValidators validate Entrepot object names.
func nameValidators() []validator.String {
	return []validator.String{stringvalidator.RegexMatches(nameRE,
		"must start with a lowercase letter or digit and contain only lowercase letters, digits, '.', '_' and '-' (max 63)")}
}

// ---- value conversion helpers ----

// strPtr returns nil for null/unknown strings.
func strPtr(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}

func boolPtr(v types.Bool) *bool {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	b := v.ValueBool()
	return &b
}

func intPtr(v types.Int64) *int {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	i := int(v.ValueInt64())
	return &i
}

func strVal(p *string) types.String {
	if p == nil {
		return types.StringNull()
	}
	return types.StringValue(*p)
}

// strValOrEmpty maps nil and "" to null (for optional attributes the API
// omits when empty).
func strValOrEmpty(p *string) types.String {
	if p == nil || *p == "" {
		return types.StringNull()
	}
	return types.StringValue(*p)
}

func boolVal(p *bool) types.Bool {
	if p == nil {
		return types.BoolNull()
	}
	return types.BoolValue(*p)
}

func intVal(p *int) types.Int64 {
	if p == nil {
		return types.Int64Null()
	}
	return types.Int64Value(int64(*p))
}

// intValNonZero maps nil and 0 to null.
func intValNonZero(p *int) types.Int64 {
	if p == nil || *p == 0 {
		return types.Int64Null()
	}
	return types.Int64Value(int64(*p))
}

// stringList converts a Terraform list of strings to a Go slice (nil for null/unknown).
func stringList(ctx context.Context, l types.List, diags *diag.Diagnostics) []string {
	if l.IsNull() || l.IsUnknown() {
		return nil
	}
	var out []string
	diags.Append(l.ElementsAs(ctx, &out, false)...)
	return out
}

// listValue converts a Go slice to a Terraform list (null for nil when nullIfEmpty).
func listValue(items []string, nullIfEmpty bool) types.List {
	if len(items) == 0 && nullIfEmpty {
		return types.ListNull(types.StringType)
	}
	vals := make([]attr.Value, 0, len(items))
	for _, s := range items {
		vals = append(vals, types.StringValue(s))
	}
	return types.ListValueMust(types.StringType, vals)
}

func stringMap(ctx context.Context, m types.Map, diags *diag.Diagnostics) map[string]string {
	if m.IsNull() || m.IsUnknown() {
		return nil
	}
	out := map[string]string{}
	diags.Append(m.ElementsAs(ctx, &out, false)...)
	return out
}

func mapValue(m map[string]string) types.Map {
	vals := make(map[string]attr.Value, len(m))
	for k, v := range m {
		vals[k] = types.StringValue(v)
	}
	return types.MapValueMust(types.StringType, vals)
}

// objectAs decodes a (known, non-null) object into target. It returns false
// when the object is null or unknown.
func objectAs(ctx context.Context, o types.Object, target any, diags *diag.Diagnostics) bool {
	if o.IsNull() || o.IsUnknown() {
		return false
	}
	diags.Append(o.As(ctx, target, basetypes.ObjectAsOptions{})...)
	return !diags.HasError()
}

// objectFrom encodes src as an object of the given attribute types.
func objectFrom(ctx context.Context, attrTypes map[string]attr.Type, src any, diags *diag.Diagnostics) types.Object {
	o, d := types.ObjectValueFrom(ctx, attrTypes, src)
	diags.Append(d...)
	return o
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

func ptr[T any](v T) *T { return &v }

// check validates a generated *XxxResponse: err (transport) first, then the
// HTTP status against ok. It works for every generated response type.
func check(res any, err error, ok ...int) error {
	if err != nil {
		return err
	}
	v := reflect.ValueOf(res)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return fmt.Errorf("no response")
	}
	v = v.Elem()
	hr, _ := v.FieldByName("HTTPResponse").Interface().(*http.Response)
	if hr == nil {
		return fmt.Errorf("no HTTP response")
	}
	return client.Check(hr, v.FieldByName("Body").Bytes(), nil, ok...)
}
