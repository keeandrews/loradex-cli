package output

import "strings"

import "testing"

func TestRedact_Authorization(t *testing.T) {
	in := "GET / with Authorization: Bearer sk_live_supersecrettoken123"
	out := Redact(in)
	if strings.Contains(out, "sk_live_supersecrettoken123") {
		t.Errorf("token leaked: %q", out)
	}
	if !strings.Contains(out, "<redacted>") {
		t.Errorf("expected redaction marker: %q", out)
	}
}

func TestRedact_PresignedURL(t *testing.T) {
	in := "uploading to https://bucket.r2.cloudflarestorage.com/o/key?X-Amz-Signature=abc123&X-Amz-Credential=AKIA"
	out := Redact(in)
	if strings.Contains(out, "abc123") || strings.Contains(out, "AKIA") {
		t.Errorf("presigned query leaked: %q", out)
	}
	if !strings.Contains(out, "<redacted>") {
		t.Errorf("expected redacted query: %q", out)
	}
}

func TestRedact_JSONFields(t *testing.T) {
	in := `{"token":"abc","code":"xyz","verifier":"vvv"}`
	out := Redact(in)
	for _, secret := range []string{"abc", "xyz", "vvv"} {
		if strings.Contains(out, secret) {
			t.Errorf("secret %q leaked: %q", secret, out)
		}
	}
}

func TestRedact_LeavesPlainURLs(t *testing.T) {
	in := "fetching https://api.loradex.ai/v1/repos"
	if Redact(in) != in {
		t.Errorf("plain URL was altered: %q", Redact(in))
	}
}
