package interpreter

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/keeandrews/loradex-cli/internal/output"
)

// Download snapshots the interpreter's HuggingFace repo into the store and
// returns its local path. Honors ctx cancellation.
func Download(ctx context.Context, e Entry, python string, p *output.Printer) (string, error) {
	if e.Repo == "" {
		return "", output.Errorf(output.ExitValidation, "no_source", "add a repo for this interpreter", "interpreter %q has no repo", e.ID)
	}
	dir, err := slugDir(e.ID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if hf := lookPath("hf"); hf != "" {
		err = runStreaming(ctx, p, hf, "download", e.Repo, "--local-dir", dir)
	} else if hc := lookPath("huggingface-cli"); hc != "" {
		err = runStreaming(ctx, p, hc, "download", e.Repo, "--local-dir", dir)
	} else if python != "" {
		script := "import sys; from huggingface_hub import snapshot_download; " +
			"snapshot_download(repo_id=sys.argv[1], local_dir=sys.argv[2])"
		err = runStreaming(ctx, p, python, "-c", script, e.Repo, dir)
	} else {
		return "", output.Errorf(output.ExitValidation, "no_hf_downloader",
			"install the HuggingFace CLI (`loradex setup --install-hf`) or configure a trainer python",
			"no HuggingFace downloader found for %q", e.Repo)
	}
	if err != nil {
		return "", err
	}
	return dir, nil
}

func runStreaming(ctx context.Context, p *output.Printer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = strippedEnv()
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return output.Errorf(output.ExitError, "download_start_failed", "", "could not start %s: %v", name, err)
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
		return output.Errorf(output.ExitError, "download_failed",
			"for gated repos, run `hf auth login` first", "download failed: %v", err)
	}
	return nil
}

func strippedEnv() []string {
	src := os.Environ()
	out := make([]string, 0, len(src))
	for _, kv := range src {
		if len(kv) >= 8 && kv[:8] == "LORADEX_" {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func lookPath(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}
