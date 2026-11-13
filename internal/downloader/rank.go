package downloader

import (
	"sort"
	"strings"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/version"
)

// RankOptions controls selection for a fresh install or an existing game.
type RankOptions struct {
	TargetPlatform   Platform
	Installed        Platform
	InstallKnown     bool
	InstalledVersion string
	LatestVersion    string
	ExcludeHosts     map[string]bool
}

// RankedLink retains the merge mode alongside the selected download.
type RankedLink struct {
	Link  db.DownloadLink
	Score int
	Patch bool
}

// RankResult reports usable links best first and why others were skipped.
type RankResult struct {
	Links           []RankedLink
	SkippedExtra    int
	SkippedPatch    int
	SkippedPlatform int
	SkippedExcluded int
}

// UpdateRankOptions builds options from the installed launcher and versions.
func UpdateRankOptions(g *db.Game) RankOptions {
	if g == nil {
		return RankOptions{}
	}
	installed := InstalledPlatform(g.ExePath)
	return RankOptions{Installed: installed, InstallKnown: installed != "" && installed != PlatformUnknown, InstalledVersion: g.Version, LatestVersion: g.LatestVersion}
}

func sameGameVersion(a, b string) bool {
	a, b = version.Canonical(a), version.Canonical(b)
	return a != "" && b != "" && version.Compare(a, b) == version.Same
}

// RankDownloadLinks applies the same eligibility and scoring on every surface.
func RankDownloadLinks(links []db.DownloadLink, opts RankOptions) RankResult {
	var result RankResult
	if opts.TargetPlatform == "" {
		opts.TargetPlatform = CurrentPlatform()
	}
	for _, link := range links {
		if link.IsDead || IsOnlineOnly(link.Name, link.URL) {
			continue
		}
		if opts.ExcludeHosts[strings.ToLower(strings.TrimSpace(link.Host))] {
			result.SkippedExcluded++
			continue
		}
		patch := false
		switch class := ClassifyLink(link.Name); class.Kind {
		case LinkExtra:
			result.SkippedExtra++
			continue
		case LinkPatch:
			if !patchApplicable(class, opts) {
				result.SkippedPatch++
				continue
			}
			patch = true
		}
		p := DetectPlatform(link.Name, link.URL)
		var score int
		if opts.InstallKnown {
			score = InstallPlatformPriority(p, opts.Installed)
			if score < 0 {
				result.SkippedPlatform++
				continue
			}
		} else {
			if !PlatformMatches(p, opts.TargetPlatform) {
				result.SkippedPlatform++
				continue
			}
			score = PlatformPriority(p, opts.TargetPlatform)
		}
		score += ScoreLinkHost(link.Host)
		if patch {
			score += 30
		}
		result.Links = append(result.Links, RankedLink{Link: link, Score: score, Patch: patch})
	}
	sort.SliceStable(result.Links, func(i, j int) bool { return result.Links[i].Score > result.Links[j].Score })
	return result
}

// patchApplicable requires a known matching base and, when stated, target.
func patchApplicable(class LinkClass, opts RankOptions) bool {
	return sameGameVersion(class.FromVersion, opts.InstalledVersion) &&
		(class.ToVersion == "" || opts.LatestVersion == "" || sameGameVersion(class.ToVersion, opts.LatestVersion))
}
