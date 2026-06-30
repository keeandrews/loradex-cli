// Package trainer abstracts local LoRA training backends. v1 ships one adapter
// (ai-toolkit). The CLI never trains itself — adapters orchestrate a subprocess.
package trainer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/keeandrews/loradex-cli/internal/profile"
)

// Config locates a backend install.
type Config struct {
	Home   string
	Python string
}

// Capabilities describes the resolved training device.
type Capabilities struct {
	Device     string // mps | cpu | cuda
	DeviceName string
	MemoryGB   int
	Version    string // backend version (best effort)
}

// Request is a fully-resolved training request.
type Request struct {
	Name           string
	Base           string
	BaseCheckpoint string
	Trigger        string
	DatasetDir     string
	CaptionMode    string
	// CaptionsHaveTrigger: the per-image .txt captions already include the
	// trigger (we generated them), so the trainer must not inject it again.
	CaptionsHaveTrigger bool
	Profile             profile.Profile
	Device              string
	CacheDir            string // .loradex/cache/<run-id>
	OutputDir           string // version dir to collect results into
	OutputFile          string // final .safetensors filename
	RawConfig           string // --config escape hatch (path); skips generation
	Samples             int
	RunID               string
}

// Plan is the displayable, side-effect-free training plan.
type Plan struct {
	Req        Request
	Trainer    string
	Device     string
	DeviceName string
	MemoryGB   int
	Steps      int
	ConfigPath string
	OutputPath string // final collected .safetensors path
	Quantize   bool
}

// Progress is a streamed training update.
type Progress struct {
	Step       int
	TotalSteps int
	Loss       float64
	Raw        string
}

// Metrics are the final training metrics.
type Metrics struct {
	FinalLoss       float64  `json:"final_loss"`
	StepsCompleted  int      `json:"steps_completed"`
	DurationSeconds int      `json:"duration_seconds"`
	PeakMemoryBytes int64    `json:"peak_memory_bytes"`
	Checkpoints     []string `json:"checkpoints"`
	Samples         []string `json:"samples"`
}

// Result is the outcome of a training run.
type Result struct {
	WeightsPath string
	NetworkRank int
	NetworkDim  int
	Metrics     Metrics
	Stopped     bool // ctrl-c → resumable
}

// GenerateRequest is a fully-resolved image-generation request. It loads the
// same base the LoRA trained on and fuses the LoRA at load.
type GenerateRequest struct {
	Name           string
	Base           string
	BaseCheckpoint string   // name_or_path (the base model dir, as in training)
	LoRAPath       string   // trained .safetensors to fuse
	Prompts        []string // inline prompts (ignored when PromptFile is set)
	PromptFile     string   // path to a newline-delimited prompts file
	Negative       string
	Width          int
	Height         int
	Steps          int     // diffusion steps (sample_steps)
	Guidance       float64 // guidance scale
	Seed           int     // -1 = random per image
	Count          int     // images per prompt (num_repeats)
	Sampler        string  // "" → flowmatch for FLUX, else ddpm
	Precision      string  // dtype, e.g. bf16
	Quantize       bool
	Device         string
	OutputDir      string
}

// GenerateProgress is a streamed generation update (raw trainer output line).
type GenerateProgress struct {
	Raw    string
	Loaded bool // true once the model has finished loading and rendering begins
}

// GenerateResult lists the rendered images (absolute paths).
type GenerateResult struct {
	Images []string
}

// Trainer is the backend interface. cmd/build depends only on this.
type Trainer interface {
	Name() string
	Detect(cfg Config) (Capabilities, error)
	Plan(req Request) (Plan, error)
	Train(ctx context.Context, plan Plan, onProgress func(Progress)) (Result, error)
}

// NewRunID returns a sortable, unique run id.
func NewRunID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return time.Now().UTC().Format("20060102-150405") + "-" + hex.EncodeToString(b[:])
}

// DetectDevice resolves the training device (auto → MPS on Apple Silicon).
func DetectDevice(requested string) Capabilities {
	if requested != "" && requested != "auto" {
		return Capabilities{Device: requested, DeviceName: requested}
	}
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		return Capabilities{Device: "mps", DeviceName: sysctl("machdep.cpu.brand_string", "Apple Silicon"), MemoryGB: memGB()}
	}
	// NVIDIA detection is out of scope for v1; default to CPU.
	return Capabilities{Device: "cpu", DeviceName: runtime.GOOS + "/" + runtime.GOARCH}
}

func sysctl(key, def string) string {
	out, err := exec.Command("sysctl", "-n", key).Output()
	if err != nil {
		return def
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return def
	}
	return s
}

func memGB() int {
	out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0
	}
	return int(n / (1024 * 1024 * 1024))
}
