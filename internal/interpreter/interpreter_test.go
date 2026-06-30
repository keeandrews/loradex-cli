package interpreter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keeandrews/loradex-cli/internal/config"
)

func TestBuiltinDefaultExists(t *testing.T) {
	if _, ok := Find(DefaultID); !ok {
		t.Fatalf("default interpreter %q not in registry", DefaultID)
	}
	e, _ := Find(DefaultID)
	if e.Repo != "Qwen/Qwen3-VL-4B-Instruct" {
		t.Errorf("default repo = %q", e.Repo)
	}
}

func TestMergeCustom(t *testing.T) {
	out := merge(builtin, []config.CustomModel{{ID: "my-vlm", Repo: "org/My-VLM"}})
	found := false
	for _, e := range out {
		if e.ID == "my-vlm" && e.Custom && e.Repo == "org/My-VLM" {
			found = true
		}
	}
	if !found {
		t.Error("custom interpreter not merged")
	}
}

func TestStoreStatus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LORADEX_HOME", home)
	e := Entry{ID: "qwen3-vl-4b", Repo: "Qwen/Qwen3-VL-4B-Instruct"}
	if IsDownloaded(e) {
		t.Error("empty store should report not downloaded")
	}
	dir := filepath.Join(home, "interpreters", "qwen3-vl-4b")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// incomplete marker → not downloaded
	if err := os.WriteFile(filepath.Join(dir, "x.incomplete"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if IsDownloaded(e) {
		t.Error("config.json present but .incomplete remains → not downloaded")
	}
	if !PartiallyDownloaded(e) {
		t.Error("should be partially downloaded")
	}
	_ = os.Remove(filepath.Join(dir, "x.incomplete"))
	if !IsDownloaded(e) {
		t.Error("config.json + no incomplete → downloaded")
	}
}
