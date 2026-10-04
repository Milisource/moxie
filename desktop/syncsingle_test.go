package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mili/moxie/internal/scraper"
)

// The cookie-free single-game fallback must still apply version, status,
// developer/overview and the canonical thread URL — just without download
// links (the cache API carries none).
func TestSyncSingleViaCacheAppliesMetadata(t *testing.T) {
	a := newTestApp(t)
	id := addGame(t, a, "Kunoichi Sekiren", "/games/kunoichi-sekiren")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"name":"Kunoichi Sekiren","version":"0.1.0 Alpha","developer":"Sekiren2023","status":"1","image_url":"","description":"A ninja game.","type":"19"}`)
	}))
	defer srv.Close()

	public := scraper.NewPublicAPI()
	public.CacheHost = srv.URL

	game, err := a.db.GetGame(id)
	if err != nil || game == nil {
		t.Fatalf("GetGame: %v", err)
	}
	if err := a.syncSingleViaCache(game, public, 276681); err != nil {
		t.Fatalf("syncSingleViaCache: %v", err)
	}

	got, err := a.db.GetGame(id)
	if err != nil || got == nil {
		t.Fatalf("GetGame after sync: %v", err)
	}
	if got.F95ThreadID != 276681 || got.F95URL != "https://f95zone.to/threads/276681/" {
		t.Errorf("association = %d/%q, want 276681/https://f95zone.to/threads/276681/",
			got.F95ThreadID, got.F95URL)
	}
	if got.LatestVersion != "0.1.0" {
		t.Errorf("LatestVersion = %q, want 0.1.0 (qualifier stripped)", got.LatestVersion)
	}
	if got.Status != "active" {
		t.Errorf("Status = %q, want active", got.Status)
	}
	if got.VersionCheckedAt.IsZero() {
		t.Error("VersionCheckedAt not stamped")
	}
	if got.Title != "Kunoichi Sekiren" {
		t.Errorf("Title = %q, want the curated local title preserved", got.Title)
	}

	meta, err := a.db.GetScrapedMeta(id)
	if err != nil || meta == nil {
		t.Fatalf("GetScrapedMeta: %v (meta=%v)", err, meta)
	}
	if meta.Developer != "Sekiren2023" || meta.Overview != "A ninja game." {
		t.Errorf("meta = %q/%q, want Sekiren2023/A ninja game.", meta.Developer, meta.Overview)
	}
}

// A missing thread ID is a clear error rather than a nil deref or a bogus
// request.
func TestSyncSingleViaCacheRequiresThreadID(t *testing.T) {
	a := newTestApp(t)
	id := addGame(t, a, "Game", "/games/game")
	game, err := a.db.GetGame(id)
	if err != nil || game == nil {
		t.Fatalf("GetGame: %v", err)
	}
	if err := a.syncSingleViaCache(game, scraper.NewPublicAPI(), 0); err == nil {
		t.Error("expected an error for a game with no thread ID")
	}
}
