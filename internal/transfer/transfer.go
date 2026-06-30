// Package transfer streams uploads and downloads with bounded memory, progress,
// and integrity verification (size cap + SHA-256). All file bytes move directly
// to/from presigned URLs.
package transfer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/keeandrews/loradex-cli/internal/api"
	"github.com/keeandrews/loradex-cli/internal/output"
	"github.com/keeandrews/loradex-cli/internal/pathsafe"
	"github.com/schollz/progressbar/v3"
)

// HashFile streams a file through SHA-256, returning the lowercase hex digest and size.
func HashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func bar(p *output.Printer, size int64, label string) io.Writer {
	if !p.ProgressEnabled() {
		return io.Discard
	}
	return progressbar.NewOptions64(size,
		progressbar.OptionSetDescription(label),
		progressbar.OptionSetWriter(p.Err),
		progressbar.OptionShowBytes(true),
		progressbar.OptionShowCount(),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionSetWidth(24),
	)
}

// Download fetches df to destPath with integrity enforcement:
//   - streamed to a temp file in the destination dir
//   - aborts if bytes exceed the declared size
//   - verifies SHA-256 before atomically renaming into place
//   - never follows or overwrites a symlink
func Download(ctx context.Context, hc *http.Client, df api.DownloadFile, destPath string, p *output.Printer) error {
	if err := pathsafe.RefuseSymlink(destPath); err != nil {
		return err
	}
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(destDir, ".loradex-dl-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		tmp.Close()
		if !committed {
			os.Remove(tmpName)
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, df.URL, nil)
	if err != nil {
		return err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return &output.CLIError{Code: output.ExitNetwork, CodeName: "network_error", Message: "download failed: " + err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &output.CLIError{Code: output.ExitNetwork, CodeName: "download_failed",
			Message: fmt.Sprintf("download of %s failed: status %d", df.Name, resp.StatusCode)}
	}

	h := sha256.New()
	dst := io.MultiWriter(tmp, h, bar(p, df.Size, df.Name))
	written, err := copyCapped(dst, resp.Body, df.Size)
	if err != nil {
		return err
	}
	if written != df.Size {
		return &output.CLIError{Code: output.ExitIntegrity, CodeName: "size_mismatch",
			Message: fmt.Sprintf("%s: expected %d bytes, got %d", df.Name, df.Size, written)}
	}
	got := hex.EncodeToString(h.Sum(nil))
	if df.SHA256 != "" && got != df.SHA256 {
		return &output.CLIError{Code: output.ExitIntegrity, CodeName: "hash_mismatch",
			Message: fmt.Sprintf("%s: content hash mismatch (got %s)", df.Name, got[:16]),
			Hint:    "the file may be corrupt or tampered — not written"}
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	_ = os.Chmod(tmpName, 0o644)
	if err := pathsafe.RefuseSymlink(destPath); err != nil {
		return err
	}
	if err := os.Rename(tmpName, destPath); err != nil {
		return err
	}
	committed = true
	return nil
}

// copyCapped copies src→dst, erroring if it would exceed cap bytes.
func copyCapped(dst io.Writer, src io.Reader, cap int64) (int64, error) {
	limited := io.LimitReader(src, cap+1)
	var total int64
	buf := make([]byte, 256<<10)
	for {
		n, err := limited.Read(buf)
		if n > 0 {
			if total+int64(n) > cap {
				return total, &output.CLIError{Code: output.ExitIntegrity, CodeName: "oversize",
					Message: "download exceeded the declared size — aborting"}
			}
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return total, werr
			}
			total += int64(n)
		}
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
}

// UploadSingle PUTs filePath to a presigned target, returning the ETag (if any).
func UploadSingle(ctx context.Context, hc *http.Client, target api.UploadTarget, filePath string, size int64, p *output.Printer) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	method := target.Method
	if method == "" {
		method = http.MethodPut
	}
	body := io.Reader(f)
	if p.ProgressEnabled() {
		body = io.TeeReader(f, bar(p, size, filepath.Base(filePath)))
	}
	req, err := http.NewRequestWithContext(ctx, method, target.URL, body)
	if err != nil {
		return "", err
	}
	req.ContentLength = size
	for k, v := range target.Headers {
		req.Header.Set(k, v)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", &output.CLIError{Code: output.ExitNetwork, CodeName: "upload_failed", Message: "upload failed: " + err.Error()}
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &output.CLIError{Code: output.ExitNetwork, CodeName: "upload_failed",
			Message: fmt.Sprintf("upload of %s failed: status %d", target.Name, resp.StatusCode)}
	}
	return resp.Header.Get("ETag"), nil
}

// UploadParts uploads a file as presigned multipart parts and returns the part ETags.
func UploadParts(ctx context.Context, hc *http.Client, target api.UploadTarget, filePath string, p *output.Printer) ([]api.PartETag, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var total int64
	for _, pt := range target.Parts {
		total += pt.Size
	}
	progress := bar(p, total, filepath.Base(filePath))

	var etags []api.PartETag
	var offset int64
	for _, pt := range target.Parts {
		section := io.NewSectionReader(f, offset, pt.Size)
		body := io.Reader(section)
		if p.ProgressEnabled() {
			body = io.TeeReader(section, progress)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, pt.URL, body)
		if err != nil {
			return nil, err
		}
		req.ContentLength = pt.Size
		resp, err := hc.Do(req)
		if err != nil {
			return nil, &output.CLIError{Code: output.ExitNetwork, CodeName: "upload_failed", Message: "part upload failed: " + err.Error()}
		}
		etag := resp.Header.Get("ETag")
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, &output.CLIError{Code: output.ExitNetwork, CodeName: "upload_failed",
				Message: fmt.Sprintf("part %d upload failed: status %d", pt.PartNumber, resp.StatusCode)}
		}
		etags = append(etags, api.PartETag{PartNumber: pt.PartNumber, ETag: etag})
		offset += pt.Size
	}
	return etags, nil
}

// CompleteMultipart finalizes a multipart upload at the storage layer.
// TODO(server-contract): confirm the exact completion payload/format.
func CompleteMultipart(ctx context.Context, hc *http.Client, completeURL string, parts []api.PartETag) error {
	if completeURL == "" {
		return nil
	}
	body, _ := json.Marshal(map[string]any{"parts": parts})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, completeURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return &output.CLIError{Code: output.ExitNetwork, CodeName: "upload_failed", Message: "multipart completion failed: " + err.Error()}
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &output.CLIError{Code: output.ExitNetwork, CodeName: "upload_failed",
			Message: fmt.Sprintf("multipart completion failed: status %d", resp.StatusCode)}
	}
	return nil
}
