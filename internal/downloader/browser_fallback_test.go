package downloader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// saveFallback swaps the package-level browser fallback and restores it.
func saveFallback(t *testing.T) {
	t.Helper()
	old := defaultBrowserFallback
	t.Cleanup(func() { defaultBrowserFallback = old })
}

// fakeFallback records its arguments and optionally writes a file into
// destDir (mimicking a browser-performed download).
func fakeFallback(t *testing.T, writeFile bool) (fn func(ctx context.Context, url, destDir string) (string, error), calls *[]string) {
	t.Helper()
	var log []string
	fn = func(_ context.Context, url, destDir string) (string, error) {
		log = append(log, url+"|"+destDir)
		if !writeFile {
			return "", os.ErrPermission
		}
		path := filepath.Join(destDir, "game.zip")
		if err := os.WriteFile(path, []byte("browser-downloaded"), 0o600); err != nil {
			return "", err
		}
		return path, nil
	}
	return fn, &log
}

// TestDownloadWithHost_FileHopChallenge_Fallback verifies the browser
// fallback fires on a Cf-Mitigated challenge at the file hop and the
// download is considered complete.
func TestDownloadWithHost_FileHopChallenge_Fallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cf-Mitigated", "challenge")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	orig := testTransportOverride
	testTransportOverride = rewriteToServer("cdn.example.com", srv.URL)
	defer func() { testTransportOverride = orig }()

	saveFallback(t)
	fb, calls := fakeFallback(t, true)
	defaultBrowserFallback = fb

	destDir := t.TempDir()
	err := DownloadWithHost("https://cdn.example.com/file.zip", "unknown", destDir, 0, nil, "")
	if err != nil {
		t.Fatalf("DownloadWithHost: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("fallback called %d times, want 1", len(*calls))
	}
	if !strings.HasPrefix((*calls)[0], "https://cdn.example.com/file.zip|"+destDir) {
		t.Errorf("fallback args = %q, want original URL + destDir", (*calls)[0])
	}
	if _, err := os.Stat(filepath.Join(destDir, "game.zip")); err != nil {
		t.Errorf("browser-downloaded file missing: %v", err)
	}
}

// TestDownloadWithHost_FileHopChallenge_FallbackFails verifies a failed
// browser attempt surfaces the original challenge error with context.
func TestDownloadWithHost_FileHopChallenge_FallbackFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cf-Mitigated", "managed_challenge")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	orig := testTransportOverride
	testTransportOverride = rewriteToServer("cdn.example.com", srv.URL)
	defer func() { testTransportOverride = orig }()

	saveFallback(t)
	fb, _ := fakeFallback(t, false)
	defaultBrowserFallback = fb

	err := DownloadWithHost("https://cdn.example.com/file.zip", "unknown", t.TempDir(), 0, nil, "")
	if err == nil {
		t.Fatal("expected error when the browser fallback fails")
	}
	if !strings.Contains(err.Error(), "browser fallback also failed") {
		t.Errorf("error = %v, want fallback-failure context", err)
	}
}

// TestDownloadWithHost_FileHopChallenge_NoFallback verifies the pre-fallback
// behavior is unchanged when no hook is installed.
func TestDownloadWithHost_FileHopChallenge_NoFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cf-Mitigated", "challenge")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	orig := testTransportOverride
	testTransportOverride = rewriteToServer("cdn.example.com", srv.URL)
	defer func() { testTransportOverride = orig }()

	saveFallback(t)
	defaultBrowserFallback = nil

	err := DownloadWithHost("https://cdn.example.com/file.zip", "unknown", t.TempDir(), 0, nil, "")
	if err == nil {
		t.Fatal("expected error without fallback")
	}
	if !strings.Contains(err.Error(), "HTTP 403") {
		t.Errorf("error = %v, want HTTP 403", err)
	}
	if !strings.Contains(err.Error(), "browser_fallback") {
		t.Errorf("error = %v, want the opt-in enable hint", err)
	}
}

// TestDownloadWithHost_ResolveChallenge_Fallback verifies the fallback fires
// when the resolver stage hits a challenge (vikingfile-style HTTP 403).
func TestDownloadWithHost_ResolveChallenge_Fallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// VikingFile resolver GETs the page first; a 403 challenge fails it.
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	// The resolver's own client dials via http.DefaultTransport — rewrite
	// vikingfile.com to the test server (NOT parallel: global state).
	origTransport := http.DefaultTransport
	http.DefaultTransport = rewriteToServer("vikingfile.com", srv.URL)
	defer func() { http.DefaultTransport = origTransport }()

	saveFallback(t)
	fb, calls := fakeFallback(t, true)
	defaultBrowserFallback = fb

	destDir := t.TempDir()
	err := DownloadWithHost("https://vikingfile.com/f/abc123", "vikingfile", destDir, 0, nil, "")
	if err != nil {
		t.Fatalf("DownloadWithHost: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("fallback called %d times, want 1", len(*calls))
	}
	if !strings.HasPrefix((*calls)[0], "https://vikingfile.com/f/abc123|") {
		t.Errorf("fallback args = %q, want the original vikingfile URL", (*calls)[0])
	}
}

// TestDownloadWithHost_ResolveFailure_NoFallback verifies plain resolve
// failures (dead link, 404) do NOT trigger the browser fallback.
func TestDownloadWithHost_ResolveFailure_NoFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = rewriteToServer("vikingfile.com", srv.URL)
	defer func() { http.DefaultTransport = origTransport }()

	saveFallback(t)
	fb, calls := fakeFallback(t, true)
	defaultBrowserFallback = fb

	err := DownloadWithHost("https://vikingfile.com/f/abc123", "vikingfile", t.TempDir(), 0, nil, "")
	if err == nil {
		t.Fatal("expected error for a 404 page (no challenge)")
	}
	if len(*calls) != 0 {
		t.Fatalf("fallback called %d times, want 0 for a plain 404", len(*calls))
	}
	if strings.Contains(err.Error(), "browser_fallback") {
		t.Errorf("plain 404 must not carry the browser-fallback hint: %v", err)
	}
}

// TestDownloadWithHost_ResolveCaptchaFailure_Fallback verifies the captcha
// marker (the vikingfile POST wall) also triggers the fallback.
func TestDownloadWithHost_ResolveCaptchaFailure_Fallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			// POST answered with a plain page — the resolver reports the
			// captcha wall as an error.
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html>no download link here</html>"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><form method="POST"><input type="hidden" name="op" value="download1"></form></html>`))
	}))
	defer srv.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = rewriteToServer("vikingfile.com", srv.URL)
	defer func() { http.DefaultTransport = origTransport }()

	saveFallback(t)
	fb, calls := fakeFallback(t, true)
	defaultBrowserFallback = fb

	destDir := t.TempDir()
	err := DownloadWithHost("https://vikingfile.com/f/abc123", "vikingfile", destDir, 0, nil, "")
	if err != nil {
		t.Fatalf("DownloadWithHost: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("fallback called %d times, want 1", len(*calls))
	}
}

// TestDownloadWithHost_ResolveChallenge_NoFallbackHint verifies a resolve
// challenge with the fallback disabled carries the opt-in hint.
func TestDownloadWithHost_ResolveChallenge_NoFallbackHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = rewriteToServer("vikingfile.com", srv.URL)
	defer func() { http.DefaultTransport = origTransport }()

	saveFallback(t)
	defaultBrowserFallback = nil

	err := DownloadWithHost("https://vikingfile.com/f/abc123", "vikingfile", t.TempDir(), 0, nil, "")
	if err == nil {
		t.Fatal("expected error for a challenged resolve without fallback")
	}
	if !strings.Contains(err.Error(), "browser_fallback") {
		t.Errorf("error = %v, want the opt-in enable hint", err)
	}
}

// TestIsChallengeFailure covers the resolve-stage challenge marker table.
func TestIsChallengeFailure(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{errString("vikingfile: POST returned 200 without download link (captcha or error)"), true},
		{errString("vikingfile: GET returned HTTP 403"), true},
		{errString("datanodes: HTTP 403 forbidden"), true},
		{errString("Cloudflare challenge"), true},
		{errString("turnstile required"), true},
		{errString("host not found"), false},
		{errString("HTTP 404"), false},
		{errString("connection refused"), false},
	}
	for _, tt := range tests {
		if got := isChallengeFailure(tt.err); got != tt.want {
			t.Errorf("isChallengeFailure(%q) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

// errString is a minimal error for the marker table.
type errString string

func (e errString) Error() string { return string(e) }
