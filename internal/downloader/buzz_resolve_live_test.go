package downloader

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Live end-to-end verification of the token-based resolver against the real
// bzzhr.to/fafda.to chain (F95-hs4y acceptance). Uses the production path:
// NewHostResolver() → browser cookies (kooky) → page → t= token →
// /download?t=&alt=true → hx-redirect → fafda.to probe.
// Run: MOXIE_LIVE=1 go test ./internal/downloader/ -run TestBuzzheavierResolveLive -v
func TestBuzzheavierResolveLive(t *testing.T) {
	if os.Getenv("MOXIE_LIVE") == "" {
		t.Skip("live probe; set MOXIE_LIVE=1")
	}
	raw := strings.TrimRight(os.Getenv("BUZZ_LINK"), "/")
	if raw == "" {
		raw = "https://bzzhr.to/e2yt4zd66jq3"
	}

	r := NewHostResolver()
	start := time.Now()
	res, err := r.Resolve(raw, "buzzheavier")
	if err != nil {
		t.Fatalf("live resolve failed: %v (origin may still be down)", err)
	}
	t.Logf("resolved in %s: %s (headers: %d)", time.Since(start), redactedURL(res.URL), len(res.Headers))
	if !strings.HasPrefix(res.URL, "https://fafda.to/") {
		t.Fatalf("resolved URL = %q, want fafda.to origin", res.URL)
	}
	if !strings.Contains(res.URL, "v=") {
		t.Fatalf("resolved URL %q missing the v= token", redactedURL(res.URL))
	}
	t.Log("LIVE PASS — buzzheavier resolves to fafda.to with a valid token")

	// File hop: plain stdlib GET with UA (per F95-hs4y) — fetch 64 KiB and
	// verify real bytes come back, proving the token downloads.
	hopReq, err := http.NewRequest(http.MethodGet, res.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	hopReq.Header.Set("User-Agent", buzzUserAgent)
	hopReq.Header.Set("Accept", "*/*")
	hopReq.Header.Set("Range", "bytes=0-65535")
	hopResp, err := http.DefaultClient.Do(hopReq)
	if err != nil {
		t.Fatalf("file hop: %v", err)
	}
	defer hopResp.Body.Close()
	hopBytes, _ := io.ReadAll(io.LimitReader(hopResp.Body, 65536))
	if hopResp.StatusCode != http.StatusPartialContent && hopResp.StatusCode != http.StatusOK {
		t.Fatalf("file hop: HTTP %d %s", hopResp.StatusCode, strings.TrimSpace(string(hopBytes)))
	}
	if len(hopBytes) == 0 {
		t.Fatal("file hop: zero bytes received")
	}
	t.Logf("FILE HOP PASS — HTTP %d, %d bytes, Content-Length %d, Content-Type %q",
		hopResp.StatusCode, len(hopBytes), hopResp.ContentLength, hopResp.Header.Get("Content-Type"))
}
