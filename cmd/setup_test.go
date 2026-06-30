package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestOnPath(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	t.Setenv("PATH", dir+string(filepath.ListSeparator)+other)
	if !onPath(dir) {
		t.Errorf("expected %s to be on PATH", dir)
	}
	if onPath(t.TempDir()) {
		t.Error("unexpected dir reported on PATH")
	}
}

func TestShellRCPath(t *testing.T) {
	home := "/home/u"
	cases := map[string]string{
		"/bin/zsh":            ".zshrc",
		"/usr/bin/fish":       "config.fish",
		"/some/unknown-shell": ".profile",
	}
	for sh, want := range cases {
		t.Setenv("SHELL", sh)
		got := shellRCPath(home)
		if filepath.Base(got) != want {
			t.Errorf("SHELL=%s → %s, want base %s", sh, got, want)
		}
	}
	// bash differs by OS; just assert it resolves under home.
	t.Setenv("SHELL", "/bin/bash")
	if got := shellRCPath(home); filepath.Dir(got) != home {
		t.Errorf("bash rc %s not under %s (GOOS=%s)", got, home, runtime.GOOS)
	}
}

func TestResolveDrawThingsPathExistingWins(t *testing.T) {
	// An existing path is returned unchanged regardless of the DT dir.
	f := filepath.Join(t.TempDir(), "real.ckpt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := resolveDrawThingsPath(f); got != f {
		t.Errorf("existing path changed: %s", got)
	}
	// A path-like arg that doesn't exist is returned as-is (validation reports it).
	rel := "./nope/missing.ckpt"
	if got := resolveDrawThingsPath(rel); got != rel {
		t.Errorf("path-like arg changed: %s", got)
	}
}
