// Package ref parses and validates "owner/repo[@version]" references and the
// slug/version grammars shared across commands.
package ref

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	slugRE    = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,98}[a-z0-9])?$`)
	versionRE = regexp.MustCompile(`^v[0-9]+$`)
	hashRE    = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Ref is a parsed repository reference.
type Ref struct {
	Owner   string // may be empty when the caller allows owner inference
	Repo    string
	Version string // "", "latest", "vN", or 64-hex hash
}

// ValidateSlug checks the owner/repo slug grammar (lowercase, digits, hyphens,
// no leading/trailing hyphen, 1–100 chars).
func ValidateSlug(s string) error {
	if !slugRE.MatchString(s) {
		return fmt.Errorf("%q is not a valid name: use lowercase letters, digits and hyphens (no leading/trailing hyphen), 1–100 chars", s)
	}
	return nil
}

// ValidateVersion checks a version label: "latest", "vN", or a 64-hex hash.
func ValidateVersion(s string) error {
	if s == "latest" || versionRE.MatchString(s) || hashRE.MatchString(s) {
		return nil
	}
	return fmt.Errorf("%q is not a valid version: use 'latest', 'vN' (e.g. v3), or a 64-hex content hash", s)
}

// ParseExplicitVersion validates a user-supplied --version that must be vN.
func ParseExplicitVersion(s string) error {
	if !versionRE.MatchString(s) {
		return fmt.Errorf("%q is not a valid version tag: expected vN, e.g. v5", s)
	}
	return nil
}

// Parse parses "owner/repo[@version]" and requires both owner and repo.
func Parse(s string) (Ref, error) {
	r, err := ParseAllowBareRepo(s)
	if err != nil {
		return Ref{}, err
	}
	if r.Owner == "" {
		return Ref{}, fmt.Errorf("expected owner/repo, got %q", s)
	}
	return r, nil
}

// ParseAllowBareRepo parses "owner/repo[@version]" or "repo[@version]" (owner
// inferred later). Rejects anything malformed.
func ParseAllowBareRepo(s string) (Ref, error) {
	if s == "" {
		return Ref{}, fmt.Errorf("empty reference")
	}
	body, version := s, ""
	if at := strings.IndexByte(s, '@'); at >= 0 {
		body, version = s[:at], s[at+1:]
		if err := ValidateVersion(version); err != nil {
			return Ref{}, err
		}
	}

	var owner, repo string
	switch parts := strings.Split(body, "/"); len(parts) {
	case 1:
		repo = parts[0]
	case 2:
		owner, repo = parts[0], parts[1]
	default:
		return Ref{}, fmt.Errorf("%q is not a valid reference (expected owner/repo)", s)
	}
	if owner != "" {
		if err := ValidateSlug(owner); err != nil {
			return Ref{}, err
		}
	}
	if err := ValidateSlug(repo); err != nil {
		return Ref{}, err
	}
	return Ref{Owner: owner, Repo: repo, Version: version}, nil
}

// String renders the ref as owner/repo[@version].
func (r Ref) String() string {
	s := r.Repo
	if r.Owner != "" {
		s = r.Owner + "/" + r.Repo
	}
	if r.Version != "" {
		s += "@" + r.Version
	}
	return s
}

// ReconcileVersion ensures a positional @version and a --version flag agree.
// Returns the effective version (defaulting to "latest" when both empty).
func ReconcileVersion(fromRef, fromFlag string) (string, error) {
	switch {
	case fromRef != "" && fromFlag != "" && fromRef != fromFlag:
		return "", fmt.Errorf("version mismatch: ref says %q but --version says %q", fromRef, fromFlag)
	case fromFlag != "":
		return fromFlag, nil
	case fromRef != "":
		return fromRef, nil
	default:
		return "latest", nil
	}
}
