package pathsafe

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateMember_Rejects(t *testing.T) {
	bad := []string{
		"",
		"/etc/passwd",
		"../escape",
		"a/../../b",
		"../../x",
		`..\windows`,
		`C:\Windows\system32`,
		`\\server\share`,
		"//server/share",
		"foo\x00bar",
		"ctrl\x01char",
		"sub/../../../etc",
	}
	for _, m := range bad {
		if err := ValidateMember(m); err == nil {
			t.Errorf("ValidateMember(%q) = nil, want error", m)
		}
	}
}

func TestValidateMember_Accepts(t *testing.T) {
	good := []string{
		"model.safetensors",
		"README.md",
		"samples/a.png",
		"nested/dir/file.txt",
		"a.b.c",
		"with-hyphen_and_underscore.bin",
	}
	for _, m := range good {
		if err := ValidateMember(m); err != nil {
			t.Errorf("ValidateMember(%q) = %v, want nil", m, err)
		}
	}
}

func TestSafeJoin_Confines(t *testing.T) {
	root := t.TempDir()
	got, err := SafeJoin(root, "samples/a.png")
	if err != nil {
		t.Fatalf("SafeJoin valid: %v", err)
	}
	if want := filepath.Join(root, "samples", "a.png"); got != want {
		t.Errorf("SafeJoin = %q, want %q", got, want)
	}
	for _, m := range []string{"../escape", "/abs", "a/../../b"} {
		if _, err := SafeJoin(root, m); err == nil {
			t.Errorf("SafeJoin(%q) = nil, want error", m)
		}
	}
}

func TestSafeJoin_SymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	// Create a symlinked subdir inside root that points outside.
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	// Joining through the symlink must be rejected (escapes root via symlink).
	if _, err := SafeJoin(root, "link/evil.txt"); err == nil {
		t.Errorf("SafeJoin through symlink = nil, want error")
	}
}

func TestRefuseSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f")
	os.WriteFile(filepath.Join(dir, "real"), []byte("x"), 0o644)
	if err := os.Symlink(filepath.Join(dir, "real"), target); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if err := RefuseSymlink(target); err == nil {
		t.Errorf("RefuseSymlink(symlink) = nil, want error")
	}
	if err := RefuseSymlink(filepath.Join(dir, "nonexistent")); err != nil {
		t.Errorf("RefuseSymlink(missing) = %v, want nil", err)
	}
}
