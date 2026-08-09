package downloader

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// sizeTestTLSServer spins up a TLS server whose certificate is valid for
// "example.com" and returns a transport that dials it via 127.0.0.1, so
// https://example.com:<port>/... URLs pass isValidDownloadURL and reach the
// test server (same pattern as validator_test.go's testTLSServerClient).
func sizeTestTLSServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *http.Transport) {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	tr := srv.Client().Transport.(*http.Transport).Clone()
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	}
	return srv, tr
}

// TestDownloadWithHost_TruncationDetected: when expectedTotal is known and
// the server cuts the transfer short, the download must fail, the .part file
// must be removed, and no final file may exist.
func TestDownloadWithHost_TruncationDetected(t *testing.T) {
	_, tr := sizeTestTLSServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write([]byte(strings.Repeat("x", 10240)))
	})
	old := testTransportOverride
	testTransportOverride = tr
	defer func() { testTransportOverride = old }()

	destDir := t.TempDir()
	err := DownloadWithContext(context.Background(), "https://example.com/file.zip", "", destDir, 1024*1024, nil, "")
	if err == nil {
		t.Fatal("expected a truncation error")
	}
	if !strings.Contains(err.Error(), "download truncated") {
		t.Fatalf("expected truncated error, got: %v", err)
	}

	entries, _ := os.ReadDir(destDir)
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("destDir not clean after truncation, found: %v", names)
	}
}

// TestDownloadWithHost_SizeMatch: a transfer that delivers exactly the
// expected bytes succeeds and produces the final file.
func TestDownloadWithHost_SizeMatch(t *testing.T) {
	const payload = 8192
	_, tr := sizeTestTLSServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write([]byte(strings.Repeat("y", payload)))
	})
	old := testTransportOverride
	testTransportOverride = tr
	defer func() { testTransportOverride = old }()

	destDir := t.TempDir()
	err := DownloadWithContext(context.Background(), "https://example.com/file.zip", "", destDir, payload, nil, "")
	if err != nil {
		t.Fatalf("download with matching size failed: %v", err)
	}
	files, _ := filepath.Glob(filepath.Join(destDir, "*"))
	if len(files) != 1 {
		t.Fatalf("expected exactly one final file, got %v", files)
	}
	fi, err := os.Stat(files[0])
	if err != nil || fi.Size() != payload {
		t.Fatalf("final file size = %v, want %d (err %v)", fi.Size(), payload, err)
	}
}
