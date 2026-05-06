package scraper

import (
	urlpkg "net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var (
	steamStoreRe = regexp.MustCompile(`store\.steampowered\.com/app/\d+`)
)

// isOnlineOnlyLink returns true if the link text or URL indicates a
// browser-playable version rather than a downloadable file. These should
// not be included in download link lists.
func isOnlineOnlyLink(text, url string) bool {
	lower := strings.ToLower(text + " " + url)
	// "Online" category links point to web/browser versions, not downloads
	if strings.Contains(lower, "online") {
		return true
	}
	// Skip known non-download platforms
	if strings.Contains(lower, "gamejolt") {
		return true
	}
	return false
}

// extractDownloadLinks finds all download links in the first post's content.
// It skips store links, online-only links, and links to unrecognised hosts.
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
		// Skip online-only / browser-playable links
		if isOnlineOnlyLink(text, href) {
			return
		}
		host := identifyHost(href, text)
		if host == "other" {
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
