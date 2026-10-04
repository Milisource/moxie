package downloader

import "strings"

// browserGatedHosts lists file hosts whose download landing pages are driven
// by JavaScript, a free-download button, or a bot wall that the plain Go
// client cannot pass. When one of these answers a download hop with a
// text/html body (a landing page) instead of file bytes, the transfer is
// rerouted through the existing browser fallback so the user's real browser
// can walk the host's flow and download the file itself.
//
// Deliberately excludes "mega": mega.nz is delegated to the megatools
// subprocess (see ErrMegaNeedsMegatools) and never reaches the text/html
// check. vikingfile is omitted too — it already routes through
// isChallengeFailure at the resolver stage, so it needs no entry here.
var browserGatedHosts = map[string]bool{
	// File-host landing / JS-driven pages observed returning 200 text/html.
	"mixdrop":     true,
	"hexload":     true,
	"dropmefiles": true,
	"uploadnow":   true,
	"googledrive": true,
	// Existing challenge-graded hosts: the Go path may surface a text/html
	// landing page when the challenge is not (or no longer) signalled by a
	// Cf-Mitigated header.
	"datanodes":   true,
	"buzzheavier": true,
	"uploadhaven": true,
	"workupload":  true,
	"krakenfiles": true,
	"bunkrr":      true,
}

// isBrowserGatedHost reports whether a host label (as produced by
// IdentifyHostInURL) belongs to a known browser-gated file host.
func isBrowserGatedHost(host string) bool {
	return browserGatedHosts[strings.ToLower(host)]
}

// isBrowserGatedURL reports whether a URL belongs to a browser-gated file
// host. It derives the host label with IdentifyHostInURL so callers holding
// a URL (rather than a label) can use the same policy.
func isBrowserGatedURL(rawURL string) bool {
	return isBrowserGatedHost(IdentifyHostInURL(rawURL))
}
