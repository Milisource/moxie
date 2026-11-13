package downloader

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A cap that appears after the pre-download probe must abort the transfer
// instead of grinding at the capped rate: the caller's link fallback then
// tries another host.
func TestDownloadWithHeaders_AbortsWhenThrottleStartsMidTransfer(t *testing.T) {
	oldInterval := throttleProbeInterval
	throttleProbeInterval = 20 * time.Millisecond
	t.Cleanup(func() { throttleProbeInterval = oldInterval })

	chunk := make([]byte, 32<<10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		for i := 0; i < 2000; i++ {
			select {
			case <-r.Context().Done():
				return
			default:
			}
			if _, err := w.Write(chunk); err != nil {
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
	}))
	defer srv.Close()
	rewriteTestHost(t, "pixeldrain.com", srv)

	probes := 0
	probe := func(context.Context) (*ThrottledError, bool) {
		probes++
		return &ThrottledError{Host: "pixeldrain", Limit: 1 << 20}, true
	}

	dir := t.TempDir()
	_, err := downloadWithHeaders(context.Background(), "https://pixeldrain.com/api/file/abc123", nil, "https://pixeldrain.com/api/file/abc123",
		"pixeldrain", dir, 0, nil, nil, nil, probe)
	if !errors.Is(err, ErrHostThrottled) {
		t.Fatalf("error = %v, want ErrHostThrottled (probes=%d)", err, probes)
	}
	if probes == 0 {
		t.Error("probe was never called during the transfer")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "abc123.part")); !os.IsNotExist(statErr) {
		t.Errorf("partial file survived the throttle abort: %v", statErr)
	}
}

// A probe that keeps reporting "no cap" must not disturb a normal download.
func TestDownloadWithHeaders_UnthrottledProbeKeepsTransfer(t *testing.T) {
	oldInterval := throttleProbeInterval
	throttleProbeInterval = 5 * time.Millisecond
	t.Cleanup(func() { throttleProbeInterval = oldInterval })

	body := []byte("complete payload")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(body)
	}))
	defer srv.Close()
	rewriteTestHost(t, "pixeldrain.com", srv)

	dir := t.TempDir()
	probe := func(context.Context) (*ThrottledError, bool) { return nil, false }
	if _, err := downloadWithHeaders(context.Background(), "https://pixeldrain.com/api/file/abc123", nil, "https://pixeldrain.com/api/file/abc123",
		"pixeldrain", dir, 0, nil, nil, nil, probe); err != nil {
		t.Fatalf("download failed: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "abc123"))
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(got) != string(body) {
		t.Errorf("content = %q, want %q", got, body)
	}
}
