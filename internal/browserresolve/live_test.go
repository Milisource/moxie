package browserresolve

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// liveEnvVar opts into the live browser test. It launches real headless
// Chrome on a copied fixture profile against a local HTTP server, proving
// the full rod pipeline: launch flags, download behavior with events,
// download-event tracking, file verification, move into the destination,
// and teardown (profile copy deleted). It is skipped by default so the
// committed test suite passes offline; the challenge-host verification
// against a real CDN needs a manual run with an actual resolved URL.
const liveEnvVar = "MOXIE_BROWSERRESOLVE_LIVE"

// TestResolveDownloadLive exercises ResolveDownload end-to-end with a real
// browser. Requirements: MOXIE_BROWSERRESOLVE_LIVE=1 and a Chrome-family
// binary on PATH.
func TestResolveDownloadLive(t *testing.T) {
	if os.Getenv(liveEnvVar) != "1" {
		t.Skipf("set %s=1 to run the live browser test", liveEnvVar)
	}
	if testing.Short() {
		t.Skip("skipping live browser test in -short mode")
	}
	if _, err := detectBrowserBinary(); err != nil {
		t.Skip("no Chrome/Chromium binary on PATH: " + err.Error())
	}

	// ~1 MiB of deterministic content so the size check is meaningful.
	payload := bytes.Repeat([]byte("moxie-browserresolve-live-test-"), 32768)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/file.zip" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="live-sample.zip"`)
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "games")
	profile := buildProfileFixture(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	got, err := ResolveDownload(ctx, srv.URL+"/file.zip", dest,
		WithProfileDir(profile),
		WithTimeout(90*time.Second),
		WithDownloadTimeout(60*time.Second),
	)
	if err != nil {
		t.Fatalf("ResolveDownload: %v", err)
	}
	if want := filepath.Join(dest, "live-sample.zip"); got != want {
		t.Errorf("ResolveDownload = %q, want %q", got, want)
	}
	content, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if !bytes.Equal(content, payload) {
		t.Errorf("downloaded %d bytes, want %d (content mismatch)", len(content), len(payload))
	}

	// Teardown guarantee: no moxie-browser-* temp dir (profile copy,
	// download dir) may survive the session.
	before := map[string]bool{}
	for _, name := range moxieTempDirs(t) {
		before[name] = true
	}
	// Give Windows a moment to release file handles before the check.
	time.Sleep(100 * time.Millisecond)
	for _, name := range moxieTempDirs(t) {
		if !before[name] {
			t.Errorf("temp dir %q survived teardown", name)
		}
	}
}

// moxieTempDirs lists the names of browserresolve temp dirs under the
// system temp directory.
func moxieTempDirs(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatalf("read temp dir: %v", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "moxie-browser-") {
			names = append(names, e.Name())
		}
	}
	return names
}
