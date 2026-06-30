package profile

// Built-in per-base training profiles, tuned to current ai-toolkit guidance.
// TODO(verify-schema): confirm exact rank/alpha/lr/step ranges per base against
// the installed ai-toolkit version; keep these as the compiled-in defaults.
var builtin = map[string]Profile{
	"flux2-klein": {
		Base: "flux2-klein", Rank: 32, Alpha: 32, LR: 1e-4, Optimizer: "adamw8bit",
		Precision: "bf16", Resolution: 1024, Batch: 1, GradAccum: 4, SaveEvery: 250,
		TrainTextEncoder: false, StepsRange: [2]int{1000, 3000}, StepsPerImage: 42, Seed: 42,
		Bucketing: true, Quantize: true, GradientCheckpointing: true, // memory-savers; relaxed on high-RAM MPS
		TrainerExtra: map[string]any{},
	},
	"flux1": {
		Base: "flux1", Rank: 32, Alpha: 32, LR: 1e-4, Optimizer: "adamw8bit",
		Precision: "bf16", Resolution: 1024, Batch: 1, GradAccum: 4, SaveEvery: 250,
		StepsRange: [2]int{1000, 3000}, StepsPerImage: 42, Seed: 42, Bucketing: true, Quantize: true,
		GradientCheckpointing: true,
		TrainerExtra:          map[string]any{},
	},
	"sdxl": {
		Base: "sdxl", Rank: 32, Alpha: 16, LR: 1e-4, Optimizer: "adamw8bit",
		Precision: "bf16", Resolution: 1024, Batch: 1, GradAccum: 4, SaveEvery: 250,
		StepsRange: [2]int{1200, 4000}, StepsPerImage: 50, Seed: 42, Bucketing: true,
		GradientCheckpointing: true,
		TrainerExtra:          map[string]any{},
	},
	"sd15": {
		Base: "sd15", Rank: 16, Alpha: 8, LR: 1e-4, Optimizer: "adamw8bit",
		Precision: "fp16", Resolution: 512, Batch: 2, GradAccum: 2, SaveEvery: 250,
		StepsRange: [2]int{800, 2500}, StepsPerImage: 60, Seed: 42, Bucketing: true,
		GradientCheckpointing: true,
		TrainerExtra:          map[string]any{},
	},
}

// builtinFor returns the built-in profile for a base (a generic default if unknown).
func builtinFor(base string) Profile {
	if p, ok := builtin[base]; ok {
		return p.clone()
	}
	// Unknown base: a conservative default (warned elsewhere).
	return Profile{
		Base: base, Rank: 16, Alpha: 16, LR: 1e-4, Optimizer: "adamw8bit", Precision: "bf16",
		Resolution: 1024, Batch: 1, GradAccum: 4, SaveEvery: 250, StepsRange: [2]int{1000, 3000},
		StepsPerImage: 42, Seed: 42, Bucketing: true, GradientCheckpointing: true, TrainerExtra: map[string]any{},
	}
}
