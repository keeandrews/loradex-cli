// Package drawthings reads Draw Things LoRA checkpoints so loradex can catalog
// and publish them. Draw Things stores models as SQLite databases (s4nnc/NNC
// format) under a .ckpt extension — NOT PyTorch checkpoints. This package
// validates that container and best-effort extracts the metadata needed to
// catalog a LoRA (base architecture, network rank, trigger, name).
//
// Validation (the magic-byte check) is dependency-free. Metadata detection
// shells out to the `sqlite3` CLI when available and degrades gracefully to
// caller-supplied values when it is not.
package drawthings

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// sqliteMagic is the 16-byte header every SQLite-3 database begins with.
var sqliteMagic = []byte("SQLite format 3\x00")

// IsCheckpoint reports whether path looks like a Draw Things (SQLite) checkpoint.
func IsCheckpoint(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	buf := make([]byte, len(sqliteMagic))
	if _, err := f.Read(buf); err != nil {
		return false, err
	}
	for i := range sqliteMagic {
		if buf[i] != sqliteMagic[i] {
			return false, nil
		}
	}
	return true, nil
}

// Meta is the best-effort metadata recovered from a Draw Things LoRA.
type Meta struct {
	Arch    string // dit (flux-family) | unet (sd-family) | ""
	Base    string // loradex base id inferred from the embedded base-model name
	Name    string // slug found in the training config
	Trigger string // trigger token (first word of the instance caption)
	Rank    int    // LoRA network rank
}

var (
	slugRE    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)
	printable = regexp.MustCompile(`^[\x20-\x7e]+$`)
)

// Detect reads metadata via the sqlite3 CLI. Every field is best-effort; absent
// sqlite3 or an unreadable field yields a zero value for that field, never an
// error — callers supply overrides.
func Detect(ctx context.Context, path string) Meta {
	var m Meta
	if _, err := exec.LookPath("sqlite3"); err != nil {
		return m
	}
	if n := sqlInt(ctx, path, "SELECT count(*) FROM tensors WHERE name GLOB '__dit__*'"); n > 0 {
		m.Arch = "dit"
	} else if n := sqlInt(ctx, path, "SELECT count(*) FROM tensors WHERE name GLOB '__unet__*'"); n > 0 {
		m.Arch = "unet"
	}
	m.Rank = detectRank(ctx, path)
	detectFromConfig(ctx, path, &m)
	return m
}

// detectRank reads the leading dimension of an up-projection tensor. Draw Things
// dim blobs are a 4-byte header followed by int32 little-endian dimensions; the
// first dimension of a __up__ tensor is the LoRA rank.
func detectRank(ctx context.Context, path string) int {
	h := sqlText(ctx, path, "SELECT hex(dim) FROM tensors WHERE name GLOB '*__up__' LIMIT 1")
	raw, err := hex.DecodeString(strings.TrimSpace(h))
	if err != nil || len(raw) < 8 {
		return 0
	}
	return int(binary.LittleEndian.Uint32(raw[4:8]))
}

// detectFromConfig scans the flatbuffers training-config blob for embedded
// strings: the base-model checkpoint name, the model slug, and the caption.
func detectFromConfig(ctx context.Context, path string, m *Meta) {
	h := sqlText(ctx, path, "SELECT hex(p) FROM loratrainingconfiguration LIMIT 1")
	raw, err := hex.DecodeString(strings.TrimSpace(h))
	if err != nil || len(raw) == 0 {
		return
	}
	for _, s := range flatbufferStrings(raw) {
		switch {
		case strings.HasSuffix(s, ".ckpt") && m.Base == "":
			m.Base = baseFromCheckpoint(s)
		case strings.Contains(s, " ") && m.Trigger == "":
			m.Trigger = strings.Fields(s)[0] // caption → first token is the trigger
		case slugRE.MatchString(s) && m.Name == "":
			m.Name = s
		}
	}
}

// flatbufferStrings extracts length-prefixed printable strings (uint32 LE length
// followed by that many ASCII bytes) from a flatbuffers blob.
func flatbufferStrings(raw []byte) []string {
	var out []string
	seen := map[string]bool{}
	for i := 0; i+4 < len(raw); i++ {
		n := int(binary.LittleEndian.Uint32(raw[i : i+4]))
		if n < 3 || n > 256 || i+4+n > len(raw) {
			continue
		}
		s := string(raw[i+4 : i+4+n])
		if printable.MatchString(s) && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// baseFromCheckpoint maps an embedded Draw Things base-model filename to the
// matching loradex base id.
func baseFromCheckpoint(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "flux_2") || strings.Contains(n, "flux2"):
		return "flux2-klein"
	case strings.Contains(n, "flux_1") || strings.Contains(n, "flux1"):
		return "flux1"
	case strings.Contains(n, "sd_xl") || strings.Contains(n, "sdxl"):
		return "sdxl"
	case strings.Contains(n, "v1.5") || strings.Contains(n, "sd_v1") || strings.Contains(n, "sd15"):
		return "sd15"
	}
	return ""
}

func sqlText(ctx context.Context, path, query string) string {
	out, err := exec.CommandContext(ctx, "sqlite3", "-readonly", "-noheader", path, query).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func sqlInt(ctx context.Context, path, query string) int {
	n, _ := strconv.Atoi(sqlText(ctx, path, query))
	return n
}
