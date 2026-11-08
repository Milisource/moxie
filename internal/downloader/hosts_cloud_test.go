package downloader

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Masked F95Zone links
// ---------------------------------------------------------------------------

func TestResolveMasked_OK(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("xhr") != "1" || r.Form.Get("download") != "1" {
			t.Errorf("unexpected form: %v", r.Form)
		}
		if r.Header.Get("X-Requested-With") != "XMLHttpRequest" {
			t.Error("missing X-Requested-With header")
		}
		if r.Header.Get("Cookie") != "xf_user=abc" {
			t.Errorf("cookie not forwarded: %q", r.Header.Get("Cookie"))
		}
		fmt.Fprint(w, `{"status":"ok","msg":"https://pixeldrain.com/u/abc123"}`)
	}))
	defer server.Close()

	r := NewHostResolver()
	r.SetF95Cookie("xf_user=abc")
	toTestServer(r, server)

	got, err := r.resolveMasked("https://f95zone.to/masked/pixeldrain.com/ok/1/2/3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://pixeldrain.com/u/abc123" {
		t.Errorf("got %q", got)
	}
}

func TestResolveMasked_Captcha(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"captcha","msg":"Please complete the CAPTCHA to continue"}`)
	}))
	defer server.Close()

	r := NewHostResolver()
	r.SetF95Cookie("xf_user=abc")
	toTestServer(r, server)

	_, err := r.resolveMasked("https://f95zone.to/masked/pixeldrain.com/captcha/1/2/3")
	if err == nil || !strings.Contains(err.Error(), "CAPTCHA") {
		t.Fatalf("expected CAPTCHA error, got %v", err)
	}
}
func TestResolveMasked_NoCookie(t *testing.T) {
	t.Parallel()
	r := NewHostResolver()
	_, err := r.resolveMasked("https://f95zone.to/masked/pixeldrain.com/1/2/3")
	if err == nil || !strings.Contains(err.Error(), "session cookie") {
		t.Fatalf("expected cookie error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// MediaFire
// ---------------------------------------------------------------------------

func TestResolveMediafire_DirectLink(t *testing.T) {
	t.Parallel()
	direct := "https://download1593.mediafire.com/abc/file.zip"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><body><a href="%s">Download</a></body></html>`, direct)
	}))
	defer server.Close()

	r := NewHostResolver()
	toTestServer(r, server)

	got, err := r.resolveMediafire("https://www.mediafire.com/file/key/name.zip/file")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.URL != direct {
		t.Errorf("got %q, want %q", got.URL, direct)
	}
	if got.Headers["Referer"] == "" {
		t.Error("expected Referer header")
	}
}

func TestResolveMediafire_NoLink(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body>Complete the captcha</body></html>`)
	}))
	defer server.Close()

	r := NewHostResolver()
	toTestServer(r, server)

	_, err := r.resolveMediafire("https://www.mediafire.com/file/key/name.zip/file")
	if err == nil {
		t.Fatal("expected error")
	}
}

// ---------------------------------------------------------------------------
// Bunkr
// ---------------------------------------------------------------------------

func TestResolveBunkrr_SignFlow(t *testing.T) {
	t.Parallel()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/f/slug":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<a href="https://dl.bunkr.cr/file/48827917">dl</a>`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/_001_v2":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"mediafiles":"https://psu.scdn.st","original":"game.rar","path":"/storage/media/game.rar"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/sign":
			if r.URL.Query().Get("path") != "/storage/media/game.rar" {
				t.Errorf("unexpected sign path: %q", r.URL.Query().Get("path"))
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"token":"tok123","ex":1794173976}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	r := NewHostResolver()
	r.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		host := req.URL.Hostname()
		if host == "bunkr.ws" || host == "dl.bunkr.cr" || host == "glb-apisign.cdn.cr" {
			testURL := server.URL + req.URL.Path
			if req.URL.RawQuery != "" {
				testURL += "?" + req.URL.RawQuery
			}
			newReq, err := http.NewRequest(req.Method, testURL, req.Body)
			if err != nil {
				return nil, err
			}
			newReq.Header = req.Header.Clone()
			newReq.Host = host
			return http.DefaultTransport.RoundTrip(newReq)
		}
		return http.DefaultTransport.RoundTrip(req)
	})

	got, err := r.resolveBunkrr("https://bunkr.ws/f/slug")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u, err := url.Parse(got.URL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "psu.scdn.st" || u.Path != "/storage/media/game.rar" {
		t.Errorf("unexpected base URL: %s", got.URL)
	}
	q := u.Query()
	if q.Get("token") != "tok123" || q.Get("ex") != "1794173976" || q.Get("n") != "game.rar" {
		t.Errorf("unexpected query: %s", got.URL)
	}
}

func TestResolveBunkrr_NoCDNLink(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body>Album page</body></html>`)
	}))
	defer server.Close()

	r := NewHostResolver()
	toTestServer(r, server)

	_, err := r.resolveBunkrr("https://bunkr.ws/f/slug")
	if err == nil {
		t.Fatal("expected error")
	}
}

// ---------------------------------------------------------------------------
// Gofile
// ---------------------------------------------------------------------------

func TestResolveGofile_ContentsAPI(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/accounts":
			fmt.Fprint(w, `{"status":"ok","data":{"token":"guest-token"}}`)
		case "/contents/abc":
			if r.Header.Get("Authorization") != "Bearer guest-token" {
				t.Errorf("missing bearer token: %q", r.Header.Get("Authorization"))
			}
			if r.URL.Query().Get("wt") == "" {
				t.Error("missing wt parameter")
			}
			fmt.Fprint(w, `{"status":"ok","data":{"type":"folder","children":{"f1":{"type":"file","link":"https://store1.gofile.io/download/abc/game.zip"}}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	r := NewHostResolver()
	r.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() == "api.gofile.io" {
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

	got, err := r.resolveGofile("https://gofile.io/d/abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.URL != "https://store1.gofile.io/download/abc/game.zip" {
		t.Errorf("unexpected URL: %s", got.URL)
	}
	if got.Headers["Cookie"] != "accountToken=guest-token" {
		t.Errorf("unexpected cookie: %q", got.Headers["Cookie"])
	}
}

// ---------------------------------------------------------------------------
// F95Zone /dl links
// ---------------------------------------------------------------------------

func TestResolveF95DL_RedirectsToHost(t *testing.T) {
	t.Parallel()
	target, _ := url.Parse("https://pixeldrain.com/u/abc123")
	r := NewHostResolver()
	r.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.EqualFold(req.URL.Hostname(), "f95zone.to") {
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": []string{"https://pixeldrain.com/u/abc123"}},
				Body:       http.NoBody,
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    &http.Request{URL: target},
		}, nil
	})

	got, err := r.Resolve("https://f95zone.to/dl/123/?u=1&k=2&r=3&t=4", "datanodes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.URL != "https://pixeldrain.com/api/file/abc123" {
		t.Errorf("unexpected URL: %s", got.URL)
	}
}

func TestResolveF95DL_CaptchaPage(t *testing.T) {
	t.Parallel()
	r := NewHostResolver()
	r.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader(`<title>Just a quick check | F95zone</title><div data-xf-init="h-captcha"></div>`)),
			Request:    req,
		}, nil
	})
	_, err := r.Resolve("https://f95zone.to/dl/123/?u=1&k=2&r=3&t=4", "datanodes")
	if err == nil || !strings.Contains(err.Error(), "hCaptcha") {
		t.Fatalf("expected hCaptcha error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Dropbox
// ---------------------------------------------------------------------------

func TestResolveDropbox_AddsDL1(t *testing.T) {
	t.Parallel()
	r := NewHostResolver()
	got, err := r.resolveDropbox("https://www.dropbox.com/scl/fi/abc/file.odt?rlkey=xyz&dl=0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u, _ := url.Parse(got.URL)
	if u.Query().Get("dl") != "1" {
		t.Errorf("dl=1 missing: %s", got.URL)
	}
	if u.Query().Get("rlkey") != "xyz" {
		t.Errorf("rlkey lost: %s", got.URL)
	}
}

// toTestServer rewrites requests from this resolver to the given httptest server.
func toTestServer(r *HostResolver, server *httptest.Server) {
	r.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
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
	})
}
