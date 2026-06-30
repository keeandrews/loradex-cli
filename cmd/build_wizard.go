package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/keeandrews/loradex-cli/internal/basemodel"
	"github.com/keeandrews/loradex-cli/internal/interpreter"
)

// wizardCfg holds the build settings collected by the interactive wizard.
type wizardCfg struct {
	datasetPath                        string
	base, interpreterID, trigger, name string
	steps, rank, alpha, resolution     int // steps 0 = auto
	lr                                 float64
	optimizer, precision               string
}

// menuLabel describes a setting row in the main menu: a label, the current
// value, and a one-line description with an example for the editor prompt.
type setting struct {
	key, label, desc string
	value            func(*wizardCfg) string
}

var buildSettings = []setting{
	{"dataset", "Training photos", "folder of images to train on (e.g. ~/Desktop/selfies)", func(c *wizardCfg) string { return orDashW(c.datasetPath) }},
	{"base", "Base model", "the model your LoRA fine-tunes (e.g. flux2-klein)", func(c *wizardCfg) string { return c.base }},
	{"interpreter", "Caption model", "vision model that auto-captions your images (e.g. qwen3-vl-4b; 'none' to skip)", func(c *wizardCfg) string { return orDashW(c.interpreterID) }},
	{"trigger", "Trigger word", "the token the LoRA binds to (e.g. ohwxman)", func(c *wizardCfg) string { return orDashW(c.trigger) }},
	{"steps", "Steps", "total training steps; more = stronger but slower ('auto' from image count, e.g. 1000)", func(c *wizardCfg) string { return stepsLabel(c.steps) }},
	{"rank", "LoRA rank", "network capacity; higher captures more detail, needs more data (e.g. 32)", func(c *wizardCfg) string { return strconv.Itoa(c.rank) }},
	{"alpha", "LoRA alpha", "scaling for the LoRA update, commonly equal to rank (e.g. 32)", func(c *wizardCfg) string { return strconv.Itoa(c.alpha) }},
	{"lr", "Learning rate", "step size; lower is safer, higher trains faster (e.g. 0.0001)", func(c *wizardCfg) string { return fmt.Sprintf("%g", c.lr) }},
	{"optimizer", "Optimizer", "optimization algorithm (adafactor is MPS-safe; adamw8bit needs CUDA)", func(c *wizardCfg) string { return c.optimizer }},
	{"precision", "Precision", "numeric precision for training (bf16 recommended on Apple Silicon)", func(c *wizardCfg) string { return c.precision }},
	{"resolution", "Resolution", "training image size in px; 1024 is standard for FLUX/SDXL (e.g. 1024)", func(c *wizardCfg) string { return strconv.Itoa(c.resolution) }},
	{"name", "Output name", "the published repo/file slug (e.g. my-portrait)", func(c *wizardCfg) string { return c.name }},
}

// runBuildWizard presents the settings menu (arrow-key navigation), letting the
// user edit each setting, and returns the chosen config. ok=false means cancel.
func runBuildWizard(header string, cfg wizardCfg) (wizardCfg, bool) {
	const startOpt = "▶  Start training"
	const cancelOpt = "✗  Cancel"
	fmt.Println()
	fmt.Println("  " + header)
	for {
		opts := make([]string, 0, len(buildSettings)+2)
		for _, s := range buildSettings {
			opts = append(opts, fmt.Sprintf("%-16s %s", s.label, s.value(&cfg)))
		}
		opts = append(opts, startOpt, cancelOpt)

		var choice string
		q := &survey.Select{
			Message:  "Configure training — ↑/↓ to choose, Enter to edit:",
			Options:  opts,
			PageSize: len(opts),
			Help:     "Edit any setting, then choose “Start training”. Defaults are prefilled.",
		}
		if err := survey.AskOne(q, &choice); err != nil {
			return cfg, false // ^C / EOF
		}
		switch choice {
		case startOpt:
			if strings.TrimSpace(cfg.datasetPath) == "" {
				fmt.Println("  ⚠ choose a Training photos folder first.")
				continue
			}
			return cfg, true
		case cancelOpt:
			return cfg, false
		}
		editSetting(settingForLabel(choice), &cfg)
	}
}

func settingForLabel(choice string) string {
	for _, s := range buildSettings {
		if strings.HasPrefix(choice, fmt.Sprintf("%-16s", s.label)) || strings.HasPrefix(choice, s.label) {
			return s.key
		}
	}
	return ""
}

func descFor(key string) string {
	for _, s := range buildSettings {
		if s.key == key {
			return s.desc
		}
	}
	return ""
}

// editSetting runs the appropriate editor (select or validated input) for a key.
func editSetting(key string, cfg *wizardCfg) {
	msg := func(label string) string { return fmt.Sprintf("%s — %s:", label, descFor(key)) }
	switch key {
	case "dataset":
		cfg.datasetPath = askPath(msg("Training photos"), cfg.datasetPath)
	case "base":
		cfg.base = pickFromRegistry(msg("Base model"), baseOptions(), cfg.base)
	case "interpreter":
		cfg.interpreterID = pickFromRegistry(msg("Caption model"), interpreterOptions(), cfg.interpreterID)
	case "trigger":
		cfg.trigger = askInput(msg("Trigger word"), cfg.trigger, nil)
	case "name":
		cfg.name = askInput(msg("Output name"), cfg.name, slugValidator)
	case "steps":
		v := askInput(msg("Steps"), stepsLabel(cfg.steps), stepsValidator)
		if strings.EqualFold(strings.TrimSpace(v), "auto") || v == "" {
			cfg.steps = 0
		} else {
			cfg.steps, _ = strconv.Atoi(strings.TrimSpace(v))
		}
	case "rank":
		cfg.rank = askInt(msg("LoRA rank"), cfg.rank, 1, 1024)
	case "alpha":
		cfg.alpha = askInt(msg("LoRA alpha"), cfg.alpha, 1, 1024)
	case "resolution":
		cfg.resolution = pickInt(msg("Resolution"), []int{512, 768, 1024, 1280}, cfg.resolution)
	case "lr":
		v := askInput(msg("Learning rate"), fmt.Sprintf("%g", cfg.lr), floatValidator)
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			cfg.lr = f
		}
	case "optimizer":
		cfg.optimizer = pickOne(msg("Optimizer"), []string{"adafactor", "adamw8bit", "prodigy"}, cfg.optimizer)
	case "precision":
		cfg.precision = pickOne(msg("Precision"), []string{"bf16", "fp16", "fp32"}, cfg.precision)
	}
}

// --- option sources ---

func baseOptions() []string {
	var out []string
	all, _ := basemodel.All()
	for _, e := range all {
		out = append(out, e.ID)
	}
	return out
}

func interpreterOptions() []string {
	out := []string{"none"}
	all, _ := interpreter.All()
	for _, e := range all {
		out = append(out, e.ID)
	}
	return out
}

// --- survey helpers ---

func pickFromRegistry(msg string, options []string, def string) string {
	return pickOne(msg, options, def)
}

func pickOne(msg string, options []string, def string) string {
	if def == "" && len(options) > 0 {
		def = options[0]
	}
	var ans string
	q := &survey.Select{Message: msg, Options: options, Default: def, PageSize: 10}
	if err := survey.AskOne(q, &ans); err != nil {
		return def
	}
	if ans == "none" {
		return ""
	}
	return ans
}

func pickInt(msg string, options []int, def int) int {
	strs := make([]string, len(options))
	defStr := strconv.Itoa(def)
	for i, v := range options {
		strs[i] = strconv.Itoa(v)
	}
	got := pickOne(msg, strs, defStr)
	n, err := strconv.Atoi(got)
	if err != nil {
		return def
	}
	return n
}

func askInput(msg, def string, validator survey.Validator) string {
	var ans string
	q := &survey.Input{Message: msg, Default: def}
	opts := []survey.AskOpt{}
	if validator != nil {
		opts = append(opts, survey.WithValidator(validator))
	}
	if err := survey.AskOne(q, &ans, opts...); err != nil {
		return def
	}
	return ans
}

// askPath prompts for a folder of training images, expanding ~ and validating
// that it exists and contains at least one image.
func askPath(msg, def string) string {
	var ans string
	q := &survey.Input{Message: msg, Default: def, Suggest: suggestDirs}
	if err := survey.AskOne(q, &ans, survey.WithValidator(imageFolderValidator)); err != nil {
		return def
	}
	return expandHome(strings.TrimSpace(ans))
}

// suggestDirs offers tab-completion over directories for the path prompt.
func suggestDirs(toComplete string) []string {
	dir := expandHome(toComplete)
	base := filepath.Dir(dir)
	if strings.HasSuffix(toComplete, "/") {
		base = dir
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, filepath.Join(base, e.Name())+"/")
		}
	}
	return out
}

func imageFolderValidator(ans interface{}) error {
	p := expandHome(strings.TrimSpace(fmt.Sprint(ans)))
	if p == "" {
		return fmt.Errorf("enter a folder path")
	}
	fi, err := os.Stat(p)
	if err != nil || !fi.IsDir() {
		return fmt.Errorf("not a folder: %s", p)
	}
	entries, _ := os.ReadDir(p)
	for _, e := range entries {
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".jpg", ".jpeg", ".png", ".webp", ".bmp":
			return nil
		}
	}
	return fmt.Errorf("no images (.jpg/.png/.webp) found in %s", p)
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
}

func askInt(msg string, def, min, max int) int {
	v := askInput(msg, strconv.Itoa(def), intRangeValidator(min, max))
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return def
	}
	return n
}

// --- validators ---

func intRangeValidator(min, max int) survey.Validator {
	return func(ans interface{}) error {
		n, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(ans)))
		if err != nil {
			return fmt.Errorf("enter a whole number")
		}
		if n < min || n > max {
			return fmt.Errorf("must be between %d and %d", min, max)
		}
		return nil
	}
}

func stepsValidator(ans interface{}) error {
	s := strings.TrimSpace(fmt.Sprint(ans))
	if s == "" || strings.EqualFold(s, "auto") {
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 1_000_000 {
		return fmt.Errorf("enter 'auto' or a number between 1 and 1000000")
	}
	return nil
}

func floatValidator(ans interface{}) error {
	f, err := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(ans)), 64)
	if err != nil || f <= 0 || f > 1 {
		return fmt.Errorf("enter a learning rate between 0 and 1 (e.g. 0.0001)")
	}
	return nil
}

func slugValidator(ans interface{}) error {
	s := strings.TrimSpace(fmt.Sprint(ans))
	if s == "" {
		return fmt.Errorf("a name is required")
	}
	for _, r := range s {
		if !(r == '-' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return fmt.Errorf("use lowercase letters, digits, and hyphens")
		}
	}
	return nil
}

// --- formatting ---

func stepsLabel(n int) string {
	if n <= 0 {
		return "auto"
	}
	return strconv.Itoa(n)
}

func orDashW(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
