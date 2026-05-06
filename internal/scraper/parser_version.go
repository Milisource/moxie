package scraper

import (
	"regexp"
)

// ---------------------------------------------------------------------------
// Version regex patterns (tried in order; first match wins)
// ---------------------------------------------------------------------------

var (
	versionPattern1 = regexp.MustCompile(`(?i)Version[:\s]+v?(\d+\.\d+(?:\.\d+)*)`)
	versionPattern2 = regexp.MustCompile(`(?i)Ver\.?\s*(\d+\.\d+(?:\.\d+)*)`)
	versionPattern3 = regexp.MustCompile(`(?i)v(\d+\.\d+(?:\.\d+)*)`)

	// versionInBrackets extracts version numbers from bracketed tags
	// like [v1.31], [ver 2.0], [version 1.5.2], [Ch. 2 v3.0], [v1.0 Alpha].
	// Matches v/ver/version anywhere inside a bracket, with optional
	// trailing letter for build identifiers.
	versionInBrackets = regexp.MustCompile(`\[[^\]]*v(?:er(?:sion)?\s+)?v?(\d+\.\d+(?:\.\d+)*(?:[a-zA-Z])?)[^\]]*\]`)
	// dateInBrackets matches date-based versions like [2018-07-18]
	// (per F95Zone title format rules when no version exists).
	dateInBrackets = regexp.MustCompile(`\[(\d{4}-\d{2}-\d{2})\]`)
	// bareVerInBrackets matches bare versions like [1.0] without v prefix.
	// Safe from false positives on [Ch. 1.5] because \] must be immediate.
	bareVerInBrackets = regexp.MustCompile(`\[(\d+\.\d+(?:\.\d+)*)\]`)
	// finalInBrackets matches [Final] — complete game sentinel with no version.
	finalInBrackets = regexp.MustCompile(`(?i)\[final\]`)
)

// ---------------------------------------------------------------------------
// Field extractors
// ---------------------------------------------------------------------------

// extractVersion searches the post body text for version patterns.
// When multiple matches are found, the longest (most specific) wins.
func extractVersion(text string) string {
	if text == "" {
		return ""
	}

	var best string
	for _, re := range []*regexp.Regexp{versionPattern1, versionPattern2, versionPattern3} {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			if len(m) >= 2 && len(m[1]) > len(best) {
				best = m[1]
			}
		}
	}
	return best
}

// ---------------------------------------------------------------------------
// Structured metadata extraction (Overview block)
// ---------------------------------------------------------------------------

// extractVersionFromMeta tries to obtain the version number from the
// structured metadata block first.  If the metadata doesn't contain a
// version (or the value is empty/non-parseable) it falls back to the
// full-text regex scan.
func extractVersionFromMeta(meta map[string]string, fullText string) string {
	if v, ok := meta["version"]; ok && v != "" {
		// Prepend "Version: " so the existing regex patterns match.
		if ver := extractVersion("Version: " + v); ver != "" {
			return ver
		}
	}
	return extractVersion(fullText)
}

// extractVersionFromBrackets tries to find a version inside bracketed
// tags in the thread title.  Tries patterns in priority order:
//
//  1. [v1.31], [ver 2.0], [version 1.5.2], [Ch. 2 v3.0], [v1.0 Alpha]
//  2. [2018-07-18] — date-based (per F95Zone title format rules)
//  3. [1.0] — bare version without v prefix
//  4. [Final] — complete game sentinel
//
// Returns the version string (without v/ver prefix), or "" if none found.
func extractVersionFromBrackets(title string) string {
	// 1. Explicit v/ver/version prefix or embedded in bracket text.
	if m := versionInBrackets.FindStringSubmatch(title); len(m) > 1 {
		return m[1]
	}
	// 2. Date in brackets: [2018-07-18].
	if m := dateInBrackets.FindStringSubmatch(title); len(m) > 1 {
		return m[1]
	}
	// 3. Bare version in brackets: [1.0].  Safe from [Ch. 1.5] false
	// positives because \] must immediately follow the last digit.
	if m := bareVerInBrackets.FindStringSubmatch(title); len(m) > 1 {
		return m[1]
	}
	// 4. [Final] — complete game, no version number.
	if finalInBrackets.MatchString(title) {
		return "Final"
	}
	return ""
}
