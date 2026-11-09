package downloader

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mili/moxie/internal/browser"
)

// Throwaway live probe for F95-j3b5: does the uTLS transport pass
// buzzheavier's Cloudflare challenge at BOTH the HTMX resolution layer and
// the file hop, using the user's real cf_clearance cookies (which stdlib TLS
// 403s on)? Run: MOXIE_LIVE=1 go test ./internal/downloader/ -run TestBuzzheavierUTLSLiveProbe -v
func TestBuzzheavierUTLSLiveProbe(t *testing.T) {
	if os.Getenv("MOXIE_LIVE") == "" {
		t.Skip("live probe; set MOXIE_LIVE=1")
	}
	raw := os.Getenv("BUZZ_LINK")
	if raw == "" {
		t.Fatal("set BUZZ_LINK to a live bzzhr.to link")
	}
	raw = strings.TrimRight(raw, "/")

	chromeUA := "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"
	ua := os.Getenv("BUZZ_UA")
	if ua == "" {
		ua = chromeUA
	}

	httmx := func(useUTLS bool) (int, string, string) {
		old := UseUTLSTransport
		defer func() { SetUTLSTransport(old) }()
		SetUTLSTransport(useUTLS)

		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, raw+"/download", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("User-Agent", ua)
		req.Header.Set("HX-Request", "true")
		req.Header.Set("HX-Current-URL", raw)
		req.Header.Set("Referer", raw)
		req.Header.Set("Accept", "*/*")
		if c, err := browser.GetCookiesForHost("bzzhr.to"); err == nil && c != "" {
			req.Header.Set("Cookie", c)
		}
		client := &http.Client{Timeout: 30 * time.Second, Transport: downloadTransport()}
		resp, err := client.Do(req)
		if err != nil {
			t.Logf("full error: %v", err)
			return 0, "", err.Error()
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return resp.StatusCode, resp.Header.Get("hx-redirect"), string(body)
	}

	// 1. HTMX layer: stdlib (baseline) vs uTLS.
	code, redir, _ := httmx(false)
	t.Logf("HTMX stdlib: status=%d hx-redirect=%q", code, redir)
	code, redir, body := httmx(true)
	t.Logf("HTMX uTLS:   status=%d hx-redirect=%q body[:60]=%q", code, redir, firstChars(body, 60))
	if code != 204 && code != 200 {
		t.Fatalf("uTLS HTMX status %d — challenge persists at resolution layer", code)
	}
	if redir == "" || redir == raw {
		t.Fatalf("hx-redirect empty or self-referential (%q) — token flow required", redir)
	}
	t.Logf("HTMX uTLS PASS → file URL: %s", redir)

	// 2. File hop: GET the resolved URL through uTLS with cookies for its host.
	old := UseUTLSTransport
	SetUTLSTransport(true)
	defer func() { SetUTLSTransport(old) }()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, redir, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Referer", raw)
	if host := hostOf(redir); host != "" {
		if c, err := browser.GetCookiesForHost(host); err == nil && c != "" {
			req.Header.Set("Cookie", c)
		}
	}
	client := &http.Client{Timeout: 30 * time.Second, Transport: downloadTransport()}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("file hop GET: %v", err)
	}
	defer resp.Body.Close()
	fb, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	ct := resp.Header.Get("Content-Type")
	challenged := resp.Header.Get("Cf-Mitigated") != "" ||
		strings.Contains(strings.ToLower(string(fb)), "cf-mitigated") ||
		strings.Contains(strings.ToLower(string(fb)), "challenge")
	t.Logf("file hop uTLS: status=%d ct=%s bytes=%d cf_mitigated=%v challenged=%v",
		resp.StatusCode, ct, len(fb), resp.Header.Get("Cf-Mitigated"), challenged)
	if resp.StatusCode == http.StatusOK && !strings.Contains(ct, "text/html") {
		t.Log("FILE HOP PASS — buzzheavier fully resolvable with uTLS")
		return
	}
	t.Log("file hop still challenged — needs browserresolve/JD for the final hop")
}

func firstChars(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func hostOf(raw string) string {
	s := strings.TrimPrefix(raw, "https://")
	s = strings.TrimPrefix(s, "http://")
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	return s
}
