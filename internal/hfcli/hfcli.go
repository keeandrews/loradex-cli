// Package hfcli locates, installs, and checks login for the HuggingFace CLI,
// which loradex uses to pull gated base models. Installation is via whatever
// Python tool manager is available (uv > pipx > pip --user); login status is a
// local check (token file or env), never a network call.
package hfcli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/keeandrews/loradex-cli/internal/config"
	"github.com/keeandrews/loradex-cli/internal/output"
)

// Status describes the HuggingFace CLI on this machine.
type Status struct {
	Installed bool
	Path      string // resolved `hf` (or `huggingface-cli`) binary
	LoggedIn  bool
}

// Detect finds the HuggingFace CLI and whether a token is present.
func Detect() Status {
	st := Status{Path: findHF(), LoggedIn: LoggedIn()}
	st.Installed = st.Path != ""
	return st
}

// findHF resolves an hf/huggingface-cli binary: recorded config path, then PATH,
// then ~/.local/bin, then the ai-toolkit venv.
func findHF() string {
	if f, err := config.Load(); err == nil && f.HuggingFace != nil && f.HuggingFace.Path != "" {
		if isExec(f.HuggingFace.Path) {
			return f.HuggingFace.Path
		}
	}
	for _, name := range []string{"hf", "huggingface-cli"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	cands := []string{}
	if home, err := os.UserHomeDir(); err == nil {
		cands = append(cands,
			filepath.Join(home, ".local", "bin", "hf"),
			filepath.Join(home, "ai-toolkit", "venv", "bin", "hf"),
		)
	}
	for _, c := range cands {
		if isExec(c) {
			return c
		}
	}
	return ""
}

// LoggedIn reports whether a HuggingFace token is available locally. Checks the
// standard env vars and the cached token file ($HF_HOME or ~/.cache/huggingface).
func LoggedIn() bool {
	for _, e := range []string{"HF_TOKEN", "HUGGING_FACE_HUB_TOKEN", "HUGGINGFACE_TOKEN"} {
		if strings.TrimSpace(os.Getenv(e)) != "" {
			return true
		}
	}
	if t := tokenFile(); t != "" {
		if data, err := os.ReadFile(t); err == nil && len(strings.TrimSpace(string(data))) > 0 {
			return true
		}
	}
	return false
}

// tokenFile returns the path HuggingFace caches its token at.
func tokenFile() string {
	if hf := strings.TrimSpace(os.Getenv("HF_HOME")); hf != "" {
		return filepath.Join(hf, "token")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cache", "huggingface", "token")
}

// Login runs `hf auth login` interactively, inheriting the terminal so the user
// can paste a token. Requires the CLI to be installed.
func Login(ctx context.Context) error {
	bin := findHF()
	if bin == "" {
		return output.Errorf(output.ExitValidation, "hf_missing",
			"run `loradex setup` (or `loradex setup --install-hf`) to install it",
			"the HuggingFace CLI is not installed")
	}
	cmd := exec.CommandContext(ctx, bin, "auth", "login")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = cleanEnv()
	if err := cmd.Run(); err != nil {
		return output.Errorf(output.ExitError, "hf_login_failed", "", "hf auth login failed: %v", err)
	}
	return nil
}

// Install installs the HuggingFace CLI via the first available tool manager and
// returns the resolved binary path. Streams progress.
func Install(ctx context.Context, p *output.Printer) (string, error) {
	type method struct {
		bin  string
		args []string
	}
	var methods []method
	if uv := lookEither("uv"); uv != "" {
		methods = append(methods, method{uv, []string{"tool", "install", "huggingface_hub"}})
	}
	if pipx, err := exec.LookPath("pipx"); err == nil {
		methods = append(methods, method{pipx, []string{"install", "huggingface_hub"}})
	}
	if py := lookPython(); py != "" {
		methods = append(methods, method{py, []string{"-m", "pip", "install", "--user", "huggingface_hub"}})
	}
	if len(methods) == 0 {
		return "", output.Errorf(output.ExitValidation, "no_installer",
			"install uv (https://docs.astral.sh/uv) or pipx, then re-run",
			"no Python tool manager found (uv/pipx/pip) to install the HuggingFace CLI")
	}
	var lastErr error
	for _, m := range methods {
		p.Info("installing HuggingFace CLI via %s…", filepath.Base(m.bin))
		if err := runStream(ctx, p, m.bin, m.args...); err != nil {
			lastErr = err
			continue
		}
		if path := findHF(); path != "" {
			return path, nil
		}
		lastErr = output.Errorf(output.ExitError, "hf_not_on_path",
			"ensure ~/.local/bin is on your PATH", "installed, but the hf binary is not on PATH yet")
	}
	return "", lastErr
}

func lookEither(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	// uv is sometimes only under ~/.langflow/uv or ~/.local/bin without PATH.
	if home, err := os.UserHomeDir(); err == nil {
		for _, c := range []string{filepath.Join(home, ".local", "bin", name)} {
			if isExec(c) {
				return c
			}
		}
	}
	return ""
}

func lookPython() string {
	for _, n := range []string{"python3", "python"} {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	return ""
}

func isExec(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
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
		return output.Errorf(output.ExitError, "install_failed", "", "%s failed: %v", filepath.Base(name), err)
	}
	return nil
}
