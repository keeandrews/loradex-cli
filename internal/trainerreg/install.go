package trainerreg

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/keeandrews/loradex-cli/internal/output"
)

// aiToolkitRepo is the upstream ai-toolkit git repository.
const aiToolkitRepo = "https://github.com/ostris/ai-toolkit"

// InstallAIToolkit clones ai-toolkit into dest (if needed), creates a venv, and
// installs its requirements plus torchaudio (which the sample/train path needs
// but requirements.txt omits). Streams progress. Honors ctx cancellation.
func InstallAIToolkit(ctx context.Context, dest string, p *output.Printer) (Status, error) {
	git, err := exec.LookPath("git")
	if err != nil {
		return Status{}, output.Errorf(output.ExitValidation, "git_missing",
			"install git, then re-run `loradex setup`", "git is required to install ai-toolkit")
	}
	py3, err := findPython3()
	if err != nil {
		return Status{}, output.Errorf(output.ExitValidation, "python_missing",
			"install Python 3.10+ (python3), then re-run `loradex setup`", "python3 is required to install ai-toolkit")
	}

	// 1. Clone (skip if already a clone).
	if !isAIToolkit(dest) {
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return Status{}, err
		}
		p.Info("cloning ai-toolkit → %s", dest)
		if err := runStream(ctx, p, git, "clone", "--depth", "1", aiToolkitRepo, dest); err != nil {
			return Status{}, err
		}
	} else {
		p.Info("ai-toolkit already present at %s — updating dependencies", dest)
	}

	// 2. Create the venv if missing.
	venvPy := filepath.Join(dest, "venv", "bin", "python")
	if runtime.GOOS == "windows" {
		venvPy = filepath.Join(dest, "venv", "Scripts", "python.exe")
	}
	if !isFile(venvPy) {
		p.Info("creating virtualenv…")
		if err := runStream(ctx, p, py3, "-m", "venv", filepath.Join(dest, "venv")); err != nil {
			return Status{}, err
		}
	}

	// 3. Install dependencies into the venv.
	reqs := filepath.Join(dest, "requirements.txt")
	p.Info("installing dependencies (this can take several minutes)…")
	if err := runStream(ctx, p, venvPy, "-m", "pip", "install", "--upgrade", "pip"); err != nil {
		return Status{}, err
	}
	if isFile(reqs) {
		if err := runStream(ctx, p, venvPy, "-m", "pip", "install", "-r", reqs); err != nil {
			return Status{}, installErr(err)
		}
	}
	// torchaudio is imported on the train/sample path but omitted from requirements.
	if err := runStream(ctx, p, venvPy, "-m", "pip", "install", "torchaudio"); err != nil {
		return Status{}, installErr(err)
	}

	st := Status{Spec: specOf(AIToolkit), Installed: true, Path: dest, Python: venvPy, Detail: dest}
	if !isAIToolkit(dest) {
		return st, output.Errorf(output.ExitError, "install_incomplete",
			"check the output above", "ai-toolkit install finished but run.py is missing in %s", dest)
	}
	return st, nil
}

func installErr(err error) error {
	return output.Errorf(output.ExitError, "install_failed",
		"see the pip output above; you can re-run `loradex setup` to retry", "dependency install failed: %v", err)
}

// findPython3 locates a Python 3 interpreter.
func findPython3() (string, error) {
	for _, name := range []string{"python3", "python"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("python3 not found")
}

// cleanEnv returns the current environment with loradex credentials/config
// stripped — install subprocesses (git, pip, python) must never see them.
func cleanEnv() []string {
	src := os.Environ()
	out := make([]string, 0, len(src))
	for _, kv := range src {
		k := kv
		if i := indexByte(kv, '='); i >= 0 {
			k = kv[:i]
		}
		if len(k) >= 8 && k[:8] == "LORADEX_" {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// runStream runs an argv command (no shell), relaying output to the progress stream.
func runStream(ctx context.Context, p *output.Printer, name string, args ...string) error {
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
		if ctx.Err() != nil {
			return output.Errorf(output.ExitError, "cancelled", "", "cancelled")
		}
		return output.Errorf(output.ExitError, "exec_failed", "", "%s failed: %v", filepath.Base(name), err)
	}
	return nil
}
