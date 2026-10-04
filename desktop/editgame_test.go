package main

import (
	"os"
	"testing"

	"github.com/mili/moxie/internal/db"
)

// A nil field means "unchanged" — the existing value must survive.
func TestEditGameNilFieldsLeaveUnchanged(t *testing.T) {
	a := newTestApp(t)
	id := addGame(t, a, "Test Game", "/games/test-game")
	if err := a.db.UpdateGame(&db.Game{
		ID:      id,
		Title:   "Test Game",
		Path:    "/games/test-game",
		Engine:  "RenPy",
		Version: "1.0",
		ExePath: "/games/test-game/game.exe",
		Notes:   "old note",
		Status:  "active",
	}); err != nil {
		t.Fatalf("UpdateGame: %v", err)
	}

	err := a.EditGame(id, EditGameFields{})
	if err != nil {
		t.Fatalf("EditGame with nil fields: %v", err)
	}

	g, err := a.db.GetGame(id)
	if err != nil || g == nil {
		t.Fatalf("GetGame: %v", err)
	}
	if g.Engine != "RenPy" || g.Version != "1.0" || g.ExePath != "/games/test-game/game.exe" || g.Notes != "old note" {
		t.Errorf("nil fields changed values: %+v", g)
	}
}

// An empty string explicitly clears the field — the whole point of the
// nullable contract.
func TestEditGameEmptyStringClearsField(t *testing.T) {
	a := newTestApp(t)
	id := addGame(t, a, "Test Game", "/games/test-game")
	if err := a.db.UpdateGame(&db.Game{
		ID:      id,
		Title:   "Test Game",
		Path:    "/games/test-game",
		Engine:  "RenPy",
		Version: "1.0",
		ExePath: "/games/test-game/game.exe",
		Notes:   "stale note",
		Status:  "active",
	}); err != nil {
		t.Fatalf("UpdateGame: %v", err)
	}

	empty := ""
	err := a.EditGame(id, EditGameFields{ExePath: &empty, Notes: &empty})
	if err != nil {
		t.Fatalf("EditGame: %v", err)
	}

	g, err := a.db.GetGame(id)
	if err != nil || g == nil {
		t.Fatalf("GetGame: %v", err)
	}
	if g.ExePath != "" {
		t.Errorf("ExePath = %q, want cleared", g.ExePath)
	}
	if g.Notes != "" {
		t.Errorf("Notes = %q, want cleared", g.Notes)
	}
	if g.Engine != "RenPy" || g.Version != "1.0" {
		t.Errorf("unrelated fields changed: %+v", g)
	}
}

// An engine-only edit must not wipe the other fields — the regression the
// old empty-string contract made possible.
func TestEditGameEngineOnlyEditKeepsOtherFields(t *testing.T) {
	a := newTestApp(t)
	id := addGame(t, a, "Test Game", "/games/test-game")
	if err := a.db.UpdateGame(&db.Game{
		ID:      id,
		Title:   "Test Game",
		Path:    "/games/test-game",
		Engine:  "RenPy",
		Version: "1.0",
		ExePath: "/games/test-game/game.exe",
		Notes:   "note",
		Status:  "active",
	}); err != nil {
		t.Fatalf("UpdateGame: %v", err)
	}

	flash := "Flash"
	err := a.EditGame(id, EditGameFields{Engine: &flash})
	if err != nil {
		t.Fatalf("EditGame: %v", err)
	}

	g, err := a.db.GetGame(id)
	if err != nil || g == nil {
		t.Fatalf("GetGame: %v", err)
	}
	if g.Engine != "Flash" {
		t.Errorf("Engine = %q, want Flash", g.Engine)
	}
	if g.Version != "1.0" || g.ExePath != "/games/test-game/game.exe" || g.Notes != "note" {
		t.Errorf("engine-only edit wiped other fields: %+v", g)
	}
}

func TestEditGameMissingGame(t *testing.T) {
	a := newTestApp(t)
	val := "x"
	if err := a.EditGame(99999, EditGameFields{Engine: &val}); err == nil {
		t.Error("EditGame on missing game: expected error, got nil")
	}
}

// A title edit is display-only: the game's directory path (and the directory
// on disk) must not move. Folder renames stay in RenameGame.
func TestEditGameTitleDoesNotTouchPath(t *testing.T) {
	a := newTestApp(t)
	dir := t.TempDir()
	id := addGame(t, a, "Old Title", dir)

	newTitle := "New Title"
	if err := a.EditGame(id, EditGameFields{Title: &newTitle}); err != nil {
		t.Fatalf("EditGame: %v", err)
	}

	g, err := a.db.GetGame(id)
	if err != nil || g == nil {
		t.Fatalf("GetGame: %v", err)
	}
	if g.Title != "New Title" {
		t.Errorf("Title = %q, want New Title", g.Title)
	}
	if g.Path != dir {
		t.Errorf("Path = %q, want unchanged %q", g.Path, dir)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("directory should still exist: %v", err)
	}
}

// Every scalar + collection field the dialog can send round-trips in one call.
func TestEditGameAllEditableFields(t *testing.T) {
	a := newTestApp(t)
	id := addGame(t, a, "Game", "/games/game")

	title := "Renamed"
	engine := "RenPy"
	version := "2.0"
	exe := "/games/game/game.exe"
	prefix := "/wine/game"
	notes := "a note"
	status := "completed"
	f95 := "https://f95zone.to/threads/12345/"
	tags := []string{"romance", "sandbox"}
	stores := map[string]string{"steam": "https://store.steampowered.com/app/1"}
	dev := "Some Dev"
	overview := "The overview."

	if err := a.EditGame(id, EditGameFields{
		Title: &title, Engine: &engine, Version: &version, ExePath: &exe,
		WinePrefix: &prefix, Notes: &notes, Status: &status, F95URL: &f95,
		Tags: tags, StoreLinks: stores, Developer: &dev, Overview: &overview,
	}); err != nil {
		t.Fatalf("EditGame: %v", err)
	}

	g, err := a.db.GetGame(id)
	if err != nil || g == nil {
		t.Fatalf("GetGame: %v", err)
	}
	if g.Title != title || g.Engine != engine || g.Version != version || g.ExePath != exe ||
		g.WinePrefix != prefix || g.Notes != notes || g.Status != status || g.F95URL != f95 {
		t.Errorf("scalar fields not saved: %+v", g)
	}
	if len(g.Tags) != 2 || g.Tags[0] != "romance" || g.Tags[1] != "sandbox" {
		t.Errorf("Tags = %v, want [romance sandbox]", g.Tags)
	}
	if g.StoreLinks["steam"] != stores["steam"] {
		t.Errorf("StoreLinks = %v, want %v", g.StoreLinks, stores)
	}

	meta, err := a.db.GetScrapedMeta(id)
	if err != nil || meta == nil {
		t.Fatalf("GetScrapedMeta: %v (meta=%v)", err, meta)
	}
	if meta.Developer != dev || meta.Overview != overview {
		t.Errorf("scraped meta = %q/%q, want %q/%q", meta.Developer, meta.Overview, dev, overview)
	}
}

// nil slices/maps mean "leave unchanged"; empty non-nil ones clear.
func TestEditGameCollectionsNilVsEmpty(t *testing.T) {
	a := newTestApp(t)
	id := addGame(t, a, "Game", "/games/game")
	if err := a.db.UpdateGame(&db.Game{
		ID: id, Title: "Game", Path: "/games/game", Engine: "Unknown", Status: "active",
		Tags: []string{"keep"}, StoreLinks: map[string]string{"steam": "https://x"},
	}); err != nil {
		t.Fatalf("UpdateGame: %v", err)
	}

	engine := "Unity"
	if err := a.EditGame(id, EditGameFields{Engine: &engine}); err != nil {
		t.Fatalf("EditGame (nil collections): %v", err)
	}
	g, _ := a.db.GetGame(id)
	if len(g.Tags) != 1 || g.Tags[0] != "keep" || g.StoreLinks["steam"] == "" {
		t.Errorf("nil collections changed values: tags=%v stores=%v", g.Tags, g.StoreLinks)
	}

	if err := a.EditGame(id, EditGameFields{Tags: []string{}, StoreLinks: map[string]string{}}); err != nil {
		t.Fatalf("EditGame (empty collections): %v", err)
	}
	g, _ = a.db.GetGame(id)
	if len(g.Tags) != 0 || len(g.StoreLinks) != 0 {
		t.Errorf("empty collections should clear: tags=%v stores=%v", g.Tags, g.StoreLinks)
	}
}

// Developer/Overview edits go to scraped_meta and must keep the cover.
func TestEditGameDeveloperOverviewPreservesCover(t *testing.T) {
	a := newTestApp(t)
	id := addGame(t, a, "Game", "/games/game")
	if err := a.db.UpsertScrapedMeta(&db.ScrapedMeta{
		GameID: id, Developer: "Old", Overview: "Old ov", CoverURL: "https://example.com/c.jpg",
	}); err != nil {
		t.Fatalf("UpsertScrapedMeta: %v", err)
	}

	dev := "New"
	ov := "New overview"
	if err := a.EditGame(id, EditGameFields{Developer: &dev, Overview: &ov}); err != nil {
		t.Fatalf("EditGame: %v", err)
	}

	meta, err := a.db.GetScrapedMeta(id)
	if err != nil || meta == nil {
		t.Fatalf("GetScrapedMeta: %v", err)
	}
	if meta.Developer != "New" || meta.Overview != "New overview" {
		t.Errorf("meta = %q/%q, want New/New overview", meta.Developer, meta.Overview)
	}
	if meta.CoverURL != "https://example.com/c.jpg" {
		t.Errorf("CoverURL = %q, want preserved", meta.CoverURL)
	}
}

func TestEditGameInvalidStatusRejected(t *testing.T) {
	a := newTestApp(t)
	id := addGame(t, a, "Game", "/games/game")
	bad := "bogus"
	if err := a.EditGame(id, EditGameFields{Status: &bad}); err == nil {
		t.Error("EditGame with invalid status: expected error, got nil")
	}
}

func TestEditGameEmptyTitleRejected(t *testing.T) {
	a := newTestApp(t)
	id := addGame(t, a, "Game", "/games/game")
	empty := "   "
	if err := a.EditGame(id, EditGameFields{Title: &empty}); err == nil {
		t.Error("EditGame with blank title: expected error, got nil")
	}
}

// Editing the F95Zone URL must resync the canonical thread ID the syncer
// actually scrapes. ResolveScrapeURL prefers F95ThreadID, so a stale ID makes
// the corrected URL a no-op and ApplyThreadData then reverts F95URL to the old
// thread — the edit silently undoes itself on the next sync.
func TestEditGameF95URLEditUpdatesThreadID(t *testing.T) {
	a := newTestApp(t)
	id := addGame(t, a, "Game", "/games/game")
	if err := a.db.UpdateGame(&db.Game{
		ID: id, Title: "Game", Path: "/games/game", Engine: "RenPy", Status: "active",
		F95URL: "https://f95zone.to/threads/wrong-slug.11111/", F95ThreadID: 11111,
	}); err != nil {
		t.Fatalf("UpdateGame: %v", err)
	}

	newURL := "https://f95zone.to/threads/right-slug.22222/"
	if err := a.EditGame(id, EditGameFields{F95URL: &newURL}); err != nil {
		t.Fatalf("EditGame: %v", err)
	}

	g, err := a.db.GetGame(id)
	if err != nil || g == nil {
		t.Fatalf("GetGame: %v", err)
	}
	if g.F95URL != newURL {
		t.Errorf("F95URL = %q, want %q", g.F95URL, newURL)
	}
	if g.F95ThreadID != 22222 {
		t.Errorf("F95ThreadID = %d, want 22222 (must follow the edited URL)", g.F95ThreadID)
	}
}

// Re-saving an already-correct URL repairs a thread ID that drifted out of
// sync before the fix, so users don't have to retype a working URL.
func TestEditGameSameURLRepairsStaleThreadID(t *testing.T) {
	a := newTestApp(t)
	id := addGame(t, a, "Game", "/games/game")
	url := "https://f95zone.to/threads/right-slug.22222/"
	if err := a.db.UpdateGame(&db.Game{
		ID: id, Title: "Game", Path: "/games/game", Engine: "RenPy", Status: "active",
		F95URL: url, F95ThreadID: 11111,
	}); err != nil {
		t.Fatalf("UpdateGame: %v", err)
	}

	if err := a.EditGame(id, EditGameFields{F95URL: &url}); err != nil {
		t.Fatalf("EditGame: %v", err)
	}

	g, _ := a.db.GetGame(id)
	if g.F95ThreadID != 22222 {
		t.Errorf("F95ThreadID = %d, want 22222 (repaired from the URL)", g.F95ThreadID)
	}
}

// Clearing the URL drops the stale thread ID so the game is truly
// disassociated rather than silently re-synced by ID.
func TestEditGameClearingF95URLDropsThreadID(t *testing.T) {
	a := newTestApp(t)
	id := addGame(t, a, "Game", "/games/game")
	if err := a.db.UpdateGame(&db.Game{
		ID: id, Title: "Game", Path: "/games/game", Engine: "RenPy", Status: "active",
		F95URL: "https://f95zone.to/threads/game.12345/", F95ThreadID: 12345,
	}); err != nil {
		t.Fatalf("UpdateGame: %v", err)
	}

	empty := ""
	if err := a.EditGame(id, EditGameFields{F95URL: &empty}); err != nil {
		t.Fatalf("EditGame: %v", err)
	}

	g, _ := a.db.GetGame(id)
	if g.F95URL != "" || g.F95ThreadID != 0 {
		t.Errorf("F95URL/F95ThreadID = %q/%d, want cleared/0", g.F95URL, g.F95ThreadID)
	}
}

// An unrelated edit (URL field omitted) must leave the association untouched.
func TestEditGameUnrelatedEditKeepsThreadID(t *testing.T) {
	a := newTestApp(t)
	id := addGame(t, a, "Game", "/games/game")
	url := "https://f95zone.to/threads/game.12345/"
	if err := a.db.UpdateGame(&db.Game{
		ID: id, Title: "Game", Path: "/games/game", Engine: "RenPy", Status: "active",
		F95URL: url, F95ThreadID: 12345,
	}); err != nil {
		t.Fatalf("UpdateGame: %v", err)
	}

	note := "hello"
	if err := a.EditGame(id, EditGameFields{Notes: &note}); err != nil {
		t.Fatalf("EditGame: %v", err)
	}

	g, _ := a.db.GetGame(id)
	if g.F95URL != url || g.F95ThreadID != 12345 {
		t.Errorf("unrelated edit changed association: %q/%d, want %q/12345", g.F95URL, g.F95ThreadID, url)
	}
}
