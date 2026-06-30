package project

import (
	"os"
	"strings"
	"testing"
)

func TestConfig_RoundTripNoSecrets(t *testing.T) {
	dir := t.TempDir()
	in := &Config{
		Version: 1, Endpoint: "https://api.loradex.ai", Owner: "keenan", Repo: "flux2-klein-portrait",
		LastPush: &LastPush{Version: "v4", SHA256: "abc", PushedAt: "2026-06-27T12:00:00Z"},
	}
	if err := SaveConfig(dir, in); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	out, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if out.Owner != in.Owner || out.Repo != in.Repo || out.LastPush.Version != "v4" {
		t.Errorf("round-trip mismatch: %+v", out)
	}

	// .loradex/config must contain no secret-looking fields (safe to commit).
	raw, _ := os.ReadFile(ConfigPath(dir))
	for _, forbidden := range []string{"token", "secret", "password", "Authorization"} {
		if strings.Contains(strings.ToLower(string(raw)), strings.ToLower(forbidden)) {
			t.Errorf(".loradex/config contains forbidden field %q", forbidden)
		}
	}
}
