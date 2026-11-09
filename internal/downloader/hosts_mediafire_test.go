package downloader

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// extractMediafireCDNURL
// ---------------------------------------------------------------------------

// TestExtractMediafireCDNURL exercises the page parser against captured and
// synthetic MediaFire page snippets: plain CDN hrefs, base64
// data-scrambled-url attributes, and pages without any link.
func TestExtractMediafireCDNURL(t *testing.T) {
	t.Parallel()

	const cdnURL = "https://download1078.mediafire.com/49twy6x75v8gEt4oW3dcXTTx_AglW94kgq4zlMBiSNrhizgBjI5p8xbatMC9WVGUvgsCkGkJYznW_aMbz16VjbDQtPbx0XOZnuetK2iwJ3PqTfJjhQy9qI0IUTioJPtn7-P4P17Ypv6NQwMeiSmskbhTJWyJ_Mbvx9V4ljyW2au1/3v72hkelm1a8y9y/Imprisoned+Queen.zip"

	scrambled := base64.StdEncoding.EncodeToString([]byte(cdnURL))

	tests := []struct {
		name string
		html string
		want string
	}{
		{
			name: "plain href on downloadButton",
			html: `<html><body>
				<div class="download_link" id="download_link">
					<a class="input popsok"
					   aria-label="Download file"
					   href="` + cdnURL + `"
					   id="downloadButton"
					   rel="nofollow"></a>
				</div>
			</body></html>`,
			want: cdnURL,
		},
		{
			name: "base64 data-scrambled-url on downloadButton",
			html: `<a class="input popsok"
				aria-label="Download file"
				data-scrambled-url="` + scrambled + `"
				id="downloadButton"
				rel="nofollow"></a>`,
			want: cdnURL,
		},
		{
			name: "scrambled URL with attributes in different order",
			html: `<a id="downloadButton" class="input popsok" rel="nofollow"
				data-scrambled-url="` + scrambled + `"
				aria-label="Download file"></a>`,
			want: cdnURL,
		},
		{
			name: "scrambled payload decoding to non-CDN host is rejected",
			html: `<a id="downloadButton"
				data-scrambled-url="` + base64.StdEncoding.EncodeToString([]byte("https://evil.example.com/payload.zip")) + `"></a>`,
			want: "",
		},
		{
			name: "download href outside downloadButton still matches",
			html: `<html><body><div class="download_link" id="download_link">
				<a class="preparing" href="#">Preparing Download</a>
				</div>
				<a href="` + cdnURL + `" id="someOtherLink">direct</a>
			</body></html>`,
			want: cdnURL,
		},
		{
			name: "no download link",
			html: `<html><body><div class="download_link" id="download_link">
				<a class="preparing" href="#">Preparing Download</a>
			</div></body></html>`,
			want: "",
		},
		{
			name: "empty page",
			html: "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := extractMediafireCDNURL(tt.html); got != tt.want {
				t.Errorf("extractMediafireCDNURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDecodeMediafireBase64 covers padded, unpadded, and invalid inputs.
func TestDecodeMediafireBase64(t *testing.T) {
	t.Parallel()

	padded := base64.StdEncoding.EncodeToString([]byte("https://download1.mediafire.com/x/y.zip"))
	unpadded := strings.TrimRight(padded, "=")

	tests := []struct {
		name    string
		encoded string
		want    string
		wantErr bool
	}{
		{name: "padded standard base64", encoded: padded, want: "https://download1.mediafire.com/x/y.zip"},
		{name: "unpadded base64", encoded: unpadded, want: "https://download1.mediafire.com/x/y.zip"},
		{name: "invalid base64", encoded: "!!!not-base64!!!", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := decodeMediafireBase64(tt.encoded)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("decodeMediafireBase64() error: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("decodeMediafireBase64() = %q, want %q", string(got), tt.want)
			}
		})
	}
}

// TestIsMediafireCDNURL validates the CDN host allow-list.
func TestIsMediafireCDNURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw  string
		want bool
	}{
		{"https://download1078.mediafire.com/x/y.zip", true},
		{"https://download.mediafire.com/x/y.zip", true},
		{"http://download1.mediafire.com/x/y.zip", true},
		{"https://www.mediafire.com/file/abc123/name.zip/file", false},
		{"https://evil.example.com/payload.zip", false},
		{"ftp://download1.mediafire.com/x", false},
		{"not a url", false},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			t.Parallel()
			if got := isMediafireCDNURL(tt.raw); got != tt.want {
				t.Errorf("isMediafireCDNURL(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parseMediafireURL
// ---------------------------------------------------------------------------

func TestParseMediafireURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		rawURL     string
		wantPage   string
		wantKind   string
		wantKey    string
		wantErrSub string
	}{
		{
			name:     "full file URL with name and /file suffix",
			rawURL:   "https://www.mediafire.com/file/3v72hkelm1a8y9y/Imprisoned_Queen.zip/file",
			wantPage: "https://www.mediafire.com/file/3v72hkelm1a8y9y/Imprisoned_Queen.zip/file",
			wantKind: "file",
			wantKey:  "3v72hkelm1a8y9y",
		},
		{
			name:     "file URL without name",
			rawURL:   "https://www.mediafire.com/file/3j6ahcjrxo2bwbz",
			wantPage: "https://www.mediafire.com/file/3j6ahcjrxo2bwbz",
			wantKind: "file",
			wantKey:  "3j6ahcjrxo2bwbz",
		},
		{
			name:     "http scheme",
			rawURL:   "http://www.mediafire.com/file/x7hg5xgpi78defg/LoQOO.pdf/file",
			wantPage: "http://www.mediafire.com/file/x7hg5xgpi78defg/LoQOO.pdf/file",
			wantKind: "file",
			wantKey:  "x7hg5xgpi78defg",
		},
		{
			name:     "file_premium path type",
			rawURL:   "https://www.mediafire.com/file_premium/abc123/name.zip/file",
			wantPage: "https://www.mediafire.com/file_premium/abc123/name.zip/file",
			wantKind: "file_premium",
			wantKey:  "abc123",
		},
		{
			name:     "folder URL",
			rawURL:   "https://www.mediafire.com/folder/xb49obyqfut8d/Merlin_Das_Trevas",
			wantPage: "https://www.mediafire.com/folder/xb49obyqfut8d/Merlin_Das_Trevas",
			wantKind: "folder",
			wantKey:  "xb49obyqfut8d",
		},
		{
			name:     "mobile subdomain normalized to www",
			rawURL:   "https://m.mediafire.com/file/abc123/name.zip/file",
			wantPage: "https://www.mediafire.com/file/abc123/name.zip/file",
			wantKind: "file",
			wantKey:  "abc123",
		},
		{
			name:       "not a mediafire URL",
			rawURL:     "https://example.com/file/abc123",
			wantErrSub: "not a mediafire.com URL",
		},
		{
			name:       "unsupported path type",
			rawURL:     "https://www.mediafire.com/view/abc123/photo.png/file",
			wantErrSub: "unsupported path type",
		},
		{
			name:       "missing key",
			rawURL:     "https://www.mediafire.com/file/",
			wantErrSub: "could not extract key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pageURL, kind, key, err := parseMediafireURL(tt.rawURL)
			if tt.wantErrSub != "" {
				if err == nil {
					t.Fatalf("parseMediafireURL(%q) expected error containing %q, got nil", tt.rawURL, tt.wantErrSub)
				}
				if !strings.Contains(err.Error(), tt.wantErrSub) {
					t.Errorf("parseMediafireURL(%q) error = %q, want substring %q", tt.rawURL, err, tt.wantErrSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseMediafireURL(%q) unexpected error: %v", tt.rawURL, err)
			}
			if pageURL != tt.wantPage {
				t.Errorf("parseMediafireURL(%q) pageURL = %q, want %q", tt.rawURL, pageURL, tt.wantPage)
			}
			if kind != tt.wantKind {
				t.Errorf("parseMediafireURL(%q) kind = %q, want %q", tt.rawURL, kind, tt.wantKind)
			}
			if key != tt.wantKey {
				t.Errorf("parseMediafireURL(%q) key = %q, want %q", tt.rawURL, key, tt.wantKey)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// resolveMediafire — mock server flows
// ---------------------------------------------------------------------------

// TestResolveMediafire_MockServer_PlainHref covers the primary path: the
// public file page embeds the direct CDN URL as a plain href on
// #downloadButton.
func TestResolveMediafire_MockServer_PlainHref(t *testing.T) {
	t.Parallel()

	const cdnURL = "https://download1078.mediafire.com/49twy6x75v8gEt4oW3dcXTTx_AglW94kgq4zlMBiSNrhizgBjI5p8xbatMC9WVGUvgsCkGkJYznW_aMbz16VjbDQtPbx0XOZnuetK2iwJ3PqTfJjhQy9qI0IUTioJPtn7-P4P17Ypv6NQwMeiSmskbhTJWyJ_Mbvx9V4ljyW2au1/3v72hkelm1a8y9y/Imprisoned+Queen.zip"
	page := `<html><body>
		<div class="download_link" id="download_link">
			<a class="input popsok"
			   aria-label="Download file"
			   href="` + cdnURL + `"
			   id="downloadButton"
			   rel="nofollow"></a>
		</div>
	</body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e, g := "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8", r.Header.Get("Accept"); e != g {
			t.Errorf("Accept header: want %q, got %q", e, g)
		}
		if ua := r.Header.Get("User-Agent"); ua == "" {
			t.Error("expected User-Agent header on page request")
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page)
	}))
	defer server.Close()

	r := NewHostResolver()
	r.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() == "www.mediafire.com" {
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

	result, err := r.Resolve("https://www.mediafire.com/file/3v72hkelm1a8y9y/Imprisoned_Queen.zip/file", "mediafire")
	if err != nil {
		t.Fatalf("Resolve mediafire plain href failed: %v", err)
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
	wantReferer := "https://www.mediafire.com/file/3v72hkelm1a8y9y/Imprisoned_Queen.zip/file"
	if result.Headers["Referer"] != wantReferer {
		t.Errorf("expected Referer %q, got %q", wantReferer, result.Headers["Referer"])
	}
}

// TestResolveMediafire_MockServer_ScrambledURL covers the base64
// data-scrambled-url variant of the download button.
func TestResolveMediafire_MockServer_ScrambledURL(t *testing.T) {
	t.Parallel()

	const cdnURL = "https://download42.mediafire.com/abcdef123456/abc123/file.zip"
	scrambled := base64.StdEncoding.EncodeToString([]byte(cdnURL))
	page := `<a class="input popsok"
		aria-label="Download file"
		data-scrambled-url="` + scrambled + `"
		id="downloadButton"
		rel="nofollow"></a>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page)
	}))
	defer server.Close()

	r := NewHostResolver()
	r.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() == "www.mediafire.com" {
			testURL := server.URL + req.URL.Path
			newReq, err := http.NewRequest(req.Method, testURL, req.Body)
			if err != nil {
				return nil, err
			}
			newReq.Header = req.Header.Clone()
			return http.DefaultTransport.RoundTrip(newReq)
		}
		return http.DefaultTransport.RoundTrip(req)
	})

	result, err := r.Resolve("https://www.mediafire.com/file/abc123/file.zip/file", "mediafire")
	if err != nil {
		t.Fatalf("Resolve mediafire scrambled failed: %v", err)
	}
	if result.URL != cdnURL {
		t.Errorf("expected decoded CDN URL %q, got %q", cdnURL, result.URL)
	}
}

// TestResolveMediafire_MockServer_APIFallback covers the fallback: the page
// has no CDN link, so the resolver queries /api/file/get_links.php and uses
// its direct_download field.
func TestResolveMediafire_MockServer_APIFallback(t *testing.T) {
	t.Parallel()

	const directURL = "https://download7.mediafire.com/xyz789/abc123/game.rar"
	page := `<html><body><div class="download_link" id="download_link">
		<a class="preparing" href="#">Preparing Download</a>
	</div></body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/file/abc123/name.zip/file":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, page)
		case r.URL.Path == "/api/file/get_links.php":
			if e, g := "abc123", r.URL.Query().Get("quick_key"); e != g {
				t.Errorf("quick_key param: want %q, got %q", e, g)
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"response":{"action":"file/get_links","links":[{"quickkey":"abc123","direct_download":%q}],"result":"Success"}}`, directURL)
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	r := NewHostResolver()
	r.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() == "www.mediafire.com" {
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

	result, err := r.Resolve("https://www.mediafire.com/file/abc123/name.zip/file", "mediafire")
	if err != nil {
		t.Fatalf("Resolve mediafire API fallback failed: %v", err)
	}
	if result.URL != directURL {
		t.Errorf("expected API direct_download URL %q, got %q", directURL, result.URL)
	}
}

// TestResolveMediafire_MockServer_APIFallbackEmpty covers failure of both the
// page parse and the API fallback.
func TestResolveMediafire_MockServer_APIFallbackEmpty(t *testing.T) {
	t.Parallel()

	page := `<html><body>nothing here</body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/file/abc123/name.zip/file":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, page)
		case r.URL.Path == "/api/file/get_links.php":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"response":{"action":"file/get_links","links":[{"quickkey":"abc123","direct_download_error":"45"}],"result":"Success"}}`)
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	r := NewHostResolver()
	r.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() == "www.mediafire.com" {
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

	_, err := r.Resolve("https://www.mediafire.com/file/abc123/name.zip/file", "mediafire")
	if err == nil {
		t.Fatal("expected error when page and API both lack a direct link")
	}
	if !strings.Contains(err.Error(), "page and API both failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestResolveMediafire_MockServer_HTTPError covers non-200 page responses.
func TestResolveMediafire_MockServer_HTTPError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	r := NewHostResolver()
	r.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() == "www.mediafire.com" {
			testURL := server.URL + req.URL.Path
			newReq, err := http.NewRequest(req.Method, testURL, req.Body)
			if err != nil {
				return nil, err
			}
			newReq.Header = req.Header.Clone()
			return http.DefaultTransport.RoundTrip(newReq)
		}
		return http.DefaultTransport.RoundTrip(req)
	})

	_, err := r.Resolve("https://www.mediafire.com/file/abc123/name.zip/file", "mediafire")
	if err == nil {
		t.Fatal("expected error for HTTP 404 page")
	}
	if !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestResolveMediafire_MockServer_FolderSingleFile covers folder links: the
// folder API returns one file, which is then resolved through the normal file
// page flow.
func TestResolveMediafire_MockServer_FolderSingleFile(t *testing.T) {
	t.Parallel()

	const cdnURL = "https://download99.mediafire.com/abc/xyz/000-Merlin.mp3"
	filePage := `<a class="input popsok"
		aria-label="Download file"
		href="` + cdnURL + `"
		id="downloadButton"
		rel="nofollow"></a>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/folder/get_content.php":
			if e, g := "xb49obyqfut8d", r.URL.Query().Get("folder_key"); e != g {
				t.Errorf("folder_key param: want %q, got %q", e, g)
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"response":{"action":"folder/get_content","folder_content":{"files":[{"quickkey":"bcb8wffy7vp1msj","filename":"000-Merlin.mp3","links":{"normal_download":"https://www.mediafire.com/file/bcb8wffy7vp1msj/000-Merlin.mp3/file"}}]},"result":"Success"}}`)
		case r.URL.Path == "/file/bcb8wffy7vp1msj/000-Merlin.mp3/file":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, filePage)
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	r := NewHostResolver()
	r.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() == "www.mediafire.com" {
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

	result, err := r.Resolve("https://www.mediafire.com/folder/xb49obyqfut8d/Merlin_Das_Trevas", "mediafire")
	if err != nil {
		t.Fatalf("Resolve mediafire folder (single file) failed: %v", err)
	}
	if result.URL != cdnURL {
		t.Errorf("expected folder file CDN URL %q, got %q", cdnURL, result.URL)
	}
}

// TestResolveMediafire_MockServer_FolderMultiFile covers multi-file folders,
// which the resolver refuses because it can only yield one URL.
func TestResolveMediafire_MockServer_FolderMultiFile(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"response":{"action":"folder/get_content","folder_content":{"files":[
			{"quickkey":"aaa111","filename":"a.zip","links":{"normal_download":"https://www.mediafire.com/file/aaa111/a.zip/file"}},
			{"quickkey":"bbb222","filename":"b.zip","links":{"normal_download":"https://www.mediafire.com/file/bbb222/b.zip/file"}}
		]},"result":"Success"}}`)
	}))
	defer server.Close()

	r := NewHostResolver()
	r.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() == "www.mediafire.com" {
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

	_, err := r.Resolve("https://www.mediafire.com/folder/xb49obyqfut8d/Merlin_Das_Trevas", "mediafire")
	if err == nil {
		t.Fatal("expected error for multi-file folder")
	}
	if !strings.Contains(err.Error(), "2 files") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestResolveMediafire_MockServer_FolderEmpty covers folders with no files.
func TestResolveMediafire_MockServer_FolderEmpty(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"response":{"action":"folder/get_content","folder_content":{"files":[]},"result":"Success"}}`)
	}))
	defer server.Close()

	r := NewHostResolver()
	r.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() == "www.mediafire.com" {
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

	_, err := r.Resolve("https://www.mediafire.com/folder/xb49obyqfut8d/Merlin_Das_Trevas", "mediafire")
	if err == nil {
		t.Fatal("expected error for empty folder")
	}
	if !strings.Contains(err.Error(), "contains no files") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestResolveMediafire_MockServer_CookiesAttached verifies browser cookies
// for mediafire.com are attached to the page request.
func TestResolveMediafire_MockServer_CookiesAttached(t *testing.T) {
	t.Parallel()

	const cdnURL = "https://download5.mediafire.com/aaa/bbb/file.zip"
	page := `<a id="downloadButton" href="` + cdnURL + `"></a>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c := r.Header.Get("Cookie"); !strings.Contains(c, "mf_session=test123") {
			t.Errorf("expected browser cookie mf_session=test123 in request, got %q", c)
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page)
	}))
	defer server.Close()

	r := NewHostResolver()
	r.cookieSource = func(hostname string) string {
		if hostname == "www.mediafire.com" {
			return "mf_session=test123"
		}
		return ""
	}
	r.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() == "www.mediafire.com" {
			testURL := server.URL + req.URL.Path
			newReq, err := http.NewRequest(req.Method, testURL, req.Body)
			if err != nil {
				return nil, err
			}
			newReq.Header = req.Header.Clone()
			return http.DefaultTransport.RoundTrip(newReq)
		}
		return http.DefaultTransport.RoundTrip(req)
	})

	if _, err := r.Resolve("https://www.mediafire.com/file/abc123/name.zip/file", "mediafire"); err != nil {
		t.Fatalf("Resolve mediafire with cookies failed: %v", err)
	}
}
