package safetensors

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func build(headerJSON string, claimedLen uint64, fileTail int) ([]byte, int64) {
	var buf bytes.Buffer
	var lenBuf [8]byte
	binary.LittleEndian.PutUint64(lenBuf[:], claimedLen)
	buf.Write(lenBuf[:])
	buf.WriteString(headerJSON)
	for i := 0; i < fileTail; i++ {
		buf.WriteByte(0)
	}
	return buf.Bytes(), int64(buf.Len())
}

func TestReadHeader_Normal(t *testing.T) {
	j := `{"__metadata__":{"ss_network_dim":"32"},"lora_down.weight":{"dtype":"F16","shape":[16,768]}}`
	data, size := build(j, uint64(len(j)), 0)
	h, err := ReadHeader(bytes.NewReader(data), size)
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	if h.NetworkDim != 32 {
		t.Errorf("NetworkDim = %d, want 32", h.NetworkDim)
	}
}

func TestReadHeader_InferRank(t *testing.T) {
	j := `{"lora_down.weight":{"dtype":"F16","shape":[8,1024]}}`
	data, size := build(j, uint64(len(j)), 0)
	h, err := ReadHeader(bytes.NewReader(data), size)
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	if h.NetworkRank != 8 {
		t.Errorf("NetworkRank = %d, want 8 (smaller dim)", h.NetworkRank)
	}
}

func TestReadHeader_HugeLenRejected(t *testing.T) {
	// Claim an enormous header without providing the bytes — must reject before allocating.
	data, _ := build("", MaxHeaderBytes+1, 0)
	if _, err := ReadHeader(bytes.NewReader(data), 1<<40); err == nil {
		t.Error("expected rejection of oversize header length")
	}
}

func TestReadHeader_LenExceedsFile(t *testing.T) {
	data, _ := build(`{}`, 100, 0) // claims 100 bytes of header but file is tiny
	if _, err := ReadHeader(bytes.NewReader(data), int64(len(data))); err == nil {
		t.Error("expected rejection when header length exceeds file size")
	}
}

func TestReadHeader_Truncated(t *testing.T) {
	j := `{"a":1}`
	// fileSize claims room but the reader is short -> ReadAt fails gracefully.
	data, _ := build(j, uint64(len(j)+50), 0)
	if _, err := ReadHeader(bytes.NewReader(data), int64(len(data)+50)); err == nil {
		t.Error("expected graceful error on truncated header")
	}
}

func TestReadHeader_ZeroLen(t *testing.T) {
	data, size := build("", 0, 0)
	if _, err := ReadHeader(bytes.NewReader(data), size); err == nil {
		t.Error("expected rejection of zero-length header")
	}
}
