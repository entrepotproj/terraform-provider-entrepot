package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Helpers shared by the scanning, cleanup and routing resources and the
// read-only data sources.

// httpURLRE loosely matches an http(s) URL with a host.
var httpURLRE = regexp.MustCompile(`^https?://[^/\s]`)

// goRegexp validates that a string is a Go (RE2) regular expression, the
// syntax the server compiles it with.
func goRegexp() validator.String { return goRegexpValidator{} }

type goRegexpValidator struct{}

func (goRegexpValidator) Description(context.Context) string {
	return "must be a valid Go (RE2) regular expression"
}

func (v goRegexpValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (goRegexpValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	validateRegexp(&resp.Diagnostics, req.Path, req.ConfigValue.ValueString())
}

// validateRegexp adds an attribute error when expr is not a Go regular expression.
func validateRegexp(diags *diag.Diagnostics, p path.Path, expr string) {
	if _, err := regexp.Compile(expr); err != nil {
		diags.AddAttributeError(p, "Invalid regular expression",
			fmt.Sprintf("%q is not a valid Go (RE2) regular expression: %s", expr, err))
	}
}

// rfc3339Timestamp validates an RFC 3339 timestamp such as 2026-12-31T23:59:59Z.
func rfc3339Timestamp() validator.String { return rfc3339Validator{} }

type rfc3339Validator struct{}

func (rfc3339Validator) Description(context.Context) string {
	return "must be an RFC 3339 timestamp, e.g. 2026-12-31T23:59:59Z"
}

func (v rfc3339Validator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (rfc3339Validator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if _, err := time.Parse(time.RFC3339, req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid timestamp",
			fmt.Sprintf("%q is not an RFC 3339 timestamp (e.g. 2026-12-31T23:59:59Z): %s", req.ConfigValue.ValueString(), err))
	}
}

// noSurroundingSpace rejects values the server would trim (which would
// otherwise produce an inconsistent result after apply).
func noSurroundingSpace(diags *diag.Diagnostics, p path.Path, v types.String) {
	if v.IsNull() || v.IsUnknown() {
		return
	}
	if s := v.ValueString(); s != strings.TrimSpace(s) {
		diags.AddAttributeError(p, "Leading or trailing whitespace", fmt.Sprintf("%q must not start or end with whitespace", s))
	}
}

// timeString renders a server timestamp in UTC RFC 3339 form.
func timeString(t time.Time) types.String {
	return types.StringValue(t.UTC().Format(time.RFC3339Nano))
}

// timeKeepPrior returns prior when it denotes the same instant as t (so a
// configured "2030-01-01T02:00:00+02:00" is not reported as changed when the
// server echoes it in UTC), else t in UTC RFC 3339 form; null for nil t.
func timeKeepPrior(prior types.String, t *time.Time) types.String {
	if t == nil {
		return types.StringNull()
	}
	if !prior.IsNull() && !prior.IsUnknown() {
		if p, err := time.Parse(time.RFC3339Nano, prior.ValueString()); err == nil &&
			p.Truncate(time.Microsecond).Equal(t.Truncate(time.Microsecond)) {
			return prior
		}
	}
	return timeString(*t)
}

// usersList handles the informational `repositories` attribute of cleanup
// policies and routing rules. That list changes whenever a repository
// references the object, i.e. through other resources, so:
//
//   - the schema uses UseStateForUnknown, so updates do not show it as
//     "(known after apply)";
//   - after Create (planned value unknown) the API's value is stored;
//   - after Update the planned (prior) value is kept, never the API's,
//     because a repository changed in the same apply could otherwise make
//     the result differ from the plan;
//   - Read always stores the server's current list.
//
// Since the attribute is Computed only, a refreshed value never produces a
// diff.
func usersList(planned types.List, api *[]string) types.List {
	if !planned.IsNull() && !planned.IsUnknown() {
		return planned
	}
	return listValue(deref(api), false)
}

// stringListOrNull is listValue for optional list attributes the server
// omits when empty.
func stringListOrNull(p *[]string) types.List {
	return listValue(deref(p), true)
}

// listOfObjects builds a list of objects (an empty, non-null list for no items).
func listOfObjects(ctx context.Context, attrTypes map[string]attr.Type, items any, diags *diag.Diagnostics) types.List {
	l, d := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: attrTypes}, items)
	diags.Append(d...)
	return l
}
