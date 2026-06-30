package ref

import "testing"

func TestValidateSlug(t *testing.T) {
	good := []string{"a", "flux2-klein-portrait", "abc123", "a-b-c"}
	bad := []string{"", "-lead", "trail-", "UP", "a..b", "white space", "x/y"}
	for _, s := range good {
		if err := ValidateSlug(s); err != nil {
			t.Errorf("ValidateSlug(%q) = %v, want nil", s, err)
		}
	}
	for _, s := range bad {
		if err := ValidateSlug(s); err == nil {
			t.Errorf("ValidateSlug(%q) = nil, want error", s)
		}
	}
}

func TestParse(t *testing.T) {
	cases := []struct {
		in               string
		wantErr          bool
		owner, repo, ver string
	}{
		{"keenan/flux2-klein-portrait", false, "keenan", "flux2-klein-portrait", ""},
		{"bob/film-look@v2", false, "bob", "film-look", "v2"},
		{"a/b@latest", false, "a", "b", "latest"},
		{"keenan/repo@v0", false, "keenan", "repo", "v0"},
		{"no-owner", true, "", "", ""},
		{"a/b/c", true, "", "", ""},
		{"a/b@bad", true, "", "", ""},
		{"a/b@vv1", true, "", "", ""},
		{"UP/x", true, "", "", ""},
	}
	for _, c := range cases {
		r, err := Parse(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("Parse(%q) = nil err, want error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("Parse(%q) = %v", c.in, err)
			continue
		}
		if r.Owner != c.owner || r.Repo != c.repo || r.Version != c.ver {
			t.Errorf("Parse(%q) = %+v", c.in, r)
		}
	}
}

func TestReconcileVersion(t *testing.T) {
	if v, _ := ReconcileVersion("", ""); v != "latest" {
		t.Errorf("default = %q, want latest", v)
	}
	if v, _ := ReconcileVersion("v2", ""); v != "v2" {
		t.Errorf("from ref = %q", v)
	}
	if v, _ := ReconcileVersion("", "v3"); v != "v3" {
		t.Errorf("from flag = %q", v)
	}
	if v, _ := ReconcileVersion("v2", "v2"); v != "v2" {
		t.Errorf("agree = %q", v)
	}
	if _, err := ReconcileVersion("v2", "v3"); err == nil {
		t.Errorf("disagree = nil, want error")
	}
}
