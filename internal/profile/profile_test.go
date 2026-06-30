package profile

import "testing"

func TestResolve_Precedence(t *testing.T) {
	p, _ := Resolve("flux2-klein", Layers{
		GlobalBase: map[string]any{"rank": 24},
		Named:      map[string]any{"rank": 20},
		Flags:      map[string]any{"rank": 16},
		Device:     "cpu", ImageCount: 30,
	})
	if p.Rank != 16 {
		t.Errorf("flags should win: rank = %d, want 16", p.Rank)
	}
}

func TestResolve_AutoSteps_Clamped(t *testing.T) {
	// flux2-klein: StepsPerImage 42, range [1000,3000].
	p, _ := Resolve("flux2-klein", Layers{Device: "cpu", ImageCount: 5}) // 5*42=210 -> clamp up to 1000
	if p.Steps != 1000 {
		t.Errorf("auto steps = %d, want clamped to 1000", p.Steps)
	}
	p2, _ := Resolve("flux2-klein", Layers{Device: "cpu", ImageCount: 200}) // 200*42 -> clamp down to 3000
	if p2.Steps != 3000 {
		t.Errorf("auto steps = %d, want clamped to 3000", p2.Steps)
	}
	p3, _ := Resolve("flux2-klein", Layers{Device: "cpu", ImageCount: 47}) // 1974 within range
	if p3.Steps != 1974 {
		t.Errorf("auto steps = %d, want 1974", p3.Steps)
	}
}

func TestResolve_MPSForcesBf16(t *testing.T) {
	p, warns := Resolve("flux2-klein", Layers{Flags: map[string]any{"precision": "fp16"}, Device: "mps", ImageCount: 30})
	if p.Precision != "bf16" {
		t.Errorf("MPS must force bf16, got %q", p.Precision)
	}
	if len(warns) == 0 {
		t.Error("expected a warning about forced precision")
	}
	if !p.Quantize {
		t.Error("flux on MPS should enable quantize")
	}
}

func TestValidate_CollectsAll(t *testing.T) {
	errs, _ := Validate(Profile{Rank: 0, Alpha: 0, Steps: 0, LR: 2, Batch: 0, GradAccum: 0, Resolution: 10, Optimizer: "adamw8bit", Precision: "bf16"})
	if len(errs) < 5 {
		t.Errorf("expected several errors, got %d: %v", len(errs), errs)
	}
}

func TestValidate_UnknownOptimizerWarns(t *testing.T) {
	_, warns := Validate(Profile{Rank: 16, Alpha: 16, Steps: 100, LR: 1e-4, Batch: 1, GradAccum: 1, Resolution: 1024, Optimizer: "newopt", Precision: "bf16"})
	if len(warns) == 0 {
		t.Error("unknown optimizer should warn, not fail")
	}
}
