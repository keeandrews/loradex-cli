// Package workspace models a loradex training workspace: a shared dataset/ and
// one or more models/<base>/ (each a publishable repo) with immutable
// versions/<vN>/ snapshots. State lives in .loradex/project.yaml (no secrets).
package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
)

// Project is .loradex/project.yaml — machine-managed, commit-safe (no secrets).
type Project struct {
	Version        int                       `yaml:"version"`
	Name           string                    `yaml:"name"`
	DefaultBase    string                    `yaml:"default_base,omitempty"`
	DefaultTrainer string                    `yaml:"default_trainer,omitempty"`
	CreatedAt      string                    `yaml:"created_at,omitempty"`
	Models         []ModelEntry              `yaml:"models"`
	Training       map[string]map[string]any `yaml:"training,omitempty"` // per-base profile overrides
}

// ModelEntry is a per-base repo recorded in project.yaml.
type ModelEntry struct {
	Base          string `yaml:"base"`
	Slug          string `yaml:"slug"`
	LatestVersion string `yaml:"latest_version,omitempty"`
}

// Paths.
func DotDir(root string) string              { return filepath.Join(root, ".loradex") }
func ProjectPath(root string) string         { return filepath.Join(root, ".loradex", "project.yaml") }
func CacheDir(root string) string            { return filepath.Join(root, ".loradex", "cache") }
func DatasetDir(root string) string          { return filepath.Join(root, "dataset") }
func ModelsDir(root string) string           { return filepath.Join(root, "models") }
func ModelDir(root, base string) string      { return filepath.Join(root, "models", base) }
func RepoYAMLPath(root, base string) string  { return filepath.Join(ModelDir(root, base), "repo.yaml") }
func ReadmePath(root, base string) string    { return filepath.Join(ModelDir(root, base), "README.md") }
func VersionsDir(root, base string) string   { return filepath.Join(ModelDir(root, base), "versions") }
func VersionDir(root, base, v string) string { return filepath.Join(VersionsDir(root, base), v) }

// IsWorkspace reports whether dir holds a project.yaml.
func IsWorkspace(dir string) bool {
	_, err := os.Stat(ProjectPath(dir))
	return err == nil
}

// FindRoot walks up from start looking for a workspace (.loradex/project.yaml).
func FindRoot(start string) (string, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	cur := abs
	for {
		if IsWorkspace(cur) {
			return cur, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", errors.New("not a loradex workspace — run `loradex init` first")
		}
		cur = parent
	}
}

// Load reads project.yaml.
func Load(root string) (*Project, error) {
	data, err := os.ReadFile(ProjectPath(root))
	if err != nil {
		return nil, err
	}
	var p Project
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// Save writes project.yaml (0644 — no secrets).
func Save(root string, p *Project) error {
	if err := os.MkdirAll(DotDir(root), 0o755); err != nil {
		return err
	}
	p.Version = 1
	data, err := yaml.Marshal(p)
	if err != nil {
		return err
	}
	return os.WriteFile(ProjectPath(root), data, 0o644)
}

// Now returns an RFC3339 timestamp (overridable in tests).
func Now() string { return time.Now().UTC().Format(time.RFC3339) }

var versionRE = regexp.MustCompile(`^v([0-9]+)$`)

// DiscoverVersions lists the version dirs for a base, sorted ascending by number.
func DiscoverVersions(root, base string) []string {
	entries, err := os.ReadDir(VersionsDir(root, base))
	if err != nil {
		return nil
	}
	var nums []int
	for _, e := range entries {
		if e.IsDir() {
			if m := versionRE.FindStringSubmatch(e.Name()); m != nil {
				n, _ := strconv.Atoi(m[1])
				nums = append(nums, n)
			}
		}
	}
	sort.Ints(nums)
	out := make([]string, len(nums))
	for i, n := range nums {
		out[i] = "v" + strconv.Itoa(n)
	}
	return out
}

// NextVersion returns the next vN for a base.
func NextVersion(root, base string) string {
	vs := DiscoverVersions(root, base)
	if len(vs) == 0 {
		return "v1"
	}
	last := vs[len(vs)-1]
	n, _ := strconv.Atoi(strings.TrimPrefix(last, "v"))
	return "v" + strconv.Itoa(n+1)
}

// LatestVersion returns the highest vN for a base, or "".
func LatestVersion(root, base string) string {
	vs := DiscoverVersions(root, base)
	if len(vs) == 0 {
		return ""
	}
	return vs[len(vs)-1]
}

// DiscoverModels lists the base models present under models/.
func DiscoverModels(root string) []string {
	entries, err := os.ReadDir(ModelsDir(root))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// Target identifies a model + version for push.
type Target struct {
	Base    string
	Version string // resolved vN
	Dir     string // version dir
}

// ResolveTarget parses a push target: "models/<base>[@vN]", "<base>[@vN]", or "" (single model).
func ResolveTarget(root, arg string) (Target, error) {
	base, version := "", ""
	if arg != "" {
		body := strings.TrimPrefix(strings.TrimPrefix(arg, "./"), "models/")
		if at := strings.IndexByte(body, '@'); at >= 0 {
			body, version = body[:at], body[at+1:]
		}
		base = strings.TrimRight(body, "/")
	}
	if base == "" {
		models := DiscoverModels(root)
		switch len(models) {
		case 1:
			base = models[0]
		case 0:
			return Target{}, errors.New("no models in this workspace — run `loradex build` first")
		default:
			return Target{}, fmt.Errorf("multiple models (%s) — specify one, e.g. `loradex push models/%s`", strings.Join(models, ", "), models[0])
		}
	}
	if version == "" {
		version = LatestVersion(root, base)
		if version == "" {
			return Target{}, fmt.Errorf("model %q has no versions yet — run `loradex build --base %s`", base, base)
		}
	}
	dir := VersionDir(root, base, version)
	if _, err := os.Stat(dir); err != nil {
		return Target{}, fmt.Errorf("version %s of %s not found", version, base)
	}
	return Target{Base: base, Version: version, Dir: dir}, nil
}

// UpsertModel records/updates a model entry in the project.
func (p *Project) UpsertModel(base, slug, latest string) {
	for i := range p.Models {
		if p.Models[i].Base == base {
			p.Models[i].Slug = slug
			if latest != "" {
				p.Models[i].LatestVersion = latest
			}
			return
		}
	}
	p.Models = append(p.Models, ModelEntry{Base: base, Slug: slug, LatestVersion: latest})
}

// CacheGitignore is written under .loradex/ so the ephemeral cache is never committed.
const CacheGitignore = "# loradex trainer working dirs — ephemeral, never push\ncache/\n"
