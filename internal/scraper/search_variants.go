package scraper

import (
	"context"
	"regexp"
	"strings"
	"unicode"
)

// ---------------------------------------------------------------------------
// Search query variants (association recall)
// ---------------------------------------------------------------------------
//
// The latest-updates search is a Redis full-text index that matches poorly
// on real-world directory names: "SiNiSistar2" doesn't match "SiNiSistar 2",
// "CyanBrain" doesn't match "Cyan Brain", and "Monster Black Market w DLC w
// Uncen" drowns the search in noise. When the primary query returns nothing,
// SearchTitleFirstHit walks progressively simplified variants so games that
// exist on F95Zone still get associated. Every variant result is still
// scored against the local title (ComputeMatchScore >= 0.3) and engine
// checked, so recall improves without new false associations.

// searchNoiseTokenRe matches trailing tokens that carry no search value:
// platform/edition markers, version-like tokens, and standalone noise words
// ("w DLC w Uncen"). Trailing-only matching keeps legit titles ("Final
// Fantasy", "Star Wars") safe — the token must be the LAST word of the
// query.
var searchNoiseTokenRe = regexp.MustCompile(`(?i)^(eng|english|uncen|uncensored|dlc|w|win|windows|mac|macos|linux|android|web|ver|final|demo|update|part\d*|v?\d+([._-]\d+)*[a-z]*|\d{4}([._-]\d{2}){1,2}|\d{6,8})$`)

// SearchQueryVariants returns ordered search-query fallbacks for a title
// that already went through SanitizeTitle. The caller tries the primary
// query first and only walks these when it returns no results:
//
//  1. trailing noise tokens dropped ("Monster Black Market w DLC w Uncen" →
//     "Monster Black Market", "Island SAGA v5" → "Island SAGA", "Sana 0824"
//     → "Sana", "Luna Kurokami's Revenge Eng" → "Luna Kurokami's Revenge")
//  2. trailing digits stripped from the last token ("SiNiSistar2" →
//     "SiNiSistar")
//  3. camel-case split ("CyanBrain" → "Cyan Brain", "SelobusArena" →
//     "Selobus Arena")
//  4. progressive word drops from the end (down to 2 words)
//  5. progressive word drops from the front (down to 2 words) — catches
//     titles whose first word is generic noise ("FreeEmberDoors" → "Ember
//     Doors")
//
// Variants are deduplicated, re-sanitized, and capped so a single game
// can never burn more than a handful of searches.
func SearchQueryVariants(title string) []string {
	base := SanitizeSearchQuery(title)
	if base == "" {
		return nil
	}

	var out []string
	add := func(q string) {
		q = SanitizeSearchQuery(q)
		if q == "" || q == base {
			return
		}
		for _, existing := range out {
			if existing == q {
				return
			}
		}
		out = append(out, q)
	}

	// 1. Drop trailing noise tokens repeatedly (each drop exposes the next
	//    candidate: "... w DLC w Uncen" → "... w DLC w" → "... w DLC").
	if v := dropTrailingNoise(base); v != base {
		add(v)
	}
	// 2. Strip trailing digits from the last token.
	if v := dropTrailingDigits(base); v != base {
		add(v)
	}
	// 3. Camel-case split.
	if v := camelSplitWords(base); v != base {
		add(v)
	}
	// 4/5. Progressive word drops from the end and front — applied to both
	//     the original query and its camel-split form, so "FreeEmberDoors"
	//     can reach "Ember Doors" via "Free Ember Doors".
	for _, src := range []string{base, camelSplitWords(base)} {
		words := strings.Fields(src)
		for len(words) > 2 {
			words = words[:len(words)-1]
			add(strings.Join(words, " "))
		}
		words = strings.Fields(src)
		for len(words) > 2 {
			words = words[1:]
			add(strings.Join(words, " "))
		}
	}

	if len(out) > 6 {
		out = out[:6]
	}
	return out
}

// dropTrailingNoise removes trailing noise tokens until the last token is
// meaningful (or only one word remains).
func dropTrailingNoise(q string) string {
	words := strings.Fields(q)
	for len(words) > 1 && searchNoiseTokenRe.MatchString(words[len(words)-1]) {
		words = words[:len(words)-1]
	}
	return strings.Join(words, " ")
}

// dropTrailingDigits removes trailing digits from the last token of a query
// ("SiNiSistar2" → "SiNiSistar"), so camel-run versioned names match the
// spaced form in thread titles.
func dropTrailingDigits(q string) string {
	words := strings.Fields(q)
	if len(words) == 0 {
		return q
	}
	last := words[len(words)-1]
	stripped := strings.TrimRight(last, "0123456789")
	if stripped == "" || stripped == last {
		return q
	}
	words[len(words)-1] = stripped
	return strings.Join(words, " ")
}

// camelSplitWords inserts a space at lowercase→uppercase boundaries so
// camelCase directory names ("CyanBrain") tokenize like their thread titles
// ("Cyan Brain"). All-caps tokens ("RPGM", "M.U.G.E.N", "HTML5") are
// untouched — the split only fires on a lowercase letter directly followed
// by an uppercase one.
func camelSplitWords(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 4)
	prevLower := false
	for _, r := range s {
		if prevLower && unicode.IsUpper(r) {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
		prevLower = unicode.IsLower(r)
	}
	return b.String()
}

// SearchTitleFirstHit searches for a title and, when the primary query
// returns no results, walks SearchQueryVariants until one does. Errors are
// returned as-is (a block must abort the run, not trigger more requests);
// only empty result sets advance to the next variant. Returns nil results
// when every variant came up empty — callers fall back to the cookie-based
// search exactly as they do today.
func (p *PublicAPI) SearchTitleFirstHit(ctx context.Context, query string) ([]LatestSearchResult, error) {
	results, err := p.SearchTitle(ctx, query)
	if err != nil || len(results) > 0 {
		return results, err
	}
	for _, variant := range SearchQueryVariants(query) {
		results, err = p.SearchTitle(ctx, variant)
		if err != nil {
			return nil, err
		}
		if len(results) > 0 {
			return results, nil
		}
	}
	return nil, nil
}
