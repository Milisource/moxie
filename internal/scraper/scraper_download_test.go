package scraper

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mili/moxie/internal/browser"
)

// downloadCookies is a realistic F95Zone cookie header for the download tests.
const downloadCookies = "xf_session=abc123; cf_clearance=def456; xf_user=ghi789"

// TestDownloadToWriter_StreamsWithCookiesAndUA verifies the basics: the body is
// streamed into the writer, the count is correct, and the authenticated
// session headers are injected.
func TestDownloadToWriter_StreamsWithCookiesAndUA(t *testing.T) {
	t.Parallel()

	payload := []byte("full-save archive bytes")
	var gotCookie, gotUA, gotAcceptLang string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		gotUA = r.Header.Get("User-Agent")
		gotAcceptLang = r.Header.Get("Accept-Language")
		w.WriteHeader(http.StatusOK)
		w.Write(payload)
	}))
	defer srv.Close()

	client := NewClientWithHTTP(downloadCookies, srv.Client())

	var buf bytes.Buffer
	n, err := client.DownloadToWriter(context.Background(), srv.URL+"/attachments/save.zip.123456/", &buf)
	if err != nil {
		t.Fatalf("DownloadToWriter failed: %v", err)
	}
	if n != int64(len(payload)) {
		t.Errorf("bytes copied = %d, want %d", n, len(payload))
	}
	if !bytes.Equal(buf.Bytes(), payload) {
		t.Errorf("streamed body = %q, want %q", buf.String(), payload)
	}
	for _, want := range []string{"xf_session=abc123", "cf_clearance=def456", "xf_user=ghi789"} {
		if !strings.Contains(gotCookie, want) {
			t.Errorf("Cookie header %q missing %q", gotCookie, want)
		}
	}
	if gotUA != platformUserAgent() {
		t.Errorf("User-Agent = %q, want %q", gotUA, platformUserAgent())
	}
	if gotAcceptLang != "en-US,en;q=0.5" {
		t.Errorf("Accept-Language = %q, want %q", gotAcceptLang, "en-US,en;q=0.5")
	}
}

// TestDownloadToWriter_FollowsRedirect covers the real attachment flow: the
// f95zone.to URL 302s to attachments.f95zone.to with signed params.
func TestDownloadToWriter_FollowsRedirect(t *testing.T) {
	t.Parallel()

	payload := []byte("redirected save payload")
	var finalCookie, finalPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/attachments/save.zip.123456/":
			http.Redirect(w, r, "/downloads/2026/10/save.zip?sig=abc123", http.StatusFound)
		default:
			finalCookie = r.Header.Get("Cookie")
			finalPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
			w.Write(payload)
		}
	}))
	defer srv.Close()

	client := NewClientWithHTTP(downloadCookies, srv.Client())

	var buf bytes.Buffer
	n, err := client.DownloadToWriter(context.Background(), srv.URL+"/attachments/save.zip.123456/", &buf)
	if err != nil {
		t.Fatalf("DownloadToWriter failed: %v", err)
	}
	if finalPath != "/downloads/2026/10/save.zip" {
		t.Errorf("redirect not followed, final path = %q", finalPath)
	}
	if n != int64(len(payload)) || !bytes.Equal(buf.Bytes(), payload) {
		t.Errorf("streamed %d bytes %q, want %d %q", n, buf.String(), len(payload), payload)
	}
	if !strings.Contains(finalCookie, "xf_session=abc123") {
		t.Errorf("cookie not re-injected on redirect: %q", finalCookie)
	}
}

// TestDownloadToWriter_RateLimitedBackoff mirrors the do() backoff contract:
// 429 becomes a *BlockedError and doubles the client's adaptive delay.
func TestDownloadToWriter_RateLimitedBackoff(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	client := NewClientWithHTTP("test=1", srv.Client())
	before := client.Delay()
	if before <= 0 {
		t.Fatalf("expected positive initial delay, got %v", before)
	}

	_, err := client.DownloadToWriter(context.Background(), srv.URL, io.Discard)
	if err == nil {
		t.Fatal("expected *BlockedError for 429, got nil")
	}
	var blocked *BlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("expected *BlockedError, got %T: %v", err, err)
	}
	if blocked.StatusCode != http.StatusTooManyRequests {
		t.Errorf("BlockedError.StatusCode = %d, want 429", blocked.StatusCode)
	}

	if after := client.Delay(); after != before*2 {
		t.Errorf("delay after 429 = %v, want %v (doubled)", after, before*2)
	}
}

// TestDownloadToWriter_ForbiddenIsBlocked covers the other blocking statuses.
func TestDownloadToWriter_ForbiddenIsBlocked(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	client := NewUnsafeClient("test=1")
	_, err := client.DownloadToWriter(context.Background(), srv.URL, io.Discard)
	var blocked *BlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("expected *BlockedError for 403, got %T: %v", err, err)
	}
	if blocked.StatusCode != http.StatusForbidden {
		t.Errorf("BlockedError.StatusCode = %d, want 403", blocked.StatusCode)
	}
}

// TestDownloadToWriter_NotFound ensures non-blocking statuses yield a plain
// error mentioning the status.
func TestDownloadToWriter_NotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewUnsafeClient("test=1")
	_, err := client.DownloadToWriter(context.Background(), srv.URL, io.Discard)
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	var blocked *BlockedError
	if errors.As(err, &blocked) {
		t.Errorf("404 must not be a *BlockedError: %v", err)
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should mention status 404, got: %v", err)
	}
}

// TestDownloadToWriter_DoesNotScanBody proves the download path never
// buffers/scans the body for Cloudflare markers: a blob full of challenge
// strings is delivered byte-for-byte as a 200 response.
func TestDownloadToWriter_DoesNotScanBody(t *testing.T) {
	t.Parallel()

	blob := []byte("<html>cf-challenge-running cf-browser-verification _cf_chl_opt</html>")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write(blob)
	}))
	defer srv.Close()

	client := NewUnsafeClient("test=1")
	var buf bytes.Buffer
	n, err := client.DownloadToWriter(context.Background(), srv.URL, &buf)
	if err != nil {
		t.Fatalf("DownloadToWriter failed: %v", err)
	}
	if n != int64(len(blob)) || !bytes.Equal(buf.Bytes(), blob) {
		t.Errorf("streamed %d bytes, want %d", n, len(blob))
	}
}

// TestDownloadToWriter_LargePayload streams a multi-megabyte archive and
// checks it arrives intact, without buffering the whole thing in the client.
func TestDownloadToWriter_LargePayload(t *testing.T) {
	t.Parallel()

	const size = 3 << 20 // 3 MiB
	payload := make([]byte, size)
	rng := rand.New(rand.NewSource(42))
	rng.Read(payload)
	wantSum := sha256.Sum256(payload)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(payload)
	}))
	defer srv.Close()

	client := NewUnsafeClient("test=1")
	var buf bytes.Buffer
	n, err := client.DownloadToWriter(context.Background(), srv.URL, &buf)
	if err != nil {
		t.Fatalf("DownloadToWriter failed: %v", err)
	}
	if n != size {
		t.Errorf("bytes copied = %d, want %d", n, size)
	}
	if gotSum := sha256.Sum256(buf.Bytes()); gotSum != wantSum {
		t.Errorf("payload checksum mismatch: got %x, want %x", gotSum, wantSum)
	}
}

// failingWriter accepts only limit bytes before failing, to exercise the
// partial-transfer contract.
type failingWriter struct {
	limit   int
	written int
}

func (f *failingWriter) Write(p []byte) (int, error) {
	if f.written+len(p) > f.limit {
		n := f.limit - f.written
		f.written = f.limit
		return n, errors.New("disk full")
	}
	f.written += len(p)
	return len(p), nil
}

func TestDownloadToWriter_WriteErrorReturnsPartialCount(t *testing.T) {
	t.Parallel()

	payload := bytes.Repeat([]byte("x"), 64*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(payload)
	}))
	defer srv.Close()

	client := NewUnsafeClient("test=1")
	fw := &failingWriter{limit: 1024}
	n, err := client.DownloadToWriter(context.Background(), srv.URL, fw)
	if err == nil {
		t.Fatal("expected a write error, got nil")
	}
	if n != 1024 {
		t.Errorf("partial byte count = %d, want 1024", n)
	}
	if !strings.Contains(err.Error(), "1024 bytes") {
		t.Errorf("error should report the partial count, got: %v", err)
	}
}

func TestDownloadToWriter_ContextCancelled(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewUnsafeClient("test=1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the request starts

	if _, err := client.DownloadToWriter(ctx, srv.URL, io.Discard); err == nil {
		t.Error("expected an error for a cancelled context")
	}
}

// TestDownloadToWriter_WaitsForMinimumDelay proves the download path goes
// through the shared pacing gate: with a fresh lastRequest and an expired-ish
// deadline the request never reaches the server, because the minimum delay is
// enforced before dialing.
func TestDownloadToWriter_WaitsForMinimumDelay(t *testing.T) {
	t.Parallel()

	requested := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewUnsafeClient("test=1")
	client.lastRequest = time.Now() // pretend a request just happened

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.DownloadToWriter(ctx, srv.URL, io.Discard)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	select {
	case <-requested:
		t.Error("request reached the server without waiting for the minimum delay")
	default:
	}
}

func TestDownloadToWriter_EmptyURL(t *testing.T) {
	t.Parallel()

	client := NewUnsafeClient("test=1")
	if _, err := client.DownloadToWriter(context.Background(), "", io.Discard); err == nil {
		t.Error("expected an error for an empty URL")
	}
}

// TestDownloadToWriter_NoFixedTimeout documents that the download client does
// not inherit c.http's total timeout: a stream slower than that timeout still
// completes, because cancellation comes from ctx.
func TestDownloadToWriter_NoFixedTimeout(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("test server does not support flushing")
			return
		}
		w.Write([]byte("first "))
		flusher.Flush()
		time.Sleep(300 * time.Millisecond)
		w.Write([]byte("second"))
	}))
	defer srv.Close()

	httpClient := srv.Client()
	httpClient.Timeout = 50 * time.Millisecond // would abort the stream below
	client := NewClientWithHTTP("test=1", httpClient)

	if client.http.Timeout != 50*time.Millisecond {
		t.Fatalf("test setup: c.http.Timeout = %v, want 50ms", client.http.Timeout)
	}
	if got := client.downloadClient().Timeout; got != 0 {
		t.Errorf("downloadClient().Timeout = %v, want 0 (no fixed timeout)", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var buf bytes.Buffer
	n, err := client.DownloadToWriter(ctx, srv.URL, &buf)
	if err != nil {
		t.Fatalf("DownloadToWriter failed: %v", err)
	}
	if got := buf.String(); got != "first second" || n != int64(len(got)) {
		t.Errorf("streamed %q (%d bytes), want %q", got, n, "first second")
	}
}

// TestLiveDownloadAttachment is an opt-in end-to-end smoke test for the real
// F95Zone attachment flow. It is skipped unless MOXIE_LIVE=1 and
// MOXIE_LIVE_ATTACHMENT_URL are set and browser cookies are readable.
//
//	MOXIE_LIVE=1 MOXIE_LIVE_ATTACHMENT_URL=https://f95zone.to/attachments/... \
//	    go test ./internal/scraper/ -run TestLiveDownloadAttachment -v
func TestLiveDownloadAttachment(t *testing.T) {
	if os.Getenv("MOXIE_LIVE") != "1" {
		t.Skip("set MOXIE_LIVE=1 and MOXIE_LIVE_ATTACHMENT_URL to run the live smoke test")
	}
	rawURL := os.Getenv("MOXIE_LIVE_ATTACHMENT_URL")
	if rawURL == "" {
		t.Skip("MOXIE_LIVE_ATTACHMENT_URL is not set")
	}

	cookies, err := browser.GetF95Cookies()
	if err != nil {
		t.Skipf("no F95Zone cookies available: %v", err)
	}
	if strings.TrimSpace(cookies) == "" {
		t.Skip("no F95Zone cookies available")
	}

	client := NewClient(cookies)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	path := filepath.Join(t.TempDir(), "attachment.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer f.Close()

	n, err := client.DownloadToWriter(ctx, rawURL, f)
	if err != nil {
		t.Fatalf("live download failed: %v", err)
	}
	if n == 0 {
		t.Fatal("live download returned 0 bytes")
	}
	t.Logf("downloaded %d bytes to %s", n, path)
}
