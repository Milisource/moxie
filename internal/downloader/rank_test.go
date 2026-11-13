package downloader

import (
	"testing"

	"github.com/mili/moxie/internal/db"
)

func TestRankDownloadLinks(t *testing.T) {
	t.Parallel()
	full := db.DownloadLink{Name: "DOWNLOAD · Win", URL: "full", Host: "gofile"}
	patch := db.DownloadLink{Name: "Update only (v0.17 -> v0.18) · Win", URL: "patch", Host: "gofile"}
	opts := RankOptions{TargetPlatform: PlatformWindows, Installed: PlatformWindows, InstallKnown: true, InstalledVersion: "v0.17", LatestVersion: "0.18"}
	tests := []struct {
		name                             string
		links                            []db.DownloadLink
		opts                             RankOptions
		urls                             []string
		extra, patch, platform, excluded int
		wantPatch                        bool
	}{
		{name: "extra dropped", links: []db.DownloadLink{{Name: "Mod · Win"}, full}, opts: opts, urls: []string{"full"}, extra: 1},
		{name: "patch for another version dropped", links: []db.DownloadLink{{Name: "Update only (0.16 -> 0.18) · Win"}, full}, opts: opts, urls: []string{"full"}, patch: 1},
		{name: "applicable patch bonus", links: []db.DownloadLink{full, patch}, opts: opts, urls: []string{"patch", "full"}, wantPatch: true},
		{name: "wrong target version dropped", links: []db.DownloadLink{{Name: "Update only (0.17 -> 0.19) · Win"}}, opts: opts, patch: 1},
		{name: "unknown installed version", links: []db.DownloadLink{patch}, opts: RankOptions{TargetPlatform: PlatformWindows}, patch: 1},
		{name: "unstated range dropped", links: []db.DownloadLink{{Name: "Update only · Win"}}, opts: opts, patch: 1},
		{name: "wrong platform known install", links: []db.DownloadLink{{Name: "DOWNLOAD · Linux"}}, opts: opts, platform: 1},
		{name: "wrong platform fresh install", links: []db.DownloadLink{{Name: "DOWNLOAD · Mac"}}, opts: RankOptions{TargetPlatform: PlatformWindows}, platform: 1},
		{name: "excluded host counted", links: []db.DownloadLink{{Name: "DOWNLOAD · Win", Host: " GoFile "}}, opts: RankOptions{TargetPlatform: PlatformWindows, ExcludeHosts: map[string]bool{"gofile": true}}, excluded: 1},
		{name: "stable tie order", links: []db.DownloadLink{full, {Name: full.Name, URL: "second", Host: full.Host}}, opts: opts, urls: []string{"full", "second"}},
		{name: "no matches counts", links: []db.DownloadLink{{Name: "Mod"}, {Name: "Update only (0.1 -> 0.18)"}, {Name: "DOWNLOAD · Mac"}, {Host: "mega"}}, opts: RankOptions{TargetPlatform: PlatformWindows, ExcludeHosts: map[string]bool{"mega": true}}, extra: 1, patch: 1, platform: 1, excluded: 1},
		{name: "dead and online skipped first", links: []db.DownloadLink{{Name: "Mod", IsDead: true, Host: "mega"}, {Name: "Online Version", Host: "mega"}}, opts: RankOptions{ExcludeHosts: map[string]bool{"mega": true}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RankDownloadLinks(tc.links, tc.opts)
			if got.SkippedExtra != tc.extra || got.SkippedPatch != tc.patch || got.SkippedPlatform != tc.platform || got.SkippedExcluded != tc.excluded {
				t.Fatalf("counts = %+v", got)
			}
			if len(got.Links) != len(tc.urls) {
				t.Fatalf("links = %+v, want %v", got.Links, tc.urls)
			}
			for i, url := range tc.urls {
				if got.Links[i].Link.URL != url {
					t.Errorf("link %d = %q, want %q", i, got.Links[i].Link.URL, url)
				}
			}
			if len(got.Links) > 0 && got.Links[0].Patch != tc.wantPatch {
				t.Errorf("Patch = %v", got.Links[0].Patch)
			}
			if tc.wantPatch && got.Links[0].Score-got.Links[1].Score != 30 {
				t.Errorf("patch bonus != 30: %+v", got.Links)
			}
		})
	}
}

func TestPatchApplicableWithoutToVersion(t *testing.T) {
	t.Parallel()
	class := LinkClass{Kind: LinkPatch, FromVersion: "0.17"}
	if !patchApplicable(class, RankOptions{InstalledVersion: "v0.17", LatestVersion: "0.18"}) {
		t.Fatal("matching open-ended patch rejected")
	}
	if patchApplicable(class, RankOptions{InstalledVersion: "0.16"}) {
		t.Fatal("wrong base accepted")
	}
}

func TestUpdateRankOptions(t *testing.T) {
	t.Parallel()
	got := UpdateRankOptions(&db.Game{ExePath: "Game.exe", Version: "0.17", LatestVersion: "0.18"})
	if !got.InstallKnown || got.Installed != PlatformWindows || got.InstalledVersion != "0.17" || got.LatestVersion != "0.18" {
		t.Fatalf("options = %+v", got)
	}
	if UpdateRankOptions(&db.Game{}).InstallKnown {
		t.Fatal("empty launcher treated as known")
	}
}
