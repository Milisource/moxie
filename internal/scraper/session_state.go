package scraper

import (
	"errors"
	"regexp"
	"strings"
)

// SessionState classifies the authentication state of a rendered F95Zone
// thread page based on XenForo's data-logged-in attribute on the <html> tag.
type SessionState int

const (
	// SessionUnknown means the attribute was missing or unparseable (e.g.
	// a non-XenForo response or a JSON endpoint).
	SessionUnknown SessionState = iota
	// SessionLoggedIn means the page was rendered for an authenticated user.
	SessionLoggedIn
	// SessionGuest means the page was rendered for a guest.
	SessionGuest
)

// pageSession inspects the raw HTML body and returns the session state
// implied by the <html data-logged-in="..."> attribute. It does not parse
// the body with goquery (callers already hold a parsed document); a regex
// over the leading tag is sufficient and avoids a second parse.
func pageSession(body string) SessionState {
	// Limit the search to the opening <html ...> tag to avoid matching the
	// attribute anywhere else in the document.
	re := regexp.MustCompile(`(?is)<html\b[^>]*?\bdata-logged-in\s*=\s*["']?(true|false)["']?`)
	m := re.FindStringSubmatch(body)
	if len(m) < 2 {
		return SessionUnknown
	}
	if strings.EqualFold(m[1], "true") {
		return SessionLoggedIn
	}
	return SessionGuest
}

// SessionError is returned when a cookie-authenticated client receives a
// guest-rendered page, i.e. the F95 session has expired.
type SessionError struct {
	HadCookie bool
}

func (e *SessionError) Error() string {
	if e != nil && e.HadCookie {
		return "scraper: F95Zone session expired (imported cookies no longer valid)"
	}
	return "scraper: F95Zone session expired"
}

// ErrSessionExpired is the sentinel callers compare against with errors.Is.
var ErrSessionExpired = errors.New("scraper: F95Zone session expired")

func (e *SessionError) Is(target error) bool {
	return target == ErrSessionExpired
}

// ErrSessionExpiredError is an alias kept for compatibility with callers
// that expect a constructor-style name.
func NewSessionError(hadCookie bool) error {
	return &SessionError{HadCookie: hadCookie}
}