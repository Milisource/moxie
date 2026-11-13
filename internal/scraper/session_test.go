package scraper

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// cfServer answers with a Cloudflare challenge unless the request carries
// cf_clearance=fresh and the matching UA.
func cfServer(t *testing.T, wantUA string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Cookie"), "cf_clearance=fresh") || r.UserAgent() != wantUA {
			w.Header().Set("cf-mitigated", "challenge")
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `<html><head><title>Just a moment...</title></head><body>Performing security verification</body></html>`)
			return
		}
		fmt.Fprint(w, "<html>ok</html>")
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Not parallel: the session hooks are package-level.

// A Cloudflare challenge calls the clearance refresher once; the retry
// carries the new cookie and the provider's (new) UA, and the circuit
// breaker stays closed.
func TestClient_ChallengeRefreshesClearance(t *testing.T) {
	const ua = "Mozilla/5.0 (X11; Linux x86_64; rv:159.0) Gecko/20100101 Firefox/159.0"
	srv := cfServer(t, ua)

	currentUA := "Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0"
	SetUserAgentProvider(func() string { return currentUA })
	var calls atomic.Int32
	SetClearanceRefresher(func(ctx context.Context) (string, error) {
		calls.Add(1)
		currentUA = ua
		return "xf_user=1; cf_clearance=fresh", nil
	})
	t.Cleanup(func() { SetUserAgentProvider(nil); SetClearanceRefresher(nil) })

	c := NewClientWithHTTP("xf_user=1; cf_clearance=stale", srv.Client())
	c.pacer.delay = 0
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/threads/1/", nil)
	body, err := c.do(req, 0)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	if body != "<html>ok</html>" || calls.Load() != 1 {
		t.Errorf("body %q after %d refreshes, want ok after 1", body, calls.Load())
	}
	if c.Blocked() {
		t.Error("circuit breaker tripped despite the successful refresh")
	}
}

// The refresher runs at most once per client: a clearance that still fails
// surfaces as a Cloudflare block instead of looping.
func TestClient_ChallengeRefreshOnlyOnce(t *testing.T) {
	srv := cfServer(t, "never-matches")
	var calls atomic.Int32
	SetClearanceRefresher(func(ctx context.Context) (string, error) {
		calls.Add(1)
		return "xf_user=1; cf_clearance=fresh", nil
	})
	t.Cleanup(func() { SetClearanceRefresher(nil) })

	c := NewClientWithHTTP("xf_user=1", srv.Client())
	c.pacer.delay = 0
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/threads/1/", nil)
		if _, err := c.do(req, 0); !IsCloudflareChallenge(err) {
			t.Fatalf("do #%d: err = %v, want a Cloudflare challenge", i+1, err)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("refresher called %d times, want 1", calls.Load())
	}
}

func TestUserAgent_FallsBackToDefault(t *testing.T) {
	SetUserAgentProvider(func() string { return "" })
	t.Cleanup(func() { SetUserAgentProvider(nil) })
	if got := UserAgent(); got != platformUserAgent() {
		t.Errorf("UserAgent = %q, want the platform default", got)
	}
}

func TestClient_ClearanceRetryHasOneFreshCookie(t *testing.T) {
	var cookies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookies = append(cookies, r.Header.Get("Cookie"))
		if len(cookies) == 1 {
			w.Header().Set("cf-mitigated", "challenge")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		count := 0
		for _, cookie := range r.Cookies() {
			if cookie.Name == "cf_clearance" {
				count++
				if cookie.Value != "fresh" {
					t.Errorf("retry clearance = %q, want fresh", cookie.Value)
				}
			}
		}
		if count != 1 {
			t.Errorf("retry has %d clearance cookies, want 1", count)
		}
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()
	SetClearanceRefresher(func(context.Context) (string, error) { return "cf_clearance=fresh", nil })
	t.Cleanup(func() { SetClearanceRefresher(nil) })
	c := NewClientWithHTTP("cf_clearance=stale", srv.Client())
	c.pacer.delay = 0
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Cookie", "custom=present")
	body, err := c.do(req, 0)
	if err != nil || body != "ok" {
		t.Fatalf("do = %q, %v", body, err)
	}
	if len(cookies) != 2 {
		t.Fatalf("got %d requests, want 2", len(cookies))
	}
	if cookies[0] != "custom=present; cf_clearance=stale" || cookies[1] != "custom=present; cf_clearance=fresh" {
		t.Errorf("cookies = %q", cookies)
	}
	if got := req.Header.Get("Cookie"); got != "custom=present" {
		t.Errorf("original cookie = %q", got)
	}
}

func TestClient_Plain503RetriesWithoutClearanceRefresh(t *testing.T) {
	var requests, refreshes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()
	SetClearanceRefresher(func(context.Context) (string, error) { refreshes.Add(1); return "cf_clearance=fresh", nil })
	t.Cleanup(func() { SetClearanceRefresher(nil) })
	c := NewClientWithHTTP("", srv.Client())
	c.pacer.delay = 0
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	body, err := c.do(req, 0)
	if err != nil || body != "ok" {
		t.Fatalf("do = %q, %v", body, err)
	}
	if requests.Load() != 2 || refreshes.Load() != 0 {
		t.Errorf("requests=%d refreshes=%d, want 2 and 0", requests.Load(), refreshes.Load())
	}
}
