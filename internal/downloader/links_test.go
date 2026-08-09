package downloader

import (
	"testing"

	"github.com/mili/moxie/internal/db"
)

// ---------------------------------------------------------------------------
// ScoreLinkHost — tier table (F95-ugim host-tier research, 2026-08-09)
// ---------------------------------------------------------------------------

func TestScoreLinkHost(t *testing.T) {
	t.Parallel()
	tests := []struct {
		host string
		want int
	}{
		// Tier 1 — plain HTTP works.
		{"pixeldrain", 25},
		{"catbox", 25},
		{"mediafire", 25},
		// Tier 2 — intermittent CF or solvable puzzle.
		{"buzzheavier", 10},
		{"workupload", 10},
		{"googledrive", 10}, // working resolver, unchanged from prior scoring
		// Tier 3 — fragile.
		{"gofile", 5},
		// Tier 4 — browser-gated / unproven, default 0.
		{"datanodes", 0},
		{"mixdrop", 0},
		{"hexload", 0},
		{"uploadhaven", 0},
		{"1cloudfile", 0},
		{"bunkrr", 0},
		{"unknown", 0},
		{"", 0},
		// Tier 5 — hard walls.
		{"vikingfile", -200},
		{"krakenfiles", -200},
		{"mega", -200},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			got := ScoreLinkHost(tt.host)
			if got != tt.want {
				t.Errorf("ScoreLinkHost(%q) = %d, want %d", tt.host, got, tt.want)
			}
		})
	}
}

func TestScoreLinkHost_TierOrdering(t *testing.T) {
	t.Parallel()
	tiers := []struct {
		name  string
		hosts []string
		want  int
	}{
		{"tier1-plain-http", []string{"pixeldrain", "catbox", "mediafire"}, 25},
		{"tier2-intermittent", []string{"buzzheavier", "workupload", "googledrive"}, 10},
		{"tier3-fragile", []string{"gofile"}, 5},
		{"tier4-browser-gated", []string{"datanodes", "mixdrop", "hexload", "uploadhaven", "1cloudfile", "bunkrr", "unknown"}, 0},
		{"tier5-hard-walls", []string{"vikingfile", "krakenfiles", "mega"}, -200},
	}
	for i, tier := range tiers {
		for _, h := range tier.hosts {
			got := ScoreLinkHost(h)
			if got != tier.want {
				t.Errorf("host %q: ScoreLinkHost = %d, want %d (%s)", h, got, tier.want, tier.name)
			}
		}
		if i > 0 && tiers[i-1].want <= tier.want {
			t.Errorf("tier %q (%d) must strictly outrank tier %q (%d)",
				tiers[i-1].name, tiers[i-1].want, tier.name, tier.want)
		}
	}
}

func TestScoreLinkHost_CaseInsensitive(t *testing.T) {
	t.Parallel()
	tests := []struct {
		host string
		want int
	}{
		{"Pixeldrain", 25},
		{"CATBOX", 25},
		{"MediaFire", 25},
		{"BuzzHeavier", 10},
		{"WORKUPLOAD", 10},
		{"Gofile", 5},
		{"BUNKRR", 0},
		{"1CloudFile", 0},
		{"VIKINGFILE", -200},
		{"KrakenFiles", -200},
		{"MEGA", -200},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			got := ScoreLinkHost(tt.host)
			if got != tt.want {
				t.Errorf("ScoreLinkHost(%q) = %d, want %d", tt.host, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ScoreDownloadLink — composite platform + host scoring
// ---------------------------------------------------------------------------

func TestScoreDownloadLink_CombinesPlatformAndHost(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		platform string
		host     string
		target   Platform
		want     int
	}{
		{"linux host bonus + platform match", "linux", "pixeldrain", PlatformLinux, 25 + PlatformPriority(PlatformLinux, PlatformLinux)},
		{"linux host bonus + platform mismatch", "windows", "pixeldrain", PlatformLinux, 25 + PlatformPriority(PlatformWindows, PlatformLinux)},
		{"hard wall penalty", "all", "mega", PlatformLinux, -200 + PlatformPriority(PlatformAll, PlatformLinux)},
		{"default host no bonus", "all", "rando", PlatformLinux, 0 + PlatformPriority(PlatformAll, PlatformLinux)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link := db.DownloadLink{Platform: db.Platform(tt.platform), Host: tt.host}
			got := ScoreDownloadLink(link, tt.target)
			if got != tt.want {
				t.Errorf("ScoreDownloadLink(host=%q) = %d, want %d", tt.host, got, tt.want)
			}
		})
	}
}
