package provider

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// testOpenPGPKey generates an RSA 2048 signing key and returns its armored
// private key (encrypted when passphrase is set), armored public key and
// fingerprint (upper-case hex).
func testOpenPGPKey(t *testing.T, passphrase string) (private, public, fingerprint string) {
	t.Helper()
	e, err := openpgp.NewEntity("Entrepot test", "tfacc", "tfacc@example.com",
		&packet.Config{Algorithm: packet.PubKeyAlgoRSA, RSABits: 2048})
	if err != nil {
		t.Fatal(err)
	}
	if passphrase != "" {
		if err := e.EncryptPrivateKeys([]byte(passphrase), nil); err != nil {
			t.Fatal(err)
		}
	}
	var priv, pub bytes.Buffer
	w, err := armor.Encode(&priv, openpgp.PrivateKeyType, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SerializePrivateWithoutSigning(w, nil); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	w, err = armor.Encode(&pub, openpgp.PublicKeyType, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Serialize(w); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	return priv.String() + "\n", pub.String() + "\n", strings.ToUpper(fmt.Sprintf("%x", e.PrimaryKey.Fingerprint))
}

func TestOpenPGPKeyInfo(t *testing.T) {
	priv, pub, fp := testOpenPGPKey(t, "")
	got, err := openpgpKeyInfo(priv)
	if err != nil || got != fp {
		t.Fatalf("openpgpKeyInfo(private) = %q, %v; want %q", got, err, fp)
	}
	if len(got) != 40 || strings.ToUpper(got) != got {
		t.Fatalf("fingerprint %q is not 40 upper-case hex digits", got)
	}
	// Encrypted keys parse without the passphrase (the fingerprint is public).
	encPriv, _, encFP := testOpenPGPKey(t, "s3cret")
	if got, err := openpgpKeyInfo(encPriv); err != nil || got != encFP {
		t.Fatalf("openpgpKeyInfo(encrypted) = %q, %v; want %q", got, err, encFP)
	}
	for name, in := range map[string]string{
		"public key": pub,
		"garbage":    "not a key",
		"empty":      "",
	} {
		if _, err := openpgpKeyInfo(in); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestSplitImportID(t *testing.T) {
	a, b, err := splitImportID("ci/0b6f-11", "<a>/<b>")
	if err != nil || a != "ci" || b != "0b6f-11" {
		t.Fatalf("got %q %q %v", a, b, err)
	}
	for _, bad := range []string{"", "ci", "ci/", "/x", "a/b/c"} {
		if _, _, err := splitImportID(bad, "<a>/<b>"); err == nil {
			t.Errorf("%q: expected an error", bad)
		}
	}
}

func TestStringSetConversions(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	if !stringSetFrom(nil, true).IsNull() {
		t.Fatal("nil with nullIfEmpty should be null")
	}
	empty := stringSetFrom(nil, false)
	if empty.IsNull() || len(empty.Elements()) != 0 {
		t.Fatal("nil without nullIfEmpty should be an empty set")
	}
	got := stringsFromSet(ctx, stringSetFrom([]string{"write", "read"}, false), &diags)
	if diags.HasError() || strings.Join(got, ",") != "read,write" {
		t.Fatalf("round trip = %v (%v)", got, diags)
	}
	if stringsFromSet(ctx, types.SetNull(types.StringType), &diags) != nil {
		t.Fatal("null set should give nil")
	}
}
