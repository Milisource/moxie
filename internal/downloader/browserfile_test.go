package downloader

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestDownloadWithHeaders_AdoptsBrowserFile covers the human-assisted handoff:
// the browser already downloaded the file, so the downloader copies it into
// destDir (sanitized name, one-time token, temp dir removed) instead of
// making an HTTP request.
func TestDownloadWithHeaders_AdoptsBrowserFile(t *testing.T) {
	srcDir, err := os.MkdirTemp("", "moxie-browser-handoff-*")
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("moxie!"), 4096)
	src := filepath.Join(srcDir, "browser-guid")
	if err := os.WriteFile(src, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	destDir := t.TempDir()

	token := RegisterBrowserFile(src, "../../Ashfall_v1.0.zip")
	if !IsBrowserFileURL(token) {
		t.Fatalf("token = %q, want a browser-file URL", token)
	}

	var last Progress
	err = downloadWithHeaders(context.Background(), token, nil,
		"https://f95zone.to/dl/16943574/", "pixeldrain", destDir, 0,
		func(p Progress) { last = p }, nil, nil)
	if err != nil {
		t.Fatalf("downloadWithHeaders: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(destDir, "Ashfall_v1.0.zip"))
	if err != nil {
		t.Fatalf("adopted file: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("adopted %d bytes, want %d", len(got), len(payload))
	}
	if last.Percent != 100 || last.BytesDownloaded != int64(len(payload)) {
		t.Errorf("progress = %+v, want 100%% of %d bytes", last, len(payload))
	}
	if _, err := os.Stat(srcDir); !os.IsNotExist(err) {
		t.Errorf("handoff dir %q still exists", srcDir)
	}

	// Tokens are single use: replaying one must not read anything.
	err = downloadWithHeaders(context.Background(), token, nil, "", "pixeldrain", destDir, 0, nil, nil, nil)
	if err == nil {
		t.Fatal("replayed browser-file token was accepted")
	}
}

func TestDownloadWithHeaders_UnknownBrowserFileToken(t *testing.T) {
	err := downloadWithHeaders(context.Background(), browserFileScheme+"deadbeef", nil, "", "pixeldrain", t.TempDir(), 0, nil, nil, nil)
	if err == nil {
		t.Fatal("unknown browser-file token was accepted")
	}
}

func TestBrowserFileName(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{"Game_v1.0.zip", "Game_v1.0.zip"},
		{"../../etc/passwd", "passwd"},
		{"/absolute/path.zip", "path.zip"},
		{"", ""},
		{".", ""},
		{"..", ""},
	}
	for _, tt := range tests {
		if got := browserFileName(tt.in); got != tt.want {
			t.Errorf("browserFileName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
