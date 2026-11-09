package browserresolve

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestInstallDownloaderFallbackLive verifies the production wiring: with
// MOXIE_BROWSER=firefox and a real Firefox installed, InstallDownloaderFallback
// installs the downloader hook (the Go path stays primary; the hook only
// fires on Cloudflare challenges).
func TestInstallDownloaderFallbackLive(t *testing.T) {
	if os.Getenv(liveEnvVar) != "1" {
		t.Skipf("set %s=1 to run the live browser test", liveEnvVar)
	}
	if _, err := detectFirefoxBinary(); err != nil {
		t.Skip("no Firefox binary found: " + err.Error())
	}
	t.Setenv(browserEnvVar, BrowserFirefox)
	installed, why := InstallDownloaderFallback()
	if !installed {
		t.Fatalf("InstallDownloaderFallback: not installed (%s)", why)
	}
	t.Log("LIVE PASS — browser fallback installed via production wiring")
}

// TestResolveDownloadFirefoxLive exercises the full Firefox raw-launch
// pipeline with the REAL firefox binary against a local HTTP server:
// profile discovery (profiles.ini) → copy → user.js → headless launch →
// download-dir polling (.part ignored) → verify → move into destDir →
// teardown (profile copy deleted).
//
// Requirements: MOXIE_BROWSERRESOLVE_LIVE=1 and a Firefox binary
// (PATH / MOXIE_FIREFOX_BIN / standard install locations).
func TestResolveDownloadFirefoxLive(t *testing.T) {
	if os.Getenv(liveEnvVar) != "1" {
		t.Skipf("set %s=1 to run the live browser test", liveEnvVar)
	}
	if testing.Short() {
		t.Skip("skipping live browser test in -short mode")
	}
	if _, err := detectFirefoxBinary(); err != nil {
		t.Skip("no Firefox binary found: " + err.Error())
	}

	// ~1 MiB of deterministic content so the size check is meaningful.
	payload := bytes.Repeat([]byte("moxie-firefox-live-test-"), 32768)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/file.zip" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="firefox-live-sample.zip"`)
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "games")
	profileRoot, _ := buildFirefoxFixture(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	got, err := ResolveDownload(ctx, srv.URL+"/file.zip", dest,
		WithBrowser(BrowserFirefox),
		WithProfileDir(profileRoot),
		WithTimeout(2*time.Minute),
		WithDownloadTimeout(90*time.Second),
	)
	if err != nil {
		t.Fatalf("ResolveDownload (firefox): %v", err)
	}
	if want := filepath.Join(dest, "firefox-live-sample.zip"); got != want {
		t.Errorf("ResolveDownload = %q, want %q", got, want)
	}
	content, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if !bytes.Equal(content, payload) {
		t.Errorf("downloaded %d bytes, want %d (content mismatch)", len(content), len(payload))
	}

	// Teardown guarantee: no moxie-browser-* temp dir may survive.
	before := map[string]bool{}
	for _, name := range moxieTempDirs(t) {
		before[name] = true
	}
	time.Sleep(100 * time.Millisecond)
	for _, name := range moxieTempDirs(t) {
		if !before[name] {
			t.Errorf("temp dir %q survived teardown", name)
		}
	}
}
