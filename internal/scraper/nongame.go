package scraper

import (
	"strings"
	"unicode"
)

// nonGameThreadPrefixes lists title prefixes that indicate a thread is NOT a
// game release.  These are request threads, recommendation threads,
// identification threads, etc.  Matching against them prevents false
// associations with non-game content.
var nonGameThreadPrefixes = []string{
	"seeking",
	"request",
	"req",
	"recommendation",
	"recommending",
	"identify",
	"identifying",
	"identification",
	"discussion",
	"question",
	"help",
	"tutorial",
	"guide",
	"looking for",
	"searching for",
	"translation request",
	"translation",
}

// IsNonGameThread returns true when the title begins with a word or phrase
// that indicates this is a request/help/discussion thread rather than an
// actual game release.
func IsNonGameThread(title string) bool {
	lower := strings.ToLower(collapseWhitespace(title))
	for _, prefix := range nonGameThreadPrefixes {
		if strings.HasPrefix(lower, prefix+" ") || lower == prefix {
			return true
		}
	}
	return false
}

// collapseWhitespace normalizes all Unicode whitespace characters (including
// non-breaking spaces, en/em spaces, etc.) to plain ASCII spaces, then
// collapses consecutive spaces into one and trims leading/trailing whitespace.
func collapseWhitespace(s string) string {
	var b strings.Builder
	lastSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
		} else {
			b.WriteRune(r)
			lastSpace = false
		}
	}
	return strings.TrimSpace(b.String())
}
