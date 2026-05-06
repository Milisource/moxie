package scraper

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var (
	developerPattern1 = regexp.MustCompile(`(?i)^Developer[:\s]+(.+)`)
	developerPattern2 = regexp.MustCompile(`(?i)Publisher[:\s]+(.+)`)

	// overviewHeadingRe matches an "Overview" line (case-insensitive, may be
	// indented, optionally followed by a colon).
	overviewHeadingRe = regexp.MustCompile(`(?mi)^\s*overview[:]?\s*$`)

	// sectionHeaderRe matches known section headings that mark the end of the
	// overview description block: Installation, Changelog, Download, Developer
	// Notes (case-insensitive, may be indented, optionally followed by a colon).
	sectionHeaderRe = regexp.MustCompile(`(?mi)^\s*(installation|changelog|download|developer notes)[:\s]`)
)

// ---------------------------------------------------------------------------
// Field extractors
// ---------------------------------------------------------------------------

// extractDeveloper searches the post body text for developer/publisher
// declarations. Returns the first match found.
func extractDeveloper(text string) string {
	if text == "" {
		return ""
	}

	for _, re := range []*regexp.Regexp{developerPattern1, developerPattern2} {
		matches := re.FindStringSubmatch(text)
		if len(matches) >= 2 {
			return strings.TrimSpace(matches[1])
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Structured metadata extraction (Overview block)
// ---------------------------------------------------------------------------

// extractOverviewText extracts the game description text between the
// "Overview" heading and the next section header (Installation, Changelog,
// Download, or Developer Notes).  Returns the full bodyText if no Overview
// heading is found, so callers still get a reasonable fallback.
func extractOverviewText(bodyText string) string {
	// Find the Overview heading line using the line-anchored regex.
	loc := overviewHeadingRe.FindStringIndex(bodyText)
	if loc == nil {
		return bodyText // fallback to full text
	}
	contentStart := loc[1] // content starts after the heading line

	// If the heading is the very last thing, there's no description.
	if contentStart >= len(bodyText) {
		return ""
	}

	// Find the next section header after the Overview block.
	endIdx := len(bodyText)
	if m := sectionHeaderRe.FindStringIndex(bodyText[contentStart:]); m != nil {
		endIdx = contentStart + m[0]
	}

	block := strings.TrimSpace(bodyText[contentStart:endIdx])
	if block == "" {
		return bodyText // empty block — fallback to full text
	}
	return block
}

// extractMetadata parses the structured metadata block between the Overview
// section and the next section header (Installation, Changelog, Download, or
// Developer Notes).  Lines that match known metadata keys are extracted as
// key-value pairs.  Keys are normalised to lowercase with underscores.
//
// For non-version values, trailing content after the first " - " separator
// is stripped (e.g. "BadColor - Steam - Itch.io" → "BadColor").  Version
// values are left intact so the caller can apply the version regex.
func extractMetadata(bodyHTML string) map[string]string {
	meta := make(map[string]string)

	// Find the Overview heading line using the line-anchored regex.
	loc := overviewHeadingRe.FindStringIndex(bodyHTML)
	if loc == nil {
		return meta
	}
	contentStart := loc[1]

	// If the heading is the very last thing, there's no content.
	if contentStart >= len(bodyHTML) {
		return meta
	}

	// Find the next section header after the Overview block.
	endIdx := len(bodyHTML)
	if m := sectionHeaderRe.FindStringIndex(bodyHTML[contentStart:]); m != nil {
		endIdx = contentStart + m[0]
	}

	// The raw-cased block between Overview and the next section.
	block := bodyHTML[contentStart:endIdx]

	// Parse each line for key: value pairs.
	lines := strings.Split(block, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		colonIdx := strings.Index(line, ":")
		if colonIdx == -1 {
			continue
		}

		key := strings.TrimSpace(line[:colonIdx])
		value := strings.TrimSpace(line[colonIdx+1:])

		// Normalise the key.
		normalised := strings.ToLower(key)
		normalised = strings.ReplaceAll(normalised, " ", "_")

		if !knownMetaKeys[normalised] {
			continue
		}

		// Strip trailing " - …" content, except for the version field.
		if normalised != "version" {
			if dashIdx := strings.Index(value, " - "); dashIdx != -1 {
				value = strings.TrimSpace(value[:dashIdx])
			}
		}

		meta[normalised] = value
	}

	return meta
}

// extractDeveloperFromMeta tries to obtain the developer name from the
// structured metadata block first.  Falls back to the full-text regex
// scan when the metadata block doesn't contain a developer field.
func extractDeveloperFromMeta(meta map[string]string, fullText string) string {
	if d, ok := meta["developer"]; ok && d != "" {
		return d
	}
	return extractDeveloper(fullText)
}

// extractTags collects all tag labels from the page.
func extractTags(doc *goquery.Document) []string {
	var tags []string
	doc.Find("a.tagItem").Each(func(_ int, s *goquery.Selection) {
		tag := strings.TrimSpace(s.Text())
		if tag != "" {
			tags = append(tags, tag)
		}
	})
	return tags
}

// extractCoverImage returns the first usable image URL found in the first post's
// content area. It prefers images with class "bbImage", then falls back to
// any <img> with a non-empty src in the bbWrapper.  data: URIs and SVGs are
// skipped — we need real downloadable images for cover art.
func extractCoverImage(content *goquery.Selection) string {
	// content is already the scoped bbWrapper selection from parseThreadHTML.
	if content.Length() == 0 {
		return ""
	}

	// Prefer images with the XenForo "bbImage" class (skipping data: URIs / SVGs).
	imgs := content.Find("img.bbImage")
	for i := 0; i < imgs.Length(); i++ {
		img := imgs.Eq(i)
		src, ok := img.Attr("src")
		if ok && isValidCoverURL(src) {
			return src
		}
	}

	// Fall back to the first <img> with a meaningful src.
	var found string
	content.Find("img").EachWithBreak(func(_ int, img *goquery.Selection) bool {
		src, ok := img.Attr("src")
		if !ok || src == "" {
			return true
		}
		if !isValidCoverURL(src) {
			return true // keep looking
		}
		// Skip small icons / smilies.
		if w, exists := img.Attr("width"); exists && w != "" {
			if pw, err := strconv.Atoi(w); err == nil && pw < 100 {
				return true
			}
		}
		found = src
		return false
	})
	return found
}

// isValidCoverURL returns true when a src URL can be downloaded and used as
// cover art (real HTTP URL, not a data: URI or SVG placeholder).
func isValidCoverURL(src string) bool {
	if strings.HasPrefix(src, "data:") {
		return false
	}
	if strings.Contains(src, ".svg") || strings.Contains(src, "svg+xml") {
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Status extraction
// ---------------------------------------------------------------------------

// statusWords maps known status labels (from tags or title prefixes) to
// their canonical database values.
var statusWords = map[string]string{
	"completed":    "completed",
	"abandoned":    "abandoned",
	"onhold":       "on_hold",
	"on hold":      "on_hold",
	"suspended":    "on_hold",
	"discontinued": "abandoned",
	"cancelled":    "abandoned",
	"dropped":      "abandoned",
}

// extractStatus determines the game status from thread tags and title prefix.
// Checks tags first (higher signal), then falls back to title prefix words.
// Returns empty string if no status is detected.
func extractStatus(tags []string, title string) string {
	// Check tags first — these are user/maintainer assigned.
	for _, t := range tags {
		lower := strings.ToLower(strings.TrimSpace(t))
		if status, ok := statusWords[lower]; ok {
			return status
		}
	}

	// Check title prefix for status words (e.g. "[Completed] Game Name").
	words := strings.Fields(strings.ToLower(title))
	for _, w := range words {
		w = strings.Trim(w, "[]()•-")
		if status, ok := statusWords[w]; ok {
			return status
		}
	}

	return ""
}
