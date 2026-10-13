package scraper

import (
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var (
	// attachmentPathRe matches XenForo attachment URLs of the form
	// /attachments/<slug>.<attachment-id>/ (trailing slash optional). The
	// slug captures the human filename so it can be recovered from the URL.
	// Query strings and fragments are ignored: attachment links are often
	// decorated with tracking parameters.
	attachmentPathRe = regexp.MustCompile(`^/attachments/([^/]+)\.(\d+)/?$`)

	// saveSignalRe matches words that mark a link as a save file rather than a
	// regular game download. Applied to the anchor text and to the link URL's
	// last path segment (unescaped). "100%" is matched without a trailing word
	// boundary so a bare "100%" (not followed by a word character) counts.
	saveSignalRe = regexp.MustCompile(`(?i)(\bsaves?\b|100\s?%|full save|gallery)`)
)

// attachmentHosts are the F95Zone hosts that serve attachment downloads.
// Links on f95zone.to redirect to attachments.f95zone.to with signed params.
var attachmentHosts = map[string]bool{
	"f95zone.to":             true,
	"attachments.f95zone.to": true,
}

// saveMediaExts lists attachment extensions that are inline images or media.
// They can never be saves and must not be offered as candidates.
var saveMediaExts = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".gif":  true,
	".webp": true,
	".bmp":  true,
	".svg":  true,
	".ico":  true,
	".mp4":  true,
	".webm": true,
	".mp3":  true,
	".ogg":  true,
}

// saveContextMaxRunes caps the surrounding post text captured for display.
const saveContextMaxRunes = 300

// extractSaveCandidates scans the first post's content for downloadable save
// files: F95Zone attachment links and external file-host links whose text or
// filename signals a save (e.g. "100% save", "full save"). Candidates are
// deduplicated by URL, first occurrence wins.
func extractSaveCandidates(content *goquery.Selection) []SaveCandidate {
	if content.Length() == 0 {
		return nil
	}

	var candidates []SaveCandidate
	seen := make(map[string]bool)

	content.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href, ok := a.Attr("href")
		if !ok || href == "" {
			return
		}
		if seen[href] {
			return
		}
		text := strings.TrimSpace(a.Text())
		if isOnlineOnlyLink(text, href) {
			return
		}

		u, err := url.Parse(href)
		if err != nil {
			return
		}

		// --- F95Zone attachments ---
		if filename, ok := attachmentFilename(u); ok {
			if isMediaFilename(filename) {
				return
			}
			seen[href] = true
			candidates = append(candidates, SaveCandidate{
				URL:      href,
				Kind:     SaveCandidateAttachment,
				Host:     "f95zone",
				Filename: filename,
				Context:  saveCandidateContext(a),
			})
			return
		}

		// --- External file hosts ---
		// Classify from the href alone: the link text is attacker-controlled
		// and may claim a trusted host while pointing elsewhere. The text and
		// URL segment may still signal that the *content* is a save.
		host := identifyHost(href, "")
		if host == "other" {
			return
		}
		seg := linkFilename(u)
		if !saveSignalRe.MatchString(text) && !saveSignalRe.MatchString(seg) {
			return
		}
		filename := seg
		if isMaskedRedirectURL(u) {
			// Masked redirect paths (f95zone.to/masked/<host>/...) are opaque
			// tokens, not filenames; the link text is the only human label.
			filename = text
		}
		if filename == "" {
			filename = text
		}
		seen[href] = true
		candidates = append(candidates, SaveCandidate{
			URL:      href,
			Kind:     SaveCandidateExternal,
			Host:     host,
			Filename: filename,
			Context:  saveCandidateContext(a),
		})
	})

	return candidates
}

// isMaskedRedirectURL reports whether u is an F95Zone masked redirect link
// (f95zone.to/masked/<host>/...), whose trailing path segments are opaque
// encryption tokens rather than filenames.
func isMaskedRedirectURL(u *url.URL) bool {
	if u == nil || !attachmentHosts[strings.ToLower(u.Hostname())] {
		return false
	}
	return strings.HasPrefix(u.EscapedPath(), "/masked/")
}

// attachmentFilename returns the human filename of an F95Zone attachment URL,
// or ("", false) when the URL is not an attachment link. XenForo attachment
// URLs look like https://f95zone.to/attachments/<name>.<id>/ — the name is the
// original filename with the attachment ID appended as a pseudo-extension.
func attachmentFilename(u *url.URL) (string, bool) {
	if u == nil || !attachmentHosts[strings.ToLower(u.Hostname())] {
		return "", false
	}
	m := attachmentPathRe.FindStringSubmatch(u.EscapedPath())
	if m == nil {
		return "", false
	}
	name := m[1]
	if decoded, err := url.PathUnescape(name); err == nil {
		name = decoded
	}
	if name == "" {
		return "", false
	}
	return name, true
}

// isMediaFilename reports whether a filename carries an inline image or media
// extension (never a save file).
func isMediaFilename(name string) bool {
	return saveMediaExts[strings.ToLower(path.Ext(name))]
}

// linkFilename returns the unescaped last path segment of a link URL, or "".
func linkFilename(u *url.URL) string {
	escaped := u.EscapedPath()
	seg := escaped[strings.LastIndex(escaped, "/")+1:]
	if seg == "" {
		return ""
	}
	if decoded, err := url.PathUnescape(seg); err == nil {
		return decoded
	}
	return seg
}

// saveCandidateContext captures the surrounding post text for display. It
// prefers the closest list item or XenForo code block, falling back to the
// parent element and finally to the anchor text. Whitespace is collapsed and
// the result is capped at saveContextMaxRunes runes.
func saveCandidateContext(a *goquery.Selection) string {
	sel := a.Closest("li, div.bbCodeBlock")
	if sel.Length() == 0 {
		sel = a.Parent()
	}
	var text string
	if sel.Length() > 0 {
		text = sel.Text()
	}
	if strings.TrimSpace(text) == "" {
		text = a.Text()
	}
	return truncateRunes(strings.Join(strings.Fields(text), " "), saveContextMaxRunes)
}

// truncateRunes caps s at max runes, trimming any trailing whitespace left by
// the cut. It is rune-safe so multi-byte characters are never split.
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimRight(string(r[:max]), " ")
}
