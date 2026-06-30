package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/AlecAivazis/survey/v2"
)

// genSetting is one row in the generate wizard menu.
type genSetting struct {
	key, label, desc string
	value            func(*genCfg) string
}

var genSettings = []genSetting{
	{"lora", "LoRA", "which trained LoRA to generate from", func(c *genCfg) string {
		if c.lora == nil {
			return "—"
		}
		return fmt.Sprintf("%s@%s", c.lora.base, c.lora.version)
	}},
	{"prompt", "Prompt", "what to generate; the trigger word is added automatically (e.g. as an astronaut)", func(c *genCfg) string { return orDashW(truncate(c.prompt, 48)) }},
	{"promptfile", "Prompt file", "a newline-delimited file of prompts (overrides Prompt)", func(c *genCfg) string { return orDashW(c.promptFile) }},
	{"negative", "Negative", "things to avoid in the image (e.g. blurry, extra fingers)", func(c *genCfg) string { return orDashW(truncate(c.negative, 40)) }},
	{"steps", "Steps", "diffusion steps; more = slower, often crisper (e.g. 25)", func(c *genCfg) string { return strconv.Itoa(c.steps) }},
	{"size", "Size", "output resolution in px (defaults to the training size)", func(c *genCfg) string {
		return fmt.Sprintf("%dx%d", orInt(c.width, sizeDefault(c)), orInt(c.height, sizeDefault(c)))
	}},
	{"guidance", "Guidance", "prompt adherence; lower = looser, higher = stricter (e.g. 4.0)", func(c *genCfg) string { return fmt.Sprintf("%g", c.guidance) }},
	{"seed", "Seed", "fixed seed for repeatable images; 0 = random (e.g. 42)", func(c *genCfg) string { return seedLabel(c.seed) }},
	{"count", "Images/prompt", "how many images to render per prompt (e.g. 4)", func(c *genCfg) string { return strconv.Itoa(c.count) }},
}

// runGenerateWizard presents the arrow-key menu and returns the chosen config.
func runGenerateWizard(header string, cfg genCfg, loras []loraChoice) (genCfg, bool) {
	const startOpt = "▶  Generate"
	const cancelOpt = "✗  Cancel"
	// Default the LoRA to the only/most recent one so size auto-populates.
	if cfg.lora == nil && len(loras) > 0 {
		cfg.lora = &loras[len(loras)-1]
	}
	fmt.Println()
	fmt.Println("  " + header)
	for {
		opts := make([]string, 0, len(genSettings)+2)
		for _, s := range genSettings {
			opts = append(opts, fmt.Sprintf("%-16s %s", s.label, s.value(&cfg)))
		}
		opts = append(opts, startOpt, cancelOpt)

		var choice string
		q := &survey.Select{
			Message:  "Configure generation — ↑/↓ to choose, Enter to edit:",
			Options:  opts,
			PageSize: len(opts),
			Help:     "Pick a LoRA and a prompt, then choose “Generate”. Size/seed are prefilled.",
		}
		if err := survey.AskOne(q, &choice); err != nil {
			return cfg, false
		}
		switch choice {
		case startOpt:
			if cfg.lora == nil {
				fmt.Println("  ⚠ choose a LoRA first.")
				continue
			}
			if cfg.promptFile == "" && strings.TrimSpace(cfg.prompt) == "" {
				fmt.Println("  ⚠ enter a Prompt or choose a Prompt file first.")
				continue
			}
			return cfg, true
		case cancelOpt:
			return cfg, false
		}
		editGenSetting(genSettingKey(choice), &cfg, loras)
	}
}

func genSettingKey(choice string) string {
	for _, s := range genSettings {
		if strings.HasPrefix(choice, fmt.Sprintf("%-16s", s.label)) || strings.HasPrefix(choice, s.label) {
			return s.key
		}
	}
	return ""
}

func genDescFor(key string) string {
	for _, s := range genSettings {
		if s.key == key {
			return s.desc
		}
	}
	return ""
}

func editGenSetting(key string, cfg *genCfg, loras []loraChoice) {
	msg := func(label string) string { return fmt.Sprintf("%s — %s:", label, genDescFor(key)) }
	switch key {
	case "lora":
		cfg.lora = pickLoRA(msg("LoRA"), loras, cfg.lora)
	case "prompt":
		cfg.prompt = askInput(msg("Prompt"), cfg.prompt, nil)
	case "promptfile":
		cfg.promptFile = expandHome(strings.TrimSpace(askInput(msg("Prompt file"), cfg.promptFile, nil)))
	case "negative":
		cfg.negative = askInput(msg("Negative"), cfg.negative, nil)
	case "steps":
		cfg.steps = askInt(msg("Steps"), cfg.steps, 1, 200)
	case "size":
		d := sizeDefault(cfg)
		n := pickInt(msg("Size"), []int{512, 768, 1024, 1280, 1536}, orInt(cfg.width, d))
		cfg.width, cfg.height = n, n
	case "guidance":
		v := askInput(msg("Guidance"), fmt.Sprintf("%g", cfg.guidance), floatGuidanceValidator)
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			cfg.guidance = f
		}
	case "seed":
		cfg.seed = askInt(msg("Seed"), cfg.seed, 0, 2_000_000_000)
	case "count":
		cfg.count = askInt(msg("Images/prompt"), cfg.count, 1, 100)
	}
}

// pickLoRA lets the user choose among discovered LoRAs; selecting one resets the
// size to that LoRA's training resolution so it auto-populates.
func pickLoRA(msg string, loras []loraChoice, cur *loraChoice) *loraChoice {
	labels := make([]string, len(loras))
	def := ""
	for i := range loras {
		labels[i] = loras[i].label()
		if cur != nil && loras[i].base == cur.base && loras[i].version == cur.version {
			def = labels[i]
		}
	}
	var ans string
	q := &survey.Select{Message: msg, Options: labels, Default: def, PageSize: 10}
	if err := survey.AskOne(q, &ans); err != nil {
		return cur
	}
	for i := range loras {
		if labels[i] == ans {
			return &loras[i]
		}
	}
	return cur
}

func sizeDefault(c *genCfg) int {
	if c.lora != nil && c.lora.resolution > 0 {
		return c.lora.resolution
	}
	return 1024
}

func floatGuidanceValidator(ans interface{}) error {
	f, err := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(ans)), 64)
	if err != nil || f <= 0 || f > 30 {
		return fmt.Errorf("enter a guidance scale between 0 and 30 (e.g. 4.0)")
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
