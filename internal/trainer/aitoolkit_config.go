package trainer

// All ai-toolkit config-schema knowledge is isolated to this file. The config is
// built from typed structs and marshalled with goccy/go-yaml — never string
// concatenation — so untrusted values (trigger, paths, trainer_extra) are
// encoded as data and cannot inject keys.
//
// TODO(verify-schema): confirm the exact ai-toolkit keys against the installed
// version; this targets the sd_trainer/LoRA schema.

import "github.com/goccy/go-yaml"

type atNetwork struct {
	Type        string `yaml:"type"`
	Linear      int    `yaml:"linear"`
	LinearAlpha int    `yaml:"linear_alpha"`
}

type atDataset struct {
	FolderPath string `yaml:"folder_path"`
	CaptionExt string `yaml:"caption_ext"`
	Resolution int    `yaml:"resolution"`
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
}

type atModel struct {
	NameOrPath string `yaml:"name_or_path"`
	IsFlux     bool   `yaml:"is_flux,omitempty"`
	Quantize   bool   `yaml:"quantize,omitempty"`
}

type atSample struct {
	SampleEvery int      `yaml:"sample_every"`
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

// baseCheckpoints maps loradex base ids to ai-toolkit model ids/paths.
// TODO(verify-schema): confirm exact model ids for the installed ai-toolkit.
var baseCheckpoints = map[string]string{
	"flux2-klein": "black-forest-labs/FLUX.2-klein-4B",
	"flux1":       "black-forest-labs/FLUX.1-dev",
	"sdxl":        "stabilityai/stable-diffusion-xl-base-1.0",
	"sd15":        "runwayml/stable-diffusion-v1-5",
}

func resolveCheckpoint(base string) string {
	if c, ok := baseCheckpoints[base]; ok {
		return c
	}
	return base
}

func isFlux(base string) bool { return base == "flux2-klein" || base == "flux1" }

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

// buildConfigYAML renders the ai-toolkit config for a request.
func buildConfigYAML(req Request) ([]byte, error) {
	p := req.Profile
	extra := map[string]any{}
	for k, v := range p.TrainerExtra {
		extra[k] = v
	}
	// Validation sampling is opt-in (--samples N). When 0, emit no prompts and
	// push sample_every past the run so ai-toolkit never generates samples.
	sample := atSample{SampleEvery: p.Steps + 1, Width: p.Resolution, Height: p.Resolution, Seed: p.Seed, Prompts: []string{}}
	if req.Samples > 0 {
		sample.SampleEvery = p.SaveEvery
		sample.Prompts = samplePrompts(req, req.Samples)
	}
	proc := atProcess{
		Type:           "sd_trainer",
		TrainingFolder: req.CacheDir,
		Device:         req.Device,
		TriggerWord:    req.Trigger,
		Network:        atNetwork{Type: "lora", Linear: p.Rank, LinearAlpha: p.Alpha},
		Save:           atSave{Dtype: p.Precision, SaveEvery: p.SaveEvery, MaxStepSavesToKeep: 4},
		Datasets:       []atDataset{{FolderPath: req.DatasetDir, CaptionExt: "txt", Resolution: p.Resolution}},
		Train: atTrain{
			Steps: p.Steps, BatchSize: p.Batch, GradientAccumulationSteps: p.GradAccum,
			LR: p.LR, Optimizer: p.Optimizer, Dtype: p.Precision, TrainTextEncoder: p.TrainTextEncoder,
			GradientCheckpointing: true, Seed: p.Seed, EnableBucket: p.Bucketing,
		},
		Model:  atModel{NameOrPath: req.BaseCheckpoint, IsFlux: isFlux(req.Base), Quantize: p.Quantize},
		Sample: sample,
		Extra:  extra,
	}
	return yaml.Marshal(atJob{Job: "extension", Config: atConfig{Name: req.Name, Process: []atProcess{proc}}})
}
