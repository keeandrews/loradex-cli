package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/keeandrews/loradex-cli/internal/api"
	"github.com/keeandrews/loradex-cli/internal/output"
)

func serve(body []byte) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
}

func quietPrinter() *output.Printer {
	return &output.Printer{JSON: false, Quiet: true, Out: os.Stdout, Err: os.Stderr}
}

func TestDownload_HashMatch(t *testing.T) {
	body := []byte("hello loradex")
	sum := sha256.Sum256(body)
	srv := serve(body)
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "out.bin")
	df := api.DownloadFile{Name: "out.bin", URL: srv.URL, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(body))}
	if err := Download(context.Background(), srv.Client(), df, dest, quietPrinter()); err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != string(body) {
		t.Errorf("content = %q", got)
	}
}

func TestDownload_HashMismatch(t *testing.T) {
	body := []byte("tampered")
	srv := serve(body)
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "out.bin")
	df := api.DownloadFile{Name: "out.bin", URL: srv.URL, SHA256: hex.EncodeToString(make([]byte, 32)), Size: int64(len(body))}
	err := Download(context.Background(), srv.Client(), df, dest, quietPrinter())
	if err == nil {
		t.Fatal("expected hash mismatch error")
	}
	if ce, ok := err.(*output.CLIError); !ok || ce.Code != output.ExitIntegrity {
		t.Errorf("exit code = %v, want %d", output.CodeOf(err), output.ExitIntegrity)
	}
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Error("temp file should have been removed on mismatch")
	}
	// No stray temp files left behind.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("leftover files: %v", entries)
	}
}

func TestDownload_Oversize(t *testing.T) {
	body := []byte("way more bytes than declared")
	srv := serve(body)
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "out.bin")
	df := api.DownloadFile{Name: "out.bin", URL: srv.URL, SHA256: "", Size: 4} // declare too small
	err := Download(context.Background(), srv.Client(), df, dest, quietPrinter())
	if err == nil {
		t.Fatal("expected oversize abort")
	}
	if output.CodeOf(err) != output.ExitIntegrity {
		t.Errorf("exit code = %d, want %d", output.CodeOf(err), output.ExitIntegrity)
	}
}
