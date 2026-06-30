// Package caption runs a vision-language interpreter over a dataset folder to
// generate per-image training captions. The model is loaded by the embedded
// caption.py, executed with the trainer's Python (which already has torch +
// transformers); loradex never bundles those heavy deps itself.
package caption

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/keeandrews/loradex-cli/internal/output"
)

//go:embed caption.py
var captionScript []byte

// DefaultPrompt is the instruction given to the interpreter for each image.
const DefaultPrompt = "Describe this image in one detailed paragraph for training an image model. " +
	"Cover the main subject and their appearance, clothing, pose and expression, plus the setting, " +
	"background, framing, and lighting. Be specific and factual. Do not begin with \"The image shows\"."

// Result summarizes a captioning run.
type Result struct {
	OK        bool   `json:"ok"`
	Captioned int    `json:"captioned"`
	Total     int    `json:"total"`
	Error     string `json:"error"`
}

// Run captions every image in imageDir using the interpreter at modelPath, run
// via the given python. A non-empty trigger is prepended to each caption.
func Run(ctx context.Context, python, modelPath, imageDir, prompt, trigger string, p *output.Printer) (Result, error) {
	if python == "" {
		return Result{}, output.Errorf(output.ExitValidation, "no_python",
			"run `loradex setup` to configure a trainer (its Python runs the interpreter)",
			"no Python available to run the interpreter")
	}
	if prompt == "" {
		prompt = DefaultPrompt
	}
	script, err := writeScript()
	if err != nil {
		return Result{}, err
	}
	args := []string{script, modelPath, imageDir, prompt}
	if trigger != "" {
		args = append(args, trigger)
	}
	cmd := exec.CommandContext(ctx, python, args...)
	cmd.Env = strippedEnv()
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return Result{}, output.Errorf(output.ExitError, "caption_start_failed", "", "could not start the interpreter: %v", err)
	}

	var res Result
	done := make(chan struct{}, 2)
	go func() { // stdout: JSON lines (per-image + final summary)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			var m map[string]any
			if json.Unmarshal([]byte(line), &m) != nil {
				continue
			}
			if cap, ok := m["caption"].(string); ok {
				p.Info("  %s → %s", m["image"], cap)
			} else if _, ok := m["ok"]; ok {
				_ = json.Unmarshal([]byte(line), &res)
			}
		}
		done <- struct{}{}
	}()
	go func() { // stderr: progress
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			if p.ProgressEnabled() {
				fmt.Fprintf(p.Err, "  %s\n", sc.Text())
			}
		}
		done <- struct{}{}
	}()
	<-done
	<-done

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return res, output.Errorf(output.ExitError, "cancelled", "", "captioning cancelled")
		}
		return res, output.Errorf(output.ExitError, "caption_failed",
			"check the interpreter output above", "captioning failed: %v", err)
	}
	if !res.OK {
		return res, output.Errorf(output.ExitError, "caption_failed", "", "captioning failed: %s", orStr(res.Error, "unknown error"))
	}
	return res, nil
}

func writeScript() (string, error) {
	dir, err := os.MkdirTemp("", "loradex-caption-")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "caption.py")
	if err := os.WriteFile(path, captionScript, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func strippedEnv() []string {
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

func orStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
