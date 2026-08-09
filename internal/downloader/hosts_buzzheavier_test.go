package downloader

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Fixture-style share-page fragments with the hx-get download attributes the
// real bzzhr.to page embeds (captured live 2026-08-09): the t= token appears
// in the plain, &amp;-encoded alt=true, and preview variants.
const buzzSharePage = `<html><body>
<button hx-get="/e2yt4zd66jq3/download?t=MTc4NjI5NDI3MDEzNw._aPxCC0ZueavdPmrhmZngHcAyBAk494J2qZzADTd-r0">Download</button>
<button hx-get="/e2yt4zd66jq3/download?t=MTc4NjI5NDI3MDEzNw._aPxCC0ZueavdPmrhmZngHcAyBAk494J2qZzADTd-r0&amp;alt=true">Download (alt)</button>
<button hx-get="/e2yt4zd66jq3/preview?t=MTc4NjI5NDI3MDEzNw._aPxCC0ZueavdPmrhmZngHcAyBAk494J2qZzADTd-r0">Preview</button>
</body></html>`

const buzzToken = "MTc4NjI5NDI3MDEzNw._aPxCC0ZueavdPmrhmZngHcAyBAk494J2qZzADTd-r0"

// TestExtractBuzzDownloadHref covers token extraction from page fixtures:
// plain and &amp;-encoded alt=true hx-get variants, single quotes, missing
// tokens, and pages without any download attribute.
func TestExtractBuzzDownloadHref(t *testing.T) {
	tests := []struct {
		name     string
		page     string
		wantTok  string
		wantPath string
		wantErr  bool
	}{
		{
			name:     "live fixture page",
			page:     buzzSharePage,
			wantTok:  buzzToken,
			wantPath: "/e2yt4zd66jq3/download",
		},
		{
			name: "single-quoted attributes",
			page: `<button hx-get='/abc123/download?t=TOK1&amp;alt=true'>x</button>`,
			wantTok:  "TOK1",
			wantPath: "/abc123/download",
		},
		{
			name: "alt=true variant only",
			page: `<div hx-get="/xyz/download?t=TOK2&amp;alt=true"></div>`,
			wantTok:  "TOK2",
			wantPath: "/xyz/download",
		},
		{
			name: "absolute hx-get href",
			page: `<div hx-get="https://bzzhr.to/abc/download?t=TOK3"></div>`,
			wantTok:  "TOK3",
			wantPath: "/abc/download",
		},
		{
			name:    "preview-only page (no download attribute)",
			page:    `<div hx-get="/abc/preview?t=TOK4"></div>`,
			wantErr: true,
		},
		{
			name:    "no hx-get at all",
			page:    `<html><body>challenge</body></html>`,
			wantErr: true,
		},
		{
			name:    "download attribute without t=",
			page:    `<div hx-get="/abc/download?alt=true"></div>`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tok, endpoint, err := extractBuzzDownloadHref([]byte(tt.page))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got token %q endpoint %q", tok, endpoint)
				}
				return
			}
			if err != nil {
				t.Fatalf("extractBuzzDownloadHref: %v", err)
			}
			if tok != tt.wantTok {
				t.Errorf("token = %q, want %q", tok, tt.wantTok)
			}
			if !strings.HasPrefix(endpoint, tt.wantPath+"?") {
				t.Errorf("endpoint = %q, want prefix %q", endpoint, tt.wantPath+"?")
			}
			// The endpoint is always rebuilt with alt=true, whatever the
			// page's own variant was.
			if !strings.Contains(endpoint, "alt=true") {
				t.Errorf("endpoint %q missing alt=true", endpoint)
			}
			if !strings.Contains(endpoint, "t=") {
				t.Errorf("endpoint %q missing t=", endpoint)
			}
			// &amp; must have been unescaped — no double-encoded params.
			if strings.Contains(endpoint, "&amp;") {
				t.Errorf("endpoint %q still contains HTML-escaped &amp;", endpoint)
			}
		})
	}
}

// newBuzzResolver builds a resolver wired to srv with a fixed browser cookie.
func newBuzzResolver(srv *httptest.Server) *HostResolver {
	r := NewHostResolver()
	r.client = srv.Client()
	r.cookieSource = func(string) string { return "cf_clearance=abc123; __cf_bm=xyz" }
	return r
}

// TestResolveBuzzheavier_TokenFlow exercises the full live-verified chain:
// page (cookie) → t= token → /download?t=&alt=true (HX headers + cookie)
// → hx-redirect fafda.to → probe 206. Asserts the alt=true preference and
// that both page and HTMX requests carry the browser cookie.
func TestResolveBuzzheavier_TokenFlow(t *testing.T) {
	var pageCookie, dlCookie, dlQuery, dlHXReq, dlPriority, dlReferer string
	var pageHits, dlHits, probeHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/e2yt4zd66jq3":
			pageHits++
			pageCookie = r.Header.Get("Cookie")
			fmt.Fprint(w, buzzSharePage)
		case r.URL.Path == "/e2yt4zd66jq3/download":
			dlHits++
			dlCookie = r.Header.Get("Cookie")
			dlQuery = r.URL.RawQuery
			dlHXReq = r.Header.Get("HX-Request")
			dlPriority = r.Header.Get("priority")
			dlReferer = r.Header.Get("Referer")
			w.Header().Set("hx-redirect", "https://fafda.to/d/e2yt4zd66jq3?v="+buzzToken)
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/d/e2yt4zd66jq3":
			probeHits++
			if r.Header.Get("Range") != "bytes=0-0" {
				t.Errorf("probe Range = %q, want bytes=0-0", r.Header.Get("Range"))
			}
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte("x"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	r := newBuzzResolver(srv)
	r.client.Transport = rewriteToServer("fafda.to", srv.URL)
	res, err := r.Resolve(srv.URL+"/e2yt4zd66jq3", "buzzheavier")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	wantURL := "https://fafda.to/d/e2yt4zd66jq3?v=" + buzzToken
	if res.URL != wantURL {
		t.Errorf("URL = %q, want %q", res.URL, wantURL)
	}
	if pageHits != 1 || dlHits != 1 || probeHits != 1 {
		t.Errorf("hits: page=%d download=%d probe=%d, want 1/1/1", pageHits, dlHits, probeHits)
	}
	if !strings.Contains(pageCookie, "cf_clearance=abc123") {
		t.Errorf("page request missing browser cookie, got %q", pageCookie)
	}
	if !strings.Contains(dlCookie, "cf_clearance=abc123") {
		t.Errorf("download request missing browser cookie, got %q", dlCookie)
	}
	if dlHXReq != "true" || dlPriority != "u=1, i" || dlReferer != srv.URL+"/e2yt4zd66jq3" {
		t.Errorf("download request headers: HX-Request=%q priority=%q Referer=%q", dlHXReq, dlPriority, dlReferer)
	}
	if !strings.Contains(dlQuery, "alt=true") || !strings.Contains(dlQuery, "t="+buzzToken) {
		t.Errorf("download query = %q, want alt=true and t=<token>", dlQuery)
	}
	if strings.Contains(dlQuery, "&amp;") {
		t.Errorf("download query %q still HTML-escaped", dlQuery)
	}
}

// TestResolveBuzzheavier_RetryThrough403 verifies the share page is retried
// through intermittent adaptive 403s (backoff) before succeeding.
func TestResolveBuzzheavier_RetryThrough403(t *testing.T) {
	oldTries, oldDelay := buzzMaxTries, buzzRetryDelay
	buzzMaxTries, buzzRetryDelay = 5, 10*time.Millisecond
	defer func() { buzzMaxTries, buzzRetryDelay = oldTries, oldDelay }()

	var pageHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/e2yt4zd66jq3":
			if atomic.AddInt32(&pageHits, 1) < 3 {
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, "Just a moment...")
				return
			}
			fmt.Fprint(w, buzzSharePage)
		case "/e2yt4zd66jq3/download":
			w.Header().Set("hx-redirect", "https://fafda.to/d/e2yt4zd66jq3?v="+buzzToken)
			w.WriteHeader(http.StatusNoContent)
		case "/d/e2yt4zd66jq3":
			w.WriteHeader(http.StatusPartialContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	r := newBuzzResolver(srv)
	r.client.Transport = rewriteToServer("fafda.to", srv.URL)
	res, err := r.Resolve(srv.URL+"/e2yt4zd66jq3", "buzzheavier")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := atomic.LoadInt32(&pageHits); got != 3 {
		t.Errorf("page fetched %d times, want 3 (2 challenged + 1 success)", got)
	}
	if !strings.Contains(res.URL, "fafda.to") {
		t.Errorf("URL = %q, want fafda.to", res.URL)
	}
}

// TestResolveBuzzheavier_PersistentChallenge verifies resolution fails after
// the retry cap when the page is stuck behind the adaptive challenge.
func TestResolveBuzzheavier_PersistentChallenge(t *testing.T) {
	oldTries, oldDelay := buzzMaxTries, buzzRetryDelay
	buzzMaxTries, buzzRetryDelay = 3, 5*time.Millisecond
	defer func() { buzzMaxTries, buzzRetryDelay = oldTries, oldDelay }()

	var pageHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&pageHits, 1)
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "Just a moment...")
	}))
	defer srv.Close()

	r := newBuzzResolver(srv)
	_, err := r.Resolve(srv.URL+"/e2yt4zd66jq3", "buzzheavier")
	if err == nil {
		t.Fatal("expected error for persistently challenged page")
	}
	if !strings.Contains(err.Error(), "challenged") {
		t.Errorf("error = %v, want challenge mention", err)
	}
	if got := atomic.LoadInt32(&pageHits); got != 3 {
		t.Errorf("page fetched %d times, want 3 (retry cap)", got)
	}
}

// TestResolveBuzzheavier_PrefersFafdaTo verifies the ts.bzzhr.to redirect
// (dead origin) is rewritten to the challenge-free fafda.to host and that
// the probe then hits it.
func TestResolveBuzzheavier_PrefersFafdaTo(t *testing.T) {
	var probeHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/e2yt4zd66jq3":
			fmt.Fprint(w, buzzSharePage)
		case "/e2yt4zd66jq3/download":
			// The non-alt backend redirect — must be rewritten to fafda.to.
			w.Header().Set("hx-redirect", "https://ts.bzzhr.to/d/e2yt4zd66jq3?v="+buzzToken)
			w.WriteHeader(http.StatusNoContent)
		case "/d/e2yt4zd66jq3":
			atomic.AddInt32(&probeHits, 1)
			w.WriteHeader(http.StatusPartialContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	r := newBuzzResolver(srv)
	r.client.Transport = rewriteToServer("fafda.to", srv.URL)

	res, err := r.Resolve(srv.URL+"/e2yt4zd66jq3", "buzzheavier")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !strings.HasPrefix(res.URL, "https://fafda.to/d/") {
		t.Errorf("URL = %q, want fafda.to origin", res.URL)
	}
	if !strings.Contains(res.URL, "v="+buzzToken) {
		t.Errorf("URL %q lost the v= token", res.URL)
	}
	if got := atomic.LoadInt32(&probeHits); got != 1 {
		t.Errorf("probe hit %d times, want 1", got)
	}
}

// TestResolveBuzzheavier_TokenRejectedRefetches verifies the 403/404 JSON
// error mapping: a rejected token re-fetches the share page for a fresh one
// (bounded by buzzMaxPageRefetch) before the probe can succeed.
func TestResolveBuzzheavier_TokenRejectedRefetches(t *testing.T) {
	var pageHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/e2yt4zd66jq3":
			// First page load issues token T1, the refetch issues T2.
			if atomic.AddInt32(&pageHits, 1) == 1 {
				fmt.Fprintf(w, `<div hx-get="/e2yt4zd66jq3/download?t=STALE_TOKEN"></div>`)
			} else {
				fmt.Fprintf(w, `<div hx-get="/e2yt4zd66jq3/download?t=FRESH_TOKEN"></div>`)
			}
		case "/e2yt4zd66jq3/download":
			tok := r.URL.Query().Get("t")
			if tok == "STALE_TOKEN" {
				w.Header().Set("hx-redirect", "https://fafda.to/d/e2yt4zd66jq3?v=STALE_TOKEN")
			} else {
				w.Header().Set("hx-redirect", "https://fafda.to/d/e2yt4zd66jq3?v=FRESH_TOKEN")
			}
			w.WriteHeader(http.StatusNoContent)
		case "/d/e2yt4zd66jq3":
			if r.URL.Query().Get("v") == "STALE_TOKEN" {
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, `{"error":"forbidden"}`)
				return
			}
			w.WriteHeader(http.StatusPartialContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	r := newBuzzResolver(srv)
	r.client.Transport = rewriteToServer("fafda.to", srv.URL)
	res, err := r.Resolve(srv.URL+"/e2yt4zd66jq3", "buzzheavier")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !strings.Contains(res.URL, "v=FRESH_TOKEN") {
		t.Errorf("URL = %q, want the fresh token", res.URL)
	}
	if got := atomic.LoadInt32(&pageHits); got != 2 {
		t.Errorf("page fetched %d times, want 2 (initial + refetch)", got)
	}
}

// TestResolveBuzzheavier_TokenRejectedExhaustsRefetch verifies a share whose
// token is rejected on every refetch fails cleanly instead of looping.
func TestResolveBuzzheavier_TokenRejectedExhaustsRefetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/e2yt4zd66jq3":
			fmt.Fprintf(w, `<div hx-get="/e2yt4zd66jq3/download?t=BAD_TOKEN"></div>`)
		case "/e2yt4zd66jq3/download":
			w.Header().Set("hx-redirect", "https://fafda.to/d/e2yt4zd66jq3?v=BAD_TOKEN")
			w.WriteHeader(http.StatusNoContent)
		case "/d/e2yt4zd66jq3":
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error":"not found"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	r := newBuzzResolver(srv)
	_, err := r.Resolve(srv.URL+"/e2yt4zd66jq3", "buzzheavier")
	if err == nil {
		t.Fatal("expected error after exhausting token refetches")
	}
	if !strings.Contains(err.Error(), "token rejected after") {
		t.Errorf("error = %v, want refetch-exhausted message", err)
	}
}

// TestResolveBuzzheavier_Origin503Retries verifies the file probe treats
// 503 {"error":"service unavailable"} as a transient origin outage: it
// retries with backoff and succeeds once the origin recovers; a persistent
// 503 fails after the retry cap.
func TestResolveBuzzheavier_Origin503Retries(t *testing.T) {
	oldTries, oldDelay, oldBackoff := buzzMaxTries, buzzRetryDelay, buzzProbeBackoff
	buzzMaxTries, buzzRetryDelay, buzzProbeBackoff = 5, 5*time.Millisecond, []time.Duration{5 * time.Millisecond, 5 * time.Millisecond}
	defer func() { buzzMaxTries, buzzRetryDelay, buzzProbeBackoff = oldTries, oldDelay, oldBackoff }()

	var probeHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/e2yt4zd66jq3":
			fmt.Fprint(w, buzzSharePage)
		case "/e2yt4zd66jq3/download":
			w.Header().Set("hx-redirect", "https://fafda.to/d/e2yt4zd66jq3?v="+buzzToken)
			w.WriteHeader(http.StatusNoContent)
		case "/d/e2yt4zd66jq3":
			if atomic.AddInt32(&probeHits, 1) < 3 {
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprint(w, `{"error":"service unavailable"}`)
				return
			}
			w.WriteHeader(http.StatusPartialContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	r := newBuzzResolver(srv)
	r.client.Transport = rewriteToServer("fafda.to", srv.URL)
	res, err := r.Resolve(srv.URL+"/e2yt4zd66jq3", "buzzheavier")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !strings.Contains(res.URL, "fafda.to") {
		t.Errorf("URL = %q, want fafda.to", res.URL)
	}
	if got := atomic.LoadInt32(&probeHits); got != 3 {
		t.Errorf("probe hit %d times, want 3 (2x 503 + success)", got)
	}
}

// TestResolveBuzzheavier_PersistentOriginDown verifies a persistently 503
// origin fails after the retry cap with a descriptive error.
func TestResolveBuzzheavier_PersistentOriginDown(t *testing.T) {
	oldTries, oldDelay, oldBackoff := buzzMaxTries, buzzRetryDelay, buzzProbeBackoff
	buzzMaxTries, buzzRetryDelay, buzzProbeBackoff = 5, 5*time.Millisecond, []time.Duration{5 * time.Millisecond, 5 * time.Millisecond}
	defer func() { buzzMaxTries, buzzRetryDelay, buzzProbeBackoff = oldTries, oldDelay, oldBackoff }()

	var probeHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/e2yt4zd66jq3":
			fmt.Fprint(w, buzzSharePage)
		case "/e2yt4zd66jq3/download":
			w.Header().Set("hx-redirect", "https://fafda.to/d/e2yt4zd66jq3?v="+buzzToken)
			w.WriteHeader(http.StatusNoContent)
		case "/d/e2yt4zd66jq3":
			atomic.AddInt32(&probeHits, 1)
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{"error":"service unavailable"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	r := newBuzzResolver(srv)
	r.client.Transport = rewriteToServer("fafda.to", srv.URL)
	_, err := r.Resolve(srv.URL+"/e2yt4zd66jq3", "buzzheavier")
	if err == nil {
		t.Fatal("expected error for persistently unavailable origin")
	}
	if !strings.Contains(err.Error(), "service unavailable") {
		t.Errorf("error = %v, want origin-unavailable message", err)
	}
	if got := atomic.LoadInt32(&probeHits); got != 3 {
		t.Errorf("probe hit %d times, want 3 (retry cap)", got)
	}
}

// TestResolveBuzzheavier_NoTokenInPage verifies a 200 page without the
// download hx-get attribute fails with a descriptive error (e.g. a folder
// share or a page format change).
func TestResolveBuzzheavier_NoTokenInPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><div hx-get="/e2yt4zd66jq3/preview?t=X">preview only</div></html>`)
	}))
	defer srv.Close()

	r := newBuzzResolver(srv)
	_, err := r.Resolve(srv.URL+"/e2yt4zd66jq3", "buzzheavier")
	if err == nil {
		t.Fatal("expected error for page without download token")
	}
	if !strings.Contains(err.Error(), "no t= token") {
		t.Errorf("error = %v, want missing-token message", err)
	}
}
