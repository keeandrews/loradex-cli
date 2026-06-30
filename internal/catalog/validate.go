package catalog

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/keeandrews/loradex-cli/internal/ref"
)

// Field length / count caps (mirror the server).
const (
	MaxDescription  = 280
	MaxMessage      = 1000
	MaxTag          = 32
	MaxTags         = 16
	MaxTrigger      = 64
	MaxTriggerWords = 16
	MaxLicense      = 64
	MaxNetwork      = 100000
)

var tagRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// FieldError is a single validation failure.
type FieldError struct {
	Field   string
	Message string
	Hint    string
}

func (e FieldError) String() string {
	s := fmt.Sprintf("%s: %s", e.Field, e.Message)
	if e.Hint != "" {
		s += " (" + e.Hint + ")"
	}
	return s
}

// Result collects all errors and warnings (errors aren't fatal until the caller decides).
type Result struct {
	Errors   []FieldError
	Warnings []string
}

// OK reports whether validation found no errors.
func (r Result) OK() bool { return len(r.Errors) == 0 }

func (r *Result) addErr(field, msg, hint string) {
	r.Errors = append(r.Errors, FieldError{Field: field, Message: msg, Hint: hint})
}
func (r *Result) warn(format string, a ...any) {
	r.Warnings = append(r.Warnings, fmt.Sprintf(format, a...))
}

// Validate checks the catalog and returns all errors + warnings at once.
func Validate(c *Catalog) Result {
	var r Result

	if err := ref.ValidateSlug(c.Name); err != nil {
		r.addErr("name", err.Error(), "lowercase, hyphenated, 1–100 chars")
	}

	switch c.Visibility {
	case "public", "private":
	default:
		r.addErr("visibility", fmt.Sprintf("%q is not valid", c.Visibility), "must be public or private")
	}

	if c.BaseModel == "" {
		r.addErr("base_model", "is required", "")
	} else if !slices.Contains(KnownBaseModels, c.BaseModel) {
		r.warn("base_model %q is not a known value %v — continuing (catalog may have grown)", c.BaseModel, KnownBaseModels)
	}

	if c.Format == "" {
		r.addErr("format", "is required", "")
	} else if !slices.Contains(KnownFormats, c.Format) {
		r.warn("format %q is not a known value %v — continuing", c.Format, KnownFormats)
	}

	checkText(&r, "description", c.Description, MaxDescription)
	checkText(&r, "license", c.License, MaxLicense)

	if c.Weights == "" {
		r.addErr("weights", "is required", "the .safetensors file to publish")
	} else if err := pathLooksLocal(c.Weights); err != nil {
		r.addErr("weights", err.Error(), "must be a file inside the project")
	}

	if len(c.Tags) > MaxTags {
		r.addErr("tags", fmt.Sprintf("too many (%d > %d)", len(c.Tags), MaxTags), "")
	}
	for i, t := range c.Tags {
		if utf8.RuneCountInString(t) > MaxTag {
			r.addErr(fmt.Sprintf("tags[%d]", i), "too long", fmt.Sprintf("max %d chars", MaxTag))
		}
		if !tagRE.MatchString(t) {
			r.addErr(fmt.Sprintf("tags[%d]", i), fmt.Sprintf("%q is invalid", t), "lowercase, hyphenated")
		}
	}

	if len(c.TriggerWords) > MaxTriggerWords {
		r.addErr("trigger_words", fmt.Sprintf("too many (%d > %d)", len(c.TriggerWords), MaxTriggerWords), "")
	}
	for i, w := range c.TriggerWords {
		if utf8.RuneCountInString(w) > MaxTrigger {
			r.addErr(fmt.Sprintf("trigger_words[%d]", i), "too long", fmt.Sprintf("max %d chars", MaxTrigger))
		}
		if hasControl(w) {
			r.addErr(fmt.Sprintf("trigger_words[%d]", i), "contains a control character", "")
		}
	}

	if c.RecommendedWeight < 0 || c.RecommendedWeight > 2 {
		r.addErr("recommended_weight", fmt.Sprintf("%.2f is out of range", c.RecommendedWeight), "must be between 0 and 2")
	}
	checkNetwork(&r, "network_rank", c.NetworkRank)
	checkNetwork(&r, "network_dim", c.NetworkDim)

	return r
}

func checkText(r *Result, field, val string, max int) {
	if hasControl(val) {
		r.addErr(field, "contains a control character", "")
	}
	if utf8.RuneCountInString(val) > max {
		r.addErr(field, fmt.Sprintf("too long (%d > %d)", utf8.RuneCountInString(val), max), "")
	}
	if !utf8.ValidString(val) {
		r.addErr(field, "contains invalid UTF-8", "")
	}
}

func checkNetwork(r *Result, field string, v int) {
	if v < 0 || v > MaxNetwork {
		r.addErr(field, fmt.Sprintf("%d is out of range", v), fmt.Sprintf("0–%d", MaxNetwork))
	}
}

// NormalizeText converts CRLF→LF and trims trailing whitespace lines.
func NormalizeText(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

func hasControl(s string) bool {
	for _, r := range s {
		if r != '\n' && r != '\t' && (r < 0x20 || r == 0x7f) {
			return true
		}
	}
	return false
}

// pathLooksLocal rejects an absolute or traversing weights path in the catalog.
func pathLooksLocal(p string) error {
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "\\") || regexp.MustCompile(`^[a-zA-Z]:`).MatchString(p) {
		return fmt.Errorf("must be a relative path")
	}
	for _, seg := range strings.Split(strings.ReplaceAll(p, "\\", "/"), "/") {
		if seg == ".." {
			return fmt.Errorf("must not contain ..")
		}
	}
	return nil
}
