package basemodel

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/keeandrews/loradex-cli/internal/config"
	"github.com/keeandrews/loradex-cli/internal/pathsafe"
)

// Dir returns the base-models store directory (created if needed).
func Dir() (string, error) { return config.ModelsDir() }

// slugDir returns the per-model directory <modelsdir>/<id>, confined to the
// store (rejects any id that would escape).
func slugDir(id string) (string, error) {
	if err := pathsafe.ValidateMember(id); err != nil {
		return "", err
	}
	if strings.ContainsAny(id, "/\\") {
		return "", fmt.Errorf("model id %q must not contain a path separator", id)
	}
	root, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, id), nil
}

// LocalPath returns the path passed to the trainer for a downloaded entry:
// the diffusers folder for repo models, or the single file for URL models.
// It does not check existence.
func LocalPath(e Entry) (string, error) {
	dir, err := slugDir(e.ID)
	if err != nil {
		return "", err
	}
	if e.URL != "" && e.Repo == "" {
		return filepath.Join(dir, fileName(e)), nil
	}
	return dir, nil
}

// fileName derives the single-file download name for a URL entry.
func fileName(e Entry) string {
	base := path.Base(e.URL)
	if base == "" || base == "." || base == "/" {
		base = e.ID + ".safetensors"
	}
	// Strip query string if present.
	if i := strings.IndexByte(base, '?'); i >= 0 {
		base = base[:i]
	}
	if base == "" {
		base = e.ID + ".safetensors"
	}
	return base
}

// IsDownloaded reports whether the entry is present and non-empty in the store.
func IsDownloaded(e Entry) bool {
	p, err := LocalPath(e)
	if err != nil {
		return false
	}
	fi, err := os.Stat(p)
	if err != nil {
		return false
	}
	if e.URL != "" && e.Repo == "" {
		return fi.Mode().IsRegular() && fi.Size() > 0
	}
	// Diffusers folder: must look populated AND have no pending partial
	// downloads. HuggingFace leaves *.incomplete markers under
	// .cache/huggingface/download while a snapshot is mid-flight, so a run that
	// was killed early (model_index.json present, big shards not) is correctly
	// reported as not-yet-downloaded — re-running then resumes.
	if !fi.IsDir() {
		return false
	}
	if hasIncompleteDownloads(p) {
		return false
	}
	if _, err := os.Stat(filepath.Join(p, "model_index.json")); err == nil {
		return true
	}
	return hasSafetensors(p)
}

func hasSafetensors(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".safetensors") {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// PartiallyDownloaded reports whether a download was started but not finished
// (HuggingFace .incomplete markers remain). Re-pulling resumes it.
func PartiallyDownloaded(e Entry) bool {
	p, err := LocalPath(e)
	if err != nil {
		return false
	}
	// For URL single-file entries there is no partial state (atomic rename).
	if e.URL != "" && e.Repo == "" {
		return false
	}
	if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
		return false
	}
	return hasIncompleteDownloads(p)
}

// hasIncompleteDownloads reports whether a HuggingFace snapshot under dir still
// has partially-downloaded files (a kill/interrupt mid-download).
func hasIncompleteDownloads(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".incomplete") {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// DirSize returns the total bytes of a downloaded model (0 if absent).
func DirSize(e Entry) int64 {
	p, err := LocalPath(e)
	if err != nil {
		return 0
	}
	var total int64
	_ = filepath.WalkDir(p, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, e := d.Info(); e == nil {
				total += fi.Size()
			}
		}
		return nil
	})
	return total
}

// Remove deletes a downloaded model from the store.
func Remove(e Entry) error {
	dir, err := slugDir(e.ID)
	if err != nil {
		return err
	}
	if err := pathsafe.RefuseSymlink(dir); err != nil {
		return err
	}
	return os.RemoveAll(dir)
}
