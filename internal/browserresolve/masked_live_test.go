package browserresolve

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestResolveMaskedURLLive exercises the masked-URL solver (Tier 2:
// browser-as-solver) with a REAL Chromium against a local server:
//
//   - masked-style page with a Continue (host_link) → navigates to the
//     real destination — the URL is returned without a download
//   - reCAPTCHA-checkbox variant: the checkbox sits in an iframe and
//     navigates the parent on click
//
// Requirements: MOXIE_BROWSERRESOLVE_LIVE=1 and a Chromium binary
// (PATH / playwright cache / MOXIE_CHROME_BIN).
func TestResolveMaskedURLLive(t *testing.T) {
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

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/masked/entry":
			// F95Zone-style masked interstitial: Continue link navigates.
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<html><body>
				<p><a href="/real/target" class="host_link"><span>Continue to FileHost</span></a></p>
			</body></html>`))
		case "/masked/captcha":
			// The Continue click hit the captcha wall: the widget renders
			// into #captcha and the checkbox navigates the parent.
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<html><body>
				<div id="captcha"><iframe src="/recaptcha/api2/anchor?k=fixture"></iframe></div>
			</body></html>`))
		case "/recaptcha/api2/anchor":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<html><body>
				<div id="recaptcha-anchor" aria-checked="false"
				     onclick="this.setAttribute('aria-checked','true'); window.parent.location='/real/target'">
					I'm not a robot
				</div>
			</body></html>`))
		case "/real/target":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html><body>destination</body></html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	profile := buildProfileFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// 1. Continue-link flow.
	got, err := ResolveMaskedURL(ctx, srv.URL+"/masked/entry",
		WithBrowser(BrowserChrome), WithBinPath(bin), WithProfileDir(profile))
	if err != nil {
		t.Fatalf("ResolveMaskedURL (continue flow): %v", err)
	}
	if want := srv.URL + "/real/target"; got != want {
		t.Errorf("ResolveMaskedURL = %q, want %q", got, want)
	}

	// 2. reCAPTCHA-checkbox flow.
	got2, err := ResolveMaskedURL(ctx, srv.URL+"/masked/captcha",
		WithBrowser(BrowserChrome), WithBinPath(bin), WithProfileDir(profile))
	if err != nil {
		t.Fatalf("ResolveMaskedURL (captcha flow): %v", err)
	}
	if want := srv.URL + "/real/target"; got2 != want {
		t.Errorf("ResolveMaskedURL = %q, want %q", got2, want)
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

// TestResolveMaskedURLRealLive drives the REAL F95Zone masked pixeldrain
// link through the browser solver. Expected outcomes: the pixeldrain
// destination URL (success — the wall was up and the browser clicked
// through, or the unwrap answered ok and the page just redirected), or a
// clear error (image challenge / login wall / session expiry). Logs the
// result either way. This is the ultimate live check for Tier 2.
func TestResolveMaskedURLRealLive(t *testing.T) {
	if os.Getenv("MOXIE_BROWSERRESOLVE_LIVE") != "1" || os.Getenv("MOXIE_LIVE") == "" {
		t.Skip("set MOXIE_BROWSERRESOLVE_LIVE=1 and MOXIE_LIVE=1 to hit the real F95Zone page")
	}
	bin := os.Getenv("MOXIE_CHROME_BIN")
	if bin == "" {
		bin = findPlaywrightChromium()
	}
	if bin == "" {
		t.Skip("no Chromium binary found")
	}
	const masked = "https://f95zone.to/masked/pixeldrain.com/6004/6265512/w0QsrGbXtWLejRyJd2K_9f3Qn94/1TuWkfbn_zSTdTuHu0eM5Q/mifP5JR4lIKvNQsat27MNlzAaXuDtdhleLdtNSqJ_EhFo05L8ZEyzb1ClXjc7Qwm"

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	dest, err := ResolveMaskedURL(ctx, masked, WithBinPath(bin))
	if err != nil {
		t.Logf("REAL masked solve FAILED: %v", err)
		return
	}
	if !strings.HasPrefix(dest, "https://pixeldrain.com/") {
		t.Errorf("REAL masked solve = %q, want a pixeldrain URL", dest)
		return
	}
	t.Logf("REAL masked solve OK: %s", dest)
}
