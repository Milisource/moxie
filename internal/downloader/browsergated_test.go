package downloader

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rewriteTestHost routes requests for host to srv for the duration of the
// test, restoring the previous transport override on cleanup.
func rewriteTestHost(t *testing.T, host string, srv *httptest.Server) {
	t.Helper()
	old := testTransportOverride
	testTransportOverride = rewriteToServer(host, srv.URL)
	t.Cleanup(func() { testTransportOverride = old })
}

// htmlLandingServer answers every request with a 200 text/html landing page,
// mimicking a JS-driven free-download host.
func htmlLandingServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html><body><button>Download</button></body></html>`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// recordingFallback is a browser-fallback stub that records every call and
// optionally writes a file into destDir (mimicking a browser download).
func recordingFallback(t *testing.T, write bool) (func(ctx context.Context, url, destDir string) (string, error), *[]string) {
	t.Helper()
	var calls []string
	return func(_ context.Context, url, destDir string) (string, error) {
		calls = append(calls, url+"|"+destDir)
		if !write {
			return "", fmt.Errorf("stub browser failure")
		}
		path := filepath.Join(destDir, "from-browser.zip")
		if err := os.WriteFile(path, []byte("browser-downloaded"), 0o600); err != nil {
			return "", err
		}
		return path, nil
	}, &calls
}

// TestIsBrowserGatedHost covers the gated/non-gated host table.
func TestIsBrowserGatedHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"mixdrop", true},
		{"hexload", true},
		{"dropmefiles", true},
		{"uploadnow", true},
		{"googledrive", true},
		{"datanodes", true},
		{"buzzheavier", true},
		{"uploadhaven", true},
		{"workupload", true},
		{"krakenfiles", true},
		{"bunkrr", true},
		{"MIXDROP", true}, // case-insensitive
		{"mega", false},   // delegated to megatools
		{"vikingfile", false},
		{"pixeldrain", false},
		{"gofile", false},
		{"mediafire", false},
		{"unknown", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isBrowserGatedHost(tt.host); got != tt.want {
			t.Errorf("isBrowserGatedHost(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}

// TestIsBrowserGatedURL covers the URL predicate, which derives the host
// label via IdentifyHostInURL.
func TestIsBrowserGatedURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://mixdrop.ag/f/abc123", true},
		{"https://drive.google.com/uc?export=download&id=x", true},
		{"https://uploadnow.io/f/x", true},
		{"https://cdn.example.com/file.zip", false},
		{"https://mega.nz/file/x", false},
		{"https://pixeldrain.com/u/x", false},
		{"not a url", false},
	}
	for _, tt := range tests {
		if got := isBrowserGatedURL(tt.url); got != tt.want {
			t.Errorf("isBrowserGatedURL(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

// TestDownloadWithHeaders_GatedHostHTML_Fallback verifies a 200 text/html
// landing page from a browser-gated host invokes the browser fallback with
// the ORIGINAL URL and reports success.
func TestDownloadWithHeaders_GatedHostHTML_Fallback(t *testing.T) {
	srv := htmlLandingServer(t)
	rewriteTestHost(t, "mixdrop.ag", srv)

	fb, calls := recordingFallback(t, true)
	destDir := t.TempDir()
	url := "https://mixdrop.ag/f/abc123"

	err := downloadWithHeaders(context.Background(), url, nil, url, "mixdrop", destDir, 0, nil, nil, fb)
	if err != nil {
		t.Fatalf("downloadWithHeaders: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("fallback called %d times, want 1", len(*calls))
	}
	if !strings.HasPrefix((*calls)[0], url+"|"+destDir) {
		t.Errorf("fallback args = %q, want original URL + destDir", (*calls)[0])
	}
	if _, err := os.Stat(filepath.Join(destDir, "from-browser.zip")); err != nil {
		t.Errorf("browser-downloaded file missing: %v", err)
	}
}

// TestDownloadWithHeaders_UnknownHostHTML_NoFallback verifies an unknown host
// returning 200 text/html keeps the fast hard reject and does NOT spend a
// browser attempt.
func TestDownloadWithHeaders_UnknownHostHTML_NoFallback(t *testing.T) {
	srv := htmlLandingServer(t)
	rewriteTestHost(t, "cdn.example.com", srv)

	fb, calls := recordingFallback(t, true)
	url := "https://cdn.example.com/file.zip"

	err := downloadWithHeaders(context.Background(), url, nil, url, "unknown", t.TempDir(), 0, nil, nil, fb)
	if err == nil {
		t.Fatal("expected the hard reject for an unknown host")
	}
	if !strings.Contains(err.Error(), "server returned HTML instead of file") {
		t.Errorf("error = %v, want the existing reject message", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("fallback called %d times, want 0 for an unknown host", len(*calls))
	}
}

// TestDownloadWithHeaders_GatedURLUnknownLabel_Fallback verifies the resolved
// hop is classified by URL even when the caller passed an "unknown" label —
// the IdentifyHostInURL path.
func TestDownloadWithHeaders_GatedURLUnknownLabel_Fallback(t *testing.T) {
	srv := htmlLandingServer(t)
	rewriteTestHost(t, "uploadnow.io", srv)

	fb, calls := recordingFallback(t, true)
	url := "https://uploadnow.io/f/x"

	if err := downloadWithHeaders(context.Background(), url, nil, url, "unknown", t.TempDir(), 0, nil, nil, fb); err != nil {
		t.Fatalf("downloadWithHeaders: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("fallback called %d times, want 1", len(*calls))
	}
}

// TestDownloadWithHeaders_GatedHostFallbackFails verifies a failed browser
// attempt surfaces an error that still carries the existing reject wording.
func TestDownloadWithHeaders_GatedHostFallbackFails(t *testing.T) {
	srv := htmlLandingServer(t)
	rewriteTestHost(t, "hexload.com", srv)

	fb, calls := recordingFallback(t, false)
	url := "https://hexload.com/abc"

	err := downloadWithHeaders(context.Background(), url, nil, url, "hexload", t.TempDir(), 0, nil, nil, fb)
	if err == nil {
		t.Fatal("expected error when the browser fallback fails")
	}
	if !strings.Contains(err.Error(), "server returned HTML instead of file") {
		t.Errorf("error = %v, want the existing reject wording", err)
	}
	if !strings.Contains(err.Error(), "browser fallback also failed") {
		t.Errorf("error = %v, want fallback-failure context", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("fallback called %d times, want 1", len(*calls))
	}
}

// TestDownloadWithHeaders_GatedHost404_NoFallback verifies a terminal 404 is
// never handed to the browser, even from a gated host.
func TestDownloadWithHeaders_GatedHost404_NoFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("<html>404</html>"))
	}))
	t.Cleanup(srv.Close)
	rewriteTestHost(t, "dropmefiles.com", srv)

	fb, calls := recordingFallback(t, true)
	url := "https://dropmefiles.com/dead"

	err := downloadWithHeaders(context.Background(), url, nil, url, "dropmefiles", t.TempDir(), 0, nil, nil, fb)
	if err == nil {
		t.Fatal("expected an error for a 404 page")
	}
	if len(*calls) != 0 {
		t.Fatalf("fallback called %d times, want 0 for a terminal 404", len(*calls))
	}
}

// TestDownloadWithHeaders_GatedHostNoFallback verifies the pre-existing
// behavior is preserved when no browser hook is installed.
func TestDownloadWithHeaders_GatedHostNoFallback(t *testing.T) {
	srv := htmlLandingServer(t)
	rewriteTestHost(t, "mixdrop.ag", srv)

	url := "https://mixdrop.ag/f/abc123"
	err := downloadWithHeaders(context.Background(), url, nil, url, "mixdrop", t.TempDir(), 0, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "server returned HTML instead of file") {
		t.Fatalf("expected the hard reject without a fallback, got %v", err)
	}
}
