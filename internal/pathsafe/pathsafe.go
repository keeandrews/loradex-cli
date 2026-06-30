// Package pathsafe sanitizes and confines untrusted "member" paths (filenames
// and repo names returned by the API). Every server-derived write path MUST be
// routed through SafeJoin.
package pathsafe

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var winDriveRE = regexp.MustCompile(`^[a-zA-Z]:`)

// ValidateMember rejects member paths that are absolute, traverse upward,
// contain control chars, or carry a Windows drive/UNC prefix.
func ValidateMember(member string) error {
	if member == "" {
		return fmt.Errorf("empty path")
	}
	for _, r := range member {
		if r == 0 || r < 0x20 || r == 0x7f {
			return fmt.Errorf("path %q contains a control character", member)
		}
	}
	// Normalize separators for inspection.
	norm := strings.ReplaceAll(member, "\\", "/")
	if strings.HasPrefix(norm, "/") {
		return fmt.Errorf("path %q must be relative", member)
	}
	if winDriveRE.MatchString(member) {
		return fmt.Errorf("path %q contains a drive prefix", member)
	}
	if strings.HasPrefix(member, "\\\\") || strings.HasPrefix(norm, "//") {
		return fmt.Errorf("path %q contains a UNC prefix", member)
	}
	for _, seg := range strings.Split(norm, "/") {
		if seg == ".." {
			return fmt.Errorf("path %q escapes the destination", member)
		}
	}
	cleaned := filepath.Clean(filepath.FromSlash(norm))
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) || filepath.IsAbs(cleaned) {
		return fmt.Errorf("path %q escapes the destination", member)
	}
	return nil
}

// SafeJoin validates member, joins it under root, and verifies the result stays
// within root even after resolving symlinks in the existing ancestry.
func SafeJoin(root, member string) (string, error) {
	if err := ValidateMember(member); err != nil {
		return "", err
	}
	cleaned := filepath.Clean(filepath.FromSlash(strings.ReplaceAll(member, "\\", "/")))
	joined := filepath.Join(root, cleaned)

	rel, err := filepath.Rel(root, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the destination", member)
	}
	if err := withinReal(root, joined); err != nil {
		return "", err
	}
	return joined, nil
}

// withinReal resolves symlinks on root and on the deepest existing ancestor of
// target, and confirms the ancestor is still inside root (defeats symlink escape).
func withinReal(root, target string) error {
	realRoot, err := resolveExisting(root)
	if err != nil {
		return err
	}
	ancestor := filepath.Dir(target)
	realAnc, err := resolveExisting(ancestor)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(realRoot, realAnc)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path %q escapes the destination via a symlink", target)
	}
	return nil
}

// resolveExisting returns EvalSymlinks of the deepest existing ancestor of p.
func resolveExisting(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	cur := abs
	for {
		if _, err := os.Lstat(cur); err == nil {
			real, err := filepath.EvalSymlinks(cur)
			if err != nil {
				return "", err
			}
			// Re-append the part of abs below cur (the non-existent tail).
			if cur == abs {
				return real, nil
			}
			tail, _ := filepath.Rel(cur, abs)
			return filepath.Join(real, tail), nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs, nil // nothing exists; use the abs path as-is
		}
		cur = parent
	}
}

// RefuseSymlink returns an error if path exists and is a symlink (never
// overwrite or follow a symlink when writing).
func RefuseSymlink(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil // doesn't exist — fine
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to overwrite symlink %q", path)
	}
	return nil
}
