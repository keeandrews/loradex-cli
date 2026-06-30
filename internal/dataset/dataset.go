// Package dataset ingests, validates, caps, and deterministically hashes a
// training image set. Captions are co-located <stem>.txt files.
package dataset

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/keeandrews/loradex-cli/internal/pathsafe"
)

// Caps (documented; mirror server-side intent).
const (
	MaxImages       = 500
	MaxImageBytes   = 50_000_000    // 50 MB
	MaxTotalBytes   = 5_000_000_000 // 5 GB
	MaxCaptionBytes = 64_000        // 64 KB
)

// allowed image content types (webp intentionally excluded for v1).
var allowedImage = map[string]string{"image/png": "png", "image/jpeg": "jpg"}

// Config is dataset/dataset.yaml.
type Config struct {
	Version        int      `yaml:"version"`
	Source         string   `yaml:"source"` // ingested | external
	ExternalPath   string   `yaml:"external_path,omitempty"`
	ImageCount     int      `yaml:"image_count"`
	Formats        []string `yaml:"formats"`
	ContentHash    string   `yaml:"content_hash"`
	CaptionMode    string   `yaml:"caption_mode"`
	CaptionModel   string   `yaml:"caption_model,omitempty"`
	ResolutionHint int      `yaml:"resolution_hint,omitempty"`
	UpdatedAt      string   `yaml:"updated_at"`
}

// Summary is the result of ingesting/validating a dataset.
type Summary struct {
	ImageCount  int
	Formats     []string
	Hash        string
	Skipped     []string
	HasCaptions bool
}

func sniff(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	return strings.SplitN(http.DetectContentType(buf[:n]), ";", 2)[0], nil
}

// Ingest copies images + captions from srcDir into datasetDir (path-safe, capped).
func Ingest(srcDir, datasetDir string) (*Summary, error) {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(datasetDir, 0o755); err != nil {
		return nil, err
	}
	s := &Summary{}
	formats := map[string]bool{}
	var total int64

	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || e.IsDir() {
			continue
		}
		full := filepath.Join(srcDir, name)
		fi, err := os.Lstat(full)
		if err != nil || fi.Mode()&os.ModeSymlink != 0 {
			s.Skipped = append(s.Skipped, name+" (symlink/unreadable)")
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		if ext == ".txt" {
			continue // captions handled alongside images
		}
		ct, err := sniff(full)
		fmtName, ok := allowedImage[ct]
		if err != nil || !ok {
			s.Skipped = append(s.Skipped, fmt.Sprintf("%s (%s — unsupported)", name, ct))
			continue
		}
		if fi.Size() > MaxImageBytes {
			s.Skipped = append(s.Skipped, fmt.Sprintf("%s (%d bytes > cap)", name, fi.Size()))
			continue
		}
		if s.ImageCount >= MaxImages {
			s.Skipped = append(s.Skipped, fmt.Sprintf("%s (max %d images)", name, MaxImages))
			continue
		}
		if total+fi.Size() > MaxTotalBytes {
			return nil, fmt.Errorf("dataset exceeds total size cap (%d bytes)", MaxTotalBytes)
		}

		dst, err := pathsafe.SafeJoin(datasetDir, name)
		if err != nil {
			return nil, err
		}
		if err := copyFile(full, dst); err != nil {
			return nil, err
		}
		// Co-located caption.
		capName := strings.TrimSuffix(name, filepath.Ext(name)) + ".txt"
		if cfi, err := os.Stat(filepath.Join(srcDir, capName)); err == nil && cfi.Size() <= MaxCaptionBytes {
			cdst, err := pathsafe.SafeJoin(datasetDir, capName)
			if err == nil {
				_ = copyFile(filepath.Join(srcDir, capName), cdst)
				s.HasCaptions = true
			}
		}
		total += fi.Size()
		s.ImageCount++
		formats[fmtName] = true
	}
	if s.ImageCount == 0 {
		return nil, fmt.Errorf("no usable images in %s", srcDir)
	}
	s.Formats = sortedKeys(formats)
	s.Hash, err = Hash(datasetDir)
	return s, err
}

// Validate inspects an external dataset folder in place (no copy).
func Validate(dir string) (*Summary, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	s := &Summary{}
	formats := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if strings.ToLower(filepath.Ext(e.Name())) == ".txt" {
			s.HasCaptions = true
			continue
		}
		ct, err := sniff(filepath.Join(dir, e.Name()))
		if fmtName, ok := allowedImage[ct]; ok && err == nil {
			s.ImageCount++
			formats[fmtName] = true
		} else {
			s.Skipped = append(s.Skipped, e.Name())
		}
	}
	if s.ImageCount == 0 {
		return nil, fmt.Errorf("no usable images in %s", dir)
	}
	s.Formats = sortedKeys(formats)
	s.Hash, err = Hash(dir)
	return s, err
}

// Hash deterministically hashes the image+caption set (order-independent).
func Hash(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if _, ok := allowedImage[contentTypeForExt(ext)]; ok || ext == ".txt" || ext == ".png" || ext == ".jpg" || ext == ".jpeg" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		fmt.Fprintf(h, "%s\x00", n)
		f, err := os.Open(filepath.Join(dir, n))
		if err != nil {
			return "", err
		}
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ResolveCaptionMode picks the caption strategy. Returns (mode, warning).
func ResolveCaptionMode(datasetDir, flag string, captionerConfigured bool) (string, string) {
	switch flag {
	case "keep", "none", "auto":
		if flag == "auto" && !captionerConfigured {
			return "auto", "no captioner configured — captions will be trigger-only at train time unless ai-toolkit provides one"
		}
		return flag, ""
	}
	// default
	if hasCaptions(datasetDir) {
		return "keep", ""
	}
	if captionerConfigured {
		return "auto", ""
	}
	return "none", "no captions found and no captioner configured — using trigger-only captions"
}

func hasCaptions(dir string) bool {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.ToLower(filepath.Ext(e.Name())) == ".txt" {
			return true
		}
	}
	return false
}

// Save / Load dataset.yaml.
func Save(datasetDir string, c *Config) error {
	c.Version = 1
	c.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(datasetDir, "dataset.yaml"), data, 0o644)
}

func copyFile(src, dst string) error {
	if err := pathsafe.RefuseSymlink(dst); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func contentTypeForExt(ext string) string {
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	}
	return ""
}
