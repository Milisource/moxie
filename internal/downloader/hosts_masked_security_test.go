package downloader

// Security regression tests for the masked-URL host gate: a URL on a foreign
// host that merely contains "/masked/" must never trigger F95Zone masked
// unwrapping and must never receive the session cookie.
//
// The followRedirect cases are NOT parallel: they replace
// http.DefaultTransport (global state), matching the convention in
// hosts_test.go.

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestIsF95Hostname(t *testing.T) {
	t.Parallel()
	cases := []struct {
		host string
		want bool
	}{
		{"f95zone.to", true},
		{"attachments.f95zone.to", true},
		{"F95ZONE.TO", true},
		{"Attachments.F95Zone.To", true},
		{"evil.example", false},
		{"f95zone.to.evil.example", false},
		{"notf95zone.to", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isF95Hostname(tc.host); got != tc.want {
			t.Errorf("isF95Hostname(%q) = %v, want %v", tc.host, got, tc.want)
		}
	}
}

func TestIsMaskedF95URL(t *testing.T) {
	t.Parallel()
	if isMaskedF95URL(nil) {
		t.Fatal("isMaskedF95URL(nil) = true, want false")
	}
	cases := []struct {
		name   string
		rawURL string
		want   bool
	}{
		{"f95 masked", "https://f95zone.to/masked/mega.nz/file=abc", true},
		{"f95 masked trailing slash", "https://f95zone.to/masked/", true},
		{"f95 masked bare path", "https://f95zone.to/masked", false},
		{"attachments host", "https://attachments.f95zone.to/masked/x", true},
		{"f95 non-masked path", "https://f95zone.to/threads/foo.123/", false},
		{"foreign masked", "https://evil.example/masked/x", false},
		{"foreign substring", "https://evil.example/path/masked/x", false},
		{"foreign host that merely looks f95", "https://f95zone.to.evil.example/masked/x", false},
	}
	for _, tc := range cases {
		u, err := url.Parse(tc.rawURL)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.rawURL, err)
		}
		if got := isMaskedF95URL(u); got != tc.want {
			t.Errorf("%s: isMaskedF95URL(%q) = %v, want %v", tc.name, tc.rawURL, got, tc.want)
		}
	}
}

func TestFollowRedirect_CookieIsHostScoped(t *testing.T) {
	// NOT parallel — replaces http.DefaultTransport which is global state.
	var gotCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotCookie = req.Header.Get("Cookie")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r := &HostResolver{f95Cookie: "xf_user=topsecret; xf_session=abc"}

	t.Run("f95 host receives the cookie", func(t *testing.T) {
		orig := http.DefaultTransport
		http.DefaultTransport = rewriteToServer("f95zone.to", srv.URL)
		defer func() { http.DefaultTransport = orig }()

		gotCookie = ""
		if _, err := r.followRedirect("https://f95zone.to/masked/x"); err != nil {
			t.Fatalf("followRedirect: %v", err)
		}
		if !strings.Contains(gotCookie, "xf_user=topsecret") {
			t.Fatalf("f95 host did not receive the session cookie, got %q", gotCookie)
		}
	})

	t.Run("foreign host never receives the cookie", func(t *testing.T) {
		orig := http.DefaultTransport
		http.DefaultTransport = rewriteToServer("evil.example", srv.URL)
		defer func() { http.DefaultTransport = orig }()

		gotCookie = ""
		if _, err := r.followRedirect("https://evil.example/masked/x"); err != nil {
			t.Fatalf("followRedirect: %v", err)
		}
		if gotCookie != "" {
			t.Fatalf("foreign host received the session cookie: %q", gotCookie)
		}
	})
}

func TestResolveDepth_ForeignMaskedURLIsNotUnwrapped(t *testing.T) {
	t.Parallel()
	consulted := false
	r := &HostResolver{
		resolvedCache: func(string) (string, bool) { consulted = true; return "", false },
	}
	res, err := r.Resolve("https://evil.example/masked/x", "other")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if consulted {
		t.Fatal("foreign /masked/ URL reached the masked-unwrap path")
	}
	if res.URL != "https://evil.example/masked/x" {
		t.Fatalf("URL = %q, want the URL passed through unchanged", res.URL)
	}
}
