package trainer

// All ai-toolkit config-schema knowledge is isolated to this file. The config is
// built from typed structs and marshalled with goccy/go-yaml — never string
// concatenation — so untrusted values (trigger, paths, trainer_extra) are
// encoded as data and cannot inject keys.
//
// TODO(verify-schema): confirm the exact ai-toolkit keys against the installed
// version; this targets the sd_trainer/LoRA schema.

import (
	"strings"

	"github.com/goccy/go-yaml"
)

type atNetwork struct {
	Type        string `yaml:"type"`
	Linear      int    `yaml:"linear"`
	LinearAlpha int    `yaml:"linear_alpha"`
}

type atDataset struct {
	FolderPath string `yaml:"folder_path"`
	CaptionExt string `yaml:"caption_ext"`
	Resolution int    `yaml:"resolution"`
	// CacheLatentsToDisk: VAE-encode every image once, store the latents on disk,
	// and reload them each step instead of re-encoding. Lets ai-toolkit move the
	// VAE off-device during training — less unified memory, no per-step VAE pass.
	// Force-disabled by ai-toolkit if the dataset has augmentations (we set none).
	CacheLatentsToDisk bool `yaml:"cache_latents_to_disk,omitempty"`
}

type atSave struct {
	Dtype              string `yaml:"dtype"`
	SaveEvery          int    `yaml:"save_every"`
	MaxStepSavesToKeep int    `yaml:"max_step_saves_to_keep"`
}

type atTrain struct {
	Steps                     int     `yaml:"steps"`
	BatchSize                 int     `yaml:"batch_size"`
	GradientAccumulationSteps int     `yaml:"gradient_accumulation_steps"`
	LR                        float64 `yaml:"lr"`
	Optimizer                 string  `yaml:"optimizer"`
	Dtype                     string  `yaml:"dtype"`
	TrainTextEncoder          bool    `yaml:"train_text_encoder"`
	GradientCheckpointing     bool    `yaml:"gradient_checkpointing"`
	Seed                      int     `yaml:"seed"`
	EnableBucket              bool    `yaml:"enable_bucket"`
	NoiseScheduler            string  `yaml:"noise_scheduler,omitempty"` // "flowmatch" for FLUX; ai-toolkit defaults to ddpm
	// CacheTextEmbeddings: encode every caption once, store the prompt embeds on
	// disk, and unload the text encoder for the rest of the run. Only set when no
	// trigger_word is injected dynamically (captions are self-contained) — caching
	// freezes the embeddings, so a dynamic trigger would never reach them.
	CacheTextEmbeddings bool `yaml:"cache_text_embeddings,omitempty"`
	// DisableSampling: skip all sample-image generation, including the pre-train
	// baseline sample that otherwise loads the text encoder and runs inference
	// before step 1. Set when the run requested no samples.
	DisableSampling bool `yaml:"disable_sampling,omitempty"`
}

type atModel struct {
	NameOrPath string `yaml:"name_or_path"`
	Arch       string `yaml:"arch,omitempty"`      // ai-toolkit architecture selector (newer models)
	IsFlux     bool   `yaml:"is_flux,omitempty"`   // legacy FLUX.1 flag
	Quantize   bool   `yaml:"quantize,omitempty"`  //
	LoRAPath   string `yaml:"lora_path,omitempty"` // fuse a trained LoRA at load (generation only)
}

type atSample struct {
	SampleEvery int      `yaml:"sample_every"`
	Sampler     string   `yaml:"sampler,omitempty"` // must match train.noise_scheduler
	Width       int      `yaml:"width"`
	Height      int      `yaml:"height"`
	Seed        int      `yaml:"seed"`
	Prompts     []string `yaml:"prompts"`
}

type atProcess struct {
	Type           string         `yaml:"type"`
	TrainingFolder string         `yaml:"training_folder"`
	Device         string         `yaml:"device"`
	TriggerWord    string         `yaml:"trigger_word,omitempty"`
	Network        atNetwork      `yaml:"network"`
	Save           atSave         `yaml:"save"`
	Datasets       []atDataset    `yaml:"datasets"`
	Train          atTrain        `yaml:"train"`
	Model          atModel        `yaml:"model"`
	Sample         atSample       `yaml:"sample"`
	Extra          map[string]any `yaml:",inline"` // trainer_extra passthrough (data only)
}

type atConfig struct {
	Name    string      `yaml:"name"`
	Process []atProcess `yaml:"process"`
}

type atJob struct {
	Job    string   `yaml:"job"`
	Config atConfig `yaml:"config"`
}

// baseCheckpoints maps loradex base ids to the model ai-toolkit loads. FLUX.2
// Klein uses the *base* 4B checkpoint — that's what ai-toolkit's flux2_klein_4b
// loader expects (flux-2-klein-base-4b.safetensors) and the right variant to
// train LoRAs on.
var baseCheckpoints = map[string]string{
	"flux2-klein": "black-forest-labs/FLUX.2-klein-base-4B",
	"flux1":       "black-forest-labs/FLUX.1-dev",
	"sdxl":        "stabilityai/stable-diffusion-xl-base-1.0",
	"sd15":        "runwayml/stable-diffusion-v1-5",
}

// baseArch maps a loradex base to ai-toolkit's architecture selector. Bases not
// listed are auto-detected by ai-toolkit (or use the legacy is_flux flag).
var baseArch = map[string]string{
	"flux2-klein": "flux2_klein_4b",
}

func resolveCheckpoint(base string) string {
	if c, ok := baseCheckpoints[base]; ok {
		return c
	}
	return base
}

func archForBase(base string) string { return baseArch[base] }

// isFluxFamily reports whether a base is a FLUX model (1 or 2). FLUX trains with
// flow-matching; ai-toolkit needs train.noise_scheduler = "flowmatch" (its
// default is ddpm, which uses the wrong timestep path for FLUX).
func isFluxFamily(base string) bool {
	return isFlux(base) || archForBase(base) != "" || strings.HasPrefix(base, "flux")
}

// isFlux reports whether a base uses the legacy FLUX.1 (is_flux) loader. FLUX.2
// uses the arch selector instead, so it is not "is_flux".
func isFlux(base string) bool { return base == "flux1" }

func samplePrompts(req Request, n int) []string {
	trig := req.Trigger
	if trig == "" {
		trig = "[trigger]"
	}
	all := []string{
		"a portrait of " + trig + ", cinematic lighting, high detail",
		trig + " in a sunlit studio, shallow depth of field",
		"a close-up of " + trig + ", natural light",
		trig + ", dramatic rim lighting, photoreal",
	}
	if n > len(all) {
		n = len(all)
	}
	return all[:n]
}

// effectiveTrigger is the trigger_word ai-toolkit should inject. When we
// generated the captions, the trigger is already baked into each .txt, so it
// must be empty to avoid double-prepending.
func effectiveTrigger(req Request) string {
	if req.CaptionsHaveTrigger {
		return ""
	}
	return req.Trigger
}

// PerfPlan summarizes the memory/speed optimizations applied to a training run.
type PerfPlan struct {
	CacheLatents        bool // VAE-encode images once to disk, then free the VAE
	CacheTextEmbeddings bool // encode captions once, then unload the text encoder
	DisableSampling     bool // skip sampling, including the pre-train baseline sample
}

// PerfPlanFor derives the optimizations enabled for a request — the single
// source of truth shared by config generation and the build plan. Latent
// caching is always safe (no augmentations are configured). Text-embedding
// caching unloads the TE but freezes the embeds, so it is gated on
// self-contained captions (no dynamic trigger_word) and not training the TE.
// Sampling is skipped when the run requested no samples.
func PerfPlanFor(req Request) PerfPlan {
	return PerfPlan{
		CacheLatents:        true,
		CacheTextEmbeddings: effectiveTrigger(req) == "" && !req.Profile.TrainTextEncoder,
		DisableSampling:     req.Samples <= 0,
	}
}

// buildConfigYAML renders the ai-toolkit config for a request.
func buildConfigYAML(req Request) ([]byte, error) {
	p := req.Profile
	extra := map[string]any{}
	for k, v := range p.TrainerExtra {
		extra[k] = v
	}
	// FLUX trains with flow-matching; the sampler must match the train scheduler.
	noiseScheduler := ""
	if isFluxFamily(req.Base) {
		noiseScheduler = "flowmatch"
	}
	// Validation sampling is opt-in (--samples N). When 0, emit no prompts and
	// push sample_every past the run so ai-toolkit never generates samples.
	sample := atSample{SampleEvery: p.Steps + 1, Sampler: noiseScheduler, Width: p.Resolution, Height: p.Resolution, Seed: p.Seed, Prompts: []string{}}
	if req.Samples > 0 {
		sample.SampleEvery = p.SaveEvery
		sample.Prompts = samplePrompts(req, req.Samples)
	}
	// When we generated captions, the trigger is already baked into each .txt —
	// don't let ai-toolkit prepend it again.
	triggerWord := effectiveTrigger(req)
	perf := PerfPlanFor(req)
	proc := atProcess{
		Type:           "sd_trainer",
		TrainingFolder: req.CacheDir,
		Device:         req.Device,
		TriggerWord:    triggerWord,
		Network:        atNetwork{Type: "lora", Linear: p.Rank, LinearAlpha: p.Alpha},
		Save:           atSave{Dtype: p.Precision, SaveEvery: p.SaveEvery, MaxStepSavesToKeep: 4},
		Datasets:       []atDataset{{FolderPath: req.DatasetDir, CaptionExt: "txt", Resolution: p.Resolution, CacheLatentsToDisk: perf.CacheLatents}},
		Train: atTrain{
			Steps: p.Steps, BatchSize: p.Batch, GradientAccumulationSteps: p.GradAccum,
			LR: p.LR, Optimizer: p.Optimizer, Dtype: p.Precision, TrainTextEncoder: p.TrainTextEncoder,
			GradientCheckpointing: p.GradientCheckpointing, Seed: p.Seed, EnableBucket: p.Bucketing, NoiseScheduler: noiseScheduler,
			CacheTextEmbeddings: perf.CacheTextEmbeddings, DisableSampling: perf.DisableSampling,
		},
		Model:  atModel{NameOrPath: req.BaseCheckpoint, Arch: archForBase(req.Base), IsFlux: isFlux(req.Base), Quantize: p.Quantize},
		Sample: sample,
		Extra:  extra,
	}
	return yaml.Marshal(atJob{Job: "extension", Config: atConfig{Name: req.Name, Process: []atProcess{proc}}})
}

// --- generation (ai-toolkit "generate" job) ---

type atGenerate struct {
	Sampler       string  `yaml:"sampler"`
	Width         int     `yaml:"width"`
	Height        int     `yaml:"height"`
	SampleSteps   int     `yaml:"sample_steps"`
	GuidanceScale float64 `yaml:"guidance_scale"`
	Seed          int     `yaml:"seed"`
	Neg           string  `yaml:"neg,omitempty"`
	NumRepeats    int     `yaml:"num_repeats"`
	Ext           string  `yaml:"ext"`
	// Prompts is either a []string of inline prompts or a string path to a
	// newline-delimited prompts file — ai-toolkit's GenerateProcess accepts both.
	Prompts any `yaml:"prompts"`
}

type atGenProcess struct {
	Type         string     `yaml:"type"` // "to_folder"
	OutputFolder string     `yaml:"output_folder"`
	Device       string     `yaml:"device"`
	Dtype        string     `yaml:"dtype"`
	Model        atModel    `yaml:"model"`
	Generate     atGenerate `yaml:"generate"`
}

type atGenConfig struct {
	Name    string         `yaml:"name"`
	Process []atGenProcess `yaml:"process"`
}

type atGenJob struct {
	Job    string      `yaml:"job"` // "generate"
	Config atGenConfig `yaml:"config"`
}

// buildGenerateYAML renders an ai-toolkit "generate" job: it loads the same base
// the LoRA was trained on (arch selector pulls the TE+VAE, exactly as in
// training), fuses the trained LoRA via model.lora_path, and renders images to
// OutputDir. Prompts may be inline or a file path.
func buildGenerateYAML(req GenerateRequest) ([]byte, error) {
	sampler := req.Sampler
	if sampler == "" {
		if isFluxFamily(req.Base) {
			sampler = "flowmatch"
		} else {
			sampler = "ddpm"
		}
	}
	var prompts any
	if req.PromptFile != "" {
		prompts = req.PromptFile // GenerateProcess reads lines from an existing path
	} else {
		prompts = req.Prompts
	}
	proc := atGenProcess{
		Type:         "to_folder",
		OutputFolder: req.OutputDir,
		Device:       req.Device,
		Dtype:        req.Precision,
		Model: atModel{
			NameOrPath: req.BaseCheckpoint,
			Arch:       archForBase(req.Base),
			IsFlux:     isFlux(req.Base),
			Quantize:   req.Quantize,
			LoRAPath:   req.LoRAPath,
		},
		Generate: atGenerate{
			Sampler:       sampler,
			Width:         req.Width,
			Height:        req.Height,
			SampleSteps:   req.Steps,
			GuidanceScale: req.Guidance,
			Seed:          req.Seed,
			Neg:           req.Negative,
			NumRepeats:    req.Count,
			Ext:           "png",
			Prompts:       prompts,
		},
	}
	return yaml.Marshal(atGenJob{Job: "generate", Config: atGenConfig{Name: req.Name, Process: []atGenProcess{proc}}})
}
