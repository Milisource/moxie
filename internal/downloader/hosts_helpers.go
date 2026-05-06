package downloader

import (
	"net/url"
	"regexp"
	"strings"
)

// extractFormAction extracts the form action attribute from an HTML response.
// Returns empty string if no form with action found.
func extractFormAction(html string) string {
	re := regexp.MustCompile(`<form[^>]*\saction=["']([^"']+)["']`)
	if m := re.FindStringSubmatch(html); len(m) >= 2 {
		action := m[1]
		if action != "" && action != "#" {
			return action
		}
	}
	return ""
}

// resolveRelativeURL resolves a possibly-relative URL against a base URL.
func resolveRelativeURL(base, rel string) string {
	if strings.HasPrefix(rel, "http://") || strings.HasPrefix(rel, "https://") {
		return rel
	}
	if strings.HasPrefix(rel, "//") {
		// Protocol-relative URL — derive scheme from base
		u, err := url.Parse(base)
		if err != nil {
			return rel
		}
		return u.Scheme + ":" + rel
	}
	u, err := url.Parse(base)
	if err != nil {
		return rel
	}
	resolved, err := u.Parse(rel)
	if err != nil {
		return rel
	}
	return resolved.String()
}

// extractDownloadLink scans HTML for download URLs. Checks in priority order:
//  1. Cloudflare R2 direct CDN URLs (used by VikingFile's direct-link response)
//  2. The id="download-link" element's href (used by VikingFile and similar hosts)
//  3. Anchor tags with an explicit download attribute
//  4. Anchor tags with href containing common archive file extensions
//  5. File URLs matching /d/<hash>/<filename> pattern (VikingFile download link)
func extractDownloadLink(html string) string {
	// Pattern 1: Cloudflare R2 direct CDN URLs (highest priority — most reliable)
	re := regexp.MustCompile(`https://[a-zA-Z0-9.-]+\.r2\.cloudflarestorage\.com/[^"'\s<>]+`)
	if m := re.FindString(html); m != "" {
		return m
	}

	// Pattern 2: Element with id="download-link" and an href attribute
	re = regexp.MustCompile(`<a[^>]*\sid=["']download-link["'][^>]*\shref=["']([^"']+)["']`)
	if m := re.FindStringSubmatch(html); len(m) >= 2 {
		return m[1]
	}

	// Pattern 3: Anchor with explicit download attribute (order: download then href)
	re = regexp.MustCompile(`<a[^>]*\sdownload[=>'"\s][^>]*\shref=["']([^"']+)["']`)
	if m := re.FindStringSubmatch(html); len(m) >= 2 {
		return m[1]
	}
	// Reversed attribute order: href then download
	re = regexp.MustCompile(`<a[^>]*\shref=["']([^"']+)["'][^>]*\sdownload[=>'"\s]`)
	if m := re.FindStringSubmatch(html); len(m) >= 2 {
		return m[1]
	}

	// Pattern 4: Anchor with href containing common archive file extensions
	re = regexp.MustCompile(`<a[^>]*\shref=["']([^"']*\.(?:zip|rar|7z|tar\.gz|tar\.xz|tar|gz|xz|bz2|mp4|mkv|avi|mp3|flac))["'][^>]*>`)
	if m := re.FindStringSubmatch(html); len(m) >= 2 {
		return m[1]
	}

	// Pattern 5: VikingFile /d/<hash>/<filename> download link pattern
	re = regexp.MustCompile(`https?://[^"'\s<>]+/d/[a-zA-Z0-9]+/[^"'\s<>]+\.(?:zip|rar|7z|tar\.gz|tar\.xz|tar|gz|xz|bz2|mp4|mkv|avi|mp3|flac|exe|pdf|dll)`)
	if m := re.FindString(html); m != "" {
		return m
	}

	return ""
}
