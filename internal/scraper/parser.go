package scraper

import (
	"encoding/json"
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

	// threadIDOnlyPattern matches the slug-agnostic form ThreadURL builds
	// (https://f95zone.to/threads/180466/). XenForo resolves by ID alone, so
	// that is the form moxie scrapes — without this fallback every scrape
	// reported ThreadID 0.
	threadIDOnlyPattern = regexp.MustCompile(`/threads/(\d+)(?:/|$|\?)`)

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
	// The h1 also carries the thread's prefix labels (engine, status,
	// category) inside span.p-title-prefix. Stripping them from the title
	// text keeps "VN Ren'Py FurrHouse" from becoming the game name, while
	// the labels themselves are kept as a separate signal.
	titleSel := doc.Find("h1.p-title-value").First()
	td.Prefixes = extractTitlePrefixes(titleSel)
	var rawTitle string
	if titleSel.Length() > 0 {
		cleaned := titleSel.Clone()
		cleaned.Find(".p-title-prefix").Remove()
		rawTitle = strings.TrimSpace(cleaned.Text())
	}
	// Strip XenForo-style bracketed tags (e.g., [v1.31], [BadColor], (Steam))
	// that duplicate metadata already captured in version/developer fields.
	td.Title = stripBracketed(rawTitle)

	// --- Status ---
	// Prefix labels are the site's own classification; check them before
	// the free-text tags and the title.
	td.Status = extractStatus(append(append([]string{}, td.Prefixes...), td.Tags...), td.Title)

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

	// Version sources, in descending order of reliability:
	//   1. the structured metadata block, maintained by the thread author;
	//   2. the bracketed tag in the thread title, the form F95Zone's
	//      posting rules mandate ("Game Name [v1.31] [Dev]");
	//   3. a regex scan of the post body — last resort, because changelogs
	//      and engine strings in the body also look like version numbers.
	td.Version = extractVersionFromMetaOnly(meta)
	if td.Version == "" {
		td.Version = extractVersionFromBrackets(rawTitle)
	}
	if td.Version == "" {
		td.Version = extractVersionFromBody(bodyText)
	}
	td.Developer = extractDeveloperFromMeta(meta, bodyText)
	td.Overview = extractOverviewText(bodyText)
	td.DownloadLinks = extractDownloadLinks(contentSel)
	td.StoreLinks = extractStoreLinks(contentSel)
	td.SaveCandidates = extractSaveCandidates(contentSel)

	// JSON-LD is a supplemental, DOM-structure-independent source — it
	// never overrides F95Zone-specific fields (title/version/tags/status),
	// only fills in fields the BBCode parsing above left empty.
	ld := extractJSONLD(doc)
	if td.CoverURL == "" {
		if img, ok := ld["image"].(string); ok && isValidCoverURL(img) {
			td.CoverURL = img
		}
	}
	if v, ok := ld["datePublished"].(string); ok {
		td.PublishedAt = v
	}
	if v, ok := ld["dateModified"].(string); ok {
		td.UpdatedAt = v
	}

	return td, nil
}

// extractJSONLD collects and merges all application/ld+json script blocks
// on the page. Multiple blocks are merged last-wins on key collision, since
// XenForo commonly emits several small JSON-LD objects (breadcrumb list,
// discussion forum posting, …) rather than one combined object. A missing
// or unparseable block is not an error — JSON-LD is optional supplemental
// data, absent on some threads, and callers must treat an empty map as
// "nothing extra available."
func extractJSONLD(doc *goquery.Document) map[string]any {
	merged := make(map[string]any)
	doc.Find(`script[type="application/ld+json"]`).Each(func(_ int, s *goquery.Selection) {
		var obj map[string]any
		if err := json.Unmarshal([]byte(s.Text()), &obj); err != nil {
			return // malformed block — skip, not fatal
		}
		for k, v := range obj {
			merged[k] = v
		}
	})
	return merged
}

// ---------------------------------------------------------------------------
// Field extractors
// ---------------------------------------------------------------------------

// extractThreadID parses the XenForo thread ID from a URL like
// "/threads/slug.12345/" or the slug-agnostic "/threads/12345/". Returns 0
// when no ID can be extracted.
func extractThreadID(url string) int64 {
	if matches := threadIDPattern.FindStringSubmatch(url); len(matches) >= 2 {
		if id, err := strconv.ParseInt(matches[1], 10, 64); err == nil {
			return id
		}
	}
	if matches := threadIDOnlyPattern.FindStringSubmatch(url); len(matches) >= 2 {
		if id, err := strconv.ParseInt(matches[1], 10, 64); err == nil {
			return id
		}
	}
	return 0
}

// extractTitlePrefixes returns the thread's prefix labels (engine, status,
// category) from h1.p-title-value span.p-title-prefix. F95Zone renders these
// as links to the forum's prefix filter, outside the title text proper.
func extractTitlePrefixes(titleSel *goquery.Selection) []string {
	if titleSel.Length() == 0 {
		return nil
	}
	labels := titleSel.Find(".p-title-prefix a")
	if labels.Length() == 0 {
		labels = titleSel.Find(".p-title-prefix .label")
	}
	seen := make(map[string]bool)
	var prefixes []string
	labels.Each(func(_ int, s *goquery.Selection) {
		label := strings.TrimSpace(s.Text())
		if label == "" || seen[label] {
			return
		}
		seen[label] = true
		prefixes = append(prefixes, label)
	})
	return prefixes
}

// stripBracketed removes bracketed tags (e.g., [v1.31], [BadColor], (Steam))
// and their leading whitespace from a string. Uses the same pattern as
// SanitizeTitle in associate.go.
func stripBracketed(s string) string {
	return strings.TrimSpace(bracketedRe.ReplaceAllString(s, ""))
}
