package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProject_RoundTripNoSecrets(t *testing.T) {
	root := t.TempDir()
	in := &Project{Name: "my-portrait", DefaultBase: "flux2-klein", DefaultTrainer: "ai-toolkit",
		Models: []ModelEntry{{Base: "flux2-klein", Slug: "my-portrait-flux2-klein", LatestVersion: "v2"}}}
	if err := Save(root, in); err != nil {
		t.Fatal(err)
	}
	out, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if out.Name != "my-portrait" || len(out.Models) != 1 || out.Models[0].LatestVersion != "v2" {
		t.Errorf("round-trip mismatch: %+v", out)
	}
	raw, _ := os.ReadFile(ProjectPath(root))
	for _, forbidden := range []string{"token", "secret", "password", "authorization"} {
		if strings.Contains(strings.ToLower(string(raw)), forbidden) {
			t.Errorf("project.yaml contains forbidden field %q", forbidden)
		}
	}
}

func TestVersionDiscovery(t *testing.T) {
	root := t.TempDir()
	base := "flux2-klein"
	for _, v := range []string{"v1", "v2", "v10", "notaversion"} {
		os.MkdirAll(VersionDir(root, base, v), 0o755)
	}
	vs := DiscoverVersions(root, base)
	if len(vs) != 3 || vs[len(vs)-1] != "v10" {
		t.Errorf("DiscoverVersions = %v (want v1,v2,v10 sorted numerically)", vs)
	}
	if NextVersion(root, base) != "v11" {
		t.Errorf("NextVersion = %q, want v11", NextVersion(root, base))
	}
}

func TestResolveTarget(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(VersionDir(root, "flux2-klein", "v1"), 0o755)
	os.MkdirAll(VersionDir(root, "flux2-klein", "v2"), 0o755)

	// single model, no arg -> latest
	tgt, err := ResolveTarget(root, "")
	if err != nil || tgt.Version != "v2" {
		t.Errorf("default target = %+v, err %v", tgt, err)
	}
	// explicit @v1
	tgt, err = ResolveTarget(root, "models/flux2-klein@v1")
	if err != nil || tgt.Version != "v1" {
		t.Errorf("explicit version = %+v, err %v", tgt, err)
	}
	// add a second model -> ambiguous default
	os.MkdirAll(VersionDir(root, "sdxl", "v1"), 0o755)
	if _, err := ResolveTarget(root, ""); err == nil {
		t.Error("expected ambiguity error with multiple models")
	}
	_ = filepath.Join // keep import
}
