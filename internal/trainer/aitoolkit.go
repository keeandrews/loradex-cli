package trainer

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/keeandrews/loradex-cli/internal/output"
	"github.com/keeandrews/loradex-cli/internal/safetensors"
)

// AIToolkit orchestrates the ai-toolkit trainer as a subprocess.
type AIToolkit struct{}

func (AIToolkit) Name() string { return "ai-toolkit" }

// ResolvePython returns the python interpreter (default <home>/venv/bin/python).
func ResolvePython(cfg Config) string {
	if cfg.Python != "" {
		return cfg.Python
	}
	if cfg.Home != "" {
		return filepath.Join(cfg.Home, "venv", "bin", "python")
	}
	return ""
}

// Detect verifies ai-toolkit is installed (never installs anything).
func (a AIToolkit) Detect(cfg Config) (Capabilities, error) {
	setup := "run `loradex setup` to install ai-toolkit (under the loradex home) or record an existing clone.\n" +
		"  or set $LORADEX_AITOOLKIT_HOME to a clone of https://github.com/ostris/ai-toolkit"
	if cfg.Home == "" {
		return Capabilities{}, &output.CLIError{Code: output.ExitValidation, CodeName: "trainer_not_configured",
			Message: "ai-toolkit is not configured", Hint: setup}
	}
	if fi, err := os.Stat(cfg.Home); err != nil || !fi.IsDir() {
		return Capabilities{}, &output.CLIError{Code: output.ExitValidation, CodeName: "trainer_not_found",
			Message: fmt.Sprintf("ai-toolkit home %q is not a directory", cfg.Home), Hint: setup}
	}
	if _, err := os.Stat(filepath.Join(cfg.Home, "run.py")); err != nil {
		return Capabilities{}, &output.CLIError{Code: output.ExitValidation, CodeName: "trainer_invalid",
			Message: fmt.Sprintf("%q does not look like ai-toolkit (no run.py)", cfg.Home), Hint: setup}
	}
	python := ResolvePython(cfg)
	if fi, err := os.Stat(python); err != nil || fi.IsDir() {
		return Capabilities{}, &output.CLIError{Code: output.ExitValidation, CodeName: "trainer_no_python",
			Message: fmt.Sprintf("python interpreter %q not found", python),
			Hint:    "set trainer.ai_toolkit.python or create the venv"}
	}
	return Capabilities{Version: "detected"}, nil
}

// Plan computes the (side-effect-free) plan.
func (a AIToolkit) Plan(req Request) (Plan, error) {
	if req.BaseCheckpoint == "" {
		req.BaseCheckpoint = resolveCheckpoint(req.Base)
	}
	return Plan{
		Req:        req,
		Trainer:    "ai-toolkit",
		Device:     req.Device,
		Steps:      req.Profile.Steps,
		ConfigPath: filepath.Join(req.CacheDir, "config.yaml"),
		OutputPath: filepath.Join(req.OutputDir, req.OutputFile),
		Quantize:   req.Profile.Quantize,
	}, nil
}

var (
	reStep = regexp.MustCompile(`(\d+)\s*/\s*(\d+)`)
	reLoss = regexp.MustCompile(`(?i)loss[:=]?\s*([0-9.]+)`)
)

// Train runs ai-toolkit via argv subprocess (no shell), streams progress, and
// collects + validates the output. Honors ctx cancellation by forwarding SIGINT.
func (a AIToolkit) Train(ctx context.Context, plan Plan, onProgress func(Progress)) (Result, error) {
	req := plan.Req
	cfg := Config{Home: trainerHome, Python: trainerPython}

	// Resolve the config file: raw escape hatch or generated.
	configPath := req.RawConfig
	if configPath == "" {
		if err := os.MkdirAll(req.CacheDir, 0o755); err != nil {
			return Result{}, err
		}
		data, err := buildConfigYAML(req)
		if err != nil {
			return Result{}, err
		}
		configPath = filepath.Join(req.CacheDir, "config.yaml")
		if err := os.WriteFile(configPath, data, 0o644); err != nil {
			return Result{}, err
		}
	}

	python := ResolvePython(cfg)
	// argv form only — never a shell. No untrusted value is interpolated.
	cmd := exec.CommandContext(ctx, python, "run.py", configPath)
	cmd.Dir = cfg.Home
	cmd.Env = childEnv(req.Device)
	// On ctrl-c, forward SIGINT (let ai-toolkit checkpoint), don't hard-kill.
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 90 * time.Second

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return Result{}, &output.CLIError{Code: output.ExitError, CodeName: "trainer_start_failed", Message: "failed to start ai-toolkit: " + err.Error()}
	}

	start := time.Now()
	var lastLoss float64
	var lastStep, totalSteps int
	var mu sync.Mutex
	tail := make([]string, 0, 40) // ring buffer of recent output for error reporting
	scan := func(r io.Reader) {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			mu.Lock()
			tail = append(tail, line)
			if len(tail) > 40 {
				tail = tail[len(tail)-40:]
			}
			mu.Unlock()
			pr := Progress{Raw: line}
			if m := reStep.FindStringSubmatch(line); m != nil {
				pr.Step, _ = strconv.Atoi(m[1])
				pr.TotalSteps, _ = strconv.Atoi(m[2])
				lastStep, totalSteps = pr.Step, pr.TotalSteps
			}
			if m := reLoss.FindStringSubmatch(line); m != nil {
				pr.Loss, _ = strconv.ParseFloat(m[1], 64)
				lastLoss = pr.Loss
			}
			if onProgress != nil {
				onProgress(pr)
			}
		}
	}
	done := make(chan struct{}, 2)
	go func() { scan(stdout); done <- struct{}{} }()
	go func() { scan(stderr); done <- struct{}{} }()
	<-done
	<-done

	err := cmd.Wait()
	if ctx.Err() != nil {
		// Cancelled — checkpoints preserved in cache; resumable.
		return Result{Stopped: true}, nil
	}
	if err != nil {
		mu.Lock()
		recent := strings.Join(tail[max(0, len(tail)-12):], "\n  ")
		mu.Unlock()
		return Result{}, &output.CLIError{Code: output.ExitError, CodeName: "training_failed",
			Message: "ai-toolkit exited with an error:\n  " + recent,
			Hint:    "cache + checkpoints preserved in " + req.CacheDir + " — fix and `loradex build --resume`"}
	}

	// Collect + validate the output.
	weights, err := findWeights(req.CacheDir)
	if err != nil {
		return Result{}, &output.CLIError{Code: output.ExitError, CodeName: "no_output",
			Message: "training finished but produced no .safetensors", Hint: "cache preserved in " + req.CacheDir}
	}
	rank, dim, err := validateSafetensors(weights)
	if err != nil {
		return Result{}, &output.CLIError{Code: output.ExitError, CodeName: "invalid_output",
			Message: "trainer output is not a valid safetensors file: " + err.Error()}
	}
	if err := os.MkdirAll(req.OutputDir, 0o755); err != nil {
		return Result{}, err
	}
	dst := filepath.Join(req.OutputDir, req.OutputFile)
	if err := moveFile(weights, dst); err != nil {
		return Result{}, err
	}
	samples := collectSamples(req.CacheDir, filepath.Join(req.OutputDir, "samples"))

	return Result{
		WeightsPath: dst, NetworkRank: rank, NetworkDim: dim,
		Metrics: Metrics{
			FinalLoss: lastLoss, StepsCompleted: orVal(lastStep, totalSteps, req.Profile.Steps),
			DurationSeconds: int(time.Since(start).Seconds()), Samples: samples,
		},
	}, nil
}

// trainerHome/trainerPython are set by the build command before Train (so the
// adapter stays stateless re: where ai-toolkit lives).
var (
	trainerHome   string
	trainerPython string
)

// Configure sets the ai-toolkit location for the adapter.
func Configure(home, python string) {
	trainerHome, trainerPython = home, python
}

// childEnv builds a minimal child environment with loradex secrets stripped.
func childEnv(device string) []string {
	var env []string
	for _, kv := range os.Environ() {
		key := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			key = kv[:i]
		}
		// Never leak loradex credentials/config into the trainer.
		if strings.HasPrefix(key, "LORADEX_") {
			continue
		}
		env = append(env, kv)
	}
	if device == "mps" {
		env = append(env,
			"PYTORCH_MPS_HIGH_WATERMARK_RATIO=0.0",
			"PYTORCH_ENABLE_MPS_FALLBACK=1",
		)
	}
	return env
}

func findWeights(cacheDir string) (string, error) {
	var best string
	var bestSize int64
	_ = filepath.WalkDir(cacheDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".safetensors") {
			return nil
		}
		if fi, e := os.Stat(p); e == nil && fi.Size() > bestSize {
			best, bestSize = p, fi.Size()
		}
		return nil
	})
	if best == "" {
		return "", fmt.Errorf("no .safetensors found")
	}
	return best, nil
}

func validateSafetensors(path string) (rank, dim int, err error) {
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() == 0 {
		return 0, 0, fmt.Errorf("missing or empty output")
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	h, err := safetensors.ReadHeader(f, fi.Size())
	if err != nil {
		return 0, 0, err
	}
	return h.NetworkRank, h.NetworkDim, nil
}

func collectSamples(cacheDir, destDir string) []string {
	var out []string
	_ = filepath.WalkDir(cacheDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if ext != ".png" && ext != ".jpg" && ext != ".jpeg" {
			return nil
		}
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			return nil
		}
		dst := filepath.Join(destDir, filepath.Base(p))
		if moveFile(p, dst) == nil {
			out = append(out, "samples/"+filepath.Base(p))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

func orVal(vals ...int) int {
	for _, v := range vals {
		if v > 0 {
			return v
		}
	}
	return 0
}
