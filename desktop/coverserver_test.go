package main

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

	// http.Get sends Host: 127.0.0.1:<port>, which must pass the loopback
	// validation — the rebinding defense only refuses foreign Hosts.
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
	if got, _ := io.ReadAll(resp.Body); !bytes.Equal(got, data) {
		t.Error("body does not match the cached cover file")
	}
}

// TestCoverServerRejectsNonLoopbackHost is the DNS-rebinding defense: a
// request that connects to the loopback listener but carries a foreign Host
// header (an attacker domain resolved to 127.0.0.1) must be refused with 403
// even when the path names a real cover.
func TestCoverServerRejectsNonLoopbackHost(t *testing.T) {
	coverDir := testCoverDir(t)
	if err := os.MkdirAll(coverDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(coverDir, "42"), makePNG(t, 32, 32), 0o644); err != nil {
		t.Fatal(err)
	}

	cs := startCoverServer()
	if cs == nil {
		t.Fatal("startCoverServer returned nil")
	}
	t.Cleanup(cs.Close)

	for _, host := range []string{"attacker.example", "127.0.0.1.attacker.example", "192.168.1.10"} {
		req, err := http.NewRequest("GET", cs.BaseURL()+"/cover/42", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = host
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET with Host %q: %v", host, err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("GET with Host %q = %d, want 403", host, resp.StatusCode)
		}
	}

	// The same cover served with the proper loopback Host still works.
	resp, err := http.Get(cs.BaseURL() + "/cover/42")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET with loopback Host = %d, want 200", resp.StatusCode)
	}
}

// TestIsLoopbackHost pins down the Host forms the cover server accepts:
// the two bracket/port variants the webview actually sends, plus plain
// hostnames. Anything else — including a rebinding domain — is refused.
func TestIsLoopbackHost(t *testing.T) {
	cs := &coverServer{}
	tests := []struct {
		host string
		want bool
	}{
		{"127.0.0.1", true},
		{"127.0.0.1:41233", true},
		{"localhost", true},
		{"localhost:5555", true},
		{"[::1]:8080", true},
		{"attacker.example", false},
		{"127.0.0.1.attacker.example", false},
		{"192.168.1.10", false},
		{"127.0.0.1.evil.com:80", false},
	}
	for _, tt := range tests {
		if got := cs.isLoopbackHost(tt.host); got != tt.want {
			t.Errorf("isLoopbackHost(%q) = %v, want %v", tt.host, got, tt.want)
		}
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

// TestCoverContentTypeTable covers every sniffed format served by the cover
// server — including the AVIF/AVIS ISOBMFF brands F95Zone's CDN serves under
// .png/.jpg URLs — and asserts the hardening behavior: unrecognized magic
// bytes are served as application/octet-stream, never mislabeled as an image.
// coverContentType is the function the server actually uses (it sits on top
// of imageMimeFromPrefix, which still falls back to "png" for unknown input).
func TestCoverContentTypeTable(t *testing.T) {
	avis := append([]byte{}, avifMagic[:8]...)
	avis = append(avis, []byte("avis")...)
	avis = append(avis, avifMagic[12:]...)

	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0}, "image/jpeg"},
		{"png", []byte{0x89, 0x50, 0x4E, 0x47}, "image/png"},
		{"webp", append([]byte("RIFF...."), []byte("WEBP")...), "image/webp"},
		{"gif", []byte("GIF89a"), "image/gif"},
		{"avif", avifMagic, "image/avif"},
		{"avis", avis, "image/avif"},
		// RIFF without the WEBP brand and arbitrary garbage must never be
		// labeled as images — the octet-stream default.
		{"riff-not-webp", []byte("RIFF....XXXX"), "application/octet-stream"},
		{"garbage", []byte("not an image"), "application/octet-stream"},
		{"empty", nil, "application/octet-stream"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tt.name)
			if err := os.WriteFile(path, tt.data, 0o644); err != nil {
				t.Fatal(err)
			}
			if got := coverContentType(path); got != tt.want {
				t.Errorf("coverContentType(%s) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

// TestDecodeCoverImageAVIF asserts AVIF covers fail with the typed
// errCoverFormatNotThumbnailable (graceful degradation: webview renders the
// full image, no thumbnail), NOT the misleading stdlib "image: unknown
// format" that used to spam the log for every cached AVIF cover.
func TestDecodeCoverImageAVIF(t *testing.T) {
	img, err := decodeCoverImage(avifMagic)
	if img != nil {
		t.Error("decodeCoverImage(avif) returned an image, want nil")
	}
	if err == nil {
		t.Fatal("decodeCoverImage(avif) must fail: no pure-Go AVIF decoder")
	}
	if !errors.Is(err, errCoverFormatNotThumbnailable) {
		t.Errorf("err = %v, want wrap of errCoverFormatNotThumbnailable", err)
	}
	if strings.Contains(err.Error(), "image: unknown format") {
		t.Errorf("err = %q, must not surface the misleading stdlib unknown-format error", err)
	}
}

// TestWriteCoverThumbSkipsAVIF asserts AVIF covers produce no .thumb file
// (the cover server falls back to the full image) and report thumbSkipAVIF
// so backfill can count them without a per-cover Warn.
func TestWriteCoverThumbSkipsAVIF(t *testing.T) {
	coverDir := testCoverDir(t)
	if err := os.MkdirAll(coverDir, 0o755); err != nil {
		t.Fatal(err)
	}

	coverPath := filepath.Join(coverDir, "21")
	if err := os.WriteFile(coverPath, avifMagic, 0o644); err != nil {
		t.Fatal(err)
	}

	if got := writeCoverThumb(coverPath); got != thumbSkipAVIF {
		t.Errorf("writeCoverThumb(avif) = %v, want thumbSkipAVIF", got)
	}
	if _, err := os.Stat(coverPath + ".thumb"); err == nil {
		t.Error("no .thumb must be written for AVIF covers")
	}
}

// TestBackfillCoverThumbsSkipsAVIFAndCorrupt mixes a thumbnailable PNG, an
// AVIF cover, and a corrupt blob: only the PNG gets a thumbnail, and the
// backfill still returns the written count so the summary line is accurate.
func TestBackfillCoverThumbsSkipsAVIFAndCorrupt(t *testing.T) {
	coverDir := testCoverDir(t)
	if err := os.MkdirAll(coverDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Thumbnailable large PNG.
	big := filepath.Join(coverDir, "5")
	if err := os.WriteFile(big, makePNG(t, 1000, 500), 0o644); err != nil {
		t.Fatal(err)
	}
	// AVIF cover — accepted for caching, skipped for thumbnails.
	avif := filepath.Join(coverDir, "6")
	if err := os.WriteFile(avif, avifMagic, 0o644); err != nil {
		t.Fatal(err)
	}
	// Corrupt blob — decode failure, no thumbnail.
	corrupt := filepath.Join(coverDir, "7")
	if err := os.WriteFile(corrupt, []byte("definitely not an image"), 0o644); err != nil {
		t.Fatal(err)
	}

	n := backfillCoverThumbs()
	if n != 1 {
		t.Fatalf("backfillCoverThumbs wrote %d thumbnails, want 1", n)
	}
	if _, err := os.Stat(big + ".thumb"); err != nil {
		t.Errorf("thumb for PNG cover not written: %v", err)
	}
	if _, err := os.Stat(avif + ".thumb"); err == nil {
		t.Error("thumb must not be written for AVIF cover")
	}
	if _, err := os.Stat(corrupt + ".thumb"); err == nil {
		t.Error("thumb must not be written for corrupt cover")
	}
}

// TestBackfillCoverThumbsMarkerSkipsPass: a version-scoped completion marker
// must suppress the whole walk on later launches — even thumbnails that are
// still missing stay missing (a deliberate trade: the decode-everything walk
// runs once per version, and covers cached after it get their thumbs from
// cacheCover's own writeCoverThumb call).
func TestBackfillCoverThumbsMarkerSkipsPass(t *testing.T) {
	coverDir := testCoverDir(t)
	if err := os.MkdirAll(coverDir, 0o755); err != nil {
		t.Fatal(err)
	}

	big := filepath.Join(coverDir, "5")
	if err := os.WriteFile(big, makePNG(t, 1000, 500), 0o644); err != nil {
		t.Fatal(err)
	}
	// Simulate a completed pass from a previous launch.
	if err := os.WriteFile(filepath.Join(coverDir, backfillMarkerName()), []byte(appVersion+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if n := backfillCoverThumbs(); n != 0 {
		t.Errorf("backfill with marker present wrote %d thumbnails, want 0", n)
	}
	if _, err := os.Stat(big + ".thumb"); err == nil {
		t.Error("marker must suppress the walk: no thumbnail may be written")
	}
}

// TestBackfillCoverThumbsCancelledCtxLeavesNoMarker: aborting the walk via
// the context argument must not write the completion marker, so the next
// launch retries the remaining covers. An uncancelled follow-up run then
// backfills normally.
func TestBackfillCoverThumbsCancelledCtxLeavesNoMarker(t *testing.T) {
	coverDir := testCoverDir(t)
	if err := os.MkdirAll(coverDir, 0o755); err != nil {
		t.Fatal(err)
	}

	big := filepath.Join(coverDir, "5")
	if err := os.WriteFile(big, makePNG(t, 1000, 500), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if n := backfillCoverThumbs(ctx); n != 0 {
		t.Errorf("backfill with cancelled ctx wrote %d thumbnails, want 0", n)
	}
	if _, err := os.Stat(filepath.Join(coverDir, backfillMarkerName())); err == nil {
		t.Error("no completion marker may be written for an aborted pass")
	}

	// The follow-up run is not poisoned by the aborted attempt.
	if n := backfillCoverThumbs(); n != 1 {
		t.Errorf("follow-up backfill wrote %d thumbnails, want 1", n)
	}
	if _, err := os.Stat(big + ".thumb"); err != nil {
		t.Errorf("thumb not written by follow-up run: %v", err)
	}
}
