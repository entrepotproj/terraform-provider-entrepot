package provider

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Helpers shared by the access-control and signing resources.

// accessActions are the actions roles grant and tokens may be scoped to.
var accessActions = []string{"read", "write", "delete", "admin"}

// builtinRoles are created by the server and cannot be changed or deleted.
var builtinRoles = []string{"viewer", "developer", "maintainer", "admin"}

// actionSetValidators validate a non-empty set of actions.
func actionSetValidators() []validator.Set {
	return []validator.Set{
		setvalidator.SizeAtLeast(1),
		setvalidator.ValueStringsAre(stringvalidator.OneOf(accessActions...)),
	}
}

// stringsFromSet converts a Terraform set of strings to a sorted Go slice
// (nil for null/unknown).
func stringsFromSet(ctx context.Context, s types.Set, diags *diag.Diagnostics) []string {
	if s.IsNull() || s.IsUnknown() {
		return nil
	}
	var out []string
	diags.Append(s.ElementsAs(ctx, &out, false)...)
	slices.Sort(out)
	return out
}

// stringSetFrom converts a Go slice to a Terraform set; nil/empty maps to
// null when nullIfEmpty.
func stringSetFrom(items []string, nullIfEmpty bool) types.Set {
	if len(items) == 0 && nullIfEmpty {
		return types.SetNull(types.StringType)
	}
	vals := make([]attr.Value, 0, len(items))
	for _, s := range items {
		vals = append(vals, types.StringValue(s))
	}
	return types.SetValueMust(types.StringType, vals)
}

// splitImportID splits an import ID of the form "<a>/<b>".
func splitImportID(id, format string) (string, string, error) {
	a, b, ok := strings.Cut(id, "/")
	if !ok || a == "" || b == "" || strings.Contains(b, "/") {
		return "", "", fmt.Errorf("expected an import ID of the form %s, got %q", format, id)
	}
	return a, b, nil
}

// openpgpKeyInfo parses an ASCII-armored OpenPGP private key the way the
// server does on import and returns its primary key fingerprint (upper-case
// hex, as the API reports it). It checks only the structure; decryption with
// the passphrase and usability for signing are checked by the server.
func openpgpKeyInfo(armored string) (string, error) {
	el, err := openpgp.ReadArmoredKeyRing(strings.NewReader(armored))
	if err != nil {
		return "", fmt.Errorf("not an ASCII-armored OpenPGP key: %v", err)
	}
	if len(el) != 1 {
		return "", fmt.Errorf("expected exactly one key, got %d", len(el))
	}
	if el[0].PrivateKey == nil {
		return "", fmt.Errorf("not a private key (export it with gpg --armor --export-secret-keys)")
	}
	return strings.ToUpper(fmt.Sprintf("%x", el[0].PrimaryKey.Fingerprint)), nil
}

// timeValue formats a timestamp as RFC 3339 (UTC).
func timeValue(t time.Time) types.String { return types.StringValue(t.UTC().Format(time.RFC3339)) }

// timePtrValue is timeValue for optional timestamps (null when nil).
func timePtrValue(t *time.Time) types.String {
	if t == nil {
		return types.StringNull()
	}
	return timeValue(*t)
}
