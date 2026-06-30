package convert

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExt(t *testing.T) {
	cases := map[Format]string{
		Safetensors: ".safetensors",
		MLX:         ".safetensors",
		Diffusers:   ".safetensors",
		DrawThings:  ".ckpt",
	}
	for f, want := range cases {
		if got := f.Ext(); got != want {
			t.Errorf("%s.Ext() = %q, want %q", f, got, want)
		}
	}
}

func TestDetectFormat(t *testing.T) {
	dir := t.TempDir()

	sq := filepath.Join(dir, "lora.ckpt")
	if err := os.WriteFile(sq, []byte("SQLite format 3\x00rest..."), 0o644); err != nil {
		t.Fatal(err)
	}
	if f, _ := DetectFormat(sq); f != DrawThings {
		t.Errorf("SQLite .ckpt detected as %q, want drawthings", f)
	}

	// A .ckpt that is NOT SQLite should still classify as drawthings by ext.
	fake := filepath.Join(dir, "weird.ckpt")
	if err := os.WriteFile(fake, []byte("not sqlite"), 0o644); err != nil {
		t.Fatal(err)
	}
	if f, _ := DetectFormat(fake); f != DrawThings {
		t.Errorf(".ckpt by extension = %q, want drawthings", f)
	}

	st := filepath.Join(dir, "lora.safetensors")
	if err := os.WriteFile(st, []byte{0x10, 0, 0, 0, 0, 0, 0, 0, '{', '}'}, 0o644); err != nil {
		t.Fatal(err)
	}
	if f, _ := DetectFormat(st); f != Safetensors {
		t.Errorf(".safetensors detected as %q, want safetensors", f)
	}
}

func TestTargetsCoverAll(t *testing.T) {
	want := map[Format]bool{Safetensors: true, MLX: true, Diffusers: true, DrawThings: true}
	for _, f := range Targets {
		delete(want, f)
	}
	if len(want) != 0 {
		t.Errorf("Targets missing: %v", want)
	}
}
