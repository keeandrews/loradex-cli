// Package convert turns a LoRA file from one format into another via a managed
// Python venv (safetensors/mlx/numpy). The conversion logic lives in the
// embedded convert.py; this file manages the toolchain and orchestration.
//
// safetensors↔mlx are faithful; diffusers is a best-effort key remap; Draw
// Things read/write are experimental (proprietary float32-only handling).
package convert

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/keeandrews/loradex-cli/internal/config"
	"github.com/keeandrews/loradex-cli/internal/output"
)

//go:embed convert.py
var convertScript []byte

// Format is a LoRA serialization format.
type Format string

const (
	Safetensors Format = "safetensors"
	MLX         Format = "mlx"
	Diffusers   Format = "diffusers"
	DrawThings  Format = "drawthings"
)

// Targets are the formats convert can produce.
var Targets = []Format{Safetensors, MLX, Diffusers, DrawThings}

// Ext returns the output file extension for a format.
func (f Format) Ext() string {
	if f == DrawThings {
		return ".ckpt"
	}
	return ".safetensors" // mlx/diffusers/safetensors all use the safetensors container
}

// Result is the parsed outcome of a conversion.
type Result struct {
	OK       bool     `json:"ok"`
	Tensors  int      `json:"tensors"`
	Warnings []string `json:"warnings"`
	Quality  string   `json:"quality"` // faithful | experimental
	Error    string   `json:"error"`
}

// DetectFormat infers a file's source format from its name and magic bytes.
func DetectFormat(path string) (Format, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	magic := make([]byte, 16)
	_, _ = f.Read(magic)
	if bytes.HasPrefix(magic, []byte("SQLite format 3\x00")) {
		return DrawThings, nil
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".safetensors":
		return Safetensors, nil
	case ".ckpt":
		return DrawThings, nil
	}
	// safetensors begins with an 8-byte little-endian header length; treat
	// anything else readable as safetensors and let the converter validate.
	return Safetensors, nil
}

// Convert runs convert.py for one (src→dst) conversion, writing outPath.
func Convert(ctx context.Context, src string, srcFmt, dstFmt Format, outPath string, p *output.Printer) (Result, error) {
	python, err := EnsureTools(ctx, p, dstFmt == MLX)
	if err != nil {
		return Result{}, err
	}
	script, err := writeScript()
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return Result{}, err
	}
	cmd := exec.CommandContext(ctx, python, script, src, string(srcFmt), string(dstFmt), outPath)
	cmd.Env = cleanEnv()
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return Result{}, output.Errorf(output.ExitError, "convert_failed", "", "could not start converter: %v", err)
	}
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			if p.ProgressEnabled() {
				fmt.Fprintf(p.Err, "  %s\n", sc.Text())
			}
		}
	}()
	waitErr := cmd.Wait()

	var res Result
	if line := lastJSONLine(stdout.Bytes()); line != nil {
		_ = json.Unmarshal(line, &res)
	}
	if !res.OK {
		msg := res.Error
		if msg == "" && waitErr != nil {
			msg = waitErr.Error()
		}
		return res, output.Errorf(output.ExitError, "convert_failed",
			"check the converter output above", "conversion failed: %s", orStr(msg, "unknown error"))
	}
	return res, nil
}

// EnsureTools creates/returns the managed conversion venv python, installing
// safetensors+numpy (and mlx when needed). Reuses the venv across runs.
func EnsureTools(ctx context.Context, p *output.Printer, needMLX bool) (string, error) {
	base, err := config.Dir()
	if err != nil {
		return "", err
	}
	toolsDir := filepath.Join(base, "tools")
	venv := filepath.Join(toolsDir, "venv")
	venvPy := filepath.Join(venv, "bin", "python")
	if runtime.GOOS == "windows" {
		venvPy = filepath.Join(venv, "Scripts", "python.exe")
	}

	if !isFile(venvPy) {
		py3, err := findPython3()
		if err != nil {
			return "", output.Errorf(output.ExitValidation, "python_missing",
				"install Python 3.10+ (python3)", "python3 is required for format conversion")
		}
		p.Info("setting up the conversion toolchain (one-time)…")
		if err := run(ctx, p, py3, "-m", "venv", venv); err != nil {
			return "", err
		}
	}
	if !importsOK(ctx, venvPy, "safetensors", "numpy") {
		p.Info("installing conversion dependencies (safetensors, numpy)…")
		_ = run(ctx, p, venvPy, "-m", "pip", "install", "--upgrade", "pip")
		if err := run(ctx, p, venvPy, "-m", "pip", "install", "safetensors", "numpy"); err != nil {
			return "", output.Errorf(output.ExitError, "deps_failed", "", "could not install conversion deps: %v", err)
		}
	}
	if needMLX && !importsOK(ctx, venvPy, "mlx.core") {
		p.Info("installing MLX (Apple Silicon)…")
		if err := run(ctx, p, venvPy, "-m", "pip", "install", "mlx"); err != nil {
			return "", output.Errorf(output.ExitValidation, "mlx_unavailable",
				"MLX requires Apple Silicon; pick another target format", "could not install MLX: %v", err)
		}
	}
	return venvPy, nil
}

func writeScript() (string, error) {
	base, err := config.Dir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "tools")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "convert.py")
	if err := os.WriteFile(path, convertScript, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func importsOK(ctx context.Context, python string, mods ...string) bool {
	args := []string{"-c", "import " + strings.Join(mods, ", ")}
	cmd := exec.CommandContext(ctx, python, args...)
	cmd.Env = cleanEnv()
	return cmd.Run() == nil
}

func findPython3() (string, error) {
	for _, n := range []string{"python3", "python"} {
		if p, err := exec.LookPath(n); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("python3 not found")
}

func run(ctx context.Context, p *output.Printer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = cleanEnv()
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return output.Errorf(output.ExitError, "exec_failed", "", "could not start %s: %v", filepath.Base(name), err)
	}
	relay := func(r io.Reader) {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			if p.ProgressEnabled() {
				fmt.Fprintf(p.Err, "  %s\n", sc.Text())
			}
		}
	}
	done := make(chan struct{}, 2)
	go func() { relay(stdout); done <- struct{}{} }()
	go func() { relay(stderr); done <- struct{}{} }()
	<-done
	<-done
	if err := cmd.Wait(); err != nil {
		return output.Errorf(output.ExitError, "exec_failed", "", "%s failed: %v", filepath.Base(name), err)
	}
	return nil
}

func lastJSONLine(b []byte) []byte {
	var last []byte
	for _, ln := range bytes.Split(b, []byte("\n")) {
		t := bytes.TrimSpace(ln)
		if len(t) > 0 && t[0] == '{' {
			last = t
		}
	}
	return last
}

func cleanEnv() []string {
	src := os.Environ()
	out := make([]string, 0, len(src))
	for _, kv := range src {
		if strings.HasPrefix(kv, "LORADEX_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func orStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
