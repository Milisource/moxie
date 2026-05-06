package scraper

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

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
	threadIDPattern = regexp.MustCompile(`/threads/[^/]*\.(\d+)/`)

	// bracketedRe strips XenForo-style bracket tags from titles.
	// Matches [tag], (tag) and their leading whitespace.
	bracketedRe = regexp.MustCompile(`\s*[\[\(][^\]\)]*[\]\)]`)
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

	// Strip <script> elements from the content selection before extracting
	// text. F95Zone (XenForo 2) stores image markup inside
	// <script type="text/template"> tags for its lightbox/gallery system,
	// and goquery's .Text() includes the raw text content of script elements.
	contentSel.Find("script").Remove()

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
	td.Overview = extractOverviewText(bodyText)
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

// stripBracketed removes bracketed tags (e.g., [v1.31], [BadColor], (Steam))
// and their leading whitespace from a string. Uses the same pattern as
// SanitizeTitle in associate.go.
func stripBracketed(s string) string {
	return strings.TrimSpace(bracketedRe.ReplaceAllString(s, ""))
}

