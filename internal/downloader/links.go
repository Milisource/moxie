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
func ScoreLinkHost(host string) int {
	switch strings.ToLower(host) {
	case "pixeldrain", "buzzheavier", "gofile", "catbox":
		return 25
	case "datanodes", "google drive", "mixdrop":
		return 10
	case "mega", "vikingfile", "workupload", "krakenfiles", "bunkrr":
		return -200
	default:
		return 0
	}
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
