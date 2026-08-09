package scraper

import (
	urlpkg "net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

var (
	steamStoreRe = regexp.MustCompile(`store\.steampowered\.com/app/\d+`)
	// platformLabelRe matches thread row labels like "<b>Win</b>:".
	platformLabelRe = regexp.MustCompile(`(?i)^\s*(win|windows|mac|macos|linux|android|web)\s*:?\s*$`)
	// sectionHeadingRe matches short bold headings that name a download
	// section ("Part 2", "Update 26", "v1.0", "DOWNLOAD", …). Anchors need a
	// guard like this so random bold words in prose are never mistaken for
	// section headings.
	sectionHeadingRe = regexp.MustCompile(`(?i)\b(part|update|patch|hotfix|dlc|demo|beta|alpha|final|chapter|ch\.|version|ver|v\s?\d|download|win|windows|mac|macos|linux|android|web)\b`)
)

// platformLabels canonicalizes platform row labels.
var platformLabels = map[string]string{
	"win": "Win", "windows": "Win",
	"mac": "Mac", "macos": "Mac",
	"linux":   "Linux",
	"android": "Android",
	"web":     "Web",
}

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
			Name: downloadLinkName(a, text, host),
		})
	})
	return links
}

// downloadLinkName builds a display name for a download link. Threads like
// "Henteria Chronicles" list the same host once per part/platform row
// ("<b>Part 2</b> … <b>Win</b>: GOFILE - MEGA - …"), so the bare anchor text
// ("GOFILE") can't tell links apart. When the anchor text is just the host
// name, the name becomes the section heading plus the platform label, e.g.
// "Part 2 · Win"; real filenames in the anchor text are kept as-is.
func downloadLinkName(a *goquery.Selection, text, host string) string {
	section, platform := linkSectionPlatform(a)
	label := strings.TrimSpace(section)
	if platform != "" {
		if label != "" {
			label += " · " + platform
		} else {
			label = platform
		}
	}
	if label != "" && (text == "" || strings.EqualFold(text, host)) {
		return label
	}
	return text
}

// linkSectionPlatform recovers the context around a download link that the
// flattened anchor text loses: the platform row label ("Win", "Mac", …) and
// the nearest preceding section heading ("Part 2", "Update 26", …).
//
// The walk starts at the anchor and checks preceding siblings, then each
// ancestor's preceding siblings, until a section heading is found. Platform
// labels are recorded but never stop the walk — they belong to the row, not
// the section, and the same block can host several labeled rows.
func linkSectionPlatform(a *goquery.Selection) (section, platform string) {
	an := a.Get(0)
	for n := an; n != nil && n.Data != "article" && n.Data != "body"; n = n.Parent {
		for sib := n.PrevSibling; sib != nil; sib = sib.PrevSibling {
			if sib.Type != html.ElementNode {
				continue
			}
			var t string
			if isHeadingElement(sib) {
				t = strings.TrimSpace(nodeText(sib))
			} else {
				// Non-heading block: only its last heading can be context.
				if last := goquery.NewDocumentFromNode(sib).Find("b, strong, h2, h3, h4").Last(); last.Length() > 0 {
					t = strings.TrimSpace(nodeText(last.Get(0)))
				}
			}
			if t == "" {
				continue
			}
			if p, ok := isPlatformLabel(t); ok {
				if platform == "" {
					platform = p
				}
				continue
			}
			if isSectionHeading(t) {
				return t, platform
			}
		}
	}
	return "", platform
}

// isHeadingElement reports whether n is an element that can carry a section
// heading in a thread post.
func isHeadingElement(n *html.Node) bool {
	switch n.Data {
	case "b", "strong", "h2", "h3", "h4":
		return true
	}
	return false
}

// nodeText collects the text of a node, treating <br> as a space so stacked
// heading lines ("DOWNLOAD<br>Part 2") normalize to "DOWNLOAD Part 2".
func nodeText(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(m *html.Node) {
		switch m.Type {
		case html.TextNode:
			sb.WriteString(m.Data)
		case html.ElementNode:
			if m.Data == "br" {
				sb.WriteString(" ")
				return
			}
		}
		for c := m.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(sb.String()), " ")
}

// isPlatformLabel returns the canonical platform label when t is a row label
// like "Win:" or "Mac". Ok=false for anything else.
func isPlatformLabel(t string) (string, bool) {
	m := platformLabelRe.FindStringSubmatch(strings.TrimSpace(t))
	if m == nil {
		return "", false
	}
	return platformLabels[strings.ToLower(m[1])], true
}

// isSectionHeading reports whether t looks like a short section heading
// rather than prose. Length-capped so long bold sentences never leak in.
func isSectionHeading(t string) bool {
	if t == "" || len(t) > 48 {
		return false
	}
	return sectionHeadingRe.MatchString(t)
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
//
// Matching is two-phase: hostname needles are suffix-matched against the
// parsed URL host (a short needle like "k2s" or "dp.ua" must not match an
// unrelated URL that merely contains the substring), and text needles fall
// back to substring matching over the link text.
func identifyHost(rawURL, text string) string {
	host := ""
	var u *urlpkg.URL
	if parsed, err := urlpkg.Parse(rawURL); err == nil && parsed.Hostname() != "" {
		u = parsed
		host = strings.ToLower(parsed.Hostname())
	}
	// F95Zone masked URLs encode the real download host in the path:
	// f95zone.to/masked/gofile.io/134272/1 → "gofile.io". The generic
	// substring match below would also catch it, but only by accident.
	if u != nil && strings.Contains(strings.ToLower(u.Path), "/masked/") {
		if parts := strings.SplitN(strings.TrimPrefix(u.Path, "/masked/"), "/", 2); len(parts) >= 1 && parts[0] != "" {
			host = strings.ToLower(parts[0])
		}
	}
	lowerURL := strings.ToLower(rawURL)
	lowerText := strings.ToLower(text)

	hostMatch := func(needles ...string) bool {
		if host == "" {
			return false
		}
		for _, n := range needles {
			if host == n || strings.HasSuffix(host, "."+n) {
				return true
			}
		}
		return false
	}
	// textMatch matches against the link text only — the URL gets
	// hostname-suffix matching above, so a needle like "k2s" appearing in
	// an unrelated URL path must not count.
	textMatch := func(needles ...string) bool {
		for _, n := range needles {
			if strings.Contains(lowerText, n) {
				return true
			}
		}
		return false
	}
	// pathMatch covers URL patterns that are not hostname-based
	// (e.g. google.com/drive links).
	pathMatch := func(needles ...string) bool {
		for _, n := range needles {
			if strings.Contains(lowerURL, n) {
				return true
			}
		}
		return false
	}

	switch {
	// --- Alphabetical by canonical host name ---
	case hostMatch("vern.cc"):
		return "vern"
	case hostMatch("1cloudfile.com", "1cloudfile.eu") || textMatch("1cloudfile", "1cloud"):
		return "1cloudfile"
	case hostMatch("akirabox.com") || textMatch("akirabox"):
		return "akirabox"
	case hostMatch("anontransfer.com") || textMatch("anontransfer"):
		return "anontransfer"
	case hostMatch("anonymfile.com") || textMatch("anonymfile"):
		return "anonymfile"
	case hostMatch("apkadmin.com") || textMatch("apkadmin"):
		return "apkadmin"
	case hostMatch("bowfile.com") || textMatch("bowfile"):
		return "bowfile"
	case hostMatch("bunkrr.su", "bunkr.ru", "bunkr.is", "bunkr.si") || textMatch("bunkrr", "bunkr"):
		return "bunkrr"
	case hostMatch("buzzheavier.com") || textMatch("buzzheavier"):
		return "buzzheavier"
	case hostMatch("catbox.moe") || textMatch("catbox"):
		return "catbox"
	case hostMatch("cyberfile.me") || textMatch("cyberfile"):
		return "cyberfile"
	case hostMatch("datanodes.to") || textMatch("datanodes"):
		return "datanodes"
	case hostMatch("delafil.com") || textMatch("delafil"):
		return "delafil"
	case hostMatch("download.gg") || textMatch("download.gg"):
		return "downloadgg"
	case hostMatch("dropmefiles.com") || textMatch("dropmefiles"):
		return "dropmefiles"
	case hostMatch("easyupload.io") || textMatch("easyupload"):
		return "easyupload"
	case hostMatch("filemail.com") || textMatch("filemail"):
		return "filemail"
	case hostMatch("files.dp.ua") || textMatch("files.dp.ua"):
		return "filesdpua"
	case hostMatch("files.fm") || textMatch("files.fm", "filesfm"):
		return "filesfm"
	case hostMatch("fromsmash.com", "from.smash.com") || textMatch("fromsmash", "from.smash"):
		return "fromsmash"
	case hostMatch("gofile.io") || textMatch("gofile"):
		return "gofile"
	case hostMatch("drive.google.com") || pathMatch("google.com/drive"):
		return "googledrive"
	case hostMatch("hexload.com", "hexupload.net") || textMatch("hexload", "hexupload"):
		return "hexload"
	case hostMatch("krakenfiles.com") || textMatch("krakenfiles"):
		return "krakenfiles"
	case hostMatch("mediafire.com") || textMatch("mediafire"):
		return "mediafire"
	case hostMatch("mega.nz", "mega.co.nz") || textMatch("mega.nz", "mega.co"):
		return "mega"
	case hostMatch("mixdrop.co", "mixdrop.to", "m1xdrop.co") || textMatch("mixdrop", "m1xdrop"):
		return "mixdrop"
	case hostMatch("pixeldrain.com") || textMatch("pixeldrain"):
		return "pixeldrain"
	case hostMatch("protondrive.com") || textMatch("proton") && textMatch("drive"):
		return "protondrive"
	case hostMatch("qu.ax") || textMatch("qu.ax"):
		return "quax"
	case hostMatch("sendgb.com") || textMatch("sendgb"):
		return "sendgb"
	case hostMatch("terminal.ink") || textMatch("terminal"):
		return "terminal"
	case hostMatch("transfer.sh") || textMatch("transfer.sh"):
		return "transfersh"
	case hostMatch("transfert.us") || textMatch("transfert"):
		return "transfert"
	case hostMatch("uploadhaven.com") || textMatch("uploadhaven"):
		return "uploadhaven"
	case hostMatch("uploadnow.io") || textMatch("uploadnow"):
		return "uploadnow"
	case hostMatch("vikingfile.com") || textMatch("vikingfile"):
		return "vikingfile"
	case hostMatch("wdho.ru") || textMatch("wdho"):
		return "wdho"
	case hostMatch("wetransfer.com") || textMatch("wetransfer"):
		return "wetransfer"
	case hostMatch("workupload.com") || textMatch("workupload"):
		return "workupload"
	case hostMatch("yourfilestore.com") || textMatch("yourfilestore"):
		return "yourfilestore"

	// --- Legacy/extra hosts ---
	case hostMatch("keep2share.cc", "k2s.cc") || textMatch("keep2share", "k2s"):
		return "keep2share"
	case hostMatch("uploaded.net", "ul.to") || textMatch("uploaded", "ul.to"):
		return "uploaded"
	case hostMatch("dropbox.com") || textMatch("dropbox"):
		return "dropbox"

	default:
		return "other"
	}
}
