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

// playwrightChromiumPaths are the Playwright-cached Chromium builds rod's
// own launcher discovery also finds (this dev machine has no system
// Chrome; the caches exist at ~/.cache/ms-playwright/chromium-*/).
var playwrightChromiumPaths = []string{
	"chromium-*/chrome-linux64/chrome",
	"chromium-*/chrome-linux/chrome",
	"chromium-*/chrome-mac/Chromium.app/Contents/MacOS/Chromium",
	"chromium-*/chrome-win/chrome.exe",
}

// findPlaywrightChromium locates a Playwright-cached Chromium binary, or ""
// when none exists.
func findPlaywrightChromium() string {
	cacheDir := filepath.Join(os.Getenv("HOME"), ".cache", "ms-playwright")
	if cacheDir == "" {
		return ""
	}
	for _, pattern := range playwrightChromiumPaths {
		matches, _ := filepath.Glob(filepath.Join(cacheDir, pattern))
		if len(matches) > 0 {
			if _, err := os.Stat(matches[0]); err == nil {
				return matches[0]
			}
		}
	}
	return ""
}

// TestResolveDownloadChromeClickLive exercises the rod engine's click
// automation with a REAL Chromium against a local server: a F95Zone-style
// masked "Continue" page (host_link) → the host page with a Download
// button → the attachment downloads. This is the flow Go's masked-URL path
// cannot pass (F95Zone's captcha wall) and raw-launch Firefox cannot click.
//
// Requirements: MOXIE_BROWSERRESOLVE_LIVE=1 and a Chromium binary
// (PATH / playwright cache / MOXIE_CHROME_BIN).
func TestResolveDownloadChromeClickLive(t *testing.T) {
	if os.Getenv(liveEnvVar) != "1" {
		t.Skipf("set %s=1 to run the live browser test", liveEnvVar)
	}
	if testing.Short() {
		t.Skip("skipping live browser test in -short mode")
	}
	bin := os.Getenv("MOXIE_CHROME_BIN")
	if bin == "" {
		bin = findPlaywrightChromium()
	}
	if bin == "" {
		if p, err := detectBrowserBinary(); err == nil {
			bin = p
		}
	}
	if bin == "" {
		t.Skip("no Chromium binary found (PATH, playwright cache, or MOXIE_CHROME_BIN)")
	}

	payload := bytes.Repeat([]byte("moxie-chrome-click-live-test-"), 32768)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/masked/entry":
			// F95Zone-style masked interstitial: click required.
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<html><body>
				<p>Click the above button to continue to the file host.</p>
				<p><a href="/host/page" class="host_link"><span>Continue to FileHost</span></a></p>
			</body></html>`))
		case "/host/page":
			// The host's file page with a download button.
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<html><body>
				<button id="dl" onclick="window.location='/file.zip'">Free Download</button>
			</body></html>`))
		case "/captcha/entry":
			// Headless-style reCAPTCHA challenge: an unchecked checkbox in
			// a same-origin iframe; clicking it navigates to the download.
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<html><body>
				<iframe src="/recaptcha/api2/anchor?k=fixture"></iframe>
			</body></html>`))
		case "/recaptcha/api2/anchor":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<html><body>
				<div id="recaptcha-anchor" aria-checked="false"
				     onclick="this.setAttribute('aria-checked','true'); window.parent.location='/file.zip'">
					I'm not a robot
				</div>
			</body></html>`))
		case "/file.zip":
			w.Header().Set("Content-Disposition", `attachment; filename="clicked-game.zip"`)
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "games")
	profile := buildProfileFixture(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	got, err := ResolveDownload(ctx, srv.URL+"/masked/entry", dest,
		WithBrowser(BrowserChrome),
		WithBinPath(bin),
		WithProfileDir(profile),
		WithTimeout(2*time.Minute),
		WithDownloadTimeout(60*time.Second),
	)
	if err != nil {
		t.Fatalf("ResolveDownload (chrome click flow): %v", err)
	}
	if want := filepath.Join(dest, "clicked-game.zip"); got != want {
		t.Errorf("ResolveDownload = %q, want %q", got, want)
	}
	content, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if !bytes.Equal(content, payload) {
		t.Errorf("downloaded %d bytes, want %d (content mismatch)", len(content), len(payload))
	}

	// The reCAPTCHA-checkbox variant: same pipeline, checkbox in an iframe
	// instead of a plain button.
	dest2 := filepath.Join(t.TempDir(), "games")
	got2, err := ResolveDownload(ctx, srv.URL+"/captcha/entry", dest2,
		WithBrowser(BrowserChrome),
		WithBinPath(bin),
		WithProfileDir(profile),
		WithTimeout(2*time.Minute),
		WithDownloadTimeout(60*time.Second),
	)
	if err != nil {
		t.Fatalf("ResolveDownload (recaptcha checkbox flow): %v", err)
	}
	if want := filepath.Join(dest2, "clicked-game.zip"); got2 != want {
		t.Errorf("ResolveDownload = %q, want %q", got2, want)
	}
	content2, err := os.ReadFile(got2)
	if err != nil {
		t.Fatalf("read recaptcha downloaded file: %v", err)
	}
	if !bytes.Equal(content2, payload) {
		t.Errorf("recaptcha download: %d bytes, want %d (content mismatch)", len(content2), len(payload))
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
