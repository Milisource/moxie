package main

import (
	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/downloader"
	"testing"
)

func TestRankDownloadLinksForMatchesSharedRanker(t *testing.T) {
	t.Parallel()
	links := []DesktopDownloadLink{
		{ID: 10, Name: "DOWNLOAD · Win", URL: "full", Host: "gofile"},
		{ID: 20, Name: "Update only (0.17 -> 0.18) · Win", URL: "patch", Host: "gofile"},
		{ID: 30, Name: "Mod · Win", URL: "mod", Host: "pixeldrain"},
	}
	game := &db.Game{ExePath: "Game.exe", Version: "0.17", LatestVersion: "0.18"}
	got, err := rankDownloadLinksFor(links, updateRankOpts(game))
	if err != nil {
		t.Fatal(err)
	}
	var stored []db.DownloadLink
	for _, link := range links {
		stored = append(stored, db.DownloadLink{URL: link.URL, Name: link.Name, Host: link.Host})
	}
	shared := downloader.RankDownloadLinks(stored, downloader.UpdateRankOptions(game))
	if len(got) == 0 || len(shared.Links) == 0 || got[0].URL != shared.Links[0].Link.URL || got[0].URL != "patch" {
		t.Fatalf("desktop=%+v shared=%+v", got, shared)
	}
	if got[0].ID != 20 {
		t.Fatalf("DTO identity lost: %+v", got[0])
	}
}
