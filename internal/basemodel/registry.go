// Package basemodel is the base-model registry: a curated list of popular
// training base models plus user-cataloged custom entries, the on-disk store
// where they download, and the HuggingFace/URL fetchers.
//
// It is distinct from internal/catalog (the loradex.yaml publish manifest) and
// from the workspace models/<base>/ LoRA repos — this concerns the *base*
// checkpoints that training starts from.
package basemodel

import (
	"sort"

	"github.com/keeandrews/loradex-cli/internal/config"
)

// Entry is one base model — built-in or custom. Either Repo (a HuggingFace repo
// id, fetched via snapshot download) or URL (a direct https single-file
// download) is set, never both.
type Entry struct {
	ID      string  // unique slug; also the loradex --base id when it maps to a known base
	Name    string  // display name
	Arch    string  // flux2 | flux1 | sdxl | sd15 | other
	Repo    string  // HuggingFace repo id
	URL     string  // direct https URL (single-file)
	Format  string  // diffusers | safetensors
	SHA256  string  // optional integrity digest for URL downloads
	License string  // license id (informational)
	SizeGB  float64 // approximate download size (informational)
	Gated   bool    // requires HuggingFace auth / license acceptance
	Custom  bool    // user-cataloged (came from config.custom_models)
	Desc    string  // one-line description
}

// builtin is the curated set of popular base models. IDs match loradex base ids
// where one exists, so `loradex build --base <id>` lines up with the registry.
var builtin = []Entry{
	{
		ID: "flux2-klein", Name: "FLUX.2 Klein (4B)", Arch: "flux2",
		Repo: "black-forest-labs/FLUX.2-klein-4B", Format: "diffusers",
		License: "FLUX.2-klein License", SizeGB: 24, Gated: false,
		Desc: "Black Forest Labs FLUX.2 Klein 4B — open weights, fits Apple Silicon",
	},
	{
		ID: "flux2-klein-9b", Name: "FLUX.2 Klein (9B)", Arch: "flux2",
		Repo: "black-forest-labs/FLUX.2-klein-9B", Format: "diffusers",
		License: "FLUX.2-klein License", SizeGB: 40, Gated: true,
		Desc: "FLUX.2 Klein 9B — larger/higher quality (gated; needs HF login)",
	},
	{
		ID: "flux1", Name: "FLUX.1 [dev]", Arch: "flux1",
		Repo: "black-forest-labs/FLUX.1-dev", Format: "diffusers",
		License: "FLUX.1-dev Non-Commercial", SizeGB: 24, Gated: true,
		Desc: "FLUX.1 dev — high quality, non-commercial license",
	},
	{
		ID: "flux1-schnell", Name: "FLUX.1 [schnell]", Arch: "flux1",
		Repo: "black-forest-labs/FLUX.1-schnell", Format: "diffusers",
		License: "Apache-2.0", SizeGB: 24, Gated: false,
		Desc: "FLUX.1 schnell — fast, Apache-2.0 (no gating)",
	},
	{
		ID: "sdxl", Name: "Stable Diffusion XL 1.0", Arch: "sdxl",
		Repo: "stabilityai/stable-diffusion-xl-base-1.0", Format: "diffusers",
		License: "CreativeML-OpenRAIL-M", SizeGB: 7, Gated: false,
		Desc: "SDXL base 1.0 — widely supported",
	},
	{
		ID: "sd15", Name: "Stable Diffusion 1.5", Arch: "sd15",
		Repo: "stable-diffusion-v1-5/stable-diffusion-v1-5", Format: "diffusers",
		License: "CreativeML-OpenRAIL-M", SizeGB: 4, Gated: false,
		Desc: "SD 1.5 — small, fast, classic",
	},
}

// Builtin returns a copy of the curated registry.
func Builtin() []Entry {
	out := make([]Entry, len(builtin))
	copy(out, builtin)
	return out
}

// All returns the merged registry: built-ins plus the user's custom models from
// config. A custom entry with the same ID as a built-in overrides it. Sorted by
// arch then ID for stable display.
func All() ([]Entry, error) {
	f, err := config.Load()
	if err != nil {
		return nil, err
	}
	return merge(builtin, f.CustomModels), nil
}

func merge(base []Entry, custom []config.CustomModel) []Entry {
	byID := map[string]Entry{}
	order := []string{}
	for _, e := range base {
		if _, ok := byID[e.ID]; !ok {
			order = append(order, e.ID)
		}
		byID[e.ID] = e
	}
	for _, c := range custom {
		if c.ID == "" {
			continue
		}
		if _, ok := byID[c.ID]; !ok {
			order = append(order, c.ID)
		}
		byID[c.ID] = Entry{
			ID: c.ID, Name: orStr(c.Name, c.ID), Arch: orStr(c.Arch, "other"),
			Repo: c.Repo, URL: c.URL, Format: orStr(c.Format, "safetensors"),
			SHA256: c.SHA256, License: c.License, SizeGB: c.SizeGB, Gated: c.Gated,
			Custom: true, Desc: "custom",
		}
	}
	out := make([]Entry, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Arch != out[j].Arch {
			return out[i].Arch < out[j].Arch
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// knownBases are the loradex base ids that have a built-in training profile, so
// `loradex build --base <id>` resolves directly.
var knownBases = map[string]bool{"flux2-klein": true, "flux1": true, "sdxl": true, "sd15": true}

// IsKnownBase reports whether id is a base loradex can train against directly.
func IsKnownBase(id string) bool { return knownBases[id] }

// archBase maps an architecture to the loradex base id that carries its training
// profile (and flux quantization handling).
var archBase = map[string]string{"flux2": "flux2-klein", "flux1": "flux1", "sdxl": "sdxl", "sd15": "sd15"}

// BaseForArch returns the trainable base id for an architecture, or "" for "other".
func BaseForArch(arch string) string { return archBase[arch] }

// Find returns the registry entry with the given id, if any.
func Find(id string) (Entry, bool) {
	all, err := All()
	if err != nil {
		return Entry{}, false
	}
	for _, e := range all {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

// Source returns the human-readable origin (repo or URL).
func (e Entry) Source() string {
	if e.Repo != "" {
		return e.Repo
	}
	return e.URL
}

func orStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
