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

// Throwaway matrix probe: repeated stdlib vs uTLS requests against the
// buzzheavier HTMX endpoint + share page, to test the "adaptive/rate-limit
// challenge, not TLS fingerprint" claim from research-buzzheavier-cloudflare.md.
// Run: MOXIE_LIVE=1 go test ./internal/downloader/ -run TestBuzzMatrix -v
func TestBuzzMatrix(t *testing.T) {
	if os.Getenv("MOXIE_LIVE") == "" {
		t.Skip("live probe; set MOXIE_LIVE=1")
	}
	raw := strings.TrimRight(os.Getenv("BUZZ_LINK"), "/")
	if raw == "" {
		raw = "https://bzzhr.to/e2yt4zd66jq3"
	}
	ua := "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"

	cookie, _ := browser.GetCookiesForHost("bzzhr.to")
	t.Logf("cookie present: %v (%d chars)", cookie != "", len(cookie))

	hit := func(useUTLS bool) (int, bool) {
		old := UseUTLSTransport
		SetUTLSTransport(useUTLS)
		defer func() { SetUTLSTransport(old) }()

		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, raw+"/download", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("User-Agent", ua)
		req.Header.Set("HX-Request", "true")
		req.Header.Set("priority", "u=1, i")
		req.Header.Set("HX-Current-URL", raw)
		req.Header.Set("Referer", raw)
		req.Header.Set("Accept", "*/*")
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		client := &http.Client{Timeout: 30 * time.Second, Transport: downloadTransport()}
		resp, err := client.Do(req)
		if err != nil {
			return 0, false
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
		challenged := resp.StatusCode == 403
		return resp.StatusCode, challenged
	}

	stats := map[string][2]int{} // label → [pass, challenge]
	for round := 1; round <= 4; round++ {
		for _, mode := range []struct {
			label string
			utls  bool
		}{{"stdlib", false}, {"utls-chrome133", true}} {
			code, ch := hit(mode.utls)
			if code == 204 || code == 200 {
				stats[mode.label] = [2]int{stats[mode.label][0] + 1, stats[mode.label][1]}
			} else {
				stats[mode.label] = [2]int{stats[mode.label][0], stats[mode.label][1] + 1}
			}
			t.Logf("round %d %s: status=%d challenged=%v", round, mode.label, code, ch)
			time.Sleep(700 * time.Millisecond)
		}
	}
	for label, s := range stats {
		t.Logf("SUMMARY %s: pass=%d challenge=%d", label, s[0], s[1])
	}
}
