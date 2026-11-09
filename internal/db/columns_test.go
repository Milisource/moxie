package db

import (
	"strings"
	"testing"
)

// scanGame reads a fixed number of destinations. If gameColumnNames and that
// scan list ever disagree, every query built from gameColumns fails at runtime
// with "expected N destination arguments in Scan" — the exact failure that hit
// GetGamesInCollection when wine_prefix was added.
func TestGameColumnsMatchScanGame(t *testing.T) {
	db := setupTestDB(t)

	id := insertTestGame(t, db, "Columns", "/games/columns", "active")

	// Every query path that feeds scanGame, exercised against a real row.
	if g, err := db.GetGame(id); err != nil || g == nil {
		t.Fatalf("GetGame: %v (nil=%v)", err, g == nil)
	}
	if g, err := db.GetGameByPath("/games/columns"); err != nil || g == nil {
		t.Fatalf("GetGameByPath: %v (nil=%v)", err, g == nil)
	}
	if _, err := db.ListGames("", ""); err != nil {
		t.Fatalf("ListGames: %v", err)
	}
	if _, err := db.ListActiveGames("", ""); err != nil {
		t.Fatalf("ListActiveGames: %v", err)
	}
	if _, err := db.GamesNeedingUpdate(); err != nil {
		t.Fatalf("GamesNeedingUpdate: %v", err)
	}
	if _, err := db.ListDeletedGames(); err != nil {
		t.Fatalf("ListDeletedGames: %v", err)
	}
	if _, err := db.SearchGames("Columns"); err != nil {
		t.Fatalf("SearchGames: %v", err)
	}

	// The join path — this is the one that silently drifted.
	coll, err := db.CreateCollection("Cols")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if err := db.AddGameToCollection(id, coll.ID); err != nil {
		t.Fatalf("AddGameToCollection: %v", err)
	}
	games, err := db.GetGamesInCollection(coll.ID)
	if err != nil {
		t.Fatalf("GetGamesInCollection: %v", err)
	}
	if len(games) != 1 {
		t.Fatalf("got %d games in collection, want 1", len(games))
	}
}

func TestGameColumnsAsQualifiesEveryName(t *testing.T) {
	qualified := gameColumnsAs("g")
	for _, name := range gameColumnNames {
		if !strings.Contains(qualified, "g."+name) {
			t.Errorf("gameColumnsAs(\"g\") is missing g.%s", name)
		}
	}
	if strings.Count(qualified, ",") != len(gameColumnNames)-1 {
		t.Errorf("qualified list has %d commas, want %d",
			strings.Count(qualified, ","), len(gameColumnNames)-1)
	}
}
