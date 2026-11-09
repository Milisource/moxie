package db

import (
	"testing"
)

func insertTestGame(t *testing.T, db *Database, title, path, status string) int64 {
	t.Helper()
	id, err := db.InsertGame(&Game{Title: title, Path: path, Status: status, Engine: "Unknown"})
	if err != nil {
		t.Fatalf("InsertGame(%q): %v", title, err)
	}
	return id
}

func TestCountGamesByStatus(t *testing.T) {
	db := setupTestDB(t)

	insertTestGame(t, db, "A", "/games/a", "active")
	insertTestGame(t, db, "B", "/games/b", "active")
	insertTestGame(t, db, "C", "/games/c", "completed")
	blank := insertTestGame(t, db, "D", "/games/d", "")
	deleted := insertTestGame(t, db, "E", "/games/e", "active")
	backup := insertTestGame(t, db, "F", "/games/f.old", "active")

	if err := db.DeleteGame(deleted); err != nil {
		t.Fatalf("DeleteGame: %v", err)
	}

	counts, err := db.CountGamesByStatus()
	if err != nil {
		t.Fatalf("CountGamesByStatus: %v", err)
	}

	if counts["active"] != 2 {
		t.Errorf("active = %d, want 2 (soft-deleted and .old must be excluded)", counts["active"])
	}
	if counts["completed"] != 1 {
		t.Errorf("completed = %d, want 1", counts["completed"])
	}
	// A game with no status recorded is grouped under "unknown".
	if counts["unknown"] < 1 {
		t.Errorf("unknown = %d, want at least 1 (game %d has blank status)", counts["unknown"], blank)
	}
	if _, ok := counts[""]; ok {
		t.Error("blank status must be normalised to \"unknown\", not left empty")
	}
	_ = backup
}

// The count must agree with the list it summarises, or the sidebar badge and
// the updates view disagree.
func TestCountGamesNeedingUpdateMatchesList(t *testing.T) {
	db := setupTestDB(t)

	mk := func(title, path, version, latest string) {
		t.Helper()
		id := insertTestGame(t, db, title, path, "active")
		g, err := db.GetGame(id)
		if err != nil || g == nil {
			t.Fatalf("GetGame(%d): %v", id, err)
		}
		g.Version = version
		g.LatestVersion = latest
		if err := db.UpdateGame(g); err != nil {
			t.Fatalf("UpdateGame: %v", err)
		}
	}

	mk("NeedsUpdate", "/games/one", "0.1", "0.2")
	mk("UpToDate", "/games/two", "0.3", "0.3")
	mk("NoLatest", "/games/three", "0.4", "")
	mk("Backup", "/games/four.old", "0.1", "0.2")

	list, err := db.GamesNeedingUpdate()
	if err != nil {
		t.Fatalf("GamesNeedingUpdate: %v", err)
	}
	n, err := db.CountGamesNeedingUpdate()
	if err != nil {
		t.Fatalf("CountGamesNeedingUpdate: %v", err)
	}

	if n != len(list) {
		t.Errorf("CountGamesNeedingUpdate = %d, GamesNeedingUpdate returned %d", n, len(list))
	}
	if n != 1 {
		t.Errorf("count = %d, want 1", n)
	}
}

func TestPlaysForGameIsScopedToOneGame(t *testing.T) {
	db := setupTestDB(t)

	a := insertTestGame(t, db, "A", "/games/a", "active")
	b := insertTestGame(t, db, "B", "/games/b", "active")

	for i := 0; i < 3; i++ {
		if err := db.RecordPlay(a, "linux"); err != nil {
			t.Fatalf("RecordPlay(a): %v", err)
		}
	}
	if err := db.RecordPlay(b, "linux"); err != nil {
		t.Fatalf("RecordPlay(b): %v", err)
	}

	plays, err := db.PlaysForGame(a, 200)
	if err != nil {
		t.Fatalf("PlaysForGame: %v", err)
	}
	if len(plays) != 3 {
		t.Fatalf("got %d plays for game A, want 3", len(plays))
	}
	for _, p := range plays {
		if p.GameID != a {
			t.Errorf("PlaysForGame(%d) returned an entry for game %d", a, p.GameID)
		}
	}

	// The limit must be respected.
	limited, err := db.PlaysForGame(a, 2)
	if err != nil {
		t.Fatalf("PlaysForGame limited: %v", err)
	}
	if len(limited) != 2 {
		t.Errorf("limit 2 returned %d rows", len(limited))
	}
}

func TestCountGamesPerCollection(t *testing.T) {
	db := setupTestDB(t)

	favs, err := db.CreateCollection("Favorites")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	empty, err := db.CreateCollection("Empty")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	a := insertTestGame(t, db, "A", "/games/a", "active")
	b := insertTestGame(t, db, "B", "/games/b", "active")
	trashed := insertTestGame(t, db, "Trashed", "/games/c", "active")

	for _, id := range []int64{a, b, trashed} {
		if err := db.AddGameToCollection(id, favs.ID); err != nil {
			t.Fatalf("AddGameToCollection: %v", err)
		}
	}
	if err := db.DeleteGame(trashed); err != nil {
		t.Fatalf("DeleteGame: %v", err)
	}

	counts, err := db.CountGamesPerCollection()
	if err != nil {
		t.Fatalf("CountGamesPerCollection: %v", err)
	}

	if counts[favs.ID] != 2 {
		t.Errorf("Favorites = %d, want 2 (soft-deleted members must not count)", counts[favs.ID])
	}
	if n, ok := counts[empty.ID]; ok {
		t.Errorf("empty collection present with count %d; want absent", n)
	}

	// The count must agree with the list the UI renders.
	games, err := db.GetGamesInCollection(favs.ID)
	if err != nil {
		t.Fatalf("GetGamesInCollection: %v", err)
	}
	active := 0
	for _, g := range games {
		if g != nil && g.DeletedAt.IsZero() {
			active++
		}
	}
	if active != counts[favs.ID] {
		t.Errorf("count %d disagrees with %d active games in the list", counts[favs.ID], active)
	}
}
