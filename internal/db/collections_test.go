package db

import "testing"

// TestGetGamesInCollection guards against the SELECT column list drifting out
// of sync with scanGame. Every column scanGame reads must appear here, in
// order — a mismatch surfaces as "expected N destination arguments in Scan".
func TestGetGamesInCollection(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	coll, err := db.CreateCollection("Favorites")
	if err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}

	game := &Game{
		Title:      "Test Game",
		Engine:     "RenPy",
		Path:       "/games/test",
		Version:    "1.0",
		SizeBytes:  1024,
		Status:     "unknown",
		WinePrefix: "/prefixes/test",
	}
	gameID, err := db.InsertGame(game)
	if err != nil {
		t.Fatalf("InsertGame failed: %v", err)
	}
	if err := db.AddGameToCollection(gameID, coll.ID); err != nil {
		t.Fatalf("AddGameToCollection failed: %v", err)
	}

	games, err := db.GetGamesInCollection(coll.ID)
	if err != nil {
		t.Fatalf("GetGamesInCollection failed: %v", err)
	}
	if len(games) != 1 {
		t.Fatalf("got %d games, want 1", len(games))
	}
	if games[0].Title != "Test Game" {
		t.Errorf("Title = %q, want %q", games[0].Title, "Test Game")
	}
	if games[0].WinePrefix != "/prefixes/test" {
		t.Errorf("WinePrefix = %q, want %q", games[0].WinePrefix, "/prefixes/test")
	}
}

// TestGetGamesInCollectionExcludesDeleted verifies soft-deleted games drop out.
func TestGetGamesInCollectionExcludesDeleted(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	coll, err := db.CreateCollection("Favorites")
	if err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}
	gameID, err := db.InsertGame(&Game{
		Title:  "Trashed",
		Engine: "RenPy",
		Path:   "/games/trashed",
		Status: "unknown",
	})
	if err != nil {
		t.Fatalf("InsertGame failed: %v", err)
	}
	if err := db.AddGameToCollection(gameID, coll.ID); err != nil {
		t.Fatalf("AddGameToCollection failed: %v", err)
	}
	if err := db.DeleteGame(gameID); err != nil {
		t.Fatalf("DeleteGame failed: %v", err)
	}

	games, err := db.GetGamesInCollection(coll.ID)
	if err != nil {
		t.Fatalf("GetGamesInCollection failed: %v", err)
	}
	if len(games) != 0 {
		t.Fatalf("got %d games, want 0 (soft-deleted)", len(games))
	}
}
