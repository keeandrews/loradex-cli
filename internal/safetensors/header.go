// Package safetensors parses the JSON header of a .safetensors file with strict
// bounds (the file is untrusted). It never reads tensor data.
package safetensors

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// MaxHeaderBytes caps the header JSON length to prevent a malicious file from
// forcing a huge allocation (real headers are < 1 MB).
const MaxHeaderBytes = 64 << 20 // 64 MiB

// Header holds the best-effort fields extracted for catalog auto-fill.
type Header struct {
	NetworkRank   int
	NetworkDim    int
	BaseModelHint string
	Metadata      map[string]string
}

type tensorInfo struct {
	Shape []int `json:"shape"`
}

// ReadHeader parses the safetensors header from r (size fileSize). All bounds
// are checked before allocating.
func ReadHeader(r io.ReaderAt, fileSize int64) (Header, error) {
	var h Header
	if fileSize < 8 {
		return h, fmt.Errorf("file too small to be safetensors")
	}
	var lenBuf [8]byte
	if _, err := r.ReadAt(lenBuf[:], 0); err != nil {
		return h, fmt.Errorf("reading header length: %w", err)
	}
	n := binary.LittleEndian.Uint64(lenBuf[:])
	switch {
	case n == 0:
		return h, fmt.Errorf("empty safetensors header")
	case n > MaxHeaderBytes:
		return h, fmt.Errorf("safetensors header too large (%d bytes)", n)
	case int64(8)+int64(n) > fileSize:
		return h, fmt.Errorf("safetensors header length exceeds file size")
	}

	buf := make([]byte, n)
	if _, err := r.ReadAt(buf, 8); err != nil {
		return h, fmt.Errorf("reading header: %w", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(buf, &raw); err != nil {
		return h, fmt.Errorf("parsing header json: %w", err)
	}

	if md, ok := raw["__metadata__"]; ok {
		_ = json.Unmarshal(md, &h.Metadata) // soft
	}
	applyMetadata(&h)
	if h.NetworkRank == 0 {
		h.NetworkRank = inferRank(raw)
	}
	if h.NetworkDim == 0 {
		h.NetworkDim = h.NetworkRank
	}
	return h, nil
}

func applyMetadata(h *Header) {
	if h.Metadata == nil {
		return
	}
	if v := atoi(h.Metadata["ss_network_dim"]); v > 0 {
		h.NetworkDim = v
		if h.NetworkRank == 0 {
			h.NetworkRank = v
		}
	}
	for _, k := range []string{"modelspec.architecture", "ss_base_model_version", "ss_sd_model_name"} {
		if v := strings.TrimSpace(h.Metadata[k]); v != "" {
			h.BaseModelHint = v
			break
		}
	}
}

// inferRank picks the smaller dimension of a lora_down/lora_A tensor shape.
func inferRank(raw map[string]json.RawMessage) int {
	for name, val := range raw {
		if name == "__metadata__" {
			continue
		}
		ln := strings.ToLower(name)
		if !strings.Contains(ln, "lora_down") && !strings.Contains(ln, "lora_a") {
			continue
		}
		var ti tensorInfo
		if json.Unmarshal(val, &ti) != nil || len(ti.Shape) == 0 {
			continue
		}
		min := ti.Shape[0]
		for _, d := range ti.Shape[1:] {
			if d > 0 && d < min {
				min = d
			}
		}
		if min > 0 {
			return min
		}
	}
	return 0
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
