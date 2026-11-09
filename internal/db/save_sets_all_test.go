package db

import (
	"testing"
	"time"
)

func TestListAllSaveSets(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	gameA := insertSaveTestGame(t, db, "All A", "/all-a")
	gameB := insertSaveTestGame(t, db, "All B", "/all-b")

	older := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)

	if _, err := db.CreateSaveSet(&SaveSet{GameID: gameA, Name: "a-old", CreatedAt: older}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateSaveSet(&SaveSet{GameID: gameA, Name: "a-new", CreatedAt: newer}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateSaveSet(&SaveSet{GameID: gameB, Name: "b-only", CreatedAt: newer}); err != nil {
		t.Fatal(err)
	}

	sets, err := db.ListAllSaveSets()
	if err != nil {
		t.Fatalf("ListAllSaveSets: %v", err)
	}
	if len(sets) != 3 {
		t.Fatalf("len = %d, want 3", len(sets))
	}

	// Ordered by game_id ascending, then newest first within each game.
	want := []struct {
		gameID int64
		name   string
	}{
		{gameA, "a-new"},
		{gameA, "a-old"},
		{gameB, "b-only"},
	}
	for i, w := range want {
		if sets[i].GameID != w.gameID || sets[i].Name != w.name {
			t.Errorf("sets[%d] = (game %d, %q), want (game %d, %q)",
				i, sets[i].GameID, sets[i].Name, w.gameID, w.name)
		}
	}
}
