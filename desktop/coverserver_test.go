package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mili/moxie/internal/config"
)

// testCoverDir redirects the config dir to a temp location for the duration
// of the test and returns the cover directory.
func testCoverDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "config")
	config.SetConfigDirForTest(dir)
	t.Cleanup(func() { config.SetConfigDirForTest("") })
	return config.CoverDir()
}

// makePNG renders a solid-color WxH PNG and returns its bytes.
func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 255), G: 90, B: 160, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func TestCoverServerServesFullImage(t *testing.T) {
	coverDir := testCoverDir(t)
	if err := os.MkdirAll(coverDir, 0o755); err != nil {
		t.Fatal(err)
	}

	data := makePNG(t, 640, 480)
	if err := os.WriteFile(filepath.Join(coverDir, "42"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	cs := startCoverServer()
	if cs == nil {
		t.Fatal("startCoverServer returned nil")
	}
	t.Cleanup(cs.Close)

	resp, err := http.Get(cs.BaseURL() + "/cover/42")
	if err != nil {
		t.Fatalf("GET /cover/42: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
	got, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(got, data) {
		t.Error("body does not match the cached cover file")
	}
}

func TestCoverServerThumbFallsBackToFullImage(t *testing.T) {
	coverDir := testCoverDir(t)
	if err := os.MkdirAll(coverDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := makePNG(t, 100, 100)
	if err := os.WriteFile(filepath.Join(coverDir, "7"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	cs := startCoverServer()
	if cs == nil {
		t.Fatal("startCoverServer returned nil")
	}
	t.Cleanup(cs.Close)

	resp, err := http.Get(cs.BaseURL() + "/cover/7/thumb")
	if err != nil {
		t.Fatalf("GET /cover/7/thumb: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(got, data) {
		t.Error("thumb endpoint must fall back to the full image when no thumbnail exists")
	}
}

func TestCoverServerThumbServesThumbnail(t *testing.T) {
	coverDir := testCoverDir(t)
	if err := os.MkdirAll(coverDir, 0o755); err != nil {
		t.Fatal(err)
	}
	full := makePNG(t, 640, 480)
	thumb := makePNG(t, 320, 240)
	if err := os.WriteFile(filepath.Join(coverDir, "9"), full, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(coverDir, "9.thumb"), thumb, 0o644); err != nil {
		t.Fatal(err)
	}

	cs := startCoverServer()
	if cs == nil {
		t.Fatal("startCoverServer returned nil")
	}
	t.Cleanup(cs.Close)

	resp, err := http.Get(cs.BaseURL() + "/cover/9/thumb")
	if err != nil {
		t.Fatalf("GET /cover/9/thumb: %v", err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(got, thumb) {
		t.Error("thumb endpoint must serve the .thumb file when it exists")
	}
}

func TestCoverServerRejectsMissingAndInvalidPaths(t *testing.T) {
	coverDir := testCoverDir(t)
	if err := os.MkdirAll(coverDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cs := startCoverServer()
	if cs == nil {
		t.Fatal("startCoverServer returned nil")
	}
	t.Cleanup(cs.Close)

	for _, path := range []string{
		"/cover/999",       // no such game
		"/cover/abc",       // non-numeric
		"/cover/0",         // id must be positive
		"/cover/-5",        // negative id
		"/cover/1/../2",    // traversal-shaped
		"/cover/%2e%2e/1",  // URL-encoded traversal
		"/cover/1/thumb/x", // unknown suffix
		"/",                // root
	} {
		resp, err := http.Get(cs.BaseURL() + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, resp.StatusCode)
		}
	}
}

func TestWriteCoverThumbDownscales(t *testing.T) {
	coverDir := testCoverDir(t)
	if err := os.MkdirAll(coverDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 1000x500 → 320x160 (ratio preserved).
	coverPath := filepath.Join(coverDir, "5")
	if err := os.WriteFile(coverPath, makePNG(t, 1000, 500), 0o644); err != nil {
		t.Fatal(err)
	}

	writeCoverThumb(coverPath)

	f, err := os.Open(coverPath + ".thumb")
	if err != nil {
		t.Fatalf("thumbnail not written: %v", err)
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		t.Fatalf("thumbnail is not a decodable JPEG: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != 320 || b.Dy() != 160 {
		t.Errorf("thumbnail dims = %dx%d, want 320x160", b.Dx(), b.Dy())
	}
}

func TestWriteCoverThumbSkipsSmallImages(t *testing.T) {
	coverDir := testCoverDir(t)
	if err := os.MkdirAll(coverDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Already smaller than the cap: no thumbnail file should appear.
	coverPath := filepath.Join(coverDir, "3")
	if err := os.WriteFile(coverPath, makePNG(t, 100, 80), 0o644); err != nil {
		t.Fatal(err)
	}
	writeCoverThumb(coverPath)
	if _, err := os.Stat(coverPath + ".thumb"); err == nil {
		t.Error("thumbnail must not be written for images already at or below the cap")
	}
}

func TestCoverSetFromDirIgnoresNonCoverFiles(t *testing.T) {
	coverDir := testCoverDir(t)
	if err := os.MkdirAll(coverDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"1", "2", "2.thumb", "not-a-number", ".hidden"} {
		if err := os.WriteFile(filepath.Join(coverDir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	set := coverSetFromDir()
	if !set[1] || !set[2] {
		t.Errorf("set = %v, want games 1 and 2 present", set)
	}
	if len(set) != 2 {
		t.Errorf("set has %d entries, want 2 (thumbnails and junk excluded)", len(set))
	}
}

func TestCacheCoverRejectsNonImageResponse(t *testing.T) {
	coverDir := testCoverDir(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html>login wall or error page</html>")
	}))
	defer ts.Close()

	a := &App{}
	if p := a.cacheCover(1, ts.URL); p != "" {
		t.Errorf("non-image response must be rejected, got path %q", p)
	}
	if _, err := os.Stat(filepath.Join(coverDir, "1")); err == nil {
		t.Error("non-image content must not be persisted as a cover")
	}
}

func TestCacheCoverRejectsOversizedResponse(t *testing.T) {
	coverDir := testCoverDir(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		// 20 MiB of repeated PNG magic — over the 16 MiB cap.
		_, _ = w.Write(bytes.Repeat([]byte{0x89, 0x50, 0x4E, 0x47}, 5<<20))
	}))
	defer ts.Close()

	a := &App{}
	if p := a.cacheCover(1, ts.URL); p != "" {
		t.Errorf("oversized response must be rejected, got path %q", p)
	}
	if _, err := os.Stat(filepath.Join(coverDir, "1")); err == nil {
		t.Error("oversized content must not be persisted as a cover")
	}
}

func TestCacheCoverCoalescesConcurrentDownloads(t *testing.T) {
	coverDir := testCoverDir(t)
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(makePNG(t, 100, 100))
	}))
	defer ts.Close()

	a := &App{ctx: context.Background()}

	var wg sync.WaitGroup
	paths := make([]string, 4)
	for i := range paths {
		wg.Add(1)
		go func() {
			defer wg.Done()
			paths[i] = a.cacheCover(11, ts.URL+"/cover.png")
		}()
	}
	wg.Wait()

	if hits.Load() != 1 {
		t.Errorf("downloads = %d, want 1 (concurrent calls must coalesce)", hits.Load())
	}
	for _, p := range paths {
		if p != filepath.Join(coverDir, "11") {
			t.Errorf("cacheCover returned %q, want %q", p, filepath.Join(coverDir, "11"))
		}
	}
	if _, err := os.Stat(filepath.Join(coverDir, "11")); err != nil {
		t.Errorf("cover file missing: %v", err)
	}
}

// Covers cached before the thumbnailing change have no .thumb sibling;
// the backfill pass must generate them, skip existing thumbs, and ignore
// non-cover files. Small covers stay thumb-less (nothing to downscale).
func TestBackfillCoverThumbs(t *testing.T) {
	coverDir := testCoverDir(t)
	if err := os.MkdirAll(coverDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Large cover needing a thumbnail.
	big := filepath.Join(coverDir, "5")
	if err := os.WriteFile(big, makePNG(t, 1000, 500), 0o644); err != nil {
		t.Fatal(err)
	}
	// Cover that already has a thumbnail — must be left alone.
	withThumb := filepath.Join(coverDir, "6")
	if err := os.WriteFile(withThumb, makePNG(t, 800, 600), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(withThumb+".thumb", []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Small cover — no thumbnail expected.
	small := filepath.Join(coverDir, "7")
	if err := os.WriteFile(small, makePNG(t, 100, 80), 0o644); err != nil {
		t.Fatal(err)
	}
	// Non-cover junk file.
	if err := os.WriteFile(filepath.Join(coverDir, "notes.txt"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}

	n := backfillCoverThumbs()
	if n != 1 {
		t.Fatalf("backfillCoverThumbs wrote %d thumbnails, want 1", n)
	}
	if _, err := os.Stat(big + ".thumb"); err != nil {
		t.Errorf("thumb for big cover not written: %v", err)
	}
	if _, err := os.Stat(withThumb + ".thumb"); err != nil {
		t.Errorf("existing thumb was removed: %v", err)
	}
	if _, err := os.Stat(small + ".thumb"); err == nil {
		t.Error("thumb must not be written for small cover")
	}

	// Idempotent: a second pass writes nothing new.
	if n := backfillCoverThumbs(); n != 0 {
		t.Errorf("second pass wrote %d thumbnails, want 0", n)
	}
}

// avifMagic is a minimal valid AVIF header (ISOBMFF "ftyp" box with the
// "avif" brand, as served by F95Zone's CDN for .png/.jpg cover URLs).
var avifMagic = []byte{
	0x00, 0x00, 0x00, 0x1C, 'f', 't', 'y', 'p', 'a', 'v', 'i', 'f',
	0x00, 0x00, 0x00, 0x00, 'a', 'v', 'i', 'f', 'm', 'i', 'f', '1',
	'm', 'i', 'a', 'f',
}

func TestKnownImageFormatAVIF(t *testing.T) {
	if !knownImageFormat(avifMagic) {
		t.Error("AVIF bytes must be accepted as a known image format")
	}
	avis := append([]byte{}, avifMagic[:8]...)
	avis = append(avis, []byte("avis")...)
	avis = append(avis, avifMagic[12:]...)
	if !knownImageFormat(avis) {
		t.Error("AVIS (AVIF sequence) bytes must be accepted")
	}
	if knownImageFormat([]byte("not an image")) {
		t.Error("garbage must be rejected")
	}
	if knownImageFormat([]byte{0x00, 0x00, 0x00, 0x1C, 'f', 't', 'y', 'p', 'h', 'e', 'i', 'c'}) {
		t.Error("non-AVIF ISOBMFF brand must be rejected")
	}
}

func TestImageMimeFromPrefixAVIF(t *testing.T) {
	if got := imageMimeFromPrefix(avifMagic); got != "avif" {
		t.Errorf("imageMimeFromPrefix(avif) = %q, want %q", got, "avif")
	}
	if got := imageMimeFromPrefix([]byte{0xFF, 0xD8, 0xFF, 0xE0}); got != "jpeg" {
		t.Errorf("imageMimeFromPrefix(jpeg) = %q, want %q", got, "jpeg")
	}
}

func TestCacheCoverAcceptsAVIF(t *testing.T) {
	coverDir := testCoverDir(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/avif")
		_, _ = w.Write(avifMagic)
	}))
	defer ts.Close()

	a := &App{}
	if p := a.cacheCover(1, ts.URL); p == "" {
		t.Fatal("AVIF response must be accepted as a cover")
	}
	if _, err := os.Stat(filepath.Join(coverDir, "1")); err != nil {
		t.Errorf("AVIF cover not persisted: %v", err)
	}
}
