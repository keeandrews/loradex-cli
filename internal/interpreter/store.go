package interpreter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/keeandrews/loradex-cli/internal/config"
	"github.com/keeandrews/loradex-cli/internal/pathsafe"
)

// Dir returns the interpreters store directory (created if needed).
func Dir() (string, error) { return config.InterpretersDir() }

func slugDir(id string) (string, error) {
	if err := pathsafe.ValidateMember(id); err != nil {
		return "", err
	}
	if strings.ContainsAny(id, "/\\") {
		return "", fmt.Errorf("interpreter id %q must not contain a path separator", id)
	}
	root, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, id), nil
}

// LocalPath returns the on-disk path of an interpreter (the model folder).
func LocalPath(e Entry) (string, error) { return slugDir(e.ID) }

// IsDownloaded reports whether the interpreter is present and complete (a
// transformers model folder with config.json and no pending .incomplete files).
func IsDownloaded(e Entry) bool {
	dir, err := slugDir(e.ID)
	if err != nil {
		return false
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return false
	}
	if hasIncompleteDownloads(dir) {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, "config.json"))
	return err == nil
}

// PartiallyDownloaded reports whether a download was started but not finished.
func PartiallyDownloaded(e Entry) bool {
	dir, err := slugDir(e.ID)
	if err != nil {
		return false
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return false
	}
	return hasIncompleteDownloads(dir)
}

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

// DirSize returns total bytes of a downloaded interpreter (0 if absent).
func DirSize(e Entry) int64 {
	dir, err := slugDir(e.ID)
	if err != nil {
		return 0
	}
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, e := d.Info(); e == nil {
				total += fi.Size()
			}
		}
		return nil
	})
	return total
}

// Remove deletes a downloaded interpreter from the store.
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
