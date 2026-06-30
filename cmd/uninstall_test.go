package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanShellRC(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/zsh")

	lxHome := filepath.Join(home, ".loradex")
	rc := filepath.Join(home, ".zshrc")
	content := strings.Join([]string{
		`export PATH="$HOME/go/bin:$PATH"`, // unrelated — must survive
		"",
		"# Added by `loradex setup`",
		`export LORADEX_HOME="` + lxHome + `"`,
		"",
		"# Added by `loradex setup`",
		`export PATH="` + filepath.Join(lxHome, "bin") + `:$PATH"`,
		`alias ll='ls -la'`, // unrelated — must survive
	}, "\n")
	if err := os.WriteFile(rc, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, ok := cleanShellRC([]string{lxHome}); !ok {
		t.Fatal("cleanShellRC returned not-ok")
	}
	got, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	for _, banned := range []string{"LORADEX_HOME", ".loradex/bin", "Added by `loradex setup`"} {
		if strings.Contains(s, banned) {
			t.Errorf("expected %q to be removed; rc:\n%s", banned, s)
		}
	}
	for _, keep := range []string{"go/bin", "alias ll"} {
		if !strings.Contains(s, keep) {
			t.Errorf("expected %q to survive; rc:\n%s", keep, s)
		}
	}
}

func TestUninstallTargetsExistingOnly(t *testing.T) {
	real := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LORADEX_HOME", real)
	targets := uninstallTargets()
	found := false
	for _, tg := range targets {
		if tg == real {
			found = true
		}
	}
	if !found {
		t.Errorf("expected %s in targets, got %v", real, targets)
	}

	// A non-existent home is not a target.
	t.Setenv("LORADEX_HOME", filepath.Join(t.TempDir(), "does-not-exist"))
	for _, tg := range uninstallTargets() {
		if strings.Contains(tg, "does-not-exist") {
			t.Errorf("non-existent home should not be a target: %v", uninstallTargets())
		}
	}
}
