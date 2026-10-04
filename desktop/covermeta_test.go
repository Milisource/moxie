package main

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"github.com/mili/moxie/internal/coverart"
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

// testJPEGBytes encodes a solid w×h JPEG and returns its bytes.
func testJPEGBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
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

func TestWideGeometry(t *testing.T) {
	cases := []struct {
		name       string
		w, h       int
		crop       image.Rectangle
		outW, outH int
	}{
		// Landscape banners keep their whole frame, capped at coverWideMaxDim.
		{"banner 16:9", 1920, 1080, image.Rect(0, 0, 1920, 1080), 960, 540},
		{"banner 2:1", 1600, 800, image.Rect(0, 0, 1600, 800), 960, 480},
		{"banner 4:3", 800, 600, image.Rect(0, 0, 800, 600), 800, 600},
		{"small banner", 560, 315, image.Rect(0, 0, 560, 315), 560, 315},
		// Portrait / square art is centre-cropped to 16:9.
		{"portrait 3:4", 600, 800, image.Rect(0, 231, 600, 568), 600, 337},
		{"square", 1000, 1000, image.Rect(0, 219, 1000, 781), 960, 540},
	}
	for _, c := range cases {
		crop, w, h := wideGeometry(c.w, c.h)
		if crop != c.crop || w != c.outW || h != c.outH {
			t.Errorf("%s: got crop %v out %dx%d, want %v %dx%d", c.name, crop, w, h, c.crop, c.outW, c.outH)
		}
	}
}

func decodeDimensions(t *testing.T, path string) (int, int) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	cfg, err := jpeg.DecodeConfig(f)
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return cfg.Width, cfg.Height
}

func TestWriteCoverWideFromBanner(t *testing.T) {
	p := filepath.Join(t.TempDir(), "5")
	writeTestJPEG(t, p, 600, 800, color.RGBA{10, 20, 30, 255}) // portrait primary
	if ok, err := coverart.StoreBanner(p, testJPEGBytes(t, 1600, 900), "f95", "u"); err != nil || !ok {
		t.Fatalf("StoreBanner: ok=%v err=%v", ok, err)
	}
	if r := writeCoverWide(p); r != thumbWritten {
		t.Fatalf("writeCoverWide = %v", r)
	}
	if w, h := decodeDimensions(t, p+".wide"); w != 960 || h != 540 {
		t.Errorf("wide = %dx%d, want 960x540 (from the 1600x900 banner)", w, h)
	}
	if m, _ := readCoverMeta(p); m.WideW != 960 || m.WideH != 540 {
		t.Errorf("meta wide = %dx%d", m.WideW, m.WideH)
	}
}

func TestWriteCoverWideFallsBackToPrimary(t *testing.T) {
	p := filepath.Join(t.TempDir(), "6")
	writeTestJPEG(t, p, 1400, 700, color.RGBA{10, 20, 30, 255}) // landscape primary, no banner
	if r := writeCoverWide(p); r != thumbWritten {
		t.Fatalf("writeCoverWide = %v", r)
	}
	if w, h := decodeDimensions(t, p+".wide"); w != 960 || h != 480 {
		t.Errorf("wide = %dx%d, want 960x480", w, h)
	}
}

func TestMigrateCoverBannerFromPrev(t *testing.T) {
	p := filepath.Join(t.TempDir(), "9")
	writeTestJPEG(t, p, 600, 900, color.RGBA{1, 2, 3, 255}) // portrait primary
	writeTestJPEG(t, p+".prev", 1920, 1080, color.RGBA{4, 5, 6, 255})
	if err := writeCoverMeta(p+".prev", coverMeta{Source: "f95", URL: "u"}); err != nil {
		t.Fatal(err)
	}
	if !migrateCoverBanner(p) {
		t.Fatal("migrateCoverBanner = false")
	}
	if m, _ := readCoverMeta(p); m.BannerW != 1920 || m.BannerH != 1080 || m.BannerURL != "u" {
		t.Errorf("meta = %+v", m)
	}
	if _, err := os.Stat(p + ".wide"); err != nil {
		t.Errorf("wide rendition not written: %v", err)
	}
	// Idempotent: a second run leaves the existing banner alone.
	if migrateCoverBanner(p) {
		t.Error("migrateCoverBanner ran twice")
	}
}
