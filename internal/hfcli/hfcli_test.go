package hfcli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoggedIn(t *testing.T) {
	// Isolate from any real token by pointing HF_HOME at an empty dir and
	// clearing the env tokens.
	hfHome := t.TempDir()
	t.Setenv("HF_HOME", hfHome)
	for _, e := range []string{"HF_TOKEN", "HUGGING_FACE_HUB_TOKEN", "HUGGINGFACE_TOKEN"} {
		t.Setenv(e, "")
	}
	if LoggedIn() {
		t.Fatal("expected not logged in with no token")
	}

	// Env var wins.
	t.Setenv("HF_TOKEN", "hf_xxx")
	if !LoggedIn() {
		t.Error("expected logged in with HF_TOKEN set")
	}
	t.Setenv("HF_TOKEN", "")

	// Token file is honored.
	if err := os.WriteFile(filepath.Join(hfHome, "token"), []byte("hf_filetoken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !LoggedIn() {
		t.Error("expected logged in with a cached token file")
	}
}
