package downloader

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// CheckLink / CheckLinkWithHost - URL validation layer
//
// CheckLink requires HTTPS URLs with non-private hostnames. Testing the full
// HTTP round-trip through CheckLink itself is impractical because it builds
// its own resolver via NewHostResolver, whose client cannot be injected, and
// isValidDownloadURL blocks loopback/private IP literals. We test:
//
//  1. URL validation rejects invalid/blocked URLs (correct security behavior)
//  2. Resolver-backed hosts are routed through HostResolver.Resolve — the
//     resolver's URL-parsing errors surface without any network I/O, which a
//     raw HEAD on the stored URL would not do
//  3. The resolved-URL HEAD path (headURL) end-to-end, by injecting a client
//     that reaches an httptest TLS server through the "example.com" host
//     name: the httptest test certificate is valid for example.com, and
//     isValidDownloadURL accepts it because it is not an IP literal
//  4. The status-code mapping via the headURL round-trips and the pure
//     statusError table.
// ---------------------------------------------------------------------------

func TestCheckLink_InvalidURL(t *testing.T) {
	t.Parallel()
	err := CheckLink("http://169.254.169.254/latest/meta-data/")
	if err == nil {
		t.Fatal("expected error for blocked URL, got nil")
	}
	if !strings.Contains(err.Error(), "invalid URL") {
		t.Errorf("expected 'invalid URL' error, got: %v", err)
	}
}

func TestCheckLink_InvalidURLBadScheme(t *testing.T) {
	t.Parallel()
	err := CheckLink("ftp://example.com/file.zip")
	if err == nil {
		t.Fatal("expected error for non-https URL, got nil")
	}
}

func TestCheckLink_InvalidURLNonHTTPS(t *testing.T) {
	t.Parallel()
	err := CheckLink("http://example.com/file.zip")
	if err == nil {
		t.Fatal("expected error for http URL, got nil")
	}
}

func TestCheckLink_InvalidLoopback(t *testing.T) {
	t.Parallel()
	err := CheckLink("https://127.0.0.1/file.zip")
	if err == nil {
		t.Fatal("expected error for loopback, got nil")
	}
}

func TestCheckLink_InvalidPrivateIP(t *testing.T) {
	t.Parallel()
	err := CheckLink("https://192.168.1.1/file.zip")
	if err == nil {
		t.Fatal("expected error for private IP, got nil")
	}
}

func TestCheckLink_InvalidMetadataEndpoint(t *testing.T) {
	t.Parallel()
	tests := []string{
		"https://169.254.169.254/latest/meta-data/",
		"https://metadata.google.internal/computeMetadata/",
		"https://100.100.100.200/latest/",
	}
	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			err := CheckLink(url)
			if err == nil {
				t.Errorf("expected error for blocked URL %q, got nil", url)
			}
			if !strings.Contains(err.Error(), "invalid URL") {
				t.Errorf("expected 'invalid URL' error for %q, got: %v", url, err)
			}
		})
	}
}

func TestCheckLinkWithHost_Mega(t *testing.T) {
	t.Parallel()
	err := CheckLinkWithHost("https://mega.nz/file/ABC123", "mega")
	if err == nil {
		t.Fatal("expected error for mega link, got nil")
	}
	if !strings.Contains(err.Error(), "Mega uses encrypted protocol") {
		t.Errorf("expected mega protocol error, got: %v", err)
	}
}

// TestCheckLinkWithHost_InvalidURLRejectedBeforeResolver ensures invalid
// URLs are rejected up front, before any resolver or HEAD logic runs — even
// for resolver-backed hosts.
func TestCheckLinkWithHost_InvalidURLRejectedBeforeResolver(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		url  string
		host string
	}{
		{"non-https", "http://example.com/file.zip", "pixeldrain"},
		{"loopback", "https://127.0.0.1/file.zip", "gofile"},
		{"metadata", "https://169.254.169.254/latest/meta-data/", "datanodes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckLinkWithHost(tt.url, tt.host)
			if err == nil {
				t.Fatal("expected error for invalid URL, got nil")
			}
			if !strings.Contains(err.Error(), "invalid URL") {
				t.Errorf("expected 'invalid URL' error, got: %v", err)
			}
		})
	}
}

// TestCheckLinkWithHost_ResolverRouting proves resolver-backed hosts are
// routed through HostResolver.Resolve: each case fails in the resolver's
// URL-parsing stage, which performs no network I/O, rather than in a raw
// HEAD request against the stored URL.
func TestCheckLinkWithHost_ResolverRouting(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		url  string
		host string
		want string // substring expected from the host resolver
	}{
		{"gofile", "https://example.com/file.zip", "gofile", "could not extract Gofile content ID"},
		{"pixeldrain", "https://example.com/file.zip", "pixeldrain", "could not extract Pixeldrain file ID"},
		{"datanodes", "https://example.com/file.zip", "datanodes", "could not extract DataNodes file code"},
		{"googledrive", "https://example.com/file.zip", "googledrive", "could not extract Google Drive file ID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckLinkWithHost(tt.url, tt.host)
			if err == nil {
				t.Fatalf("expected resolver error for host %q, got nil", tt.host)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("expected error containing %q, got: %v", tt.want, err)
			}
		})
	}
}

// testTLSServerClient returns an http.Client that reaches an httptest TLS
// server via its 127.0.0.1 listener while the URL keeps a host name that
// passes isValidDownloadURL. The httptest test certificate is valid for
// "example.com", so URLs of the form https://example.com:<port>/... pass the
// validity guard and are dialed straight to the test server.
func testTLSServerClient(srv *httptest.Server) *http.Client {
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	tr := srv.Client().Transport.(*http.Transport).Clone()
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	}
	return &http.Client{Transport: tr, Timeout: 10 * time.Second}
}

func TestHeadURL_ValidStatus(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("expected HEAD request, got %s", r.Method)
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("expected User-Agent header on HEAD request")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r := &HostResolver{client: testTLSServerClient(srv)}
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	if err := r.headURL(fmt.Sprintf("https://example.com:%d/file.zip", port), nil); err != nil {
		t.Errorf("expected nil for 200 response, got: %v", err)
	}
}

func TestHeadURL_AppliesResolverHeaders(t *testing.T) {
	var gotReferer, gotCookie string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReferer = r.Header.Get("Referer")
		gotCookie = r.Header.Get("Cookie")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r := &HostResolver{client: testTLSServerClient(srv)}
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	err := r.headURL(fmt.Sprintf("https://example.com:%d/file.zip", port), map[string]string{
		"Referer": "https://host.example/page",
		"Cookie":  "session=abc123",
	})
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
	if gotReferer != "https://host.example/page" {
		t.Errorf("expected resolver Referer header on HEAD, got %q", gotReferer)
	}
	if gotCookie != "session=abc123" {
		t.Errorf("expected resolver Cookie header on HEAD, got %q", gotCookie)
	}
}

func TestHeadURL_FollowsRedirects(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r := &HostResolver{client: testTLSServerClient(srv)}
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	if err := r.headURL(fmt.Sprintf("https://example.com:%d/redirect", port), nil); err != nil {
		t.Errorf("expected redirect to resolve to a live link, got: %v", err)
	}
}

func TestHeadURL_DeadStatuses(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/404":
			w.WriteHeader(http.StatusNotFound)
		case "/403":
			w.WriteHeader(http.StatusForbidden)
		case "/410":
			w.WriteHeader(http.StatusGone)
		case "/429":
			w.WriteHeader(http.StatusTooManyRequests)
		case "/503":
			w.WriteHeader(http.StatusServiceUnavailable)
		case "/500":
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	r := &HostResolver{client: testTLSServerClient(srv)}
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	tests := []struct {
		path string
		want string
	}{
		{"/404", "404 Not Found - file removed"},
		{"/403", "403 Forbidden - access denied or DMCA'd"},
		{"/410", "410 Gone - permanently removed"},
		{"/503", "503 Service Unavailable - host down"},
		{"/429", "429 Too Many Requests - rate limited"},
		{"/500", "500 Server Error - host error"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			err := r.headURL(fmt.Sprintf("https://example.com:%d%s", port, tt.path), nil)
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tt.path)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("expected error containing %q, got: %v", tt.want, err)
			}
		})
	}
}

func TestHeadURL_InvalidTarget(t *testing.T) {
	t.Parallel()
	// Zero-value resolver: the URL guard must reject bad targets before any
	// client or cookie plumbing is touched.
	r := &HostResolver{}
	tests := []string{
		"",
		"ftp://example.com/file.zip",
		"https://127.0.0.1/file.zip",
		"https://192.168.1.1/file.zip",
		"https://169.254.169.254/latest/meta-data/",
	}
	for _, u := range tests {
		t.Run(u, func(t *testing.T) {
			err := r.headURL(u, nil)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", u)
			}
			if !strings.Contains(err.Error(), "invalid URL") {
				t.Errorf("expected 'invalid URL' error for %q, got: %v", u, err)
			}
		})
	}
}

func TestStatusError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		code int
		want string // "" means nil error (live link)
	}{
		{http.StatusOK, ""},
		{http.StatusPartialContent, ""},
		{http.StatusFound, ""}, // 3xx served without a followable redirect → still live
		{http.StatusNotFound, "404 Not Found - file removed"},
		{http.StatusForbidden, "403 Forbidden - access denied or DMCA'd"},
		{http.StatusGone, "410 Gone - permanently removed"},
		{http.StatusServiceUnavailable, "503 Service Unavailable - host down"},
		{http.StatusTooManyRequests, "429 Too Many Requests - rate limited"},
		{http.StatusInternalServerError, "500 Server Error - host error"},
		{http.StatusMethodNotAllowed, "405 Client Error"},
	}
	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.code), func(t *testing.T) {
			err := statusError(tt.code)
			if tt.want == "" {
				if err != nil {
					t.Errorf("expected nil for status %d, got: %v", tt.code, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error for status %d, got nil", tt.code)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("expected error containing %q, got: %v", tt.want, err)
			}
		})
	}
}
