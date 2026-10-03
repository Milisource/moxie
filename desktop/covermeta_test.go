package main

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

func TestThumbGeometry(t *testing.T) {
	cases := []struct {
		name       string
		w, h       int
		crop       image.Rectangle
		outW, outH int
	}{
		// Portrait 2:3 (Steam capsule 600x900) → crop height to 3:4, no upscale.
		{"steam capsule", 600, 900, image.Rect(0, 50, 600, 850), 600, 800},
		// Big portrait scaled down to the box.
		{"big portrait", 1200, 1600, image.Rect(0, 0, 1200, 1600), 600, 800},
		// DLsite 4:3 at 560x420 → crop sides to 315x420, kept at native size.
		{"dlsite 4:3", 560, 420, image.Rect(122, 0, 437, 420), 315, 420},
		// Wide banner (aspect > 1.6) → no crop, width capped.
		{"banner", 1920, 600, image.Rect(0, 0, 1920, 600), 720, 225},
		// Small banner: never upscaled.
		{"small banner", 500, 150, image.Rect(0, 0, 500, 150), 500, 150},
	}
	for _, c := range cases {
		crop, w, h := thumbGeometry(c.w, c.h)
		if crop != c.crop || w != c.outW || h != c.outH {
			t.Errorf("%s: got crop %v out %dx%d, want %v %dx%d", c.name, crop, w, h, c.crop, c.outW, c.outH)
		}
	}
}

func writeTestJPEG(t *testing.T, path string, w, h int, c color.RGBA) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWriteCoverThumbRecordsMeta(t *testing.T) {
	p := filepath.Join(t.TempDir(), "7")
	writeTestJPEG(t, p, 1600, 900, color.RGBA{200, 40, 40, 255})
	if err := writeCoverMeta(p, coverMeta{Source: "steam", Locked: true}); err != nil {
		t.Fatal(err)
	}
	if r := writeCoverThumb(p); r != thumbWritten {
		t.Fatalf("writeCoverThumb = %v", r)
	}
	m, ok := readCoverMeta(p)
	if !ok || m.W != 1600 || m.H != 900 {
		t.Fatalf("meta = %+v, %v", m, ok)
	}
	if m.Source != "steam" || !m.Locked {
		t.Errorf("source/lock not preserved: %+v", m)
	}
	if m.Tone == "" || m.Tone[1] < 'b' { // red channel ≈ 0xc8
		t.Errorf("tone = %q, want reddish", m.Tone)
	}
	f, _ := os.Open(p + ".thumb")
	defer f.Close()
	cfg, err := jpeg.DecodeConfig(f)
	if err != nil || cfg.Width != 720 || cfg.Height != 405 {
		t.Errorf("thumb = %dx%d, %v; want 720x405 (wide, uncropped)", cfg.Width, cfg.Height, err)
	}
}

func TestEnsureCoverLarge(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "1")
	writeTestJPEG(t, small, 800, 600, color.RGBA{1, 2, 3, 255})
	if got := ensureCoverLarge(small); got != small {
		t.Errorf("small cover: got %s, want original", got)
	}
	big := filepath.Join(dir, "2")
	writeTestJPEG(t, big, 2800, 1400, color.RGBA{1, 2, 3, 255})
	got := ensureCoverLarge(big)
	if got != big+".large" {
		t.Fatalf("big cover: got %s", got)
	}
	f, _ := os.Open(got)
	defer f.Close()
	cfg, _ := jpeg.DecodeConfig(f)
	if cfg.Width != coverLargeMaxDim || cfg.Height != 700 {
		t.Errorf("large = %dx%d", cfg.Width, cfg.Height)
	}
}
