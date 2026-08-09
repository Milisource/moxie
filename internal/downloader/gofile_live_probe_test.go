package downloader

import (
	"context"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

// Throwaway live probe for F95-nml7: resolve a REAL masked gofile link end
// to end (unwrap → guest token → wt → contents API → direct link → HEAD).
// Run: MOXIE_LIVE=1 go test ./internal/downloader/ -run TestGofileLiveProbe -v
func TestGofileLiveProbe(t *testing.T) {
	if os.Getenv("MOXIE_LIVE") == "" {
		t.Skip("live probe; set MOXIE_LIVE=1")
	}
	masked := os.Getenv("GOFILE_MASKED")
	if masked == "" {
		t.Fatal("set GOFILE_MASKED to a live masked gofile URL")
	}
	r := NewHostResolver()
	r.SetF95Cookie(os.Getenv("F95_COOKIE"))

	real, err := r.unwrapMasked(masked)
	if err != nil {
		t.Fatalf("unwrapMasked: %v", err)
	}
	t.Logf("masked → %s", real)

	res, err := r.resolveGofile(real)
	if err != nil {
		t.Fatalf("resolveGofile: %v", err)
	}
	t.Logf("resolved → %s", res.URL)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodHead, res.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range res.Headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("HEAD direct URL: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	t.Logf("direct URL HEAD: %d content-type=%s content-length=%s",
		resp.StatusCode, resp.Header.Get("Content-Type"), resp.Header.Get("Content-Length"))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("direct URL status %d", resp.StatusCode)
	}
}
