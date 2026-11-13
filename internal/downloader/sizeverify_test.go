package downloader

import (
	"context"
	"io"
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

// TestDownloadWithHost_TruncationDetected: with no Content-Length to compare
// against, a body under half the stated size is treated as truncated, the
// .part file is removed, and no final file exists.
func TestDownloadWithHost_TruncationDetected(t *testing.T) {
	old := testTransportOverride
	testTransportOverride = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{}, ContentLength: -1,
			Body: io.NopCloser(strings.NewReader(strings.Repeat("x", 10240))), Request: req,
		}, nil
	})
	defer func() { testTransportOverride = old }()

	destDir := t.TempDir()
	_, err := DownloadWithContext(context.Background(), "https://example.com/file.zip", "", destDir, 1024*1024, nil, "")
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

// TestDownloadWithHost_StatedSizeMismatchKeepsCompleteTransfer: a complete
// transfer whose Content-Length matched is kept even when the scraped size is
// wildly different — the thread's size is display metadata, not a contract.
func TestDownloadWithHost_StatedSizeMismatchKeepsCompleteTransfer(t *testing.T) {
	old := testTransportOverride
	testTransportOverride = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{}, ContentLength: 10240,
			Body: io.NopCloser(strings.NewReader(strings.Repeat("x", 10240))), Request: req,
		}, nil
	})
	defer func() { testTransportOverride = old }()

	destDir := t.TempDir()
	path, err := DownloadWithContext(context.Background(), "https://example.com/file.zip", "", destDir, 1024*1024, nil, "")
	if err != nil {
		t.Fatalf("complete transfer rejected: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Size() != 10240 {
		t.Fatalf("path=%q stat=%v err=%v", path, fi, err)
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
	_, err := DownloadWithContext(context.Background(), "https://example.com/file.zip", "", destDir, payload, nil, "")
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

// TestDownloadWithHost_ContentLengthTruncationDetected: when the scraped size
// is unknown (expectedTotal 0) but the server declares a Content-Length larger
// than the body it delivers, the short transfer must still be rejected. The
// custom transport returns a response whose ContentLength lies about the body,
// which Go's net/http would otherwise report as a clean short read.
func TestDownloadWithHost_ContentLengthTruncationDetected(t *testing.T) {
	old := testTransportOverride
	testTransportOverride = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			Status:        "200 OK",
			StatusCode:    http.StatusOK,
			Header:        http.Header{"Content-Type": []string{"application/octet-stream"}},
			ContentLength: 9999,
			Body:          io.NopCloser(strings.NewReader("short body")),
			Request:       req,
		}, nil
	})
	defer func() { testTransportOverride = old }()

	destDir := t.TempDir()
	_, err := DownloadWithContext(context.Background(), "https://example.com/file.zip", "", destDir, 0, nil, "")
	if err == nil {
		t.Fatal("expected a truncation error from Content-Length")
	}
	if !strings.Contains(err.Error(), "download truncated") {
		t.Fatalf("expected truncated error, got: %v", err)
	}
	entries, _ := os.ReadDir(destDir)
	if len(entries) != 0 {
		t.Fatalf("destDir not clean after truncation, found: %v", entries)
	}
}

func TestDownloadWithHost_ApproximateScrapedSize(t *testing.T) {
	for _, tc := range []struct {
		name           string
		actual, stated int64
		contentLength  int64
		fail           bool
	}{
		{"five percent smaller", 9500, 10000, 9500, false},
		{"less than half but Content-Length matched", 4900, 10000, 4900, false},
		{"less than half with no Content-Length", 4900, 10000, -1, true},
		{"exactly half with no Content-Length", 5000, 10000, -1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := testTransportOverride
			testTransportOverride = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK, Header: http.Header{}, ContentLength: tc.contentLength,
					Body: io.NopCloser(strings.NewReader(strings.Repeat("x", int(tc.actual)))), Request: req,
				}, nil
			})
			defer func() { testTransportOverride = old }()
			path, err := DownloadWithContext(context.Background(), "https://example.com/file.zip", "", t.TempDir(), tc.stated, nil, "")
			if (err != nil) != tc.fail {
				t.Fatalf("path=%q err=%v", path, err)
			}
			if !tc.fail && path == "" {
				t.Fatal("missing returned path")
			}
		})
	}
}

// TestDownloadResumesURLNamedPartial pins the resume contract: the partial is
// read from the URL-derived name before the request, so it must not be moved
// to the Content-Disposition name until the rename after a complete transfer.
// Otherwise every retry of a multi-GB download starts from zero.
func TestDownloadResumesURLNamedPartial(t *testing.T) {
	const head, tail = "hello", " world"
	destDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(destDir, "file.zip.part"), []byte(head), 0600); err != nil {
		t.Fatal(err)
	}

	var gotRange string
	old := testTransportOverride
	testTransportOverride = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotRange = req.Header.Get("Range")
		status, body := http.StatusOK, head
		header := http.Header{}
		if gotRange == "bytes=5-" {
			status, body = http.StatusPartialContent, tail
			header.Set("Content-Range", "bytes 5-10/11")
		}
		header.Set("Content-Disposition", "attachment; filename=Real-Name.zip")
		return &http.Response{
			StatusCode: status, Header: header, ContentLength: int64(len(body)),
			Body: io.NopCloser(strings.NewReader(body)), Request: req,
		}, nil
	})
	defer func() { testTransportOverride = old }()

	path, err := DownloadWithContext(context.Background(), "https://example.com/file.zip", "", destDir, 0, nil, "")
	if err != nil {
		t.Fatalf("resume failed: %v", err)
	}
	if gotRange != "bytes=5-" {
		t.Fatalf("expected a resume Range request, got %q", gotRange)
	}
	if filepath.Base(path) != "Real-Name.zip" {
		t.Fatalf("final name = %q, want Content-Disposition name", filepath.Base(path))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != head+tail {
		t.Fatalf("resumed content = %q, want %q", data, head+tail)
	}
}

// TestDownloadResumeRejectsMismatchedContentRange: a 206 that starts anywhere
// other than the partial's size would splice two files together, so it must
// fail and discard the unusable partial rather than produce a corrupt archive.
func TestDownloadResumeRejectsMismatchedContentRange(t *testing.T) {
	destDir := t.TempDir()
	partPath := filepath.Join(destDir, "file.zip.part")
	if err := os.WriteFile(partPath, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}

	old := testTransportOverride
	testTransportOverride = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		header := http.Header{}
		header.Set("Content-Range", "bytes 0-9/10")
		return &http.Response{
			StatusCode: http.StatusPartialContent, Header: header, ContentLength: 10,
			Body: io.NopCloser(strings.NewReader("0123456789")), Request: req,
		}, nil
	})
	defer func() { testTransportOverride = old }()

	_, err := DownloadWithContext(context.Background(), "https://example.com/file.zip", "", destDir, 0, nil, "")
	if err == nil || !strings.Contains(err.Error(), "resume offset mismatch") {
		t.Fatalf("expected a resume offset error, got: %v", err)
	}
	if _, statErr := os.Stat(partPath); !os.IsNotExist(statErr) {
		t.Fatalf("mismatched partial was kept: %v", statErr)
	}
}
