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

// htmlGameServer serves a text/html payload, optionally with the
// Content-Disposition filename a file host uses for a single-file HTML game.
func htmlGameServer(t *testing.T, filename string, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		if filename != "" {
			w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
		}
		w.Header().Set("Content-Length", itoa(len(body)))
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// TestDownloadWithHeaders_HTMLGameByContentDisposition: pixeldrain/datanodes
// serve an HTML game as text/html with an attachment filename. That is the
// payload, not a challenge page, even though the URL has no .html suffix.
func TestDownloadWithHeaders_HTMLGameByContentDisposition(t *testing.T) {
	body := []byte("<!DOCTYPE html>\n<html><body>" + strings.Repeat("game", 2048) + "</body></html>")
	srv := htmlGameServer(t, "Ashfall_RPG_v0.5.0.html", body)
	rewriteTestHost(t, "pixeldrain.com", srv)

	dir := t.TempDir()
	url := "https://pixeldrain.com/api/file/abc123?download"
	if err := downloadWithHeaders(context.Background(), url, nil, url, "pixeldrain", dir, 0, nil, nil, nil); err != nil {
		t.Fatalf("downloadWithHeaders: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "Ashfall_RPG_v0.5.0.html"))
	if err != nil {
		t.Fatalf("downloaded HTML game missing: %v", err)
	}
	if string(got) != string(body) {
		t.Errorf("downloaded %d bytes, want %d", len(got), len(body))
	}
}

// TestDownloadWithHeaders_HTMLGameByURLPath: a direct .html link (datanodes/
// buzzheavier style) needs no Content-Disposition to be accepted.
func TestDownloadWithHeaders_HTMLGameByURLPath(t *testing.T) {
	body := []byte("<!DOCTYPE html>\n<html><body>small single-file game</body></html>")
	srv := htmlGameServer(t, "", body)
	rewriteTestHost(t, "cdn.example.com", srv)

	dir := t.TempDir()
	url := "https://cdn.example.com/files/Ashfall_RPG_v0.5.0.html"
	if err := downloadWithHeaders(context.Background(), url, nil, url, "unknown", dir, 0, nil, nil, nil); err != nil {
		t.Fatalf("downloadWithHeaders: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Ashfall_RPG_v0.5.0.html")); err != nil {
		t.Fatalf("downloaded HTML game missing: %v", err)
	}
}

// TestDownloadWithHeaders_HTMLChallengeStillRejected: the acceptance rules
// must not weaken bot-page detection for ordinary download URLs.
func TestDownloadWithHeaders_HTMLChallengeStillRejected(t *testing.T) {
	srv := htmlGameServer(t, "", []byte(`<html><body>Just a quick check</body></html>`))
	rewriteTestHost(t, "cdn.example.com", srv)

	url := "https://cdn.example.com/download/12345"
	err := downloadWithHeaders(context.Background(), url, nil, url, "unknown", t.TempDir(), 0, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "server returned HTML instead of file") {
		t.Fatalf("error = %v, want the HTML reject", err)
	}
}

func TestIsValidGameFile_HTML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{"game.html", "game.htm", "GAME.HTML"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, make([]byte, 5000), 0o600); err != nil {
			t.Fatal(err)
		}
		if !IsValidGameFile(path) {
			t.Errorf("IsValidGameFile(%q) = false, want true", name)
		}
	}
}
