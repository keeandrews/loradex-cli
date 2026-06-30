package drawthings

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestIsCheckpoint(t *testing.T) {
	dir := t.TempDir()
	sq := filepath.Join(dir, "model.ckpt")
	if err := os.WriteFile(sq, append([]byte("SQLite format 3\x00"), 0, 0, 0), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, err := IsCheckpoint(sq); !ok || err != nil {
		t.Errorf("expected SQLite file to be a checkpoint, got ok=%v err=%v", ok, err)
	}

	pt := filepath.Join(dir, "model.safetensors")
	if err := os.WriteFile(pt, []byte("PK\x03\x04 not sqlite at all here"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, _ := IsCheckpoint(pt); ok {
		t.Error("non-SQLite file should not be a checkpoint")
	}
}

func TestBaseFromCheckpoint(t *testing.T) {
	cases := map[string]string{
		"flux_2_klein_base_4b_q8p.ckpt": "flux2-klein",
		"flux_1_dev_q8p.ckpt":           "flux1",
		"sd_xl_base_1.0_f16.ckpt":       "sdxl",
		"sd_v1.5_f16.ckpt":              "sd15",
		"something_unknown.ckpt":        "",
	}
	for in, want := range cases {
		if got := baseFromCheckpoint(in); got != want {
			t.Errorf("baseFromCheckpoint(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFlatbufferStrings(t *testing.T) {
	// Build a blob with two length-prefixed printable strings and some noise.
	var blob []byte
	add := func(s string) {
		var l [4]byte
		binary.LittleEndian.PutUint32(l[:], uint32(len(s)))
		blob = append(blob, l[:]...)
		blob = append(blob, s...)
	}
	blob = append(blob, 0x00, 0xff, 0x12) // noise
	add("ohwxman a photograph")
	add("flux_2_klein_base_4b_q8p.ckpt")
	add("keenan-v4")

	got := flatbufferStrings(blob)
	want := map[string]bool{"ohwxman a photograph": true, "flux_2_klein_base_4b_q8p.ckpt": true, "keenan-v4": true}
	found := 0
	for _, s := range got {
		if want[s] {
			found++
		}
	}
	if found != len(want) {
		t.Errorf("expected to find all %d strings, found %d in %v", len(want), found, got)
	}
}
