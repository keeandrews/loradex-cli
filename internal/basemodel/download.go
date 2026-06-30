package basemodel

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/keeandrews/loradex-cli/internal/output"
	"github.com/keeandrews/loradex-cli/internal/pathsafe"
	"github.com/schollz/progressbar/v3"
)

// Download fetches an entry into the store and returns its local path. Repo
// entries snapshot from HuggingFace; URL entries stream a single file over
// HTTPS with optional SHA-256 verification. Honors ctx cancellation.
func Download(ctx context.Context, e Entry, python string, p *output.Printer) (string, error) {
	dir, err := slugDir(e.ID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	switch {
	case e.Repo != "":
		if err := downloadHF(ctx, e, dir, python, p); err != nil {
			return "", err
		}
	case e.URL != "":
		if err := downloadURL(ctx, e, dir, p); err != nil {
			return "", err
		}
	default:
		return "", output.Errorf(output.ExitValidation, "no_source",
			"add a repo or url for this model", "model %q has no repo or url", e.ID)
	}
	return LocalPath(e)
}

// downloadHF snapshots a HuggingFace repo into dir. It prefers the `hf` CLI,
// then `huggingface-cli`, then the trainer's python (huggingface_hub). Auth for
// gated repos uses the ambient HF login / HF_TOKEN.
func downloadHF(ctx context.Context, e Entry, dir, python string, p *output.Printer) error {
	if hf := lookPath("hf"); hf != "" {
		return runStreaming(ctx, p, hf, "download", e.Repo, "--local-dir", dir)
	}
	if hc := lookPath("huggingface-cli"); hc != "" {
		return runStreaming(ctx, p, hc, "download", e.Repo, "--local-dir", dir)
	}
	if python != "" {
		// argv form only; the repo id and dir are passed as separate argv, not
		// interpolated into the script source.
		script := "import sys; from huggingface_hub import snapshot_download; " +
			"snapshot_download(repo_id=sys.argv[1], local_dir=sys.argv[2])"
		return runStreaming(ctx, p, python, "-c", script, e.Repo, dir)
	}
	return output.Errorf(output.ExitValidation, "no_hf_downloader",
		"install the HuggingFace CLI (`pip install huggingface_hub[cli]`) or configure trainer.ai_toolkit.python",
		"no HuggingFace downloader found for %q", e.Repo)
}

// downloadURL streams a single file over HTTPS into dir, refusing non-HTTPS and
// verifying SHA-256 when the entry declares one.
func downloadURL(ctx context.Context, e Entry, dir string, p *output.Printer) error {
	if !strings.HasPrefix(strings.ToLower(e.URL), "https://") {
		return output.Errorf(output.ExitValidation, "insecure_url", "use an https:// URL", "refusing non-HTTPS URL for %q", e.ID)
	}
	dst := filepath.Join(dir, fileName(e))
	if err := pathsafe.RefuseSymlink(dst); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.URL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return output.Errorf(output.ExitNetwork, "download_failed", "check the URL and your connection", "download failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return output.Errorf(output.ExitNetwork, "download_failed", "", "download failed: HTTP %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp(dir, ".dl-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	h := sha256.New()
	bar := progressbar.NewOptions64(resp.ContentLength,
		progressbar.OptionSetDescription("downloading "+e.ID),
		progressbar.OptionSetWriter(p.Err),
		progressbar.OptionShowBytes(true),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionSetVisibility(p.ProgressEnabled()),
	)
	if _, err := io.Copy(io.MultiWriter(tmp, h, bar), resp.Body); err != nil {
		tmp.Close()
		return output.Errorf(output.ExitNetwork, "download_failed", "", "download interrupted: %v", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if e.SHA256 != "" {
		got := hex.EncodeToString(h.Sum(nil))
		if !strings.EqualFold(got, e.SHA256) {
			return output.Errorf(output.ExitIntegrity, "hash_mismatch",
				"the file does not match the cataloged sha256", "sha256 mismatch: got %s, want %s", got, e.SHA256)
		}
	}
	return os.Rename(tmpName, dst)
}

// runStreaming runs an argv command, relaying its output to the progress stream.
// loradex credentials/config are stripped from the child environment.
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
			"for gated repos, run `hf auth login` first and accept the model license on huggingface.co",
			"download failed: %v", err)
	}
	return nil
}

// strippedEnv is the current environment without loradex credentials/config.
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

// lookPath is exec.LookPath returning "" on miss.
func lookPath(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}
