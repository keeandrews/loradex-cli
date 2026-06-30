package cmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/keeandrews/loradex-cli/internal/workspace"
)

func TestPushDryRun_NoMutations(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.Method == http.MethodGet && r.URL.Path == "/v1/me" {
			w.Write([]byte(`{"handle":"keenan","plan":"free","storage_used":1000,"storage_quota":21474836480}`))
			return
		}
		http.Error(w, `{"error":{"code":"x","message":"dry-run should not call this"}}`, 500)
	}))
	defer srv.Close()

	// Build a minimal workspace: project + one model + one version.
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LORADEX_ENDPOINT", srv.URL)
	t.Setenv("LORADEX_TOKEN", "test-token")

	if err := workspace.Save(root, &workspace.Project{Version: 1, Name: "my-portrait"}); err != nil {
		t.Fatal(err)
	}
	base := "flux2-klein"
	os.MkdirAll(workspace.ModelDir(root, base), 0o755)
	mustWrite(t, workspace.RepoYAMLPath(root, base), `name: my-portrait-flux2-klein
description: test
visibility: public
base_model: flux2-klein
format: safetensors
license: MIT
weights: my-portrait-flux2-klein.safetensors
trigger_words: [ohwxman]
network_rank: 32
network_dim: 32
recommended_weight: 0.8
tags: [portrait]
`)
	vdir := workspace.VersionDir(root, base, "v1")
	os.MkdirAll(vdir, 0o755)
	mustWrite(t, filepath.Join(vdir, "my-portrait-flux2-klein.safetensors"), "fake weights bytes")
	mustWrite(t, filepath.Join(vdir, "README.md"), "# my-portrait\n")

	g = globalFlags{}
	rootCmd.SetArgs([]string{"push", "models/flux2-klein", "--path", root, "--dry-run", "-q", "--insecure"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("push --dry-run error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, c := range calls {
		if strings.HasPrefix(c, "POST ") || strings.HasPrefix(c, "PUT ") {
			t.Errorf("dry-run made a mutating call: %s", c)
		}
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
