package basemodel

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keeandrews/loradex-cli/internal/config"
)

func TestBuiltinHasKnownBases(t *testing.T) {
	for _, base := range []string{"flux2-klein", "flux1", "sdxl", "sd15"} {
		if _, ok := Find(base); !ok {
			t.Errorf("expected built-in entry for %q", base)
		}
		if !IsKnownBase(base) {
			t.Errorf("expected %q to be a known base", base)
		}
	}
}

func TestMergeCustomOverridesAndAppends(t *testing.T) {
	out := merge(builtin, []config.CustomModel{
		{ID: "my-flux", Repo: "me/flux", Arch: "flux2"},
		{ID: "sdxl", Repo: "me/sdxl-fork"}, // overrides built-in
	})
	var myFlux, sdxl *Entry
	for i := range out {
		switch out[i].ID {
		case "my-flux":
			myFlux = &out[i]
		case "sdxl":
			sdxl = &out[i]
		}
	}
	if myFlux == nil || !myFlux.Custom || myFlux.Repo != "me/flux" {
		t.Fatalf("custom entry not merged: %+v", myFlux)
	}
	if sdxl == nil || sdxl.Repo != "me/sdxl-fork" {
		t.Fatalf("custom entry did not override built-in: %+v", sdxl)
	}
}

func TestValidateCustom(t *testing.T) {
	cases := []struct {
		name string
		c    config.CustomModel
		ok   bool
	}{
		{"repo ok", config.CustomModel{ID: "a-model", Repo: "x/y"}, true},
		{"url ok", config.CustomModel{ID: "a-model", URL: "https://h/f.safetensors"}, true},
		{"both set", config.CustomModel{ID: "a-model", Repo: "x/y", URL: "https://h/f"}, false},
		{"neither set", config.CustomModel{ID: "a-model"}, false},
		{"http url", config.CustomModel{ID: "a-model", URL: "http://h/f"}, false},
		{"bad arch", config.CustomModel{ID: "a-model", Repo: "x/y", Arch: "nope"}, false},
		{"reserved id", config.CustomModel{ID: "sdxl", Repo: "x/y"}, false},
		{"bad slug", config.CustomModel{ID: "Bad ID", Repo: "x/y"}, false},
	}
	for _, tc := range cases {
		err := ValidateCustom(tc.c)
		if (err == nil) != tc.ok {
			t.Errorf("%s: got err=%v, want ok=%v", tc.name, err, tc.ok)
		}
	}
}

func TestStoreStatusAndConfinement(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("LORADEX_MODELS_DIR", tmp)
	// A path-traversal id must be rejected by the store.
	if _, err := slugDir("../escape"); err == nil {
		t.Error("expected slugDir to reject traversal id")
	}

	diff := Entry{ID: "sdxl", Repo: "x/y", Format: "diffusers"}
	if IsDownloaded(diff) {
		t.Error("empty store should report not downloaded")
	}
	dir := filepath.Join(tmp, "sdxl")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "model_index.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !IsDownloaded(diff) {
		t.Error("diffusers folder with model_index.json should be downloaded")
	}

	// A partial HuggingFace download (model_index.json present but .incomplete
	// markers remain) must read as NOT downloaded, but resumable.
	inc := filepath.Join(dir, ".cache", "huggingface", "download", "transformer")
	if err := os.MkdirAll(inc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inc, "shard.incomplete"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if IsDownloaded(diff) {
		t.Error("a partial download (with .incomplete markers) must not be 'downloaded'")
	}
	if !PartiallyDownloaded(diff) {
		t.Error("a partial download should be reported as resumable")
	}
	// Once the markers are gone, it's complete again.
	if err := os.RemoveAll(filepath.Join(dir, ".cache")); err != nil {
		t.Fatal(err)
	}
	if !IsDownloaded(diff) {
		t.Error("a finished download (no .incomplete) should be downloaded")
	}

	single := Entry{ID: "my-single", URL: "https://h/weights.safetensors", Format: "safetensors"}
	p, err := LocalPath(single)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != "weights.safetensors" {
		t.Errorf("unexpected single-file path: %s", p)
	}
}
