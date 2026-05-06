package downloader

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// IdentifyHostInURL
// ---------------------------------------------------------------------------

func TestIdentifyHostInURL_MajorHosts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		url  string
		want string
	}{
		{"https://mega.nz/file/abc123", "mega"},
		{"https://mega.co.nz/file/abc123", "mega"},
		{"https://pixeldrain.com/u/abc123", "pixeldrain"},
		{"https://buzzheavier.com/file", "buzzheavier"},
		{"https://gofile.io/d/abc123", "gofile"},
		{"https://vikingfile.com/file", "vikingfile"},
		{"https://www.mediafire.com/file/abc123", "mediafire"},
		{"https://workupload.com/file/abc123", "workupload"},
		{"https://bunkrr.su/file/abc123", "bunkrr"},
		{"https://bunkr.su/file/abc123", "bunkrr"},
		{"https://krakenfiles.com/view/abc123", "krakenfiles"},
		{"https://uploadhaven.com/download/abc123", "uploadhaven"},
		{"https://wetransfer.com/downloads/abc123", "wetransfer"},
		{"https://sendgb.com/file/abc123", "sendgb"},
		{"https://1cloudfile.com/file/abc123", "1cloudfile"},
		{"https://akirabox.com/file/abc123", "akirabox"},
		{"https://anontransfer.com/file/abc123", "anontransfer"},
		{"https://anonymfile.com/file/abc123", "anonymfile"},
		{"https://apkadmin.com/file/abc123", "apkadmin"},
		{"https://bowfile.com/file/abc123", "bowfile"},
		{"https://catbox.moe/file/abc123", "catbox"},
		{"https://cyberfile.me/file/abc123", "cyberfile"},
		{"https://datanodes.to/file/abc123", "datanodes"},
		{"https://delafil.com/file/abc123", "delafil"},
		{"https://download.gg/file/abc123", "downloadgg"},
		{"https://dropmefiles.com/file/abc123", "dropmefiles"},
		{"https://easyupload.io/file/abc123", "easyupload"},
		{"https://filemail.com/file/abc123", "filemail"},
		{"https://files.dp.ua/file/abc123", "filesdpua"},
		{"https://files.fm/file/abc123", "filesfm"},
		{"https://fromsmash.com/file/abc123", "fromsmash"},
		{"https://drive.google.com/file/d/abc123", "google drive"},
		{"https://hexload.com/file/abc123", "hexload"},
		{"https://mixdrop.co/file/abc123", "mixdrop"},
		{"https://qu.ax/file/abc123", "quax"},
		{"https://transfer.sh/file/abc123", "transfersh"},
		{"https://uploadnow.io/file/abc123", "uploadnow"},
		{"https://wdho.ru/file/abc123", "wdho"},
		{"https://yourfilestore.com/file/abc123", "yourfilestore"},
		{"https://keep2share.cc/file/abc123", "keep2share"},
		{"https://k2s.cc/file/abc123", "keep2share"},
		{"https://uploaded.net/file/abc123", "uploaded"},
		{"https://ul.to/file/abc123", "uploaded"},
		{"https://dropbox.com/s/abc123/file", "dropbox"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := IdentifyHostInURL(tt.url)
			if got != tt.want {
				t.Errorf("IdentifyHostInURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestIdentifyHostInURL_Unknown(t *testing.T) {
	t.Parallel()
	tests := []string{
		"https://example.com/file.zip",
		"https://unknown-host.net/download",
		"",
		"not-a-url",
	}
	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			got := IdentifyHostInURL(url)
			if got != "unknown" {
				t.Errorf("IdentifyHostInURL(%q) = %q, want %q", url, got, "unknown")
			}
		})
	}
}

func TestIdentifyHostInURL_CaseInsensitive(t *testing.T) {
	t.Parallel()
	tests := []struct {
		url  string
		want string
	}{
		{"https://MEGA.NZ/file/abc123", "mega"},
		{"https://PIXELDRAIN.COM/u/abc123", "pixeldrain"},
		{"https://BuzzHeavier.com/file", "buzzheavier"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := IdentifyHostInURL(tt.url)
			if got != tt.want {
				t.Errorf("IdentifyHostInURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// NewHostResolver
// ---------------------------------------------------------------------------

func TestNewHostResolver(t *testing.T) {
	t.Parallel()
	r := NewHostResolver()
	if r == nil {
		t.Fatal("NewHostResolver() returned nil")
	}
	if r.client == nil {
		t.Fatal("NewHostResolver() returned resolver with nil client")
	}
}

// ---------------------------------------------------------------------------
// HostResolver.Resolve - Pixeldrain
// ---------------------------------------------------------------------------

func TestResolvePixeldrain_ValidURL(t *testing.T) {
	t.Parallel()
	r := NewHostResolver()
	result, err := r.Resolve("https://pixeldrain.com/u/abc123", "pixeldrain")
	if err != nil {
		t.Fatalf("Resolve pixeldrain failed: %v", err)
	}
	if result == nil {
		t.Fatal("Resolve pixeldrain returned nil result")
	}
	expectedURL := "https://pixeldrain.com/api/file/abc123"
	if result.URL != expectedURL {
		t.Errorf("expected URL %q, got %q", expectedURL, result.URL)
	}
	if result.Headers == nil {
		t.Fatal("expected non-nil headers")
	}
	if ua, ok := result.Headers["User-Agent"]; !ok || ua == "" {
		t.Errorf("expected User-Agent header, got %q", ua)
	}
}

func TestResolvePixeldrain_APIFormat(t *testing.T) {
	t.Parallel()
	r := NewHostResolver()
	result, err := r.Resolve("https://pixeldrain.com/api/file/xyz789", "pixeldrain")
	if err != nil {
		t.Fatalf("Resolve pixeldrain API format failed: %v", err)
	}
	expectedURL := "https://pixeldrain.com/api/file/xyz789"
	if result.URL != expectedURL {
		t.Errorf("expected URL %q, got %q", expectedURL, result.URL)
	}
}

func TestResolvePixeldrain_InvalidURL(t *testing.T) {
	t.Parallel()
	r := NewHostResolver()
	_, err := r.Resolve("https://example.com/file", "pixeldrain")
	if err == nil {
		t.Fatal("expected error for invalid pixeldrain URL, got nil")
	}
	if !strings.Contains(err.Error(), "could not extract Pixeldrain file ID") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// ---------------------------------------------------------------------------
// HostResolver.Resolve - Mega
// ---------------------------------------------------------------------------

func TestResolveMega_ReturnsError(t *testing.T) {
	t.Parallel()
	r := NewHostResolver()
	result, err := r.Resolve("https://mega.nz/file/abc123", "mega")
	if err == nil {
		t.Fatal("expected error for mega URL, got nil")
	}
	if result != nil {
		t.Errorf("expected nil result for mega, got %+v", result)
	}
	if !strings.Contains(err.Error(), "megatools") {
		t.Errorf("expected error to mention megatools, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// HostResolver.Resolve - VikingFile
// ---------------------------------------------------------------------------

func TestResolveVikingFile_FormPostFlow(t *testing.T) {
	// NOT parallel — replaces http.DefaultTransport which is global state.

	fileHash := "k3rSl0cvF9"
	filename := "game.zip"
	cdnURL := "https://cdn.vikingfile.com/game.zip"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			http.SetCookie(w, &http.Cookie{Name: "PHPSESSID", Value: "test-session-123"})
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<html><body>
				<h2>%s</h2>
				<p id="size">472.46 MB</p>
				<form method="POST">
					<input type="hidden" name="op" value="download1">
					<input type="hidden" name="id" value="%s">
					<input type="hidden" name="rand" value="abc123rand">
					<input type="hidden" name="method_free" value="1">
					<input type="hidden" name="down_direct" value="1">
				</form>
			</body></html>`, filename, fileHash)

		case http.MethodPost:
			if err := r.ParseForm(); err != nil {
				t.Errorf("POST parse form error: %v", err)
			}
			if got := r.FormValue("op"); got != "download1" {
				t.Errorf("POST op = %q, want download1", got)
			}
			if got := r.FormValue("id"); got != fileHash {
				t.Errorf("POST id = %q, want %q", got, fileHash)
			}
			if got := r.FormValue("method_free"); got != "1" {
				t.Errorf("POST method_free = %q, want 1", got)
			}
			if got := r.FormValue("down_direct"); got != "1" {
				t.Errorf("POST down_direct = %q, want 1", got)
			}
			// Verify session cookie forwarded from GET
			cookie := r.Header.Get("Cookie")
			if !strings.Contains(cookie, "PHPSESSID=test-session-123") {
				t.Error("POST request missing session cookie from GET response")
			}

			w.Header().Set("Location", cdnURL)
			w.WriteHeader(http.StatusFound)
		}
	}))
	defer server.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Hostname(), "vikingfile") {
			testURL := server.URL + req.URL.Path
			if req.URL.RawQuery != "" {
				testURL += "?" + req.URL.RawQuery
			}
			newReq, err := http.NewRequest(req.Method, testURL, req.Body)
			if err != nil {
				return nil, err
			}
			newReq.Header = req.Header.Clone()
			return origTransport.RoundTrip(newReq)
		}
		return origTransport.RoundTrip(req)
	})
	defer func() { http.DefaultTransport = origTransport }()

	r := NewHostResolver()
	result, err := r.Resolve("https://vikingfile.com/f/"+fileHash, "vikingfile")
	if err != nil {
		t.Fatalf("Resolve vikingfile form post flow failed: %v", err)
	}
	if result.URL != cdnURL {
		t.Errorf("expected CDN URL %q, got %q", cdnURL, result.URL)
	}
	if result.Headers == nil {
		t.Fatal("expected non-nil headers")
	}
	if ua := result.Headers["User-Agent"]; ua == "" {
		t.Error("expected User-Agent header")
	}
	if result.Headers["Referer"] == "" {
		t.Error("expected Referer header")
	}
}

func TestResolveVikingFile_DirectLinkInPage(t *testing.T) {
	// NOT parallel — replaces http.DefaultTransport which is global state.

	cdnURL := "https://cdn.vikingfile.com/game.zip"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><body>
			<h2>Game.zip</h2>
			<p id="size">472.46 MB</p>
			<a class="button" href="%s">Download</a>
		</body></html>`, cdnURL)
	}))
	defer server.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Hostname(), "vikingfile") {
			testURL := server.URL + req.URL.Path
			if req.URL.RawQuery != "" {
				testURL += "?" + req.URL.RawQuery
			}
			newReq, err := http.NewRequest(req.Method, testURL, req.Body)
			if err != nil {
				return nil, err
			}
			newReq.Header = req.Header.Clone()
			return origTransport.RoundTrip(newReq)
		}
		return origTransport.RoundTrip(req)
	})
	defer func() { http.DefaultTransport = origTransport }()

	r := NewHostResolver()
	result, err := r.Resolve("https://vikingfile.com/f/abc123", "vikingfile")
	if err != nil {
		t.Fatalf("Resolve vikingfile direct link failed: %v", err)
	}
	if result.URL != cdnURL {
		t.Errorf("expected CDN URL %q, got %q", cdnURL, result.URL)
	}
	if result.Headers == nil {
		t.Fatal("expected non-nil headers")
	}
}

func TestResolveVikingFile_DownloadLinkWithID(t *testing.T) {
	// NOT parallel — replaces http.DefaultTransport which is global state.

	dlURL := "https://vikingfile.com/d/Q4GEdYpgph/CyanBrain-1.1.4.zip"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><body>
			<h2>CyanBrain-1.1.4.zip</h2>
			<p id="size">472.46 MB</p>
			<a id="download-link" rel="noopener noreferrer" class="button" href="%s">Download</a>
		</body></html>`, dlURL)
	}))
	defer server.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Hostname(), "vikingfile") {
			testURL := server.URL + req.URL.Path
			if req.URL.RawQuery != "" {
				testURL += "?" + req.URL.RawQuery
			}
			newReq, err := http.NewRequest(req.Method, testURL, req.Body)
			if err != nil {
				return nil, err
			}
			newReq.Header = req.Header.Clone()
			return origTransport.RoundTrip(newReq)
		}
		return origTransport.RoundTrip(req)
	})
	defer func() { http.DefaultTransport = origTransport }()

	r := NewHostResolver()
	result, err := r.Resolve("https://vikingfile.com/f/k3rSl0cvF9", "vikingfile")
	if err != nil {
		t.Fatalf("Resolve vikingfile download-link failed: %v", err)
	}
	if result.URL != dlURL {
		t.Errorf("expected URL %q, got %q", dlURL, result.URL)
	}
}

func TestResolveVikingFile_PostWithFormAction(t *testing.T) {
	// NOT parallel — replaces http.DefaultTransport which is global state.

	fileHash := "abc123"
	cdnURL := "https://cdn.vikingfile.com/game.rar"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			http.SetCookie(w, &http.Cookie{Name: "xfss", Value: "session-form-action"})
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<html><body>
				<form method="POST" action="/download">
					<input type="hidden" name="op" value="download1">
					<input type="hidden" name="id" value="%s">
					<input type="hidden" name="rand" value="testrand">
					<input type="hidden" name="method_free" value="1">
				</form>
			</body></html>`, fileHash)

		case r.Method == http.MethodPost && r.URL.Path == "/download":
			if err := r.ParseForm(); err != nil {
				t.Errorf("POST parse form error: %v", err)
			}
			if got := r.FormValue("op"); got != "download1" {
				t.Errorf("POST op = %q, want download1", got)
			}
			if got := r.FormValue("id"); got != fileHash {
				t.Errorf("POST id = %q, want %q", got, fileHash)
			}
			w.Header().Set("Location", cdnURL)
			w.WriteHeader(http.StatusFound)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Hostname(), "vikingfile") {
			testURL := server.URL + req.URL.Path
			if req.URL.RawQuery != "" {
				testURL += "?" + req.URL.RawQuery
			}
			newReq, err := http.NewRequest(req.Method, testURL, req.Body)
			if err != nil {
				return nil, err
			}
			newReq.Header = req.Header.Clone()
			return origTransport.RoundTrip(newReq)
		}
		return origTransport.RoundTrip(req)
	})
	defer func() { http.DefaultTransport = origTransport }()

	r := NewHostResolver()
	result, err := r.Resolve("https://vikingfile.com/f/"+fileHash, "vikingfile")
	if err != nil {
		t.Fatalf("Resolve vikingfile form action flow failed: %v", err)
	}
	if result.URL != cdnURL {
		t.Errorf("expected CDN URL %q, got %q", cdnURL, result.URL)
	}
}

func TestResolveVikingFile_PostReturns200WithBodyLink(t *testing.T) {
	// NOT parallel — replaces http.DefaultTransport which is global state.

	fileHash := "bodycode456"
	cdnURL := "https://cdn.vikingfile.com/game.7z"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			http.SetCookie(w, &http.Cookie{Name: "xfss", Value: "session-body"})
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<html><body>
				<form method="POST">
					<input type="hidden" name="op" value="download1">
					<input type="hidden" name="id" value="%s">
					<input type="hidden" name="rand" value="bodyrand">
					<input type="hidden" name="method_free" value="1">
				</form>
			</body></html>`, fileHash)

		case http.MethodPost:
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `<html><body>
				<a href="%s">Download Link</a>
			</body></html>`, cdnURL)
		}
	}))
	defer server.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Hostname(), "vikingfile") {
			testURL := server.URL + req.URL.Path
			if req.URL.RawQuery != "" {
				testURL += "?" + req.URL.RawQuery
			}
			newReq, err := http.NewRequest(req.Method, testURL, req.Body)
			if err != nil {
				return nil, err
			}
			newReq.Header = req.Header.Clone()
			return origTransport.RoundTrip(newReq)
		}
		return origTransport.RoundTrip(req)
	})
	defer func() { http.DefaultTransport = origTransport }()

	r := NewHostResolver()
	result, err := r.Resolve("https://vikingfile.com/f/"+fileHash, "vikingfile")
	if err != nil {
		t.Fatalf("Resolve vikingfile body fallback failed: %v", err)
	}
	if result.URL != cdnURL {
		t.Errorf("expected CDN URL %q, got %q", cdnURL, result.URL)
	}
}

func TestResolveVikingFile_NoFormAndNoLink_ReturnsError(t *testing.T) {
	// NOT parallel — replaces http.DefaultTransport which is global state.

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body>
			<h2>File.zip</h2>
			<p id="size">100 MB</p>
			<p>This file requires a captcha to download.</p>
		</body></html>`)
	}))
	defer server.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Hostname(), "vikingfile") {
			testURL := server.URL + req.URL.Path
			if req.URL.RawQuery != "" {
				testURL += "?" + req.URL.RawQuery
			}
			newReq, err := http.NewRequest(req.Method, testURL, req.Body)
			if err != nil {
				return nil, err
			}
			newReq.Header = req.Header.Clone()
			return origTransport.RoundTrip(newReq)
		}
		return origTransport.RoundTrip(req)
	})
	defer func() { http.DefaultTransport = origTransport }()

	r := NewHostResolver()
	_, err := r.Resolve("https://vikingfile.com/f/abc123", "vikingfile")
	if err == nil {
		t.Fatal("expected error for unresolvable VikingFile URL, got nil")
	}
	if !strings.Contains(err.Error(), "could not resolve download URL") {
		t.Errorf("expected error mentioning 'could not resolve download URL', got: %v", err)
	}
}

func TestResolveVikingFile_GETError(t *testing.T) {
	// NOT parallel — replaces http.DefaultTransport which is global state.

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "Internal server error")
	}))
	defer server.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Hostname(), "vikingfile") {
			testURL := server.URL + req.URL.Path
			if req.URL.RawQuery != "" {
				testURL += "?" + req.URL.RawQuery
			}
			newReq, err := http.NewRequest(req.Method, testURL, req.Body)
			if err != nil {
				return nil, err
			}
			newReq.Header = req.Header.Clone()
			return origTransport.RoundTrip(newReq)
		}
		return origTransport.RoundTrip(req)
	})
	defer func() { http.DefaultTransport = origTransport }()

	r := NewHostResolver()
	_, err := r.Resolve("https://vikingfile.com/f/abc123", "vikingfile")
	if err == nil {
		t.Fatal("expected error for HTTP 500 GET response")
	}
	if !strings.Contains(err.Error(), "GET returned HTTP 500") {
		t.Errorf("expected error mentioning 'GET returned HTTP 500', got: %v", err)
	}
}

func TestResolveVikingFile_InvalidURL(t *testing.T) {
	t.Parallel()
	r := NewHostResolver()
	_, err := r.Resolve("https://example.com/file", "vikingfile")
	// The resolver will try to GET this and fail or return the unresolvable error
	// (it doesn't validate URL format up front; the GET will either fail or return no form/link)
	if err == nil {
		t.Fatal("expected error for invalid VikingFile URL, got nil")
	}
}

// ---------------------------------------------------------------------------
// HostResolver.Resolve - Unknown host (generic pass-through)
// ---------------------------------------------------------------------------

func TestResolveUnknownHost(t *testing.T) {
	t.Parallel()
	r := NewHostResolver()
	url := "https://example.com/file.zip"
	result, err := r.Resolve(url, "unknown")
	if err != nil {
		t.Fatalf("Resolve unknown host failed: %v", err)
	}
	if result.URL != url {
		t.Errorf("expected URL %q, got %q", url, result.URL)
	}
	if result.Headers == nil {
		t.Fatal("expected non-nil headers map")
	}
	if len(result.Headers) != 0 {
		t.Errorf("expected empty headers for unknown host, got %v", result.Headers)
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// roundTripFunc adapts a function to http.RoundTripper for test purposes.
type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// ---------------------------------------------------------------------------
// HostResolver.Resolve - DataNodes
// ---------------------------------------------------------------------------

func TestResolveDatanodes_InvalidURL(t *testing.T) {
	t.Parallel()
	r := NewHostResolver()
	_, err := r.Resolve("https://example.com/file", "datanodes")
	if err == nil {
		t.Fatal("expected error for invalid datanodes URL")
	}
	if !strings.Contains(err.Error(), "could not extract DataNodes file code") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestResolveDatanodes_EmptyURL(t *testing.T) {
	t.Parallel()
	r := NewHostResolver()
	_, err := r.Resolve("", "datanodes")
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestResolveDatanodes_MockServer_RedirectFlow(t *testing.T) {
	// NOT parallel — replaces http.DefaultTransport which is global state.

	fileCode := "testcode123"
	cdnURL := "https://cdn.datanodes.to/file/game.zip"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			http.SetCookie(w, &http.Cookie{Name: "xfss", Value: "test-session-xyz"})
			http.SetCookie(w, &http.Cookie{Name: "PHPSESSID", Value: "abc123session"})

			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<html><body>
				<form method="POST">
					<input type="hidden" name="op" value="download">
					<input type="hidden" name="id" value="%s">
					<input type="hidden" name="rand" value="random456">
					<input type="hidden" name="method_free" value="1">
					<input type="hidden" name="file_code" value="%s">
				</form>
			</body></html>`, fileCode, fileCode)

		case http.MethodPost:
			cookie := r.Header.Get("Cookie")
			if !strings.Contains(cookie, "xfss=test-session-xyz") {
				t.Error("POST request missing xfss cookie from GET response")
			}
			if !strings.Contains(cookie, "PHPSESSID=abc123session") {
				t.Error("POST request missing PHPSESSID cookie from GET response")
			}

			if err := r.ParseForm(); err == nil {
				if got := r.FormValue("op"); got != "download" {
					t.Errorf("POST op = %q, want download", got)
				}
				if got := r.FormValue("id"); got != fileCode {
					t.Errorf("POST id = %q, want %q", got, fileCode)
				}
				if got := r.FormValue("rand"); got != "random456" {
					t.Errorf("POST rand = %q, want random456", got)
				}
			}

			w.Header().Set("Location", cdnURL)
			w.WriteHeader(http.StatusFound)
		}
	}))
	defer server.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() == "datanodes.to" {
			testURL := server.URL + req.URL.Path
			if req.URL.RawQuery != "" {
				testURL += "?" + req.URL.RawQuery
			}
			newReq, err := http.NewRequest(req.Method, testURL, req.Body)
			if err != nil {
				return nil, err
			}
			newReq.Header = req.Header.Clone()
			return origTransport.RoundTrip(newReq)
		}
		return origTransport.RoundTrip(req)
	})
	defer func() { http.DefaultTransport = origTransport }()

	r := NewHostResolver()
	result, err := r.Resolve("https://datanodes.to/download/"+fileCode, "datanodes")
	if err != nil {
		t.Fatalf("Resolve datanodes redirect flow failed: %v", err)
	}
	if result.URL != cdnURL {
		t.Errorf("expected CDN URL %q, got %q", cdnURL, result.URL)
	}
	if result.Headers == nil {
		t.Fatal("expected non-nil headers")
	}
	if ua := result.Headers["User-Agent"]; ua == "" {
		t.Error("expected User-Agent header in resolved result")
	}
	expectedReferer := "https://datanodes.to/download/" + fileCode
	if result.Headers["Referer"] != expectedReferer {
		t.Errorf("expected Referer %q, got %q", expectedReferer, result.Headers["Referer"])
	}
}

func TestResolveDatanodes_MockServer_BodyFallback(t *testing.T) {
	// NOT parallel — replaces http.DefaultTransport which is global state.

	fileCode := "bodycode456"
	cdnURL := "https://cdn.datanodes.to/file/game.rar"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			http.SetCookie(w, &http.Cookie{Name: "xfss", Value: "session-body-fallback"})
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<html><body>
				<form method="POST">
					<input type="hidden" name="op" value="download">
					<input type="hidden" name="id" value="%s">
					<input type="hidden" name="rand" value="bodyrand">
					<input type="hidden" name="method_free" value="1">
					<input type="hidden" name="file_code" value="%s">
				</form>
			</body></html>`, fileCode, fileCode)

		case http.MethodPost:
			// Return 200 with download link embedded in the HTML body.
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<html><body>
				<a href="%s">Download Link</a>
				<p>Your download should start automatically.</p>
			</body></html>`, cdnURL)
		}
	}))
	defer server.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() == "datanodes.to" {
			testURL := server.URL + req.URL.Path
			if req.URL.RawQuery != "" {
				testURL += "?" + req.URL.RawQuery
			}
			newReq, err := http.NewRequest(req.Method, testURL, req.Body)
			if err != nil {
				return nil, err
			}
			newReq.Header = req.Header.Clone()
			return origTransport.RoundTrip(newReq)
		}
		return origTransport.RoundTrip(req)
	})
	defer func() { http.DefaultTransport = origTransport }()

	r := NewHostResolver()
	result, err := r.Resolve("https://datanodes.to/download/"+fileCode, "datanodes")
	if err != nil {
		t.Fatalf("Resolve datanodes body fallback failed: %v", err)
	}
	if result.URL != cdnURL {
		t.Errorf("expected CDN URL %q, got %q", cdnURL, result.URL)
	}
	if result.Headers == nil {
		t.Fatal("expected non-nil headers")
	}
}

func TestResolveDatanodes_MockServer_AutoFillFileCode(t *testing.T) {
	// NOT parallel — replaces http.DefaultTransport which is global state.

	fileCode := "autofill789"
	cdnURL := "https://cdn.datanodes.to/file/movie.mp4"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			http.SetCookie(w, &http.Cookie{Name: "xfss", Value: "session-autofill"})
			w.Header().Set("Content-Type", "text/html")
			// Note: no file_code hidden field — resolver should auto-fill it.
			fmt.Fprintf(w, `<html><body>
				<form method="POST">
					<input type="hidden" name="op" value="download">
					<input type="hidden" name="id" value="%s">
					<input type="hidden" name="rand" value="autofill">
					<input type="hidden" name="method_free" value="1">
				</form>
			</body></html>`, fileCode)

		case http.MethodPost:
			// Verify that file_code was auto-filled by the resolver from URL.
			if err := r.ParseForm(); err == nil {
				if got := r.FormValue("file_code"); got != fileCode {
					t.Errorf("POST file_code = %q, want %q (expected auto-fill from URL)", got, fileCode)
				}
			}
			w.Header().Set("Location", cdnURL)
			w.WriteHeader(http.StatusFound)
		}
	}))
	defer server.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() == "datanodes.to" {
			testURL := server.URL + req.URL.Path
			if req.URL.RawQuery != "" {
				testURL += "?" + req.URL.RawQuery
			}
			newReq, err := http.NewRequest(req.Method, testURL, req.Body)
			if err != nil {
				return nil, err
			}
			newReq.Header = req.Header.Clone()
			return origTransport.RoundTrip(newReq)
		}
		return origTransport.RoundTrip(req)
	})
	defer func() { http.DefaultTransport = origTransport }()

	r := NewHostResolver()
	result, err := r.Resolve("https://datanodes.to/download/"+fileCode, "datanodes")
	if err != nil {
		t.Fatalf("Resolve datanodes auto-fill failed: %v", err)
	}
	if result.URL != cdnURL {
		t.Errorf("expected CDN URL %q, got %q", cdnURL, result.URL)
	}
}

func TestResolveDatanodes_MockServer_GETError(t *testing.T) {
	// NOT parallel — replaces http.DefaultTransport which is global state.

	fileCode := "error500"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "Internal server error")
	}))
	defer server.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() == "datanodes.to" {
			testURL := server.URL + req.URL.Path
			if req.URL.RawQuery != "" {
				testURL += "?" + req.URL.RawQuery
			}
			newReq, err := http.NewRequest(req.Method, testURL, req.Body)
			if err != nil {
				return nil, err
			}
			newReq.Header = req.Header.Clone()
			return origTransport.RoundTrip(newReq)
		}
		return origTransport.RoundTrip(req)
	})
	defer func() { http.DefaultTransport = origTransport }()

	r := NewHostResolver()
	_, err := r.Resolve("https://datanodes.to/download/"+fileCode, "datanodes")
	if err == nil {
		t.Fatal("expected error for HTTP 500 GET response")
	}
	if !strings.Contains(err.Error(), "GET returned HTTP 500") {
		t.Errorf("expected error mentioning 'GET returned HTTP 500', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// HostResolver.Resolve - MixDrop
// ---------------------------------------------------------------------------

func TestIdentifyHostInURL_MixDropDomains(t *testing.T) {
	t.Parallel()
	domains := []struct {
		url  string
		want string
	}{
		{"https://mixdrop.co/file/abc123", "mixdrop"},
		{"https://mixdrop.ag/file/abc123", "mixdrop"},
		{"https://mixdrop.net/file/abc123", "mixdrop"},
		{"https://m1xdrop.click/file/abc123", "mixdrop"},
		{"http://mixdrop.co/file/abc123", "mixdrop"},
		{"https://www.mixdrop.co/file/abc123", "mixdrop"},
	}
	for _, tt := range domains {
		t.Run(tt.want, func(t *testing.T) {
			got := IdentifyHostInURL(tt.url)
			if got != tt.want {
				t.Errorf("IdentifyHostInURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestResolveMixdrop_ReturnsURLAsIs(t *testing.T) {
	t.Parallel()
	r := NewHostResolver()
	inputURL := "https://mixdrop.co/file/abc123"
	result, err := r.Resolve(inputURL, "mixdrop")
	if err != nil {
		t.Fatalf("Resolve mixdrop failed: %v", err)
	}
	if result.URL != inputURL {
		t.Errorf("expected URL %q, got %q", inputURL, result.URL)
	}
	if result.Headers == nil {
		t.Fatal("expected non-nil headers")
	}
	if ua, ok := result.Headers["User-Agent"]; !ok || ua == "" {
		t.Error("expected User-Agent header")
	}
}

func TestResolveMixdrop_AllDomains(t *testing.T) {
	t.Parallel()
	r := NewHostResolver()
	domains := []string{
		"https://mixdrop.co/file/abc123",
		"https://mixdrop.ag/file/xyz789",
		"https://mixdrop.net/file/def456",
		"https://m1xdrop.click/file/ghi012",
	}
	for _, url := range domains {
		t.Run(url, func(t *testing.T) {
			result, err := r.Resolve(url, "mixdrop")
			if err != nil {
				t.Fatalf("Resolve mixdrop(%q) failed: %v", url, err)
			}
			if result.URL != url {
				t.Errorf("expected URL to pass through unchanged, got %q", result.URL)
			}
			if ua := result.Headers["User-Agent"]; ua == "" {
				t.Error("expected User-Agent header")
			}
		})
	}
}

func TestResolveMixdrop_MockServerVerifyPassThrough(t *testing.T) {
	// Verify that MixDrop does NOT make any HTTP requests — the resolver
	// returns the URL as-is without contacting any server.
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("resolver must not make HTTP requests for mixdrop pass-through")
	}))
	defer server.Close()

	r := NewHostResolver()
	// Use a mixdrop-style URL (the mock server is never contacted).
	result, err := r.Resolve("https://mixdrop.co/file/abc123", "mixdrop")
	if err != nil {
		t.Fatalf("Resolve mixdrop failed: %v", err)
	}
	if result.URL != "https://mixdrop.co/file/abc123" {
		t.Errorf("expected URL to pass through unchanged, got %q", result.URL)
	}
	if result.Headers == nil {
		t.Fatal("expected non-nil headers")
	}
}

// ---------------------------------------------------------------------------
// extractGoogleDriveFileID
// ---------------------------------------------------------------------------

func TestExtractGoogleDriveFileID_ValidURLs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		url  string
		want string
	}{
		// Pattern 1: /file/d/<FILE_ID>/view (most common share link)
		{"https://drive.google.com/file/d/abc123/view", "abc123"},
		{"https://drive.google.com/file/d/ABC-DEF_123/view", "ABC-DEF_123"},
		{"https://drive.google.com/file/d/xyz789/view?usp=sharing", "xyz789"},

		// Pattern 2: ?id=<FILE_ID> query parameter
		{"https://drive.google.com/uc?export=download&id=FILE_ID_123", "FILE_ID_123"},
		{"https://drive.google.com/uc?id=simple-id", "simple-id"},

		// Pattern 3: /open?id=<FILE_ID>
		{"https://drive.google.com/open?id=ABCDEF123", "ABCDEF123"},
		{"https://drive.google.com/open?id=abc-123_def", "abc-123_def"},

		// Pattern 1 should take precedence over query parameter
		{"https://drive.google.com/file/d/PATH_ID/view?id=IGNORED", "PATH_ID"},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := extractGoogleDriveFileID(tt.url)
			if got != tt.want {
				t.Errorf("extractGoogleDriveFileID(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestExtractGoogleDriveFileID_EdgeCases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		url  string
	}{
		{"empty URL", ""},
		{"no file ID in path", "https://drive.google.com/"},
		{"malformed: missing ID segment", "https://drive.google.com/file/d/"},
		{"malformed: double slash", "https://drive.google.com/file//view"},
		// Note: extractGoogleDriveFileID is a regex-based extractor; it does NOT
		// validate the hostname. Host validation is done by IdentifyHostInURL.
		{"no path or query", "https://drive.google.com"},
		{"query without id param", "https://drive.google.com/uc?export=download"},
		{"not a URL at all", "not-a-url"},
		{"null byte in path", "https://drive.google.com/file/d/\x00/view"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractGoogleDriveFileID(tt.url)
			if got != "" {
				t.Errorf("extractGoogleDriveFileID(%q) = %q, want empty string", tt.url, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// extractGoogleDriveConfirm
// ---------------------------------------------------------------------------

func TestExtractGoogleDriveConfirm_Found(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		html        string
		wantConfirm string
	}{
		{
			name: "uc-download-link anchor with confirm token",
			html: `<html><body>
				<a class="uc-download-link" href="/uc?export=download&confirm=TOKEN_ABC&id=123">Download</a>
			</body></html>`,
			wantConfirm: "TOKEN_ABC",
		},
		{
			name: "generic confirm parameter in form action",
			html: `<html><body>
				<form action="/uc?confirm=XYZ789&id=456">...</form>
			</body></html>`,
			wantConfirm: "XYZ789",
		},
		{
			name: "confirm token with hyphens and underscores",
			html: `<html><body>
				<a class="uc-download-link" href="/uc?export=download&confirm=MY_TOKEN-123_abc">Click</a>
			</body></html>`,
			wantConfirm: "MY_TOKEN-123_abc",
		},
		{
			name: "confirm token inside JavaScript URL string",
			html: `<html><script>
				var url = "/uc?confirm=SOME_TOKEN&id=789";
			</script></html>`,
			wantConfirm: "SOME_TOKEN",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractGoogleDriveConfirm([]byte(tt.html))
			if got != tt.wantConfirm {
				t.Errorf("extractGoogleDriveConfirm = %q, want %q", got, tt.wantConfirm)
			}
		})
	}
}

func TestExtractGoogleDriveConfirm_Missing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		html string
	}{
		{"empty body", ""},
		{"no confirm token in page", "<html><body>Error: file not found</body></html>"},
		{"binary content (non-HTML)", "PK\x03\x04\x00\x00\x00\x00"},
		{"confirm parameter with empty value", `<a href="/uc?export=download&confirm=">Download</a>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractGoogleDriveConfirm([]byte(tt.html))
			if got != "" {
				t.Errorf("extractGoogleDriveConfirm(%q) = %q, want empty string", tt.html, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// HostResolver.Resolve - Google Drive
// ---------------------------------------------------------------------------

func TestResolveGoogleDrive_InvalidURL(t *testing.T) {
	t.Parallel()
	r := NewHostResolver()
	_, err := r.Resolve("https://example.com/file", "google drive")
	if err == nil {
		t.Fatal("expected error for invalid google drive URL")
	}
	if !strings.Contains(err.Error(), "could not extract Google Drive file ID") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestResolveGoogleDrive_EmptyURL(t *testing.T) {
	t.Parallel()
	r := NewHostResolver()
	_, err := r.Resolve("", "google drive")
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestResolveGoogleDrive_MockServer_SmallFile(t *testing.T) {
	t.Parallel()

	fileID := "small-file-123"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e, g := "download", r.URL.Query().Get("export"); e != g {
			t.Errorf("export param: want %q, got %q", e, g)
		}
		if e, g := fileID, r.URL.Query().Get("id"); e != g {
			t.Errorf("id param: want %q, got %q", e, g)
		}
		// Return binary content — small file, no confirm needed.
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="small.zip"`)
		w.Write([]byte("mock binary content for small file"))
	}))
	defer server.Close()

	r := NewHostResolver()
	r.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() == "drive.google.com" {
			testURL := server.URL + req.URL.Path
			if req.URL.RawQuery != "" {
				testURL += "?" + req.URL.RawQuery
			}
			newReq, err := http.NewRequest(req.Method, testURL, req.Body)
			if err != nil {
				return nil, err
			}
			newReq.Header = req.Header.Clone()
			return http.DefaultTransport.RoundTrip(newReq)
		}
		return http.DefaultTransport.RoundTrip(req)
	})

	testURL := fmt.Sprintf("https://drive.google.com/file/d/%s/view", fileID)
	result, err := r.Resolve(testURL, "google drive")
	if err != nil {
		t.Fatalf("Resolve google drive small file failed: %v", err)
	}
	expectedUC := fmt.Sprintf("https://drive.google.com/uc?export=download&id=%s", fileID)
	if result.URL != expectedUC {
		t.Errorf("expected URL %q, got %q", expectedUC, result.URL)
	}
	if result.Headers == nil {
		t.Fatal("expected non-nil headers")
	}
	if ua := result.Headers["User-Agent"]; ua == "" {
		t.Error("expected User-Agent header")
	}
}

func TestResolveGoogleDrive_MockServer_LargeFileWithConfirm(t *testing.T) {
	t.Parallel()

	fileID := "large-file-456"
	confirmToken := "CONFIRM_ABC_789"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e, g := "download", r.URL.Query().Get("export"); e != g {
			t.Errorf("export param: want %q, got %q", e, g)
		}
		if e, g := fileID, r.URL.Query().Get("id"); e != g {
			t.Errorf("id param: want %q, got %q", e, g)
		}
		// The resolver should NOT send a confirm param on the initial request.
		if confirm := r.URL.Query().Get("confirm"); confirm != "" {
			t.Errorf("initial GET should not have confirm param, got %q", confirm)
		}

		// Return virus-scan warning page with confirm token.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><body>
			<p>Google Drive: virus scan warning</p>
			<a class="uc-download-link" href="/uc?export=download&confirm=%s&id=%s">Download anyway</a>
			<form id="download-form" action="/uc?confirm=%s&id=%s" method="POST"></form>
		</body></html>`, confirmToken, fileID, confirmToken, fileID)
	}))
	defer server.Close()

	r := NewHostResolver()
	r.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() == "drive.google.com" {
			testURL := server.URL + req.URL.Path
			if req.URL.RawQuery != "" {
				testURL += "?" + req.URL.RawQuery
			}
			newReq, err := http.NewRequest(req.Method, testURL, req.Body)
			if err != nil {
				return nil, err
			}
			newReq.Header = req.Header.Clone()
			return http.DefaultTransport.RoundTrip(newReq)
		}
		return http.DefaultTransport.RoundTrip(req)
	})

	testURL := fmt.Sprintf("https://drive.google.com/open?id=%s", fileID)
	result, err := r.Resolve(testURL, "google drive")
	if err != nil {
		t.Fatalf("Resolve google drive large file failed: %v", err)
	}
	expectedURL := fmt.Sprintf(
		"https://drive.google.com/uc?export=download&confirm=%s&id=%s",
		confirmToken, fileID,
	)
	if result.URL != expectedURL {
		t.Errorf("expected URL with confirm token %q, got %q", expectedURL, result.URL)
	}
}

func TestResolveGoogleDrive_MockServer_MissingConfirmToken(t *testing.T) {
	t.Parallel()

	fileID := "no-confirm-789"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return HTML page WITHOUT any confirm token (e.g., error page).
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><body>
			<h1>Error</h1>
			<p>This file cannot be scanned for viruses.</p>
		</body></html>`)
	}))
	defer server.Close()

	r := NewHostResolver()
	r.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() == "drive.google.com" {
			testURL := server.URL + req.URL.Path
			if req.URL.RawQuery != "" {
				testURL += "?" + req.URL.RawQuery
			}
			newReq, err := http.NewRequest(req.Method, testURL, req.Body)
			if err != nil {
				return nil, err
			}
			newReq.Header = req.Header.Clone()
			return http.DefaultTransport.RoundTrip(newReq)
		}
		return http.DefaultTransport.RoundTrip(req)
	})

	testURL := fmt.Sprintf("https://drive.google.com/file/d/%s/view", fileID)
	result, err := r.Resolve(testURL, "google drive")
	if err != nil {
		t.Fatalf("Resolve google drive missing confirm failed: %v", err)
	}
	// Should fall through to UC URL when confirm token is not found.
	expectedURL := fmt.Sprintf("https://drive.google.com/uc?export=download&id=%s", fileID)
	if result.URL != expectedURL {
		t.Errorf("expected fallback URL %q, got %q", expectedURL, result.URL)
	}
}

func TestResolveGoogleDrive_MockServer_HTMLErrorWithoutConfirm(t *testing.T) {
	t.Parallel()

	// Google Drive returns an HTML error page when something goes wrong (e.g.,
	// file is too large, rate limited). The resolver parses it for a confirm
	// token and falls through to the UC URL if none is found — it does NOT
	// treat non-200 status codes as errors; the caller's fallback loop handles
	// retries.

	fileID := "error-html-000"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return HTML error page without any confirm token.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `<html><body><h1>503 Service Unavailable</h1>
			<p>Google Drive is temporarily unavailable.</p>
		</body></html>`)
	}))
	defer server.Close()

	r := NewHostResolver()
	r.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() == "drive.google.com" {
			testURL := server.URL + req.URL.Path
			if req.URL.RawQuery != "" {
				testURL += "?" + req.URL.RawQuery
			}
			newReq, err := http.NewRequest(req.Method, testURL, req.Body)
			if err != nil {
				return nil, err
			}
			newReq.Header = req.Header.Clone()
			return http.DefaultTransport.RoundTrip(newReq)
		}
		return http.DefaultTransport.RoundTrip(req)
	})

	testURL := fmt.Sprintf("https://drive.google.com/file/d/%s/view", fileID)
	result, err := r.Resolve(testURL, "google drive")
	if err != nil {
		t.Fatalf("expected fallback to UC URL on error HTML, got error: %v", err)
	}
	expectedURL := fmt.Sprintf("https://drive.google.com/uc?export=download&id=%s", fileID)
	if result.URL != expectedURL {
		t.Errorf("expected fallback URL %q, got %q", expectedURL, result.URL)
	}
}

func TestResolveGoogleDrive_MockServer_NonHTMLContent(t *testing.T) {
	t.Parallel()

	// Test that non-HTML content types (e.g., application/json error responses)
	// cause the resolver to return the UC URL without parsing the body.

	fileID := "json-error-111"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":"rateLimitExceeded","message":"Too many requests"}`)
	}))
	defer server.Close()

	r := NewHostResolver()
	r.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() == "drive.google.com" {
			testURL := server.URL + req.URL.Path
			if req.URL.RawQuery != "" {
				testURL += "?" + req.URL.RawQuery
			}
			newReq, err := http.NewRequest(req.Method, testURL, req.Body)
			if err != nil {
				return nil, err
			}
			newReq.Header = req.Header.Clone()
			return http.DefaultTransport.RoundTrip(newReq)
		}
		return http.DefaultTransport.RoundTrip(req)
	})

	testURL := fmt.Sprintf("https://drive.google.com/file/d/%s/view", fileID)
	result, err := r.Resolve(testURL, "google drive")
	if err != nil {
		t.Fatalf("expected UC URL for JSON response, got error: %v", err)
	}
	expectedURL := fmt.Sprintf("https://drive.google.com/uc?export=download&id=%s", fileID)
	if result.URL != expectedURL {
		t.Errorf("expected URL %q, got %q", expectedURL, result.URL)
	}
}
