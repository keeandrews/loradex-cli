// Package trainerreg is the registry of training backends loradex knows how to
// detect, record, and (for some) install. The setup wizard uses it to present a
// checklist; build/import read the recorded locations from config.
//
// Detection is read-only and never mutates config — callers persist what they
// choose to keep. Only ai-toolkit is auto-installable for now; the others are
// detect-and-record.
package trainerreg

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/keeandrews/loradex-cli/internal/config"
)

// IDs of known trainers.
const (
	AIToolkit  = "ai-toolkit"
	DrawThings = "draw-things"
	ComfyUI    = "comfyui"
)

// Spec is the static description of a trainer backend.
type Spec struct {
	ID              string
	Name            string
	AutoInstallable bool // loradex can install it under <home>/trainers
	Orchestrated    bool // `loradex build` can drive it
	Desc            string
}

// Status is a Spec plus the result of probing this machine.
type Status struct {
	Spec
	Installed bool   // a usable install was found
	Path      string // install dir or app bundle
	Python    string // interpreter (ai-toolkit venv)
	ModelsDir string // a trainer's own model/output folder (Draw Things)
	Detail    string // one-line human note
}

// Specs returns the known trainers in display order.
func Specs() []Spec {
	return []Spec{
		{ID: AIToolkit, Name: "ai-toolkit", AutoInstallable: true, Orchestrated: true,
			Desc: "ostris/ai-toolkit — LoRA training (FLUX, SDXL, SD). Drives `loradex build`."},
		{ID: DrawThings, Name: "Draw Things", AutoInstallable: false, Orchestrated: false,
			Desc: "macOS app. loradex imports/publishes its LoRAs (`loradex import`)."},
		{ID: ComfyUI, Name: "ComfyUI", AutoInstallable: false, Orchestrated: false,
			Desc: "Detected and recorded; orchestration not yet wired."},
	}
}

func specOf(id string) Spec {
	for _, s := range Specs() {
		if s.ID == id {
			return s
		}
	}
	return Spec{ID: id, Name: id}
}

// DetectAll probes every known trainer.
func DetectAll() []Status {
	f, _ := config.Load()
	out := make([]Status, 0, len(Specs()))
	for _, s := range Specs() {
		out = append(out, detect(s, f))
	}
	return out
}

// Detect probes a single trainer by id.
func Detect(id string) Status {
	f, _ := config.Load()
	return detect(specOf(id), f)
}

func detect(s Spec, f *config.File) Status {
	st := Status{Spec: s}
	switch s.ID {
	case AIToolkit:
		detectAIToolkit(&st, f)
	case DrawThings:
		detectDrawThings(&st, f)
	case ComfyUI:
		detectComfyUI(&st, f)
	}
	return st
}

// aitoolkitCandidates lists install dirs to probe, highest priority first.
func aitoolkitCandidates(f *config.File) []string {
	var c []string
	if f != nil {
		if t, ok := f.Trainers[AIToolkit]; ok && t.Path != "" {
			c = append(c, t.Path)
		}
		if f.Trainer != nil && f.Trainer.AIToolkit.Home != "" {
			c = append(c, f.Trainer.AIToolkit.Home)
		}
	}
	if h := strings.TrimSpace(os.Getenv("LORADEX_AITOOLKIT_HOME")); h != "" {
		c = append(c, h)
	}
	if td, err := config.TrainersDir(); err == nil {
		c = append(c, filepath.Join(td, "ai-toolkit"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		c = append(c, filepath.Join(home, "ai-toolkit"))
	}
	return c
}

func detectAIToolkit(st *Status, f *config.File) {
	for _, dir := range aitoolkitCandidates(f) {
		if isAIToolkit(dir) {
			st.Installed = true
			st.Path = dir
			st.Python = resolveAIToolkitPython(dir, f)
			st.Detail = dir
			if st.Python == "" || !isFile(st.Python) {
				st.Detail = dir + " (no venv — run setup to create one)"
			}
			return
		}
	}
	st.Detail = "not found"
}

// isAIToolkit reports whether dir looks like an ai-toolkit clone.
func isAIToolkit(dir string) bool {
	if dir == "" {
		return false
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return false
	}
	return isFile(filepath.Join(dir, "run.py"))
}

func resolveAIToolkitPython(dir string, f *config.File) string {
	if f != nil {
		if t, ok := f.Trainers[AIToolkit]; ok && t.Python != "" {
			return t.Python
		}
		if f.Trainer != nil && f.Trainer.AIToolkit.Python != "" {
			return f.Trainer.AIToolkit.Python
		}
	}
	return filepath.Join(dir, "venv", "bin", "python")
}

func detectDrawThings(st *Status, f *config.File) {
	if runtime.GOOS != "darwin" {
		st.Detail = "macOS only"
		return
	}
	// Recorded location wins.
	if f != nil {
		if t, ok := f.Trainers[DrawThings]; ok && t.ModelsDir != "" && isDir(t.ModelsDir) {
			st.Installed = true
			st.Path = t.Path
			st.ModelsDir = t.ModelsDir
			st.Detail = t.ModelsDir
			return
		}
	}
	app := "/Applications/Draw Things.app"
	models := ""
	if home, err := os.UserHomeDir(); err == nil {
		m := filepath.Join(home, "Library/Containers/com.liuliu.draw-things/Data/Documents/Models")
		if isDir(m) {
			models = m
		}
	}
	if isDir(app) || models != "" {
		st.Installed = true
		if isDir(app) {
			st.Path = app
		}
		st.ModelsDir = models
		st.Detail = orFirst(models, app, "installed")
		return
	}
	st.Detail = "not found"
}

func detectComfyUI(st *Status, f *config.File) {
	if f != nil {
		if t, ok := f.Trainers[ComfyUI]; ok && t.Path != "" && isDir(t.Path) {
			st.Installed = true
			st.Path = t.Path
			st.Detail = t.Path
			return
		}
	}
	home, err := os.UserHomeDir()
	if err == nil {
		for _, cand := range []string{"ComfyUI", "comfyui", "Documents/ComfyUI"} {
			p := filepath.Join(home, cand)
			if isFile(filepath.Join(p, "main.py")) {
				st.Installed = true
				st.Path = p
				st.Detail = p
				return
			}
		}
	}
	st.Detail = "not found"
}

// ToConfig converts a Status into the config entry the wizard persists.
func (s Status) ToConfig(enabled bool) config.TrainerInfo {
	return config.TrainerInfo{Path: s.Path, Python: s.Python, ModelsDir: s.ModelsDir, Enabled: enabled}
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func orFirst(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
