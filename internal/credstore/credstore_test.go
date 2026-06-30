package credstore

import (
	"os"
	"runtime"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestKey_EndpointIsolation(t *testing.T) {
	keyring.MockInit()
	keyA := Key("default", "https://api.loradex.ai")
	keyB := Key("default", "https://evil.example")
	if keyA == keyB {
		t.Fatal("keys for different hosts must differ")
	}
	if err := Set(keyA, Credential{Token: "secret-a", Handle: "keenan"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	// A token for host A must NOT be returned for host B.
	if _, ok, _ := Get(keyB); ok {
		t.Error("token for host A was returned for host B")
	}
	if c, ok, _ := Get(keyA); !ok || c.Token != "secret-a" {
		t.Errorf("Get(keyA) = %+v, ok=%v", c, ok)
	}
}

func TestFileFallback_Perms0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("perm bits not enforced on windows")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := fileSet(Key("default", "https://api.loradex.ai"), Credential{Token: "x"}); err != nil {
		t.Fatalf("fileSet: %v", err)
	}
	p, _ := credPath()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("credentials.json perms = %o, want 600", fi.Mode().Perm())
	}
}
