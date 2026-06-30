package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/keeandrews/loradex-cli/internal/catalog"
	"github.com/keeandrews/loradex-cli/internal/output"
	"github.com/keeandrews/loradex-cli/internal/trainer"
	"github.com/keeandrews/loradex-cli/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	genPath, genLoRA, genPromptFile, genNegative, genDevice string
	genWidth, genHeight, genSteps, genSeed, genCount        int
	genGuidance                                             float64
	genDryRun                                               bool
)

// loraChoice is a trained LoRA discovered in the project, with the bits we need
// to auto-populate generation defaults (base, training resolution, trigger).
type loraChoice struct {
	base, version, slug, path, trigger string
	resolution                         int
}

func (l loraChoice) label() string {
	t := l.trigger
	if t == "" {
		t = "—"
	}
	return fmt.Sprintf("%s@%s  (%s, trigger: %s)", l.base, l.version, l.slug, t)
}

var generateCmd = &cobra.Command{
	Use:     "generate [prompt]",
	Aliases: []string{"gen"},
	Short:   "Generate images from a trained LoRA (orchestrates ai-toolkit)",
	Long: `Generate images locally from a LoRA you trained in this project. Loads the same
base model the LoRA was trained on, fuses the LoRA, and renders to a folder.

The interactive wizard lets you pick a LoRA and prefills size/seed/steps from the
saved training config. Pass a prompt inline or point at a text file of prompts.

Examples:
  loradex generate "ohwxman as an astronaut, cinematic"
  loradex generate --lora flux2-klein@v1 "a portrait of ohwxman"
  loradex generate --prompt-file prompts.txt --count 2
  loradex generate                                   # full wizard`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		p := output.New(g.json, g.quiet, g.verbose, g.noColor)

		root, err := resolveWorkspaceRoot(genPath)
		if err != nil {
			return err
		}
		if _, err := workspace.Load(root); err != nil {
			return err
		}
		loras := discoverLoRAs(root)
		if len(loras) == 0 {
			return output.Errorf(output.ExitValidation, "no_loras",
				"train one first: `loradex build`", "no trained LoRAs found in this project")
		}

		interactive := p.IsTTY() && !g.yes && !g.json

		// Seed config from flags / args, then let the wizard refine.
		cfg := genCfg{
			width: genWidth, height: genHeight, steps: orInt(genSteps, 25),
			guidance: orFloat(genGuidance, 4.0), seed: genSeed, count: orInt(genCount, 1),
			negative: genNegative, promptFile: genPromptFile,
		}
		if len(args) == 1 {
			cfg.prompt = args[0]
		}
		cfg.lora = resolveLoRAFlag(loras, genLoRA) // nil if unset/unmatched

		if interactive {
			var ok bool
			cfg, ok = runGenerateWizard(fmt.Sprintf("Generate images in %q", filepath.Base(root)), cfg, loras)
			if !ok {
				return output.Errorf(output.ExitError, "aborted", "", "generation cancelled")
			}
		}

		// Validate the resolved config.
		if cfg.lora == nil {
			return output.Errorf(output.ExitValidation, "no_lora_selected",
				"pass --lora <base>@<version>", "no LoRA selected")
		}
		if cfg.promptFile == "" && strings.TrimSpace(cfg.prompt) == "" {
			return output.Errorf(output.ExitValidation, "no_prompt",
				"pass a prompt or --prompt-file", "a prompt (or --prompt-file) is required")
		}
		if cfg.promptFile != "" {
			if _, err := os.Stat(cfg.promptFile); err != nil {
				return output.Errorf(output.ExitValidation, "prompt_file_missing", "", "prompt file not found: %s", cfg.promptFile)
			}
		}

		base := cfg.lora.base
		home, python := trainerLocation()
		if home == "" {
			return output.Errorf(output.ExitValidation, "no_trainer",
				"run `loradex setup`", "ai-toolkit is not configured")
		}
		trainer.Configure(home, python)
		tr := trainer.AIToolkit{}
		if _, err := tr.Detect(trainer.Config{Home: home, Python: python}); err != nil {
			return err
		}
		dev := trainer.DetectDevice(genDevice)

		ckpt, err := ensureBaseModel(cmd, p, base, "")
		if err != nil {
			return err
		}

		// Default size from the LoRA's training resolution.
		res := orInt(cfg.lora.resolution, 1024)
		w, h := orInt(cfg.width, res), orInt(cfg.height, res)

		// Render into a fresh, timestamped folder under the version dir.
		runID := trainer.NewRunID()
		outDir := filepath.Join(workspace.VersionDir(root, base, cfg.lora.version), "generated", runID)

		req := trainer.GenerateRequest{
			Name: "loradex-generate", Base: base, BaseCheckpoint: ckpt, LoRAPath: cfg.lora.path,
			Negative: cfg.negative, Width: w, Height: h, Steps: cfg.steps, Guidance: cfg.guidance,
			Seed: orSeed(cfg.seed), Count: cfg.count, Precision: "bf16", Quantize: isFluxQuantize(base, dev.Device),
			Device: dev.Device, OutputDir: outDir,
		}
		if cfg.promptFile != "" {
			req.PromptFile = cfg.promptFile
		} else {
			req.Prompts = []string{withTrigger(cfg.prompt, cfg.lora.trigger)}
		}

		printGeneratePlan(p, cfg, base, w, h, dev, outDir)

		if genDryRun {
			data, err := tr.GenerateConfig(req)
			if err != nil {
				return err
			}
			if g.json {
				return p.JSONOut(map[string]any{"base": base, "version": cfg.lora.version, "output_dir": outDir, "config": string(data)})
			}
			p.Info("\n  ai-toolkit generate config (dry run — not executed):\n")
			fmt.Println(string(data))
			return nil
		}

		if !interactive && !g.yes && !g.json && !confirm(p, "Generate now?") {
			return output.Errorf(output.ExitError, "aborted", "", "cancelled")
		}

		res2, err := generateWithSpinner(cmd, p, tr, req)
		if err != nil {
			return err
		}

		if g.json {
			return p.JSONOut(map[string]any{"base": base, "version": cfg.lora.version, "images": res2.Images, "output_dir": outDir})
		}
		p.Success("generated %d image(s)", len(res2.Images))
		for _, img := range res2.Images {
			p.Info("  %s", img)
		}
		return nil
	},
}

// genCfg holds the generation settings collected interactively or from flags.
type genCfg struct {
	lora                         *loraChoice
	prompt, promptFile, negative string
	width, height, steps, seed   int
	guidance                     float64
	count                        int
}

// generateWithSpinner runs generation, showing a spinner during the silent model
// load and a second during rendering.
func generateWithSpinner(cmd *cobra.Command, p *output.Printer, tr trainer.AIToolkit, req trainer.GenerateRequest) (trainer.GenerateResult, error) {
	load := startSpinner(p, "loading model — first run can take a few minutes")
	var render *spinner
	res, err := tr.Generate(cmd.Context(), req, func(pr trainer.GenerateProgress) {
		if pr.Loaded && render == nil {
			load.stop()
			render = startSpinner(p, "rendering images")
		}
	})
	load.stop()
	if render != nil {
		render.stop()
	}
	return res, err
}

// discoverLoRAs enumerates trained LoRAs across the project's models/versions.
func discoverLoRAs(root string) []loraChoice {
	var out []loraChoice
	for _, base := range workspace.DiscoverModels(root) {
		trigger := ""
		if c, err := catalog.Load(workspace.RepoYAMLPath(root, base)); err == nil && len(c.TriggerWords) > 0 {
			trigger = c.TriggerWords[0]
		}
		for _, v := range workspace.DiscoverVersions(root, base) {
			vdir := workspace.VersionDir(root, base, v)
			weights := findVersionWeights(vdir)
			if weights == "" {
				continue
			}
			lc := loraChoice{base: base, version: v, path: weights,
				slug: strings.TrimSuffix(filepath.Base(weights), ".safetensors"), trigger: trigger}
			lc.resolution = readTrainingResolution(filepath.Join(vdir, "training.yaml"))
			out = append(out, lc)
		}
	}
	return out
}

// findVersionWeights returns the .safetensors in a version dir (largest), or "".
func findVersionWeights(vdir string) string {
	entries, err := os.ReadDir(vdir)
	if err != nil {
		return ""
	}
	var best string
	var bestSize int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".safetensors") {
			continue
		}
		if fi, err := e.Info(); err == nil && fi.Size() > bestSize {
			best, bestSize = filepath.Join(vdir, e.Name()), fi.Size()
		}
	}
	return best
}

// readTrainingResolution pulls dataset.resolution from a saved training.yaml.
func readTrainingResolution(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var t struct {
		Dataset struct {
			Resolution int `yaml:"resolution"`
		} `yaml:"dataset"`
	}
	if yaml.Unmarshal(data, &t) != nil {
		return 0
	}
	return t.Dataset.Resolution
}

func resolveLoRAFlag(loras []loraChoice, flag string) *loraChoice {
	if flag == "" {
		return nil
	}
	for i := range loras {
		l := &loras[i]
		if flag == l.base+"@"+l.version || flag == l.version || flag == l.slug {
			return l
		}
	}
	return nil
}

func printGeneratePlan(p *output.Printer, cfg genCfg, base string, w, h int, dev trainer.Capabilities, outDir string) {
	if g.json {
		return
	}
	p.Info("")
	p.Info("  loradex generate")
	p.Info("  ───────────────────────────────────────────")
	p.Info("  LoRA         %s@%s  (%s)", base, cfg.lora.version, cfg.lora.slug)
	if cfg.promptFile != "" {
		p.Info("  Prompts      %s (file)", cfg.promptFile)
	} else {
		p.Info("  Prompt       %s", withTrigger(cfg.prompt, cfg.lora.trigger))
	}
	if cfg.negative != "" {
		p.Info("  Negative     %s", cfg.negative)
	}
	p.Info("  Settings     %dx%d · %d steps · guidance %g · seed %s · %d/prompt", w, h, cfg.steps, cfg.guidance, seedLabel(cfg.seed), cfg.count)
	p.Info("  Device       %s (%s)", dev.DeviceName, dev.Device)
	p.Info("  Output       %s", outDir)
	p.Info("  ───────────────────────────────────────────")
}

// withTrigger prepends the trigger to an inline prompt if it isn't already there.
func withTrigger(prompt, trigger string) string {
	prompt = strings.TrimSpace(prompt)
	if trigger == "" || strings.Contains(strings.ToLower(prompt), strings.ToLower(trigger)) {
		return prompt
	}
	return trigger + " " + prompt
}

func seedLabel(s int) string {
	if s <= 0 {
		return "random"
	}
	return fmt.Sprintf("%d", s)
}

func orSeed(s int) int {
	if s <= 0 {
		return -1
	}
	return s
}

func orInt(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}

func orFloat(v, def float64) float64 {
	if v > 0 {
		return v
	}
	return def
}

// isFluxQuantize mirrors training: FLUX bases are quantized on MPS to fit memory.
func isFluxQuantize(base, device string) bool {
	return strings.HasPrefix(base, "flux") && device == "mps"
}

func init() {
	f := generateCmd.Flags()
	f.StringVar(&genPath, "path", "", "workspace path (default: current/active project)")
	f.StringVar(&genLoRA, "lora", "", "LoRA to use: <base>@<version> (e.g. flux2-klein@v1)")
	f.StringVar(&genPromptFile, "prompt-file", "", "path to a newline-delimited prompts file")
	f.StringVar(&genNegative, "negative", "", "negative prompt")
	f.IntVar(&genWidth, "width", 0, "image width (default: training resolution)")
	f.IntVar(&genHeight, "height", 0, "image height (default: training resolution)")
	f.IntVar(&genSteps, "steps", 0, "diffusion steps (default 25)")
	f.Float64Var(&genGuidance, "guidance", 0, "guidance scale (default 4.0)")
	f.IntVar(&genSeed, "seed", 0, "seed (0/unset = random)")
	f.IntVar(&genCount, "count", 0, "images per prompt (default 1)")
	f.StringVar(&genDevice, "device", "", "device: auto|mps|cpu")
	f.BoolVar(&genDryRun, "dry-run", false, "print the ai-toolkit config without generating")
	rootCmd.AddCommand(generateCmd)
}
