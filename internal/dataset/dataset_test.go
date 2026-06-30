package dataset

import (
	"os"
	"path/filepath"
	"testing"
)

var pngSig = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}
var jpgSig = []byte{0xff, 0xd8, 0xff, 0xe0, 0, 0, 0, 0}

func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIngest_SkipsNonImages(t *testing.T) {
	src := t.TempDir()
	write(t, filepath.Join(src, "a.png"), pngSig)
	write(t, filepath.Join(src, "a.txt"), []byte("a caption"))
	write(t, filepath.Join(src, "b.jpg"), jpgSig)
	write(t, filepath.Join(src, "c.webp"), []byte("RIFF0000WEBPVP8 ")) // unsupported
	write(t, filepath.Join(src, "d.bin"), []byte("not an image at all"))

	dst := filepath.Join(t.TempDir(), "dataset")
	s, err := Ingest(src, dst)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if s.ImageCount != 2 {
		t.Errorf("ImageCount = %d, want 2", s.ImageCount)
	}
	if !s.HasCaptions {
		t.Error("expected captions detected")
	}
	if len(s.Skipped) < 2 {
		t.Errorf("expected webp + bin skipped, got %v", s.Skipped)
	}
	// caption co-located with a.png copied.
	if _, err := os.Stat(filepath.Join(dst, "a.txt")); err != nil {
		t.Errorf("caption not copied: %v", err)
	}
}

func TestIngest_ReplacesNotAppends(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "dataset")

	// First source: two images + a stale caption + metadata that must survive.
	src1 := t.TempDir()
	write(t, filepath.Join(src1, "old1.png"), pngSig)
	write(t, filepath.Join(src1, "old2.jpg"), jpgSig)
	if _, err := Ingest(src1, dst); err != nil {
		t.Fatalf("first Ingest: %v", err)
	}
	// A metadata file the trainer/project owns — must be preserved across re-ingest.
	write(t, filepath.Join(dst, "dataset.yaml"), []byte("image_count: 2\n"))

	// Second source with different images: dataset must mirror src2, not pile up.
	src2 := t.TempDir()
	write(t, filepath.Join(src2, "new1.png"), pngSig)
	s, err := Ingest(src2, dst)
	if err != nil {
		t.Fatalf("second Ingest: %v", err)
	}
	if s.ImageCount != 1 {
		t.Errorf("ImageCount = %d, want 1 (replace, not append)", s.ImageCount)
	}
	for _, stale := range []string{"old1.png", "old2.jpg"} {
		if _, err := os.Stat(filepath.Join(dst, stale)); !os.IsNotExist(err) {
			t.Errorf("stale image %s survived re-ingest", stale)
		}
	}
	if _, err := os.Stat(filepath.Join(dst, "new1.png")); err != nil {
		t.Errorf("new image not present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "dataset.yaml")); err != nil {
		t.Errorf("metadata dataset.yaml was wrongly removed: %v", err)
	}
}

func TestHash_DeterministicOrderIndependent(t *testing.T) {
	d1 := t.TempDir()
	write(t, filepath.Join(d1, "z.png"), append(pngSig, 1))
	write(t, filepath.Join(d1, "a.png"), append(pngSig, 2))
	h1, err := Hash(d1)
	if err != nil {
		t.Fatal(err)
	}
	// Same content, created in a different order.
	d2 := t.TempDir()
	write(t, filepath.Join(d2, "a.png"), append(pngSig, 2))
	write(t, filepath.Join(d2, "z.png"), append(pngSig, 1))
	h2, _ := Hash(d2)
	if h1 != h2 {
		t.Errorf("hash not order-independent: %s != %s", h1, h2)
	}
	// Changing content changes the hash.
	write(t, filepath.Join(d2, "a.png"), append(pngSig, 9))
	h3, _ := Hash(d2)
	if h3 == h1 {
		t.Error("hash should change with content")
	}
}

func TestResolveCaptionMode(t *testing.T) {
	dir := t.TempDir()
	if m, _ := ResolveCaptionMode(dir, "", false); m != "none" {
		t.Errorf("no captions + no captioner default = %q, want none", m)
	}
	write(t, filepath.Join(dir, "a.txt"), []byte("x"))
	if m, _ := ResolveCaptionMode(dir, "", false); m != "keep" {
		t.Errorf("captions present default = %q, want keep", m)
	}
	if m, _ := ResolveCaptionMode(dir, "none", false); m != "none" {
		t.Errorf("explicit none = %q", m)
	}
}
