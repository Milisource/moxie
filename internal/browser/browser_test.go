package browser

import (
	"testing"
	"time"

	"github.com/browserutils/kooky"
)

// cookie is a test helper that creates a kooky.Cookie with the given name/value.
// It avoids struct-literal embedding syntax for http.Cookie, which some Go
// versions' vet tool flags as "invalid field name http.Cookie in struct literal".
func cookie(name, value string) *kooky.Cookie {
	c := &kooky.Cookie{}
	c.Name = name
	c.Value = value
	return c
}

// domainCookie is cookie() with a domain set.
func domainCookie(name, value, domain string) *kooky.Cookie {
	c := cookie(name, value)
	c.Domain = domain
	return c
}

// ---------------------------------------------------------------------------
// filterF95Cookies
// ---------------------------------------------------------------------------

// Regression: kooky's Domain filter is exact-match and drops domain-scoped
// cookies stored with a leading dot; the local filter must accept them.
func TestFilterF95Cookies_DotDomain(t *testing.T) {
	t.Parallel()
	cookies := []*kooky.Cookie{
		domainCookie("xf_session", "sess", ".f95zone.to"),
		domainCookie("xf_user", "u1", "f95zone.to"),
		domainCookie("cf_clearance", "cf", "www.f95zone.to"),
		domainCookie("other", "x", "example.com"),
		domainCookie("lookalike", "x", "notf95zone.to"),
		nil,
	}
	got := filterF95Cookies(cookies)
	if len(got) != 3 {
		t.Fatalf("filterF95Cookies kept %d cookies, want 3 (got: %v)", len(got), got)
	}
	// All kept cookies must be f95zone-owned (both bare and dot-prefixed).
	for _, c := range got {
		if !f95ZoneDomain(c.Domain) {
			t.Errorf("kept cookie with domain %q", c.Domain)
		}
	}
}

func TestFilterF95Cookies_Empty(t *testing.T) {
	t.Parallel()
	if got := filterF95Cookies(nil); len(got) != 0 {
		t.Errorf("filterF95Cookies(nil) = %v, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// dedupCookies
// ---------------------------------------------------------------------------

func TestDedupCookies_KeepsNewest(t *testing.T) {
	t.Parallel()
	old := cookie("xf_session", "old")
	old.Creation = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mid := cookie("xf_session", "mid")
	mid.Creation = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	new := cookie("xf_session", "new")
	new.Creation = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	got := dedupCookies([]*kooky.Cookie{old, new, mid})
	if len(got) != 1 {
		t.Fatalf("dedupCookies = %v, want 1 cookie", got)
	}
	if got[0].Value != "new" {
		t.Errorf("dedupCookies kept value %q, want newest %q", got[0].Value, "new")
	}
}

func TestDedupCookies_FallsBackToExpires(t *testing.T) {
	t.Parallel()
	// Both Creation times unset — newer Expires wins as a proxy.
	soon := cookie("xf_user", "soon")
	soon.Expires = time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	later := cookie("xf_user", "later")
	later.Expires = time.Date(2027, 12, 31, 0, 0, 0, 0, time.UTC)

	got := dedupCookies([]*kooky.Cookie{later, soon})
	if len(got) != 1 || got[0].Value != "later" {
		t.Errorf("dedupCookies = %v, want single cookie with value %q", got, "later")
	}
}

func TestDedupCookies_KeepsDistinctNames(t *testing.T) {
	t.Parallel()
	cookies := []*kooky.Cookie{
		cookie("xf_session", "a"),
		cookie("xf_user", "b"),
		cookie("cf_clearance", "c"),
	}
	got := dedupCookies(cookies)
	if len(got) != 3 {
		t.Errorf("dedupCookies = %v, want all 3 distinct names kept", got)
	}
}

func TestDedupCookies_FirstKeptWhenTimesEqual(t *testing.T) {
	t.Parallel()
	// Identical timestamps (all zero) — the first occurrence wins.
	a := cookie("xf_session", "first")
	b := cookie("xf_session", "second")
	got := dedupCookies([]*kooky.Cookie{a, b})
	if len(got) != 1 || got[0].Value != "first" {
		t.Errorf("dedupCookies = %v, want first occurrence kept", got)
	}
}

// ---------------------------------------------------------------------------
// sanitizeHeaderValue
// ---------------------------------------------------------------------------

func TestSanitizeHeaderValue_StripsControlChars(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain value", "abc123", "abc123"},
		{"carriage return", "abc\r123", "abc123"},
		{"newline", "abc\n123", "abc123"},
		{"null byte", "abc\x00123", "abc123"},
		{"mixed control chars", "a\rb\nc\x00d", "abcd"},
		{"only control chars", "\r\n\x00", ""},
		{"empty string", "", ""},
		{"unicode preserved", "héllo", "héllo"},
		{"spaces preserved", "hello world", "hello world"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeHeaderValue(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeHeaderValue(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// buildCookieHeader
// ---------------------------------------------------------------------------

func TestBuildCookieHeader_SingleCookie(t *testing.T) {
	t.Parallel()
	cookies := []*kooky.Cookie{
		cookie("xf_session", "abc123"),
	}
	got := buildCookieHeader(cookies)
	want := "xf_session=abc123"
	if got != want {
		t.Errorf("buildCookieHeader = %q, want %q", got, want)
	}
}

func TestBuildCookieHeader_MultipleCookies(t *testing.T) {
	t.Parallel()
	cookies := []*kooky.Cookie{
		cookie("xf_user", "ghi789"),
		cookie("cf_clearance", "def456"),
		cookie("xf_session", "abc123"),
	}
	// buildCookieHeader does NOT sort — cookies are joined as-is.
	// The caller (GetF95Cookies) sorts before calling this.
	got := buildCookieHeader(cookies)
	want := "xf_user=ghi789; cf_clearance=def456; xf_session=abc123"
	if got != want {
		t.Errorf("buildCookieHeader = %q, want %q", got, want)
	}
}

func TestBuildCookieHeader_SanitizesValues(t *testing.T) {
	t.Parallel()
	cookies := []*kooky.Cookie{
		cookie("session", "abc\r\n\x00123"),
		cookie("token", "clean"),
	}
	got := buildCookieHeader(cookies)
	want := "session=abc123; token=clean"
	if got != want {
		t.Errorf("buildCookieHeader = %q, want %q", got, want)
	}
}

func TestBuildCookieHeader_SkipsEmptyValues(t *testing.T) {
	t.Parallel()
	cookies := []*kooky.Cookie{
		cookie("valid", "data"),
		cookie("empty", ""),
		cookie("control_only", "\r\n\x00"),
	}
	got := buildCookieHeader(cookies)
	want := "valid=data"
	if got != want {
		t.Errorf("buildCookieHeader = %q, want %q", got, want)
	}
}

func TestBuildCookieHeader_EmptyCookies(t *testing.T) {
	t.Parallel()
	got := buildCookieHeader(nil)
	if got != "" {
		t.Errorf("buildCookieHeader(nil) = %q, want empty", got)
	}
	got = buildCookieHeader([]*kooky.Cookie{})
	if got != "" {
		t.Errorf("buildCookieHeader(empty) = %q, want empty", got)
	}
}
