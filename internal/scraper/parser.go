package scraper

import (
	"fmt"
	urlpkg "net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// ---------------------------------------------------------------------------
// Version regex patterns (tried in order; first match wins)
// ---------------------------------------------------------------------------

// knownMetaKeys is the set of recognised metadata field names (normalised
// to lowercase with underscores).  Only lines whose key appears in this
// set are extracted from the header block.
var knownMetaKeys = map[string]bool{
	"thread_updated": true,
	"release_date":   true,
	"developer":      true,
	"publisher":      true,
	"censored":       true,
	"version":        true,
	"os":             true,
	"language":       true,
	"other_games":    true,
	"store":          true,
	"genre":          true,
}

var (
	versionPattern1 = regexp.MustCompile(`(?i)Version[:\s]+v?(\d+\.\d+(?:\.\d+)*)`)
	versionPattern2 = regexp.MustCompile(`(?i)Ver\.?\s*(\d+\.\d+(?:\.\d+)*)`)
	versionPattern3 = regexp.MustCompile(`(?i)v(\d+\.\d+(?:\.\d+)*)`)

	developerPattern1 = regexp.MustCompile(`(?i)^Developer[:\s]+(.+)`)
	developerPattern2 = regexp.MustCompile(`(?i)Publisher[:\s]+(.+)`)

	threadIDPattern = regexp.MustCompile(`/threads/[^/]*\.(\d+)/`)

	// bracketedRe strips XenForo-style bracket tags from titles.
	// Matches [tag], (tag) and their leading whitespace.
	bracketedRe = regexp.MustCompile(`\s*[\[\(][^\]\)]*[\]\)]`)

	// versionInBrackets extracts version numbers from bracketed tags
	// like [v1.31], [ver 2.0], [version 1.5.2].
	versionInBrackets = regexp.MustCompile(`\[v(?:er(?:sion)?\s+)?v?(\d+\.\d+(?:\.\d+)*)\]`)

	steamStoreRe = regexp.MustCompile(`store\.steampowered\.com/app/\d+`)
)

// ---------------------------------------------------------------------------
// Main parse entry point
// ---------------------------------------------------------------------------

// parseThreadHTML parses a XenForo 2.x thread HTML page and returns structured
// metadata. threadURL is used exclusively for extracting the thread ID.
func parseThreadHTML(html string, threadURL string) (*ThreadData, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("scraper: failed to parse HTML: %w", err)
	}

	// --- First-post body selection (cached for multiple extractors) ---
	contentSel := doc.Find("article.message-content .bbWrapper").First()
	if contentSel.Length() == 0 {
		contentSel = doc.Find(".bbWrapper").First()
	}

	td := &ThreadData{
		ThreadID: extractThreadID(threadURL),
		Tags:     extractTags(doc),
		CoverURL: extractCoverImage(contentSel),
	}

	// --- Title ---
	var rawTitle string
	if sel := doc.Find("h1.p-title-value").First(); sel.Length() > 0 {
		rawTitle = strings.TrimSpace(sel.Text())
	}
	// Strip XenForo-style bracketed tags (e.g., [v1.31], [BadColor], (Steam))
	// that duplicate metadata already captured in version/developer fields.
	td.Title = stripBracketed(rawTitle)

	// --- Status ---
	td.Status = extractStatus(td.Tags, td.Title)

	// --- First-post body text ---
	bodyText := strings.TrimSpace(contentSel.Text())

	// Extract structured metadata from the header block (between Overview
	// and the next section).  Fall back to full-text regex when the
	// structured block is absent or incomplete.
	meta := extractMetadata(bodyText)

	td.Version = extractVersionFromMeta(meta, bodyText)
	// Fallback: if version wasn't found in the post body, try bracketed
	// tags in the raw thread title (e.g., "Game [v1.31]").
	if td.Version == "" {
		td.Version = extractVersionFromBrackets(rawTitle)
	}
	td.Developer = extractDeveloperFromMeta(meta, bodyText)
	td.Overview = bodyText
	td.DownloadLinks = extractDownloadLinks(contentSel)
	td.StoreLinks = extractStoreLinks(contentSel)

	return td, nil
}

// ---------------------------------------------------------------------------
// Field extractors
// ---------------------------------------------------------------------------

// extractThreadID parses the XenForo thread ID from a URL like
// "/threads/slug.12345/". Returns 0 when no ID can be extracted.
func extractThreadID(url string) int64 {
	matches := threadIDPattern.FindStringSubmatch(url)
	if len(matches) >= 2 {
		id, err := strconv.ParseInt(matches[1], 10, 64)
		if err == nil {
			return id
		}
	}
	return 0
}

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
	lower := strings.ToLower(bodyHTML)

	// Locate the "Overview" heading.
	overviewStart := strings.Index(lower, "overview")
	if overviewStart == -1 {
		return meta
	}
	// Back up to the start of the line that contains "overview".
	for overviewStart > 0 && lower[overviewStart-1] != '\n' {
		overviewStart--
	}

	// Find the end of the Overview heading line.
	rest := lower[overviewStart:]
	nl := strings.IndexByte(rest, '\n')
	if nl == -1 {
		return meta // no content after the heading
	}
	contentStart := overviewStart + nl + 1

	// Locate the next section header after the Overview line.
	sectionEndpoints := []string{
		"\ninstallation:", "\nchangelog:", "\ndownload:", "\ndeveloper notes:",
		"\ninstallation\n", "\nchangelog\n", "\ndownload\n", "\ndeveloper notes\n",
	}
	contentAfter := lower[contentStart:]
	endIdx := len(bodyHTML)
	for _, ep := range sectionEndpoints {
		if idx := strings.Index(contentAfter, ep); idx != -1 {
			if pos := contentStart + idx; pos < endIdx {
				endIdx = pos
			}
		}
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

// extractDownloadLinks finds all download links in the first post's content.
func extractDownloadLinks(content *goquery.Selection) []DownloadLink {
	if content.Length() == 0 {
		return nil
	}

	var links []DownloadLink
	content.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href, ok := a.Attr("href")
		if !ok || href == "" {
			return
		}
		text := strings.TrimSpace(a.Text())
		host := identifyHost(href, text)
		if host == "other" {
			// Only include "other" links if they have obvious host keywords.
			// Otherwise skip generic links.
			return
		}
		links = append(links, DownloadLink{
			URL:  href,
			Host: host,
			Name: text,
		})
	})
	return links
}

// storeLinkMatchers maps store key names to match functions that validate
// whether a URL is a genuine game page (not a curator, help article, etc.).
var storeLinkMatchers = []struct {
	key   string
	match func(url string) bool
}{
	{"steam", func(url string) bool {
		return steamStoreRe.MatchString(url)
	}},
	{"itch", func(url string) bool {
		lower := strings.ToLower(url)
		if !strings.Contains(lower, "itch.io") {
			return false
		}
		u, err := urlpkg.Parse(url)
		if err != nil {
			return false
		}
		return len(strings.Trim(u.Path, "/")) > 0
	}},
	{"dlsite", func(url string) bool {
		lower := strings.ToLower(url)
		if !strings.Contains(lower, "dlsite.com") {
			return false
		}
		if strings.Contains(lower, "/hc/") {
			return false
		}
		if strings.Contains(lower, "/home/") {
			return false
		}
		if strings.Contains(lower, "/help/") {
			return false
		}
		return true
	}},
}

// extractStoreLinks scans the first post body for links to known game stores.
// Returns a map of store key → URL (e.g., "steam" → "https://store.steampowered.com/app/12345/").
func extractStoreLinks(content *goquery.Selection) map[string]string {
	links := make(map[string]string)

	if content.Length() == 0 {
		return links
	}

	content.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href, ok := a.Attr("href")
		if !ok || href == "" {
			return
		}

		for _, sm := range storeLinkMatchers {
			if sm.match(href) {
				if _, exists := links[sm.key]; !exists {
					links[sm.key] = href
				}
				return
			}
		}
	})

	return links
}

// identifyHost returns a short host label from the F95Zone approved hosts list.
// See: https://f95zone.to/threads/approved-file-hosts-updated-2025-02-26.3432/
func identifyHost(url, text string) string {
	lower := strings.ToLower(url + " " + text)
	switch {
	// --- Alphabetical by canonical host name ---
	case strings.Contains(lower, "vern.cc"):
		return "vern"
	case strings.Contains(lower, "1cloudfile") || strings.Contains(lower, "1cloud"):
		return "1cloudfile"
	case strings.Contains(lower, "akirabox"):
		return "akirabox"
	case strings.Contains(lower, "anontransfer"):
		return "anontransfer"
	case strings.Contains(lower, "anonymfile"):
		return "anonymfile"
	case strings.Contains(lower, "apkadmin"):
		return "apkadmin"
	case strings.Contains(lower, "bowfile"):
		return "bowfile"
	case strings.Contains(lower, "bunkrr") || strings.Contains(lower, "bunkr"):
		return "bunkrr"
	case strings.Contains(lower, "buzzheavier"):
		return "buzzheavier"
	case strings.Contains(lower, "catbox"):
		return "catbox"
	case strings.Contains(lower, "cyberfile"):
		return "cyberfile"
	case strings.Contains(lower, "datanodes"):
		return "datanodes"
	case strings.Contains(lower, "delafil"):
		return "delafil"
	case strings.Contains(lower, "download.gg"):
		return "downloadgg"
	case strings.Contains(lower, "dropmefiles"):
		return "dropmefiles"
	case strings.Contains(lower, "easyupload"):
		return "easyupload"
	case strings.Contains(lower, "filemail"):
		return "filemail"
	case strings.Contains(lower, "files.dp.ua") || strings.Contains(lower, "dp.ua"):
		return "filesdpua"
	case strings.Contains(lower, "files.fm") || strings.Contains(lower, "filesfm"):
		return "filesfm"
	case strings.Contains(lower, "fromsmash") || strings.Contains(lower, "from.smash"):
		return "fromsmash"
	case strings.Contains(lower, "gofile"):
		return "gofile"
	case strings.Contains(lower, "drive.google") || strings.Contains(lower, "google.com/drive"):
		return "googledrive"
	case strings.Contains(lower, "hexload") || strings.Contains(lower, "hexupload"):
		return "hexload"
	case strings.Contains(lower, "krakenfiles"):
		return "krakenfiles"
	case strings.Contains(lower, "mediafire"):
		return "mediafire"
	case strings.Contains(lower, "mega.nz") || strings.Contains(lower, "mega.co"):
		return "mega"
	case strings.Contains(lower, "mixdrop"):
		return "mixdrop"
	case strings.Contains(lower, "pixeldrain"):
		return "pixeldrain"
	case strings.Contains(lower, "proton") && strings.Contains(lower, "drive"):
		return "protondrive"
	case strings.Contains(lower, "qu.ax"):
		return "quax"
	case strings.Contains(lower, "sendgb"):
		return "sendgb"
	case strings.Contains(lower, "terminal"):
		return "terminal"
	case strings.Contains(lower, "transfer.sh"):
		return "transfersh"
	case strings.Contains(lower, "transfert"):
		return "transfert"
	case strings.Contains(lower, "uploadhaven"):
		return "uploadhaven"
	case strings.Contains(lower, "uploadnow"):
		return "uploadnow"
	case strings.Contains(lower, "vikingfile"):
		return "vikingfile"
	case strings.Contains(lower, "wdho"):
		return "wdho"
	case strings.Contains(lower, "wetransfer"):
		return "wetransfer"
	case strings.Contains(lower, "workupload"):
		return "workupload"
	case strings.Contains(lower, "yourfilestore"):
		return "yourfilestore"

	// --- Legacy/extra hosts ---
	case strings.Contains(lower, "keep2share") || strings.Contains(lower, "k2s"):
		return "keep2share"
	case strings.Contains(lower, "uploaded") || strings.Contains(lower, "ul.to"):
		return "uploaded"
	case strings.Contains(lower, "dropbox"):
		return "dropbox"

	default:
		return "other"
	}
}

// ---------------------------------------------------------------------------
// Status extraction
// ---------------------------------------------------------------------------

// statusWords maps known status labels (from tags or title prefixes) to
// their canonical database values.
var statusWords = map[string]string{
	"completed":  "completed",
	"abandoned":  "abandoned",
	"onhold":     "on_hold",
	"on hold":    "on_hold",
	"suspended":  "on_hold",
	"discontinued": "abandoned",
	"cancelled":  "abandoned",
	"dropped":    "abandoned",
}

// stripBracketed removes bracketed tags (e.g., [v1.31], [BadColor], (Steam))
// and their leading whitespace from a string. Uses the same pattern as
// SanitizeTitle in associate.go.
func stripBracketed(s string) string {
	return strings.TrimSpace(bracketedRe.ReplaceAllString(s, ""))
}

// extractVersionFromBrackets tries to find a version inside bracketed
// tags in the thread title (e.g., [v1.31], [ver 2.0], [v0.5.2]).
// Returns the version string without the v/ver prefix, or "" if none found.
func extractVersionFromBrackets(title string) string {
	matches := versionInBrackets.FindStringSubmatch(title)
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
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
