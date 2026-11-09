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

// Throwaway: fetch the buzzheavier share page with the browser cookie via
// stdlib transport and dump it, to look for the Turnstile widget.
// Run: MOXIE_LIVE=1 go test ./internal/downloader/ -run TestBuzzPageDump -v
func TestBuzzPageDump(t *testing.T) {
	if os.Getenv("MOXIE_LIVE") == "" {
		t.Skip("live probe; set MOXIE_LIVE=1")
	}
	raw := strings.TrimRight(os.Getenv("BUZZ_LINK"), "/")
	if raw == "" {
		raw = "https://bzzhr.to/e2yt4zd66jq3"
	}
	cookie, _ := browser.GetCookiesForHost("bzzhr.to")
	for attempt := 1; attempt <= 5; attempt++ {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")
		req.Header.Set("Accept", "text/html,application/xhtml+xml")
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		client := &http.Client{Timeout: 20 * time.Second, Transport: downloadTransport()}
		resp, err := client.Do(req)
		if err != nil {
			t.Logf("attempt %d: err %v", attempt, err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Logf("attempt %d: status=%d bytes=%d", attempt, resp.StatusCode, len(body))
		if resp.StatusCode == 200 {
			os.WriteFile("/tmp/opencode/buzz_page_200.html", body, 0644)
			for _, marker := range []string{"turnstile", "sitekey", "cf-chl", "hx-get", "download?t="} {
				if strings.Contains(strings.ToLower(string(body)), marker) {
					t.Logf("  marker %q FOUND", marker)
				}
			}
			return
		}
		time.Sleep(800 * time.Millisecond)
	}
	t.Log("no 200 captured in 5 attempts")
}
