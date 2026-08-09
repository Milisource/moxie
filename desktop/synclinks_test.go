package main

import (
	"testing"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/scraper"
)

func TestSyncDownloadLinks_KeepsIDsAndDeadState(t *testing.T) {
	a := newTestApp(t)
	id := addGame(t, a, "Test Game", "/games/test-game")

	// Seed a link, then mark it dead — the user's verdict.
	linkID, err := a.db.CreateDownloadLink(&db.DownloadLink{
		GameID: id, URL: "https://pixeldrain.com/u/abc", Host: "pixeldrain", Name: "Link A", IsDead: false,
	})
	if err != nil {
		t.Fatalf("CreateDownloadLink: %v", err)
	}
	if err := a.db.MarkDownloadLinkDead(linkID, "user said dead"); err != nil {
		t.Fatalf("MarkDownloadLinkDead: %v", err)
	}

	// Re-sync with the same link plus one new one.
	a.syncDownloadLinks(id, []scraper.DownloadLink{
		{URL: "https://pixeldrain.com/u/abc", Host: "pixeldrain", Name: "Link A"},
		{URL: "https://gofile.io/d/new", Host: "gofile", Name: "Link B"},
	})

	links, err := a.db.ListDownloadLinks(id, "", true)
	if err != nil {
		t.Fatalf("ListDownloadLinks: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("got %d links, want 2 (existing kept + new inserted)", len(links))
	}
	for _, l := range links {
		if l.URL == "https://pixeldrain.com/u/abc" {
			if l.ID != linkID {
				t.Errorf("existing link id = %d, want %d (ID must not churn)", l.ID, linkID)
			}
			if !l.IsDead {
				t.Error("existing link IsDead = false, want preserved dead state")
			}
		}
		if l.URL == "https://gofile.io/d/new" && l.IsDead {
			t.Error("new link must start alive")
		}
	}
}

func TestSyncDownloadLinks_RemovesStaleLinks(t *testing.T) {
	a := newTestApp(t)
	id := addGame(t, a, "Test Game", "/games/test-game")

	if _, err := a.db.CreateDownloadLink(&db.DownloadLink{
		GameID: id, URL: "https://old.example.com/1", Host: "old", Name: "Old Link", IsDead: false,
	}); err != nil {
		t.Fatalf("CreateDownloadLink: %v", err)
	}

	// The thread no longer carries the old link.
	a.syncDownloadLinks(id, []scraper.DownloadLink{
		{URL: "https://new.example.com/2", Host: "new", Name: "New Link"},
	})

	links, err := a.db.ListDownloadLinks(id, "", true)
	if err != nil {
		t.Fatalf("ListDownloadLinks: %v", err)
	}
	if len(links) != 1 || links[0].URL != "https://new.example.com/2" {
		t.Fatalf("links after sync = %+v, want only the new link (stale removed)", links)
	}
}

func TestSyncDownloadLinks_Idempotent(t *testing.T) {
	a := newTestApp(t)
	id := addGame(t, a, "Test Game", "/games/test-game")

	links := []scraper.DownloadLink{
		{URL: "https://pixeldrain.com/u/abc", Host: "pixeldrain", Name: "Link A"},
		{URL: "https://gofile.io/d/b", Host: "gofile", Name: "Link B"},
	}
	a.syncDownloadLinks(id, links)
	first, _ := a.db.ListDownloadLinks(id, "", true)

	a.syncDownloadLinks(id, links)
	second, _ := a.db.ListDownloadLinks(id, "", true)

	if len(second) != len(first) {
		t.Fatalf("link count changed across identical syncs: %d -> %d", len(first), len(second))
	}
	for i := range first {
		if first[i].ID != second[i].ID || first[i].URL != second[i].URL {
			t.Errorf("link %d changed: %+v -> %+v", i, first[i], second[i])
		}
	}
}
