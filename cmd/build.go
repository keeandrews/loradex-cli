package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/keeandrews/loradex-cli/internal/basemodel"
	"github.com/keeandrews/loradex-cli/internal/caption"
	"github.com/keeandrews/loradex-cli/internal/catalog"
	"github.com/keeandrews/loradex-cli/internal/config"
	"github.com/keeandrews/loradex-cli/internal/dataset"
	"github.com/keeandrews/loradex-cli/internal/interpreter"
	"github.com/keeandrews/loradex-cli/internal/output"
	"github.com/keeandrews/loradex-cli/internal/profile"
	"github.com/keeandrews/loradex-cli/internal/project"
	"github.com/keeandrews/loradex-cli/internal/ref"
	"github.com/keeandrews/loradex-cli/internal/trainer"
	"github.com/keeandrews/loradex-cli/internal/trainerreg"
	"github.com/keeandrews/loradex-cli/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	bPath, bDataset, bBase, bTrigger, bType, bName, bCaption, bProfile                  string
	bTrainer, bDevice, bConfig, bCheckpoint, bInterpreter                               string
	bDryRun, bResume                                                                    bool
	bSteps, bRank, bAlpha, bBatch, bGradAccum, bResolution, bSeed, bSaveEvery, bSamples int
	bLR                                                                                 float64
	bOptimizer, bPrecision                                                              string
	bNoBucketing, bTrainTextEncoder, bInit                                              bool
)

var buildCmd = &cobra.Command{
	Use:   "build [images]",
	Short: "Train a LoRA locally (orchestrates ai-toolkit) into a versioned, pushable model",
	Long: `Train a LoRA on this machine by orchestrating ai-toolkit, then collect the
output into a versioned, cataloged model folder ready to push.

Examples:
  loradex build ./photos --base flux2-klein --trigger ohwxman
  loradex build --base sdxl                          # retrain the shared dataset on another base
  loradex build ./photos --base flux2-klein --dry-run
  loradex build --base flux2-klein --resume
  loradex build --base flux2-klein --profile fast-portrait -y`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		p := output.New(g.json, g.quiet, g.verbose, g.noColor)

		// 1. Locate workspace (CWD, --path, or the active managed project).
		root, err := resolveWorkspaceRoot(bPath)
		if err != nil {
			if !bInit {
				return err
			}
			root = orDefault(bPath, ".")
			if err := scaffoldWorkspace(root, ""); err != nil {
				return err
			}
		}
		proj, err := workspace.Load(root)
		if err != nil {
			return err
		}

		// 4. Resolve base/target: --base > project default > global config default.
		base := bBase
		if base == "" {
			base = proj.DefaultBase
		}
		if base == "" {
			if f, _ := config.Load(); f != nil {
				base = f.DefaultBase
			}
		}
		if base == "" {
			return output.Usage("specify --base (e.g. flux2-klein), or set one with `loradex config set default-base <id>`")
		}

		// 2. Resolve dataset.
		dsDir, dsSummary, source, err := resolveDataset(p, root, args)
		if err != nil {
			return err
		}
		// 3. Captions. A captioner (interpreter) is "configured" if one is
		// resolvable — that makes the default/auto mode generate real captions.
		interp := resolveInterpreter()
		captionMode, capWarn := dataset.ResolveCaptionMode(dsDir, bCaption, interp != "")
		if capWarn != "" {
			p.Info("note: %s", capWarn)
		}

		// 4b. Repo (models/<base>): create from defaults if new.
		slug := bName
		if slug == "" {
			slug = proj.Name + "-" + base
		}
		if err := ref.ValidateSlug(slug); err != nil {
			return output.Validation("%v", err)
		}
		cat, err := ensureRepo(root, base, slug, bTrigger, captionMode)
		if err != nil {
			return err
		}
		nextV := workspace.NextVersion(root, base)
		versionDir := workspace.VersionDir(root, base, nextV)

		// 5. Resolve + validate the profile.
		dev := trainer.DetectDevice(bDevice)
		prof, warnings := profile.Resolve(base, profile.Layers{
			GlobalBase: globalTraining(base), Named: namedProfile(bProfile), ProjectBase: proj.Training[base],
			Flags: buildFlagOverrides(cmd), Device: dev.Device, ImageCount: dsSummary.ImageCount,
		})
		for _, w := range warnings {
			p.Info("note: %s", w)
		}
		if errs, warns := profile.Validate(prof); len(errs) > 0 {
			for _, e := range errs {
				p.Info("  · %s", e)
			}
			return output.Errorf(output.ExitValidation, "invalid_profile", "adjust the profile or flags", "%d profile error(s)", len(errs))
		} else {
			for _, w := range warns {
				p.Info("note: %s", w)
			}
		}

		// 6. Trainer discovery.
		home, python := trainerLocation()
		trainer.Configure(home, python)
		tr := trainer.AIToolkit{}
		if _, derr := tr.Detect(trainer.Config{Home: home, Python: python}); derr != nil {
			if !bDryRun {
				return derr
			}
			p.Info("note: %v (dry-run continues)", derr)
		}

		// Build the request + plan.
		runID := trainer.NewRunID()
		if bResume {
			if last := latestCacheRun(root); last != "" {
				runID = last
			}
		}
		outFile := slug + ".safetensors"
		req := trainer.Request{
			BaseCheckpoint: resolveCheckpoint(base),
			Name:           slug, Base: base, Trigger: bTrigger, DatasetDir: dsDir, CaptionMode: captionMode,
			Profile: prof, Device: dev.Device, CacheDir: filepath.Join(workspace.CacheDir(root), runID),
			OutputDir: versionDir, OutputFile: outFile, RawConfig: bConfig, Samples: bSamples, RunID: runID,
		}
		plan, err := tr.Plan(req)
		if err != nil {
			return err
		}

		// 7. Confirm.
		printBuildPlan(p, proj, cat, base, nextV, dsDir, dsSummary, source, dev, prof, plan)
		if bDryRun {
			p.Info("dry-run — no training performed")
			return nil
		}
		if !g.yes && !confirm(p, "Proceed with training?") {
			return output.Errorf(output.ExitError, "aborted", "", "aborted")
		}

		// 7b. Ensure the base checkpoint is available (offer download if known).
		ckpt, err := ensureBaseModel(cmd, p, base, plan.Req.BaseCheckpoint)
		if err != nil {
			return err
		}
		plan.Req.BaseCheckpoint = ckpt

		// 7c. Caption the dataset with the interpreter (mode auto) before training.
		if captionMode == "auto" && interp != "" {
			if err := captionDataset(cmd, p, interp, dsDir, bTrigger); err != nil {
				return err
			}
			plan.Req.CaptionsHaveTrigger = bTrigger != ""
		}

		// 8. Train.
		p.Info("training… (cache: %s)", req.CacheDir)
		res, err := tr.Train(cmd.Context(), plan, func(pr trainer.Progress) {
			if p.ProgressEnabled() && pr.TotalSteps > 0 {
				fmt.Fprintf(p.Err, "\rstep %d/%d  loss %.4f   ", pr.Step, pr.TotalSteps, pr.Loss)
			}
		})
		if p.ProgressEnabled() {
			fmt.Fprintln(p.Err)
		}
		if err != nil {
			return err
		}
		if res.Stopped {
			p.Info("training stopped — checkpoints preserved. resume with `loradex build --base %s --resume`", base)
			return nil
		}

		// 9. Collect & catalog.
		if prof.Rank > 0 && cat.NetworkRank == 0 {
			cat.NetworkRank = res.NetworkRank
			cat.NetworkDim = res.NetworkDim
			_ = os.WriteFile(workspace.RepoYAMLPath(root, base), []byte(project.RenderCatalog(cat)), 0o644)
		}
		writeVersionArtifacts(root, base, nextV, slug, cat, prof, dev, dsSummary, res, captionMode)
		proj.UpsertModel(base, slug, nextV)
		_ = workspace.Save(root, proj)
		_ = dataset.Save(dsDir, &dataset.Config{Source: source, ImageCount: dsSummary.ImageCount,
			Formats: dsSummary.Formats, ContentHash: dsSummary.Hash, CaptionMode: captionMode, ResolutionHint: prof.Resolution})

		if g.json {
			return p.JSONOut(map[string]any{"base": base, "version": nextV, "weights": res.WeightsPath,
				"steps": res.Metrics.StepsCompleted, "duration_seconds": res.Metrics.DurationSeconds})
		}
		p.Success("Trained %s %s (%s) in %s", base, nextV, output.HumanSize(fileSize(res.WeightsPath)), fmtDuration(res.Metrics.DurationSeconds))
		p.Printf("  loradex push models/%s\n", base)
		return nil
	},
}

// --- helpers ---

func resolveDataset(p *output.Printer, root string, args []string) (dir string, sum *dataset.Summary, source string, err error) {
	switch {
	case bDataset != "":
		s, e := dataset.Validate(bDataset)
		return bDataset, s, "external", e
	case len(args) == 1:
		s, e := dataset.Ingest(args[0], workspace.DatasetDir(root))
		if e == nil {
			for _, sk := range s.Skipped {
				p.Info("skipped %s", sk)
			}
		}
		return workspace.DatasetDir(root), s, "ingested", e
	default:
		dsDir := workspace.DatasetDir(root)
		if _, e := os.Stat(dsDir); e != nil {
			return "", nil, "", output.Usage("no dataset — pass a folder of images or --dataset <path>")
		}
		s, e := dataset.Validate(dsDir)
		return dsDir, s, "ingested", e
	}
}

func ensureRepo(root, base, slug, trigger, _ string) (*catalog.Catalog, error) {
	rp := workspace.RepoYAMLPath(root, base)
	if c, err := catalog.Load(rp); err == nil {
		return c, nil
	}
	if err := os.MkdirAll(workspace.ModelDir(root, base), 0o755); err != nil {
		return nil, err
	}
	trig := []string{}
	if trigger != "" {
		trig = []string{trigger}
	}
	c := &catalog.Catalog{
		Name: slug, Visibility: "public", BaseModel: base, Format: "safetensors",
		License: "CreativeML-OpenRAIL-M", Weights: slug + ".safetensors",
		TriggerWords: trig, RecommendedWeight: 0.8, Tags: []string{},
	}
	if err := os.WriteFile(rp, []byte(project.RenderCatalog(c)), 0o644); err != nil {
		return nil, err
	}
	_ = os.WriteFile(workspace.ReadmePath(root, base), []byte(project.RenderReadme(c)), 0o644)
	return c, nil
}

func writeVersionArtifacts(root, base, v, slug string, cat *catalog.Catalog, prof profile.Profile, dev trainer.Capabilities, ds *dataset.Summary, res trainer.Result, captionMode string) {
	dir := workspace.VersionDir(root, base, v)
	_ = os.MkdirAll(dir, 0o755)

	training := map[string]any{
		"version": 1, "loradex_version": "dev",
		"trainer":    map[string]any{"name": "ai-toolkit", "version": "detected"},
		"base_model": base, "base_checkpoint": base,
		"device":  map[string]any{"type": dev.Device, "name": dev.DeviceName, "memory_gb": dev.MemoryGB},
		"profile": base + ":resolved",
		"dataset": map[string]any{"hash": ds.Hash, "image_count": ds.ImageCount, "resolution": prof.Resolution, "bucketing": prof.Bucketing, "caption_mode": captionMode},
		"hyperparameters": map[string]any{
			"network": map[string]any{"type": "lora", "rank": prof.Rank, "alpha": prof.Alpha},
			"steps":   prof.Steps, "optimizer": prof.Optimizer, "lr": prof.LR, "batch": prof.Batch,
			"grad_accum": prof.GradAccum, "precision": prof.Precision, "train_text_encoder": prof.TrainTextEncoder,
			"seed": prof.Seed, "save_every": prof.SaveEvery, "trainer_extra": prof.TrainerExtra,
		},
		"output": map[string]any{"file": slug + ".safetensors", "sha256": "", "size": fileSize(res.WeightsPath)},
		"timing": map[string]any{"duration_seconds": res.Metrics.DurationSeconds},
	}
	if data, err := yaml.Marshal(training); err == nil {
		_ = os.WriteFile(filepath.Join(dir, "training.yaml"), data, 0o644)
	}
	metrics, _ := json.MarshalIndent(res.Metrics, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "metrics.json"), metrics, 0o644)
	_ = os.WriteFile(filepath.Join(dir, "README.md"), []byte(project.RenderReadme(cat)), 0o644)
}

func printBuildPlan(p *output.Printer, proj *workspace.Project, cat *catalog.Catalog, base, v, dsDir string, ds *dataset.Summary, source string, dev trainer.Capabilities, prof profile.Profile, plan trainer.Plan) {
	if g.json {
		_ = p.JSONOut(map[string]any{
			"project": proj.Name, "base": base, "version": v, "slug": cat.Name,
			"dataset": map[string]any{"dir": dsDir, "images": ds.ImageCount, "hash": ds.Hash, "source": source},
			"device":  dev.Device, "device_name": dev.DeviceName,
			"profile": map[string]any{"rank": prof.Rank, "alpha": prof.Alpha, "steps": prof.Steps, "lr": prof.LR, "precision": prof.Precision},
			"output":  plan.OutputPath,
		})
		return
	}
	p.Info("")
	p.Info("  loradex build — review training plan")
	p.Info("  ───────────────────────────────────────────")
	p.Info("  Project      %s", proj.Name)
	p.Info("  Model        %-12s →  repo: %s", base, cat.Name)
	p.Info("  New version  %-6s            models/%s/versions/%s/", v, base, v)
	p.Info("  Dataset      %s   (%d images, %v)", dsDir, ds.ImageCount, ds.Formats)
	p.Info("    hash %s   captions: %s   trigger: %s", short16(ds.Hash), cat.TriggerWords, dashList(cat.TriggerWords))
	p.Info("  Trainer      ai-toolkit · device: %s (%s)", dev.DeviceName, dev.Device)
	p.Info("    network    LoRA rank %d / alpha %d", prof.Rank, prof.Alpha)
	p.Info("    steps      %d   optimizer %s   lr %g   precision %s", prof.Steps, prof.Optimizer, prof.LR, prof.Precision)
	if plan.Quantize {
		p.Info("    quantize   on (required for this base on MPS)")
	}
	p.Info("  Output       %s", plan.OutputPath)
	p.Info("  ───────────────────────────────────────────")
}

func buildFlagOverrides(cmd *cobra.Command) map[string]any {
	m := map[string]any{}
	set := func(name, key string, val any) {
		if cmd.Flags().Changed(name) {
			m[key] = val
		}
	}
	set("steps", "steps", bSteps)
	set("rank", "rank", bRank)
	set("alpha", "alpha", bAlpha)
	set("lr", "lr", bLR)
	set("batch", "batch", bBatch)
	set("grad-accum", "grad_accum", bGradAccum)
	set("optimizer", "optimizer", bOptimizer)
	set("precision", "precision", bPrecision)
	set("resolution", "resolution", bResolution)
	set("seed", "seed", bSeed)
	set("save-every", "save_every", bSaveEvery)
	set("train-text-encoder", "train_text_encoder", bTrainTextEncoder)
	if cmd.Flags().Changed("no-bucketing") {
		m["bucketing"] = !bNoBucketing
	}
	return m
}

func globalTraining(base string) map[string]any {
	f, _ := config.Load()
	if f.Training != nil {
		return f.Training[base]
	}
	return nil
}

func namedProfile(name string) map[string]any {
	if name == "" {
		return nil
	}
	f, _ := config.Load()
	if f.Profiles != nil {
		return f.Profiles[name]
	}
	return nil
}

// resolveCheckpoint picks the base model path, in precedence order:
// --checkpoint > config base_checkpoints > a downloaded registry model > "".
// An empty result means "let ai-toolkit fetch the HF default".
func resolveCheckpoint(base string) string {
	if bCheckpoint != "" {
		return bCheckpoint
	}
	if f, err := config.Load(); err == nil && f.BaseCheckpoints != nil {
		if c := f.BaseCheckpoints[base]; c != "" {
			return c
		}
	}
	if e, ok := basemodel.Find(base); ok && basemodel.IsDownloaded(e) {
		if path, err := basemodel.LocalPath(e); err == nil {
			return path
		}
	}
	return ""
}

// ensureBaseModel makes the base checkpoint available before training. If the
// checkpoint is already resolved (explicit, config, or a downloaded model) it is
// returned unchanged. Otherwise, when the base is a registry model that isn't
// downloaded, it offers to download it (auto-yes with -y); declining or a
// non-interactive run falls back to "" so ai-toolkit fetches from HuggingFace.
func ensureBaseModel(cmd *cobra.Command, p *output.Printer, base, current string) (string, error) {
	if current != "" {
		return current, nil
	}
	e, ok := basemodel.Find(base)
	if !ok {
		return "", nil
	}
	if basemodel.IsDownloaded(e) {
		return basemodel.LocalPath(e)
	}
	if g.json || !p.IsTTY() {
		p.Info("note: base %q not downloaded — ai-toolkit will fetch %s (or run `loradex models pull %s`)", base, e.Source(), base)
		return "", nil
	}
	if g.yes || confirm(p, fmt.Sprintf("Base model %q isn't downloaded. Download %s (~%gGB) now?", base, e.Source(), e.SizeGB)) {
		return pullModel(cmd, p, base, false)
	}
	p.Info("note: continuing without a local copy — ai-toolkit will fetch %s", e.Source())
	return "", nil
}

// resolveInterpreter picks the caption model: --interpreter > config default.
func resolveInterpreter() string {
	if bInterpreter != "" {
		return bInterpreter
	}
	if f, err := config.Load(); err == nil {
		return f.DefaultInterpreter
	}
	return ""
}

// captionDataset generates per-image captions with the interpreter, downloading
// it first if needed. Captions are written as <stem>.txt next to each image.
func captionDataset(cmd *cobra.Command, p *output.Printer, interpID, dsDir, trigger string) error {
	e, ok := interpreter.Find(interpID)
	if !ok {
		return output.Errorf(output.ExitValidation, "unknown_interpreter",
			"see `loradex interpreters list`", "unknown interpreter %q", interpID)
	}
	if !interpreter.IsDownloaded(e) {
		interactive := p.IsTTY() && !g.yes && !g.json
		if !interactive && !g.yes {
			return output.Errorf(output.ExitValidation, "interpreter_missing",
				"run `loradex interpreters pull "+interpID+"`", "caption model %q is not downloaded", interpID)
		}
		if g.yes || confirm(p, fmt.Sprintf("Caption model %q isn't downloaded. Download %s (~%gGB) now?", interpID, e.Repo, e.SizeGB)) {
			if _, err := pullInterpreter(cmd, p, interpID, false); err != nil {
				return err
			}
		} else {
			return output.Errorf(output.ExitError, "aborted", "", "captioning needs the interpreter; aborted")
		}
	}
	modelPath, err := interpreter.LocalPath(e)
	if err != nil {
		return err
	}
	_, python := trainerLocation()
	p.Info("captioning dataset with %s…", e.Name)
	res, err := caption.Run(cmd.Context(), python, modelPath, dsDir, caption.DefaultPrompt, trigger, p)
	if err != nil {
		return err
	}
	p.Success("captioned %d/%d images", res.Captioned, res.Total)
	return nil
}

// trainerLocation resolves where ai-toolkit lives via the central registry,
// which honors (in order) the trainers config map, the legacy trainer.ai_toolkit
// block, $LORADEX_AITOOLKIT_HOME, <home>/trainers/ai-toolkit, and ~/ai-toolkit.
func trainerLocation() (home, python string) {
	st := trainerreg.Detect(trainerreg.AIToolkit)
	return st.Path, st.Python
}

func latestCacheRun(root string) string {
	entries, err := os.ReadDir(workspace.CacheDir(root))
	if err != nil {
		return ""
	}
	var last string
	for _, e := range entries {
		if e.IsDir() && e.Name() > last {
			last = e.Name()
		}
	}
	return last
}

func fileSize(path string) int64 {
	if fi, err := os.Stat(path); err == nil {
		return fi.Size()
	}
	return 0
}

func fmtDuration(secs int) string {
	d := time.Duration(secs) * time.Second
	return d.Truncate(time.Second).String()
}

func dashList(s []string) string {
	if len(s) == 0 {
		return "—"
	}
	return s[0]
}

func init() {
	f := buildCmd.Flags()
	f.StringVar(&bPath, "path", ".", "workspace root")
	f.StringVar(&bDataset, "dataset", "", "use this folder as the dataset (external)")
	f.StringVar(&bBase, "base", "", "base model (e.g. flux2-klein)")
	f.StringVar(&bTrigger, "trigger", "", "trigger token")
	f.StringVar(&bType, "type", "subject", "subject | style | concept")
	f.StringVar(&bName, "name", "", "repo/output slug (default <project>-<base>)")
	f.StringVar(&bCaption, "caption", "", "auto | keep | none")
	f.StringVar(&bProfile, "profile", "", "named profile from loradex config")
	f.BoolVar(&bDryRun, "dry-run", false, "print the plan and exit (no training)")
	f.BoolVar(&bInit, "init", false, "auto-init a workspace if missing")
	// hyperparameter overrides
	f.IntVar(&bSteps, "steps", 0, "training steps (default: auto from image count)")
	f.IntVar(&bRank, "rank", 0, "LoRA rank")
	f.IntVar(&bAlpha, "alpha", 0, "LoRA alpha")
	f.Float64Var(&bLR, "lr", 0, "learning rate")
	f.IntVar(&bBatch, "batch", 0, "batch size")
	f.IntVar(&bGradAccum, "grad-accum", 0, "gradient accumulation steps")
	f.StringVar(&bOptimizer, "optimizer", "", "adamw8bit | adafactor | prodigy")
	f.StringVar(&bPrecision, "precision", "", "bf16 | fp16 | fp32")
	f.IntVar(&bResolution, "resolution", 0, "training resolution")
	f.BoolVar(&bNoBucketing, "no-bucketing", false, "disable aspect-ratio bucketing")
	f.IntVar(&bSeed, "seed", 0, "random seed")
	f.IntVar(&bSaveEvery, "save-every", 0, "checkpoint/sample interval")
	f.BoolVar(&bTrainTextEncoder, "train-text-encoder", false, "train the text encoder")
	// backend
	f.StringVar(&bTrainer, "trainer", "ai-toolkit", "training backend")
	f.StringVar(&bDevice, "device", "auto", "auto | mps | cpu | cuda")
	f.BoolVar(&bResume, "resume", false, "resume the last interrupted run")
	f.IntVar(&bSamples, "samples", 0, "number of validation samples")
	f.StringVar(&bConfig, "config", "", "raw ai-toolkit config file (escape hatch)")
	f.StringVar(&bCheckpoint, "checkpoint", "", "base model path or HF id (overrides the built-in mapping; use a local model)")
	f.StringVar(&bInterpreter, "interpreter", "", "caption model to auto-caption the dataset (default: config default_interpreter)")
	rootCmd.AddCommand(buildCmd)
}
