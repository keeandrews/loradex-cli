// Package config manages the non-secret CLI configuration directory and file
// (config.yaml), plus default values. Secrets live in the credstore package.
package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/goccy/go-yaml"
)

// Defaults.
const (
	DefaultEndpoint = "https://api.loradex.ai"
	DefaultWeb      = "https://loradex.ai"
	DefaultProfile  = "default"
)

// File is the on-disk, non-secret configuration (config.yaml).
type File struct {
	Endpoint  string `yaml:"endpoint,omitempty"`
	Web       string `yaml:"web,omitempty"`
	OutputDir string `yaml:"output_dir,omitempty"`
	Profile   string `yaml:"profile,omitempty"`

	// CurrentProject is the active managed project (a folder under <home>/projects)
	// used when the working directory is not itself a workspace.
	CurrentProject string `yaml:"current_project,omitempty"`

	// Global defaults (overridable per-command / per-project).
	DefaultBase        string `yaml:"default_base,omitempty"`        // base model id, e.g. flux2-klein
	DefaultInterpreter string `yaml:"default_interpreter,omitempty"` // caption model id, e.g. qwen3-vl-4b

	// Training configuration (consumed by internal/trainer + internal/profile).
	Trainer         *TrainerConfig            `yaml:"trainer,omitempty"`
	Training        map[string]map[string]any `yaml:"training,omitempty"`         // per-base profile overrides
	Profiles        map[string]map[string]any `yaml:"profiles,omitempty"`         // named profiles
	BaseCheckpoints map[string]string         `yaml:"base_checkpoints,omitempty"` // base -> local path / HF id

	// Base-model registry (consumed by internal/basemodel).
	ModelsDir    string        `yaml:"models_dir,omitempty"`    // where downloaded base models live (default <home>/models)
	CustomModels []CustomModel `yaml:"custom_models,omitempty"` // user-cataloged "other" models

	// Interpreter (caption model) registry (consumed by internal/interpreter).
	CustomInterpreters []CustomModel `yaml:"custom_interpreters,omitempty"`

	// Trainer backends discovered/installed by the setup wizard.
	Trainers map[string]TrainerInfo `yaml:"trainers,omitempty"`

	// HuggingFace CLI (for pulling base models), recorded by the setup wizard.
	HuggingFace *ToolInfo `yaml:"huggingface,omitempty"`
}

// ToolInfo records a helper tool's location and whether it's enabled.
type ToolInfo struct {
	Path    string `yaml:"path,omitempty"`
	Enabled bool   `yaml:"enabled,omitempty"`
}

// TrainerInfo records where a trainer backend lives. Populated by `loradex
// setup`; consumed by build/import. Fields are backend-specific (Python for
// ai-toolkit, ModelsDir for Draw Things).
type TrainerInfo struct {
	Path      string `yaml:"path,omitempty"`       // install dir or app bundle
	Python    string `yaml:"python,omitempty"`     // interpreter (venv) for python trainers
	ModelsDir string `yaml:"models_dir,omitempty"` // a trainer's own model/output folder
	Enabled   bool   `yaml:"enabled,omitempty"`    // selected in the wizard
}

// SetTrainer upserts a trainer entry (does not persist; call Save).
func (f *File) SetTrainer(id string, info TrainerInfo) {
	if f.Trainers == nil {
		f.Trainers = map[string]TrainerInfo{}
	}
	f.Trainers[id] = info
}

// CustomModel is a user-cataloged base model (the "other" option). Either Repo
// (HuggingFace repo id) or URL (direct single-file download) is set, not both.
type CustomModel struct {
	ID      string  `yaml:"id"`                // unique slug, also the loradex --base id
	Name    string  `yaml:"name,omitempty"`    // display name
	Arch    string  `yaml:"arch,omitempty"`    // flux2 | flux1 | sdxl | sd15 | other
	Repo    string  `yaml:"repo,omitempty"`    // HuggingFace repo id (snapshot download)
	URL     string  `yaml:"url,omitempty"`     // direct https URL (single-file download)
	Format  string  `yaml:"format,omitempty"`  // diffusers | safetensors
	SHA256  string  `yaml:"sha256,omitempty"`  // optional integrity digest for URL downloads
	License string  `yaml:"license,omitempty"` // license id (informational)
	SizeGB  float64 `yaml:"size_gb,omitempty"` // approximate size (informational)
	Gated   bool    `yaml:"gated,omitempty"`   // requires HuggingFace auth
}

// TrainerConfig points loradex at local trainer backends.
type TrainerConfig struct {
	AIToolkit AIToolkitConfig `yaml:"ai_toolkit"`
}

// AIToolkitConfig locates an ai-toolkit install (never auto-installed).
type AIToolkitConfig struct {
	Home   string `yaml:"home,omitempty"`   // path to the cloned ai-toolkit repo
	Python string `yaml:"python,omitempty"` // python interpreter (default <home>/venv/bin/python)
}

// Dir returns the loradex home directory, creating it 0700 if needed. This is
// the single portable root: config.yaml, credentials, models/, and trainers/
// all live under it. Precedence: $LORADEX_HOME > <user-config-dir>/loradex.
func Dir() (string, error) {
	dir := strings.TrimSpace(os.Getenv("LORADEX_HOME"))
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(base, "loradex")
	} else if !filepath.IsAbs(dir) {
		// A relative LORADEX_HOME is too surprising to honor silently.
		abs, err := filepath.Abs(dir)
		if err != nil {
			return "", err
		}
		dir = abs
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	// Tighten perms if a pre-existing dir was looser (best effort; skip on Windows).
	if runtime.GOOS != "windows" {
		_ = os.Chmod(dir, 0o700)
	}
	return dir, nil
}

// DefaultHome returns the home loradex would use with no $LORADEX_HOME set
// (without creating anything) — for display in the installer.
func DefaultHome() string {
	base, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "loradex")
}

// ProjectsDir returns <home>/projects, where managed projects live.
func ProjectsDir() (string, error) {
	base, err := Dir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "projects")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// SetCurrentProject records the active managed project and persists config.
func SetCurrentProject(slug string) error {
	f, err := Load()
	if err != nil {
		return err
	}
	f.CurrentProject = slug
	return Save(f)
}

// TrainersDir returns <home>/trainers, where loradex installs trainer backends.
func TrainersDir() (string, error) {
	base, err := Dir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "trainers")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// InterpretersDir returns <home>/interpreters, where caption (VLM) models live.
func InterpretersDir() (string, error) {
	base, err := Dir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "interpreters")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// Path returns the absolute path of a file inside the config dir.
func Path(name string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// ModelsDir returns the directory where downloaded base models live, creating
// it 0700 if needed. Precedence: config.models_dir > env LORADEX_MODELS_DIR >
// <configdir>/models.
func ModelsDir() (string, error) {
	var dir string
	if f, err := Load(); err == nil && f.ModelsDir != "" {
		dir = f.ModelsDir
	}
	if dir == "" {
		dir = os.Getenv("LORADEX_MODELS_DIR")
	}
	if dir == "" {
		base, err := Dir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(base, "models")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// Load reads config.yaml. A missing file yields a zero-value File and no error.
func Load() (*File, error) {
	p, err := Path("config.yaml")
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return &File{}, nil
	}
	if err != nil {
		return nil, err
	}
	var f File
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// Save writes config.yaml (0600).
func Save(f *File) error {
	p, err := Path("config.yaml")
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(f)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// Resolve applies precedence: explicit flag > env > file > default.
func Resolve(flagVal, envVal, fileVal, def string) string {
	switch {
	case flagVal != "":
		return flagVal
	case envVal != "":
		return envVal
	case fileVal != "":
		return fileVal
	default:
		return def
	}
}
