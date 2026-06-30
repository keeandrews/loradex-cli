// Package profile resolves training hyperparameters from layered overrides
// (built-in per-base default < global config < named profile < project override
// < explicit flags), applies Apple-Silicon (MPS) overrides, derives the auto
// step count, and validates the result. trainer_extra passes through verbatim.
package profile

import (
	"fmt"
)

// Profile is the modeled subset of trainer knobs plus a passthrough.
type Profile struct {
	Base                  string
	Rank                  int
	Alpha                 int
	LR                    float64
	Optimizer             string
	Precision             string
	Resolution            int
	Batch                 int
	GradAccum             int
	SaveEvery             int
	TrainTextEncoder      bool
	Steps                 int // 0 = auto-derive
	StepsRange            [2]int
	StepsPerImage         int
	Seed                  int
	Bucketing             bool
	Quantize              bool
	GradientCheckpointing bool
	TrainerExtra          map[string]any
}

func (p Profile) clone() Profile {
	extra := map[string]any{}
	for k, v := range p.TrainerExtra {
		extra[k] = v
	}
	p.TrainerExtra = extra
	return p
}

var (
	knownOptimizers = map[string]bool{"adamw8bit": true, "adamw": true, "adafactor": true, "prodigy": true}
	knownPrecisions = map[string]bool{"bf16": true, "fp16": true, "fp32": true}
)

// Layers are the override maps in precedence order (low → high).
type Layers struct {
	GlobalBase  map[string]any // config.training.<base>
	Named       map[string]any // config.profiles.<name>
	ProjectBase map[string]any // .loradex/project.yaml training.<base>
	Flags       map[string]any // explicit build flags
	Device      string         // mps | cpu | cuda
	MemoryGB    int            // unified/system memory (drives the MPS speed/fit tradeoff)
	ImageCount  int
}

func isFluxBase(base string) bool { return base == "flux2-klein" || base == "flux1" }

// fastPathRAMGB is the unified-memory level at/above which MPS training drops the
// memory-savers (quantization, gradient checkpointing) for speed. This is only
// safe BECAUSE latent + text-embedding caching frees the VAE and text encoder
// during training (see trainer.PerfPlanFor) — that headroom is what lets the
// unquantized transformer fit. Calibrated on a 48 GB M3 Max, where FLUX.2 Klein
// 4B at 512px measured ~4.4 s/it on the speed path with no steady-state swap,
// vs ~27 s/it quantized+checkpointed and a degrading 22→80 s/it with the savers
// off but caching absent. The original 32 GB threshold here disabled the savers
// without caching, so it overflowed memory and thrashed.
const fastPathRAMGB = 48

// speedPathFits reports whether a base is small enough to train unquantized in
// fastPathRAMGB of unified memory. FLUX.1 is ~12B (~24 GB bf16) and must stay
// quantized; everything else here fits with caching freeing the encoders.
func speedPathFits(base string) bool { return base != "flux1" }

// Resolve merges layers into a concrete Profile and returns it with warnings.
func Resolve(base string, l Layers) (Profile, []string) {
	var warnings []string
	p := builtinFor(base)
	if _, ok := builtin[base]; !ok {
		warnings = append(warnings, fmt.Sprintf("no built-in profile for base %q — using a generic default", base))
	}

	// Apple-Silicon (MPS) DEFAULTS — applied before the user layers so explicit
	// flags/config still win. The tradeoff is driven by available memory, and it
	// is only valid because latent + text-embedding caching frees the encoders
	// during training (always-on in trainer.PerfPlanFor).
	if l.Device == "mps" {
		if l.MemoryGB >= fastPathRAMGB && speedPathFits(base) {
			// Ample memory + a model that fits unquantized: prefer speed. With the
			// encoders cached out, quantization (per-matmul dequant on MPS) and
			// gradient checkpointing (forward recompute) are pure per-step overhead.
			p.Quantize = false
			p.GradientCheckpointing = false
			p.GradAccum = 1
		} else {
			// Limited memory, or a model too large to fit unquantized: keep the
			// memory-savers on so it fits without swapping.
			p.GradientCheckpointing = true
			if isFluxBase(base) {
				p.Quantize = true
			}
			if p.GradAccum < 4 {
				p.GradAccum = 4
			}
		}
	}

	for _, m := range []map[string]any{l.GlobalBase, l.Named, l.ProjectBase, l.Flags} {
		applyMap(&p, m)
	}

	// Apple-Silicon hard constraints — must hold regardless of user input.
	if l.Device == "mps" {
		if p.Precision != "bf16" {
			warnings = append(warnings, fmt.Sprintf("precision %q forced to bf16 on Apple Silicon (MPS)", p.Precision))
			p.Precision = "bf16"
		}
		if p.Batch != 1 {
			warnings = append(warnings, "batch forced to 1 on MPS (use --grad-accum for effective batch)")
			p.Batch = 1
		}
		// bitsandbytes 8-bit optimizers need CUDA — switch to a MPS-capable one.
		if p.Optimizer == "adamw8bit" {
			warnings = append(warnings, "optimizer adamw8bit needs CUDA — using adafactor on MPS")
			p.Optimizer = "adafactor"
		}
	}

	// Auto step count when not explicitly set.
	if p.Steps == 0 {
		steps := l.ImageCount * p.StepsPerImage
		if steps < p.StepsRange[0] {
			steps = p.StepsRange[0]
		}
		if p.StepsRange[1] > 0 && steps > p.StepsRange[1] {
			steps = p.StepsRange[1]
		}
		p.Steps = steps
	}
	return p, warnings
}

// Validate checks the resolved profile; collects all errors + warnings.
func Validate(p Profile) (errs []string, warnings []string) {
	add := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }
	if p.Rank < 1 || p.Rank > 1024 {
		add("rank %d out of range (1–1024)", p.Rank)
	}
	if p.Alpha < 1 || p.Alpha > 1024 {
		add("alpha %d out of range (1–1024)", p.Alpha)
	}
	if p.Steps < 1 || p.Steps > 1_000_000 {
		add("steps %d out of range", p.Steps)
	}
	if p.LR <= 0 || p.LR > 1 {
		add("lr %g out of range (0–1)", p.LR)
	}
	if p.Batch < 1 {
		add("batch must be >= 1")
	}
	if p.GradAccum < 1 {
		add("grad_accum must be >= 1")
	}
	if p.Resolution < 64 || p.Resolution > 4096 {
		add("resolution %d out of range (64–4096)", p.Resolution)
	}
	if !knownOptimizers[p.Optimizer] {
		warnings = append(warnings, fmt.Sprintf("optimizer %q is not a known value — passing through", p.Optimizer))
	}
	if !knownPrecisions[p.Precision] {
		warnings = append(warnings, fmt.Sprintf("precision %q is not a known value — passing through", p.Precision))
	}
	return errs, warnings
}

// applyMap overlays known keys from m onto p; trainer_extra merges.
func applyMap(p *Profile, m map[string]any) {
	if m == nil {
		return
	}
	if v, ok := m["base"]; ok {
		if s, ok := v.(string); ok && s != "" {
			p.Base = s
		}
	}
	if v, ok := toInt(m["rank"]); ok {
		p.Rank = v
	}
	if v, ok := toInt(m["alpha"]); ok {
		p.Alpha = v
	}
	if v, ok := toFloat(m["lr"]); ok {
		p.LR = v
	}
	if v, ok := m["optimizer"].(string); ok && v != "" {
		p.Optimizer = v
	}
	if v, ok := m["precision"].(string); ok && v != "" {
		p.Precision = v
	}
	if v, ok := toInt(m["resolution"]); ok {
		p.Resolution = v
	}
	if v, ok := toInt(m["batch"]); ok {
		p.Batch = v
	}
	if v, ok := toInt(m["grad_accum"]); ok {
		p.GradAccum = v
	}
	if v, ok := toInt(m["save_every"]); ok {
		p.SaveEvery = v
	}
	if v, ok := m["train_text_encoder"].(bool); ok {
		p.TrainTextEncoder = v
	}
	if v, ok := toInt(m["steps"]); ok {
		p.Steps = v
	}
	if v, ok := toInt(m["seed"]); ok {
		p.Seed = v
	}
	if v, ok := m["bucketing"].(bool); ok {
		p.Bucketing = v
	}
	if v, ok := m["quantize"].(bool); ok {
		p.Quantize = v
	}
	if v, ok := m["gradient_checkpointing"].(bool); ok {
		p.GradientCheckpointing = v
	}
	if v, ok := toInt(m["steps_per_image"]); ok {
		p.StepsPerImage = v
	}
	if rng, ok := m["steps_range"].([]any); ok && len(rng) == 2 {
		lo, _ := toInt(rng[0])
		hi, _ := toInt(rng[1])
		p.StepsRange = [2]int{lo, hi}
	}
	if te, ok := m["trainer_extra"].(map[string]any); ok {
		if p.TrainerExtra == nil {
			p.TrainerExtra = map[string]any{}
		}
		for k, v := range te {
			p.TrainerExtra[k] = v
		}
	}
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case uint64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	}
	return 0, false
}
