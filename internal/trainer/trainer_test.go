package trainer

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/keeandrews/loradex-cli/internal/profile"
)

func TestChildEnv_StripsLoradexSecrets(t *testing.T) {
	t.Setenv("LORADEX_TOKEN", "lor9_secret")
	t.Setenv("LORADEX_ENDPOINT", "https://api.loradex.ai")
	t.Setenv("HF_TOKEN", "hf_user_owned") // user's own — kept
	env := childEnv("mps")
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "LORADEX_") {
		t.Errorf("loradex secret leaked into child env")
	}
	if !strings.Contains(joined, "HF_TOKEN=hf_user_owned") {
		t.Errorf("user env should be preserved")
	}
	if !strings.Contains(joined, "PYTORCH_MPS_HIGH_WATERMARK_RATIO") {
		t.Errorf("MPS env var missing")
	}
}

func TestBuildConfig_NoInjection(t *testing.T) {
	req := Request{
		Name: "x", Base: "flux2-klein", Trigger: "evil: \nkey: injected\n#${VAR}", DatasetDir: "/d", BaseCheckpoint: "ckpt",
		Profile: profile.Profile{
			Rank: 16, Alpha: 16, Steps: 100, LR: 1e-4, Optimizer: "adamw8bit", Precision: "bf16",
			Resolution: 1024, Batch: 1, GradAccum: 4, SaveEvery: 50, Seed: 42, Bucketing: true,
			TrainerExtra: map[string]any{"network_kwargs": map[string]any{"only_if_contains": []string{"foo"}}},
		},
	}
	data, err := buildConfigYAML(req)
	if err != nil {
		t.Fatal(err)
	}
	// Round-trip: the trigger must survive as a single data value (no key injection).
	var got map[string]any
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("generated YAML is invalid: %v", err)
	}
	proc := got["config"].(map[string]any)["process"].([]any)[0].(map[string]any)
	if proc["trigger_word"] != req.Trigger {
		t.Errorf("trigger not preserved as data: %q", proc["trigger_word"])
	}
	if _, leaked := got["key"]; leaked {
		t.Errorf("injection created a top-level key")
	}
	if _, ok := proc["network_kwargs"]; !ok {
		t.Errorf("trainer_extra key not passed through under process")
	}
}

func TestBuildGenerate_FluxDefaultsAndLoRA(t *testing.T) {
	req := GenerateRequest{
		Name: "g", Base: "flux2-klein", BaseCheckpoint: "/models/flux2-klein", LoRAPath: "/v1/lora.safetensors",
		Prompts: []string{"ohwxman as an astronaut"}, Width: 1024, Height: 1024, Steps: 25,
		Guidance: 4.0, Seed: -1, Count: 2, Precision: "bf16", Quantize: true, Device: "mps", OutputDir: "/out",
	}
	data, err := buildGenerateYAML(req)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("invalid YAML: %v", err)
	}
	if got["job"] != "generate" {
		t.Errorf("job = %v, want generate", got["job"])
	}
	proc := got["config"].(map[string]any)["process"].([]any)[0].(map[string]any)
	model := proc["model"].(map[string]any)
	if model["arch"] != "flux2_klein_4b" {
		t.Errorf("arch = %v, want flux2_klein_4b", model["arch"])
	}
	if model["lora_path"] != req.LoRAPath {
		t.Errorf("lora_path = %v, want %s", model["lora_path"], req.LoRAPath)
	}
	gen := proc["generate"].(map[string]any)
	if gen["sampler"] != "flowmatch" {
		t.Errorf("sampler = %v, want flowmatch (FLUX default)", gen["sampler"])
	}
	if prompts, ok := gen["prompts"].([]any); !ok || len(prompts) != 1 {
		t.Errorf("inline prompts not rendered as a list: %v", gen["prompts"])
	}
}

func TestBuildGenerate_PromptFileTakesPrecedence(t *testing.T) {
	req := GenerateRequest{
		Name: "g", Base: "flux2-klein", BaseCheckpoint: "/m", LoRAPath: "/l.safetensors",
		Prompts: []string{"ignored"}, PromptFile: "/tmp/prompts.txt", Width: 1024, Height: 1024,
		Steps: 20, Guidance: 4, Count: 1, OutputDir: "/out",
	}
	data, err := buildGenerateYAML(req)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = yaml.Unmarshal(data, &got)
	gen := got["config"].(map[string]any)["process"].([]any)[0].(map[string]any)["generate"].(map[string]any)
	if gen["prompts"] != "/tmp/prompts.txt" {
		t.Errorf("prompts = %v, want the file path", gen["prompts"])
	}
}

func TestDetect_MissingHome(t *testing.T) {
	if _, err := (AIToolkit{}).Detect(Config{}); err == nil {
		t.Error("expected error when home is unset")
	}
	if _, err := (AIToolkit{}).Detect(Config{Home: t.TempDir()}); err == nil {
		t.Error("expected error when run.py is missing")
	}
}

func writeFakeSafetensors(t *testing.T, path string) {
	t.Helper()
	header := `{"lora_down.weight":{"dtype":"F16","shape":[16,32]}}`
	var buf []byte
	lenBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(lenBuf, uint64(len(header)))
	buf = append(buf, lenBuf...)
	buf = append(buf, []byte(header)...)
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
}

func setupFakeTrainer(t *testing.T, script string) (home, python string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-trainer test uses /bin/sh")
	}
	home = t.TempDir()
	os.WriteFile(filepath.Join(home, "run.py"), []byte("# fake"), 0o644)
	python = filepath.Join(home, "fakepython")
	os.WriteFile(python, []byte(script), 0o755)
	return home, python
}

func TestTrain_CollectsValidOutput(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src.safetensors")
	writeFakeSafetensors(t, src)
	t.Setenv("FAKE_SAFETENSORS", src)

	// Fake "python": args are run.py <configPath>; write a .safetensors into the cache dir.
	home, python := setupFakeTrainer(t, `#!/bin/sh
out="$(dirname "$2")/model.safetensors"
cp "$FAKE_SAFETENSORS" "$out"
echo "step 100/100 loss: 0.05"
exit 0
`)
	Configure(home, python)

	cache := t.TempDir()
	outDir := t.TempDir()
	req := Request{Name: "x", Base: "flux2-klein", BaseCheckpoint: "ckpt", DatasetDir: t.TempDir(),
		Profile: profile.Profile{Rank: 16, Alpha: 16, Steps: 100, LR: 1e-4, Optimizer: "adamw8bit", Precision: "bf16", Resolution: 1024, Batch: 1, GradAccum: 4, SaveEvery: 50},
		Device:  "cpu", CacheDir: cache, OutputDir: outDir, OutputFile: "final.safetensors"}
	plan, _ := (AIToolkit{}).Plan(req)
	res, err := (AIToolkit{}).Train(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("Train: %v", err)
	}
	if res.NetworkRank != 16 {
		t.Errorf("rank from header = %d, want 16", res.NetworkRank)
	}
	if _, err := os.Stat(filepath.Join(outDir, "final.safetensors")); err != nil {
		t.Errorf("output not collected: %v", err)
	}
}

func TestTrain_NoOutputNoVersion(t *testing.T) {
	home, python := setupFakeTrainer(t, "#!/bin/sh\nexit 0\n") // produces nothing
	Configure(home, python)
	req := Request{Name: "x", Base: "flux2-klein", BaseCheckpoint: "ckpt", DatasetDir: t.TempDir(),
		Profile: profile.Profile{Rank: 16, Alpha: 16, Steps: 10, LR: 1e-4, Optimizer: "adamw8bit", Precision: "bf16", Resolution: 1024, Batch: 1, GradAccum: 1, SaveEvery: 5},
		Device:  "cpu", CacheDir: t.TempDir(), OutputDir: t.TempDir(), OutputFile: "final.safetensors"}
	plan, _ := (AIToolkit{}).Plan(req)
	if _, err := (AIToolkit{}).Train(context.Background(), plan, nil); err == nil {
		t.Error("expected error when no output produced")
	}
}

func TestTrain_CancelIsResumable(t *testing.T) {
	home, python := setupFakeTrainer(t, "#!/bin/sh\nsleep 10\n")
	Configure(home, python)
	req := Request{Name: "x", Base: "flux2-klein", BaseCheckpoint: "ckpt", DatasetDir: t.TempDir(),
		Profile: profile.Profile{Rank: 16, Alpha: 16, Steps: 10, LR: 1e-4, Optimizer: "adamw8bit", Precision: "bf16", Resolution: 1024, Batch: 1, GradAccum: 1, SaveEvery: 5},
		Device:  "cpu", CacheDir: t.TempDir(), OutputDir: t.TempDir(), OutputFile: "final.safetensors"}
	plan, _ := (AIToolkit{}).Plan(req)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(300 * time.Millisecond); cancel() }()
	res, err := (AIToolkit{}).Train(ctx, plan, nil)
	if err != nil {
		t.Fatalf("cancel should not error: %v", err)
	}
	if !res.Stopped {
		t.Error("expected Stopped=true after cancel")
	}
}
