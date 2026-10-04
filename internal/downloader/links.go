package downloader

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mili/moxie/internal/db"
)

// IsOnlineOnly returns true if the link text or URL indicates a browser-only version.
func IsOnlineOnly(name, url string) bool {
	lower := strings.ToLower(name + " " + url)
	return strings.Contains(lower, "online") || strings.Contains(lower, "gamejolt")
}

// ScoreLinkHost adjusts a download link's priority score based on host reliability.
// Higher scores are preferred. Callers should add this to their platform priority score.
//
// Tiers (calibrated 2026-08-09, F95-ugim host-tier research):
//
//	 +25  plain HTTP works (pixeldrain, catbox, mediafire)
//	 +10  intermittent challenges or solvable puzzles (buzzheavier, workupload);
//	      googledrive keeps +10 (working resolver, unchanged)
//	  +5  fragile (gofile — premium-gated API, free path is a web scrape)
//	   0   browser-gated or unproven (datanodes, mixdrop, hexload, uploadhaven,
//	       bunkrr, 1cloudfile, vikingfile, ...)
//	-200  hard walls (krakenfiles; mega until megatools lands)
//
// A host that is currently serving server-side-capped downloads (detected by
// the pre-download probe, e.g. pixeldrain past its free quota) is penalised by
// throttlePenalty so any working alternative outranks it.
func ScoreLinkHost(host string) int {
	return hostTierScore(host) - throttlePenaltyFor(host)
}

// hostTierScore is the static host-reliability tier table (see ScoreLinkHost).
func hostTierScore(host string) int {
	switch strings.ToLower(host) {
	case "pixeldrain", "catbox", "mediafire":
		return 25
	case "buzzheavier", "googledrive", "workupload":
		return 10
	case "gofile":
		return 5
	case "vikingfile":
		// Resolvable as of F95-ob9m: the Cloudflare Turnstile widget is
		// cleared in a real browser and the revealed #download-link href is
		// fetched by the Go path with cookies + Referer. Live end-to-end is
		// not yet confirmed, so it stays on the browser-gated tier rather
		// than the +10 solvable tier (buzzheavier/workupload).
		return 0
	case "mega", "krakenfiles":
		return -200
	default:
		return 0
	}
}

// throttlePenaltyFor sinks a host currently serving capped downloads (e.g.
// pixeldrain past its free 24h quota) below every working alternative, so
// ranking tries another host instead of a 1 MiB/s grind. See hosthealth.go.
func throttlePenaltyFor(host string) int {
	if HostThrottled(host) {
		return throttlePenalty
	}
	return 0
}

// ScoreDownloadLink returns a composite score for a download link combining
// platform priority and host reliability. Higher scores are preferred.
func ScoreDownloadLink(link db.DownloadLink, targetPlatform Platform) int {
	return PlatformPriority(Platform(link.Platform), targetPlatform) + ScoreLinkHost(link.Host)
}

// FindMostRecentFile returns the path of the most recently modified regular file
// in a directory, or empty string if the directory is empty or unreadable.
func FindMostRecentFile(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var best os.DirEntry
	var bestTime time.Time
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(bestTime) {
			best = e
			bestTime = info.ModTime()
		}
	}
	if best == nil {
		return ""
	}
	return filepath.Join(dir, best.Name())
}
