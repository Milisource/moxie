package downloader

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResolveBuzzheavierHTTP_HTMXRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/abc123":
			fmt.Fprint(w, `<a class="download-btn" hx-get="/abc123/download?t=signed">Download</a>`)
		case "/abc123/download":
			if r.Header.Get("HX-Request") != "true" {
				t.Errorf("missing HX-Request header")
			}
			w.Header().Set("HX-Redirect", "https://ts.bzzhr.to/d/abc123?v=token")
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	r := NewHostResolver()
	toTestServer(r, server)
	got, err := r.resolveBuzzheavierHTTP("https://bzzhr.to/abc123")
	if err != nil {
		t.Fatalf("resolveBuzzheavierHTTP: %v", err)
	}
	if got != "https://ts.bzzhr.to/d/abc123?v=token" {
		t.Errorf("unexpected URL: %s", got)
	}
}

func TestResolveBuzzheavierHTTP_NoEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html>no button here</html>`)
	}))
	defer server.Close()

	r := NewHostResolver()
	toTestServer(r, server)
	if _, err := r.resolveBuzzheavierHTTP("https://bzzhr.to/abc123"); err == nil {
		t.Fatal("expected an error when the HTMX endpoint is missing")
	}
}

func TestMixdropDownloadPageURL(t *testing.T) {
	cases := map[string]string{
		"https://mixdrop.co/e/abc123":    "https://mixdrop.co/f/abc123?download",
		"https://mixdrop.ag/f/abc123":    "https://mixdrop.ag/f/abc123?download",
		"https://mxdrop.click/e/abc123/": "https://mxdrop.click/f/abc123/?download",
	}
	for in, want := range cases {
		if got := mixdropDownloadPageURL(in); got != want {
			t.Errorf("mixdropDownloadPageURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveUploadHaven_RequiresBrowser(t *testing.T) {
	old := BrowserResolverEnabled()
	SetBrowserResolver(false)
	defer SetBrowserResolver(old)

	r := NewHostResolver()
	_, err := r.resolveUploadHavenWithFallback("https://uploadhaven.com/download/abc123")
	if err == nil || !strings.Contains(err.Error(), "--browser") {
		t.Fatalf("expected browser requirement, got %v", err)
	}
}

func TestResolveWDHO_DetectsInteractiveChallenge(t *testing.T) {
	old := BrowserResolverEnabled()
	SetBrowserResolver(false)
	defer SetBrowserResolver(old)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<form action="/kbhw" method="post"><input name="antibot" value="x"><button class="btn-color">GREEN</button></form>`)
	}))
	defer server.Close()

	r := NewHostResolver()
	toTestServer(r, server)
	_, err := r.resolveWDHO("https://wdho.ru/abc")
	if err == nil || !strings.Contains(err.Error(), "interactive") {
		t.Fatalf("expected interactive challenge error, got %v", err)
	}
}

func TestResolveWDHOHTTP_SignedToken(t *testing.T) {
	oldDelay := wdhoTokenDelay
	wdhoTokenDelay = 10 * time.Millisecond
	defer func() { wdhoTokenDelay = oldDelay }()

	direct := "https://wdho.ru/kbhw?pt=abc123"
	encoded := base64.StdEncoding.EncodeToString([]byte(direct))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "filehosting", Value: "test-cookie", Path: "/"})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<div class="download-timer"></div><script>var e='%s';</script>`, encoded)
	}))
	defer server.Close()

	r := NewHostResolver()
	toTestServer(r, server)
	res, err := r.resolveWDHOHTTP("https://wdho.ru/kbhw")
	if err != nil {
		t.Fatalf("resolveWDHOHTTP: %v", err)
	}
	if res.URL != direct {
		t.Errorf("URL = %q, want %q", res.URL, direct)
	}
	if !strings.Contains(res.Headers["Cookie"], "filehosting=test-cookie") {
		t.Errorf("missing session cookie: %q", res.Headers["Cookie"])
	}
}

func TestResolveWDHOHTTP_Challenge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<form action="/kbhw" method="post"><input name="antibot" value="x"><button class="btn-color">GREEN</button></form>`)
	}))
	defer server.Close()

	r := NewHostResolver()
	toTestServer(r, server)
	_, err := r.resolveWDHOHTTP("https://wdho.ru/kbhw")
	if err == nil || !strings.Contains(err.Error(), "colour") {
		t.Fatalf("expected colour challenge error, got %v", err)
	}
}
