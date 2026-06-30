// Package project manages a local loradex working copy: the .loradex/config
// machine state (no secrets), loradex.yaml, README.md, and samples/.
package project

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/keeandrews/loradex-cli/internal/catalog"
)

// LastPush records the most recent successful push (machine state).
type LastPush struct {
	Version  string `json:"version"`
	SHA256   string `json:"sha256"`
	PushedAt string `json:"pushed_at"`
}

// Config is .loradex/config — committed-safe, NO secrets.
type Config struct {
	Version  int       `json:"version"`
	Endpoint string    `json:"endpoint"`
	Owner    string    `json:"owner"`
	Repo     string    `json:"repo"`
	LastPush *LastPush `json:"last_push,omitempty"`
}

// Project is a loaded working copy.
type Project struct {
	Dir     string
	Config  *Config
	Catalog *catalog.Catalog
}

// Paths.
func ConfigDir(dir string) string   { return filepath.Join(dir, ".loradex") }
func ConfigPath(dir string) string  { return filepath.Join(dir, ".loradex", "config") }
func CatalogPath(dir string) string { return filepath.Join(dir, "loradex.yaml") }
func ReadmePath(dir string) string  { return filepath.Join(dir, "README.md") }
func SamplesDir(dir string) string  { return filepath.Join(dir, "samples") }

// IsProject reports whether dir contains a loradex.yaml.
func IsProject(dir string) bool {
	_, err := os.Stat(CatalogPath(dir))
	return err == nil
}

// Load reads loradex.yaml and (if present) .loradex/config.
func Load(dir string) (*Project, error) {
	cat, err := catalog.Load(CatalogPath(dir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("no loradex.yaml here — run `loradex init` first")
		}
		return nil, err
	}
	cfg, err := LoadConfig(dir)
	if err != nil {
		return nil, err
	}
	return &Project{Dir: dir, Config: cfg, Catalog: cat}, nil
}

// LoadConfig reads .loradex/config (returns a zero Config if absent).
func LoadConfig(dir string) (*Config, error) {
	data, err := os.ReadFile(ConfigPath(dir))
	if errors.Is(err, os.ErrNotExist) {
		return &Config{Version: 1}, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// SaveConfig writes .loradex/config (0644 — no secrets, safe to commit).
func SaveConfig(dir string, c *Config) error {
	if err := os.MkdirAll(ConfigDir(dir), 0o755); err != nil {
		return err
	}
	c.Version = 1
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ConfigPath(dir), data, 0o644)
}
