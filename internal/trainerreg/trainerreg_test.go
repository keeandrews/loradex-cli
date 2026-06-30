package trainerreg

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keeandrews/loradex-cli/internal/config"
)

func TestSpecsCoverKnownIDs(t *testing.T) {
	ids := map[string]bool{}
	for _, s := range Specs() {
		ids[s.ID] = true
	}
	for _, want := range []string{AIToolkit, DrawThings, ComfyUI} {
		if !ids[want] {
			t.Errorf("Specs() missing %q", want)
		}
	}
	if !specOf(AIToolkit).AutoInstallable {
		t.Error("ai-toolkit should be auto-installable")
	}
	if specOf(DrawThings).AutoInstallable {
		t.Error("Draw Things should not be auto-installable")
	}
}

func TestDetectAIToolkitFromHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LORADEX_HOME", home)
	t.Setenv("LORADEX_AITOOLKIT_HOME", "")

	// Plant a fake ai-toolkit clone under <home>/trainers/ai-toolkit.
	td := filepath.Join(home, "trainers", "ai-toolkit")
	if err := os.MkdirAll(td, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(td, "run.py"), []byte("# fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	st := Detect(AIToolkit)
	if !st.Installed {
		t.Fatalf("expected ai-toolkit detected under %s", td)
	}
	if st.Path != td {
		t.Errorf("path = %q, want %q", st.Path, td)
	}
	if filepath.Base(st.Python) != "python" {
		t.Errorf("python = %q, want a venv python", st.Python)
	}
}

func TestDetectRespectsConfigOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LORADEX_HOME", home)
	t.Setenv("LORADEX_AITOOLKIT_HOME", "")

	custom := filepath.Join(t.TempDir(), "my-aitk")
	if err := os.MkdirAll(custom, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(custom, "run.py"), []byte("# fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &config.File{Trainers: map[string]config.TrainerInfo{
		AIToolkit: {Path: custom, Python: "/usr/bin/python3"},
	}}
	if err := config.Save(f); err != nil {
		t.Fatal(err)
	}

	st := Detect(AIToolkit)
	if st.Path != custom {
		t.Errorf("path = %q, want config override %q", st.Path, custom)
	}
	if st.Python != "/usr/bin/python3" {
		t.Errorf("python = %q, want config override", st.Python)
	}
}
