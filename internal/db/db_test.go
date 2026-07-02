package db

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// testingTB is satisfied by both *testing.T and *testing.B so that
// setupTestDB can be used from benchmarks as well.
type testingTB interface {
	Helper()
	Fatal(args ...any)
	Fatalf(format string, args ...any)
	Cleanup(func())
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func setupTestDB(t testingTB) *Database {
	t.Helper()
	f, err := os.CreateTemp("", "test-*.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	f.Close()
	t.Cleanup(func() { os.Remove(path) })

	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// ---------------------------------------------------------------------------
// Open / Close
// ---------------------------------------------------------------------------

func TestOpenClose(t *testing.T) {
	f, err := os.CreateTemp("", "test-*.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	if db == nil {
		t.Fatal("Open returned nil Database")
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// InsertGame / GetGame
// ---------------------------------------------------------------------------

func TestInsertAndGetGame(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	g := &Game{
		Title:       "Test Game",
		Engine:      "RenPy",
		Path:        "/games/test-game",
		ExePath:     "/games/test-game/game.exe",
		Version:     "1.0",
		SizeBytes:   1024,
		F95URL:      "https://f95zone.to/threads/12345",
		F95ThreadID: 12345,
		Tags:        []string{"adult", "rpg", "fantasy"},
		Status:      "active",
		Notes:       "A test game for unit testing",
	}

	id, err := db.InsertGame(g)
	if err != nil {
		t.Fatalf("InsertGame failed: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero ID after insert")
	}
	if g.ID != id {
		t.Errorf("g.ID = %d, want %d", g.ID, id)
	}

	got, err := db.GetGame(id)
	if err != nil {
		t.Fatalf("GetGame failed: %v", err)
	}
	if got == nil {
		t.Fatal("GetGame returned nil")
	}

	// Basic fields
	if got.Title != "Test Game" {
		t.Errorf("Title = %q, want %q", got.Title, "Test Game")
	}
	if got.Engine != "RenPy" {
		t.Errorf("Engine = %q, want %q", got.Engine, "RenPy")
	}
	if got.Path != "/games/test-game" {
		t.Errorf("Path = %q, want %q", got.Path, "/games/test-game")
	}
	if got.ExePath != "/games/test-game/game.exe" {
		t.Errorf("ExePath = %q, want %q", got.ExePath, "/games/test-game/game.exe")
	}
	if got.Version != "1.0" {
		t.Errorf("Version = %q, want %q", got.Version, "1.0")
	}
	if got.SizeBytes != 1024 {
		t.Errorf("SizeBytes = %d, want %d", got.SizeBytes, 1024)
	}
	if got.F95URL != "https://f95zone.to/threads/12345" {
		t.Errorf("F95URL = %q, want %q", got.F95URL, "https://f95zone.to/threads/12345")
	}
	if got.F95ThreadID != 12345 {
		t.Errorf("F95ThreadID = %d, want %d", got.F95ThreadID, 12345)
	}
	if got.Status != "active" {
		t.Errorf("Status = %q, want %q", got.Status, "active")
	}
	if got.Notes != "A test game for unit testing" {
		t.Errorf("Notes = %q, want %q", got.Notes, "A test game for unit testing")
	}

	// Tags
	if len(got.Tags) != 3 {
		t.Fatalf("len(Tags) = %d, want 3", len(got.Tags))
	}
	if got.Tags[0] != "adult" || got.Tags[1] != "rpg" || got.Tags[2] != "fantasy" {
		t.Errorf("Tags = %v, want [adult rpg fantasy]", got.Tags)
	}

	// Timestamps
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should not be zero")
	}
	if got.UpdatedAt.Before(got.CreatedAt) {
		t.Error("UpdatedAt should not be before CreatedAt")
	}
}

func TestInsertGameEmptyTags(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	g := &Game{
		Title:  "No Tags",
		Engine: "Unity",
		Path:   "/no-tags",
	}
	id, err := db.InsertGame(g)
	if err != nil {
		t.Fatalf("InsertGame failed: %v", err)
	}

	got, err := db.GetGame(id)
	if err != nil {
		t.Fatalf("GetGame failed: %v", err)
	}
	if got == nil {
		t.Fatal("GetGame returned nil")
	}
	if len(got.Tags) != 0 {
		t.Errorf("Tags = %v, want empty slice", got.Tags)
	}
}

func TestGetGameNotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	got, err := db.GetGame(99999)
	if err != nil {
		t.Fatalf("GetGame failed: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for non-existent game")
	}
}

// ---------------------------------------------------------------------------
// GetGameByPath
// ---------------------------------------------------------------------------

func TestGetGameByPath(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	g := &Game{
		Title:  "Path Test",
		Engine: "Unity",
		Path:   "/unique/path",
	}
	if _, err := db.InsertGame(g); err != nil {
		t.Fatal(err)
	}

	// Found
	got, err := db.GetGameByPath("/unique/path")
	if err != nil {
		t.Fatalf("GetGameByPath failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected game, got nil")
	}
	if got.Title != "Path Test" {
		t.Errorf("Title = %q, want %q", got.Title, "Path Test")
	}

	// Not found
	got, err = db.GetGameByPath("/nonexistent")
	if err != nil {
		t.Fatalf("GetGameByPath failed: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for non-existent path")
	}
}

// ---------------------------------------------------------------------------
// ListGames
// ---------------------------------------------------------------------------

func TestListGames(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	games := []Game{
		{Title: "Alpha", Engine: "Unity", Status: "active", Path: "/a"},
		{Title: "Beta", Engine: "RenPy", Status: "active", Path: "/b"},
		{Title: "Gamma", Engine: "Unity", Status: "completed", Path: "/c"},
		{Title: "Delta", Engine: "RPGM", Status: "abandoned", Path: "/d"},
	}
	for i := range games {
		if _, err := db.InsertGame(&games[i]); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("no filter", func(t *testing.T) {
		all, err := db.ListGames("", "")
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 4 {
			t.Fatalf("expected 4 games, got %d", len(all))
		}
	})

	t.Run("engine filter", func(t *testing.T) {
		unity, err := db.ListGames("Unity", "")
		if err != nil {
			t.Fatal(err)
		}
		if len(unity) != 2 {
			t.Fatalf("expected 2 Unity games, got %d", len(unity))
		}
		for _, g := range unity {
			if g.Engine != "Unity" {
				t.Errorf("game %q has engine %q", g.Title, g.Engine)
			}
		}
	})

	t.Run("status filter", func(t *testing.T) {
		completed, err := db.ListGames("", "completed")
		if err != nil {
			t.Fatal(err)
		}
		if len(completed) != 1 {
			t.Fatalf("expected 1 completed game, got %d", len(completed))
		}
		if completed[0].Title != "Gamma" {
			t.Errorf("expected Gamma, got %q", completed[0].Title)
		}
	})

	t.Run("both filters", func(t *testing.T) {
		filtered, err := db.ListGames("Unity", "completed")
		if err != nil {
			t.Fatal(err)
		}
		if len(filtered) != 1 {
			t.Fatalf("expected 1 game, got %d", len(filtered))
		}
		if filtered[0].Title != "Gamma" {
			t.Errorf("expected Gamma, got %q", filtered[0].Title)
		}
	})

	t.Run("no matches", func(t *testing.T) {
		none, err := db.ListGames("Tads", "active")
		if err != nil {
			t.Fatal(err)
		}
		if len(none) != 0 {
			t.Fatalf("expected 0 games, got %d", len(none))
		}
	})
}

// ---------------------------------------------------------------------------
// SearchGames
// ---------------------------------------------------------------------------

func TestSearchGames(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	games := []Game{
		{Title: "The Witcher", Engine: "RenPy", Path: "/witcher"},
		{Title: "Cyberpunk 2077", Engine: "Unity", Path: "/cyberpunk"},
		{Title: "Witch Hunter", Engine: "RPGM", Path: "/witch"},
	}
	for i := range games {
		if _, err := db.InsertGame(&games[i]); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("full word match", func(t *testing.T) {
		results, err := db.SearchGames("Witch")
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}
	})

	t.Run("partial match", func(t *testing.T) {
		results, err := db.SearchGames("itch")
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}
	})

	t.Run("no match", func(t *testing.T) {
		results, err := db.SearchGames("ZZZZZ")
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 0 {
			t.Fatalf("expected 0 results, got %d", len(results))
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		results, err := db.SearchGames("witch")
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}
	})

	t.Run("empty query", func(t *testing.T) {
		results, err := db.SearchGames("")
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 3 {
			t.Fatalf("expected 3 results, got %d", len(results))
		}
	})
}

// ---------------------------------------------------------------------------
// UpdateGame
// ---------------------------------------------------------------------------

func TestUpdateGame(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	g := &Game{
		Title:    "Original Title",
		Engine:   "Unity",
		Path:     "/update-test",
		SizeBytes: 500,
		Tags:     []string{"original"},
		Status:   "active",
	}
	id, err := db.InsertGame(g)
	if err != nil {
		t.Fatal(err)
	}

	// Capture original CreatedAt for later comparison.
	originalCreated := g.CreatedAt

	// Sleep to ensure the stored timestamp (RFC3339, second precision) changes.
	time.Sleep(1100 * time.Millisecond)

	// Update fields.
	g.Title = "Updated Title"
	g.Engine = "RenPy"
	g.Version = "2.0"
	g.Status = "completed"
	g.Tags = []string{"updated"}
	g.Notes = "Updated notes"
	g.SizeBytes = 999

	if err := db.UpdateGame(g); err != nil {
		t.Fatalf("UpdateGame failed: %v", err)
	}

	got, err := db.GetGame(id)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("GetGame returned nil after update")
	}

	if got.Title != "Updated Title" {
		t.Errorf("Title = %q, want %q", got.Title, "Updated Title")
	}
	if got.Engine != "RenPy" {
		t.Errorf("Engine = %q, want %q", got.Engine, "RenPy")
	}
	if got.Version != "2.0" {
		t.Errorf("Version = %q, want %q", got.Version, "2.0")
	}
	if got.Status != "completed" {
		t.Errorf("Status = %q, want %q", got.Status, "completed")
	}
	if got.SizeBytes != 999 {
		t.Errorf("SizeBytes = %d, want %d", got.SizeBytes, 999)
	}
	if got.Notes != "Updated notes" {
		t.Errorf("Notes = %q, want %q", got.Notes, "Updated notes")
	}
	if len(got.Tags) != 1 || got.Tags[0] != "updated" {
		t.Errorf("Tags = %v, want [updated]", got.Tags)
	}

	// CreatedAt should remain unchanged.
	if !got.CreatedAt.Equal(originalCreated) {
		t.Error("CreatedAt should not change after update")
	}
	// UpdatedAt should have advanced.
	if !got.UpdatedAt.After(got.CreatedAt) {
		t.Error("UpdatedAt should be after CreatedAt after update")
	}
}

// ---------------------------------------------------------------------------
// DeleteGame
// ---------------------------------------------------------------------------

func TestDeleteGame(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	g := &Game{Title: "Delete Me", Engine: "Unity", Path: "/delete-me"}
	id, err := db.InsertGame(g)
	if err != nil {
		t.Fatal(err)
	}

	// Soft delete — game should still exist with deleted_at set.
	if err := db.DeleteGame(id); err != nil {
		t.Fatalf("DeleteGame failed: %v", err)
	}

	got, err := db.GetGame(id)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("GetGame should still return the game after soft delete")
	}
	if got.DeletedAt.IsZero() {
		t.Fatal("deleted_at should be set after soft delete")
	}

	// The game should NOT appear in active lists.
	active, err := db.ListActiveGames("", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range active {
		if a.ID == id {
			t.Fatal("soft-deleted game should not appear in ListActiveGames")
		}
	}

	// But it SHOULD appear in ListDeletedGames.
	deleted, err := db.ListDeletedGames()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range deleted {
		if d.ID == id {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("soft-deleted game should appear in ListDeletedGames")
	}

	// Permanent delete.
	if err := db.DeleteGamePermanent(id); err != nil {
		t.Fatalf("DeleteGamePermanent failed: %v", err)
	}
	got, err = db.GetGame(id)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatal("GetGame should return nil after permanent delete")
	}

	// Deleting a non-existent ID should not error.
	if err := db.DeleteGame(99999); err != nil {
		t.Errorf("DeleteGame on non-existent ID: %v", err)
	}

	// Restore test.
	id2, err := db.InsertGame(&Game{Title: "Restore Me", Engine: "Unity", Path: "/restore-me"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteGame(id2); err != nil {
		t.Fatal(err)
	}
	if err := db.RestoreGame(id2); err != nil {
		t.Fatalf("RestoreGame failed: %v", err)
	}
	got, err = db.GetGame(id2)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("GetGame should return game after restore")
	}
	if !got.DeletedAt.IsZero() {
		t.Fatal("deleted_at should be cleared after restore")
	}
}

// ---------------------------------------------------------------------------
// ScrapedMeta
// ---------------------------------------------------------------------------

func TestScrapedMeta(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Create a parent game first (foreign key).
	g := &Game{Title: "Scrape Target", Engine: "RenPy", Path: "/scrape-target"}
	id, err := db.InsertGame(g)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("insert and get", func(t *testing.T) {
		m := &ScrapedMeta{
			GameID:    id,
			Developer: "DevStudio",
			Overview:  "An amazing adult game with deep story.",
			CoverURL:  "https://example.com/cover.jpg",
		}

		if err := db.UpsertScrapedMeta(m); err != nil {
			t.Fatalf("UpsertScrapedMeta failed: %v", err)
		}

		got, err := db.GetScrapedMeta(id)
		if err != nil {
			t.Fatalf("GetScrapedMeta failed: %v", err)
		}
		if got == nil {
			t.Fatal("expected meta, got nil")
		}

		if got.GameID != id {
			t.Errorf("GameID = %d, want %d", got.GameID, id)
		}
		if got.Developer != "DevStudio" {
			t.Errorf("Developer = %q, want %q", got.Developer, "DevStudio")
		}
		if got.Overview != "An amazing adult game with deep story." {
			t.Errorf("Overview = %q, want %q", got.Overview, "An amazing adult game with deep story.")
		}
		if got.CoverURL != "https://example.com/cover.jpg" {
			t.Errorf("CoverURL = %q, want %q", got.CoverURL, "https://example.com/cover.jpg")
		}
		if got.LastScraped.IsZero() {
			t.Error("LastScraped should not be zero")
		}
	})

	t.Run("upsert replaces", func(t *testing.T) {
		m := &ScrapedMeta{
			GameID:    id,
			Developer: "NewStudio",
			Overview:  "",
			CoverURL:  "",
		}

		if err := db.UpsertScrapedMeta(m); err != nil {
			t.Fatalf("UpsertScrapedMeta failed: %v", err)
		}

		got, err := db.GetScrapedMeta(id)
		if err != nil {
			t.Fatal(err)
		}
		if got.Developer != "NewStudio" {
			t.Errorf("Developer = %q, want %q", got.Developer, "NewStudio")
		}
		if got.Overview != "" {
			t.Errorf("Overview should be empty, got %q", got.Overview)
		}
	})

	t.Run("not found", func(t *testing.T) {
		got, err := db.GetScrapedMeta(99999)
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Fatal("expected nil for non-existent scraped_meta")
		}
	})
}

func TestScrapedMetaCascadeDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping cascade delete test in short mode")
	}

	db := setupTestDB(t)
	defer db.Close()

	g := &Game{Title: "Cascade", Engine: "Unity", Path: "/cascade"}
	id, _ := db.InsertGame(g)

	m := &ScrapedMeta{
		GameID:    id,
		Developer: "CascadeDev",
	}
	if err := db.UpsertScrapedMeta(m); err != nil {
		t.Fatal(err)
	}

	// Permanently delete the parent game — scraped_meta should cascade.
	if err := db.DeleteGamePermanent(id); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetScrapedMeta(id)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatal("scraped_meta should be cascade-deleted with the game")
	}
}

// ---------------------------------------------------------------------------
// Aggregates
// ---------------------------------------------------------------------------

func TestGameCountAndTotalSize(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	t.Run("empty database", func(t *testing.T) {
		count, err := db.GameCount()
		if err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("expected 0, got %d", count)
		}

		size, err := db.TotalSize()
		if err != nil {
			t.Fatal(err)
		}
		if size != 0 {
			t.Errorf("expected 0, got %d", size)
		}
	})

	t.Run("with games", func(t *testing.T) {
		games := []Game{
			{Title: "Small", Engine: "Unity", Path: "/small", SizeBytes: 100},
			{Title: "Medium", Engine: "RenPy", Path: "/medium", SizeBytes: 200},
			{Title: "Large", Engine: "RPGM", Path: "/large", SizeBytes: 700},
		}
		for i := range games {
			if _, err := db.InsertGame(&games[i]); err != nil {
				t.Fatal(err)
			}
		}

		count, err := db.GameCount()
		if err != nil {
			t.Fatal(err)
		}
		if count != 3 {
			t.Errorf("expected 3, got %d", count)
		}

		size, err := db.TotalSize()
		if err != nil {
			t.Fatal(err)
		}
		if size != 1000 {
			t.Errorf("expected 1000, got %d", size)
		}
	})
}

// ---------------------------------------------------------------------------
// StoreLinks / SteamAppID round-trips
// ---------------------------------------------------------------------------

func TestInsertGetGame_WithStoreLinks(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	g := &Game{
		Title:      "Store Links Test",
		Engine:     "RenPy",
		Path:       "/store-links-test",
		StoreLinks: map[string]string{"steam": "https://store.steampowered.com/app/12345/"},
		SteamAppID: 12345,
	}

	id, err := db.InsertGame(g)
	if err != nil {
		t.Fatalf("InsertGame failed: %v", err)
	}

	got, err := db.GetGame(id)
	if err != nil {
		t.Fatalf("GetGame failed: %v", err)
	}
	if got == nil {
		t.Fatal("GetGame returned nil")
	}

	if len(got.StoreLinks) != 1 {
		t.Fatalf("expected 1 store link, got %d", len(got.StoreLinks))
	}
	if got.StoreLinks["steam"] != "https://store.steampowered.com/app/12345/" {
		t.Errorf(`StoreLinks["steam"] = %q, want %q`,
			got.StoreLinks["steam"], "https://store.steampowered.com/app/12345/")
	}
	if got.SteamAppID != 12345 {
		t.Errorf("SteamAppID = %d, want 12345", got.SteamAppID)
	}
}

func TestInsertGetGame_MultipleStoreLinks(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	links := map[string]string{
		"steam":  "https://store.steampowered.com/app/67890/",
		"itch":   "https://developer.itch.io/game-name",
		"dlsite": "https://www.dlsite.com/work/abc123/",
	}

	g := &Game{
		Title:      "Multi Store Test",
		Engine:     "Unity",
		Path:       "/multi-store-test",
		StoreLinks: links,
		SteamAppID: 67890,
	}

	id, err := db.InsertGame(g)
	if err != nil {
		t.Fatalf("InsertGame failed: %v", err)
	}

	got, err := db.GetGame(id)
	if err != nil {
		t.Fatalf("GetGame failed: %v", err)
	}
	if got == nil {
		t.Fatal("GetGame returned nil")
	}

	if len(got.StoreLinks) != len(links) {
		t.Fatalf("expected %d store links, got %d", len(links), len(got.StoreLinks))
	}
	for k, v := range links {
		if got.StoreLinks[k] != v {
			t.Errorf("StoreLinks[%q] = %q, want %q", k, got.StoreLinks[k], v)
		}
	}
	if got.SteamAppID != 67890 {
		t.Errorf("SteamAppID = %d, want 67890", got.SteamAppID)
	}
}

func TestUpdateGame_StoreLinks(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	g := &Game{
		Title:  "Update Store Links",
		Engine: "RenPy",
		Path:   "/update-store-links",
	}
	id, err := db.InsertGame(g)
	if err != nil {
		t.Fatal(err)
	}

	// Update with store links.
	g.StoreLinks = map[string]string{"steam": "https://store.steampowered.com/app/99999/"}
	g.SteamAppID = 99999
	if err := db.UpdateGame(g); err != nil {
		t.Fatalf("UpdateGame failed: %v", err)
	}

	got, err := db.GetGame(id)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("GetGame returned nil after update")
	}

	if len(got.StoreLinks) != 1 {
		t.Fatalf("expected 1 store link after update, got %d", len(got.StoreLinks))
	}
	if got.StoreLinks["steam"] != "https://store.steampowered.com/app/99999/" {
		t.Errorf(`StoreLinks["steam"] = %q, want %q`,
			got.StoreLinks["steam"], "https://store.steampowered.com/app/99999/")
	}
	if got.SteamAppID != 99999 {
		t.Errorf("SteamAppID = %d, want 99999", got.SteamAppID)
	}

	// Clear store links via update.
	g.StoreLinks = map[string]string{}
	g.SteamAppID = 0
	if err := db.UpdateGame(g); err != nil {
		t.Fatalf("UpdateGame (clear) failed: %v", err)
	}

	got, err = db.GetGame(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.StoreLinks) != 0 {
		t.Errorf("expected empty StoreLinks after clearing, got %d entries", len(got.StoreLinks))
	}
	if got.SteamAppID != 0 {
		t.Errorf("expected SteamAppID = 0 after clearing, got %d", got.SteamAppID)
	}
}

// ---------------------------------------------------------------------------
// Null / optional field round-trips
// ---------------------------------------------------------------------------

func TestOptionalFieldsNull(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	g := &Game{
		Title:  "Minimal Game",
		Engine: "Others",
		Path:   "/minimal",
		// ExePath, Version, F95URL, F95ThreadID, Tags, Notes intentionally zero.
	}
	id, err := db.InsertGame(g)
	if err != nil {
		t.Fatal(err)
	}

	got, err := db.GetGame(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.ExePath != "" {
		t.Errorf("ExePath = %q, want empty", got.ExePath)
	}
	if got.Version != "" {
		t.Errorf("Version = %q, want empty", got.Version)
	}
	if got.F95URL != "" {
		t.Errorf("F95URL = %q, want empty", got.F95URL)
	}
	if got.F95ThreadID != 0 {
		t.Errorf("F95ThreadID = %d, want 0", got.F95ThreadID)
	}
	if got.Notes != "" {
		t.Errorf("Notes = %q, want empty", got.Notes)
	}
	if got.Status != "unknown" {
		t.Errorf("Status = %q, want 'unknown'", got.Status)
	}
	if len(got.StoreLinks) != 0 {
		t.Errorf("StoreLinks = %v, want empty map", got.StoreLinks)
	}
	if got.SteamAppID != 0 {
		t.Errorf("SteamAppID = %d, want 0", got.SteamAppID)
	}
}

// ---------------------------------------------------------------------------
// Duplicate path constraint
// ---------------------------------------------------------------------------

func TestDuplicatePath(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	g1 := &Game{Title: "First", Engine: "Unity", Path: "/duplicate"}
	if _, err := db.InsertGame(g1); err != nil {
		t.Fatal(err)
	}

	g2 := &Game{Title: "Second", Engine: "RenPy", Path: "/duplicate"}
	_, err := db.InsertGame(g2)
	if err == nil {
		t.Fatal("expected error for duplicate path")
	}
}

// ---------------------------------------------------------------------------
// Engine / status CHECK constraint
// ---------------------------------------------------------------------------

func TestInvalidEngine(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	g := &Game{Title: "Bad Engine", Engine: "UnrealEngine", Path: "/bad-engine"}
	if _, err := db.InsertGame(g); err != nil {
		t.Fatalf("UnrealEngine should be valid: %v", err)
	}

	g2 := &Game{Title: "Invalid", Engine: "UnrealEngine5", Path: "/invalid"}
	_, err := db.InsertGame(g2)
	if err == nil {
		t.Fatal("expected error for invalid engine value")
	}
}

func TestInvalidStatus(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	g := &Game{Title: "Bad Status", Engine: "Unity", Path: "/bad-status", Status: "playing"}
	_, err := db.InsertGame(g)
	if err == nil {
		t.Fatal("expected error for invalid status value")
	}
}

// ---------------------------------------------------------------------------
// Benchmark: insert a batch of games
// ---------------------------------------------------------------------------

func BenchmarkInsertGames(b *testing.B) {
	db := setupTestDB(b)
	defer db.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g := &Game{
			Title:  "Bench Game",
			Engine: "Unity",
			Path:   fmt.Sprintf("/bench-%d", i),
		}
		if _, err := db.InsertGame(g); err != nil {
			b.Fatal(err)
		}
	}
}
