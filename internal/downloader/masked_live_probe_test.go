package downloader

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

// Throwaway live probe: resolve a real masked pixeldrain link with browser
// cookies to see what the unwrap endpoint answers (ok vs captcha), and
// whether the session cookie is logged in at all.
// Run: MOXIE_LIVE=1 go test ./internal/downloader/ -run TestMaskedUnwrapLive -v
func TestMaskedUnwrapLive(t *testing.T) {
	if os.Getenv("MOXIE_LIVE") == "" {
		t.Skip("live probe; set MOXIE_LIVE=1")
	}
	r := NewHostResolver()
	masked := "https://f95zone.to/masked/pixeldrain.com/6004/6265512/w0QsrGbXtWLejRyJd2K_9f3Qn94/1TuWkfbn_zSTdTuHu0eM5Q/mifP5JR4lIKvNQsat27MNlzAaXuDtdhleLdtNSqJ_EhFo05L8ZEyzb1ClXjc7Qwm"

	// 1. Is the session logged in? GET the homepage and sniff for the
	// logged-in user menu vs the login link.
	req, _ := http.NewRequest("GET", "https://f95zone.to/", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")
	r.attachBrowserCookies(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("homepage GET: %v", err)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	resp.Body.Close()
	home := string(body)
	t.Logf("homepage: %d bytes, status %d", len(home), resp.StatusCode)
	switch {
	case strings.Contains(home, "href=\"login/login\""):
		t.Log("→ NOT LOGGED IN (login link present) — session cookie is stale")
	case strings.Contains(home, "account/") || strings.Contains(home, "user-menu"):
		t.Log("→ LOGGED IN (user menu present)")
	default:
		t.Log("→ ambiguous homepage markers")
	}

	// 2. The masked unwrap itself.
	realURL, err := r.unwrapMasked(masked)
	if err != nil {
		t.Logf("UNWRAP FAILED: %v", err)
		if strings.Contains(err.Error(), "captcha") {
			t.Log("→ F95Zone is serving the captcha wall to these cookies")
		}
	}

	// 3. What does the masked page itself contain (what a browser would see)?
	mreq, _ := http.NewRequest("GET", masked, nil)
	mreq.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")
	r.attachBrowserCookies(mreq)
	mresp, err := http.DefaultClient.Do(mreq)
	if err != nil {
		t.Fatalf("masked page GET: %v", err)
	}
	mbody, _ := io.ReadAll(io.LimitReader(mresp.Body, 2<<20))
	mresp.Body.Close()
	mpage := string(mbody)
	t.Logf("masked page: %d bytes, status %d", len(mpage), mresp.StatusCode)
	for _, marker := range []string{"recaptcha", "g-recaptcha", "cf-turnstile", "h-captcha", "continue", "Continue", "auto_submit", "submit()", "meta http-equiv=\"refresh\"", "onload"} {
		if strings.Contains(mpage, marker) {
			t.Logf("  marker %q FOUND", marker)
		}
	}
	if i := strings.Index(mpage, "Continue"); i >= 0 {
		t.Logf("  continue context: %s", cut(mpage[i-200:i+200], 400))
	}
	// Dump ALL script/recaptcha wiring so we can see whether the captcha
	// callback auto-submits or waits for the button click.
	for _, needle := range []string{"grecaptcha", "recaptcha", "onload", "host_link", "loading", "window.location", "location.href", "click", "submit"} {
		if i := strings.Index(strings.ToLower(mpage), strings.ToLower(needle)); i >= 0 {
			t.Logf("  [%s] context: %s", needle, cut(mpage[i-100:i+300], 380))
		}
	}
	if realURL != "" {
		t.Logf("UNWRAP OK: %s", realURL)
	}
}

func cut(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
