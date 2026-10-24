package browserresolve

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mili/moxie/internal/downloader"
)

// TestAwaitBrowserDownload_FinishedFile covers event-less flows (rod#971):
// the file lands in the session download dir without CDP events, and the
// helper still hands it over once it is complete.
func TestAwaitBrowserDownload_FinishedFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	payload := make([]byte, 4096)
	if err := os.WriteFile(filepath.Join(dir, "browser-guid"), payload, 0o600); err != nil {
		t.Fatal(err)
	}

	path, name, ok := awaitBrowserDownload(context.Background(), newDownloadTracker(), dir, 3*time.Second, time.Second)
	if !ok {
		t.Fatal("awaitBrowserDownload did not find the finished file")
	}
	if filepath.Base(path) != "browser-guid" {
		t.Errorf("path = %q", path)
	}
	if name != "" {
		t.Errorf("name = %q, want empty (no CDP event)", name)
	}
}

func TestAwaitBrowserDownload_NoDownload(t *testing.T) {
	t.Parallel()
	start := time.Now()
	_, _, ok := awaitBrowserDownload(context.Background(), newDownloadTracker(), t.TempDir(), 150*time.Millisecond, time.Second)
	if ok {
		t.Fatal("awaitBrowserDownload reported a file in an empty dir")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("took %s, want a bounded grace wait", elapsed)
	}
}

func TestAwaitBrowserDownload_CanceledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, ok := awaitBrowserDownload(ctx, newDownloadTracker(), t.TempDir(), time.Second, time.Second); ok {
		t.Fatal("reported a download after context cancellation")
	}
}

// TestHandOffBrowserFile verifies the file is moved out of the browser's
// session dir and registered under an opaque, single-use token.
func TestHandOffBrowserFile(t *testing.T) {
	handoffDir := t.TempDir()
	orig := browserHandoffDir
	browserHandoffDir = func() (string, error) { return handoffDir, nil }
	t.Cleanup(func() { browserHandoffDir = orig })

	src := filepath.Join(t.TempDir(), "browser-guid")
	if err := os.WriteFile(src, []byte("game payload"), 0o600); err != nil {
		t.Fatal(err)
	}

	token, err := handOffBrowserFile(src, "Game_v1.0.zip")
	if err != nil {
		t.Fatalf("handOffBrowserFile: %v", err)
	}
	if !downloader.IsBrowserFileURL(token) {
		t.Errorf("token = %q, want a browser-file URL", token)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("source %q still exists", src)
	}
	if _, err := os.Stat(filepath.Join(handoffDir, "browser-guid")); err != nil {
		t.Errorf("handed-off file missing: %v", err)
	}
}
