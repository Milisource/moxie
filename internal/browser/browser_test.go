package browser

import (
	"testing"

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
