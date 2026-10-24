package db

import "testing"

func TestGamesNeedingUpdate_ExcludesSameAndOlderVersions(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	games := []Game{
		{Title: "Genuine update", Engine: "RenPy", Path: "/games/update", Version: "0.9", LatestVersion: "1.0", Status: "active"},
		{Title: "Same release", Engine: "RenPy", Path: "/games/same", Version: "1.0", LatestVersion: "1.0", Status: "active"},
		{Title: "Reformatted same", Engine: "RenPy", Path: "/games/reformat", Version: "1.0.0", LatestVersion: "1.0", Status: "active"},
		{Title: "Thread reverted", Engine: "RenPy", Path: "/games/reverted", Version: "1.2", LatestVersion: "1.1", Status: "active"},
		{Title: "No latest", Engine: "RenPy", Path: "/games/nolatest", Version: "1.0", Status: "active"},
	}
	for i := range games {
		if _, err := database.InsertGame(&games[i]); err != nil {
			t.Fatalf("insert %s: %v", games[i].Title, err)
		}
	}

	pending, err := database.GamesNeedingUpdate()
	if err != nil {
		t.Fatalf("GamesNeedingUpdate: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected exactly 1 pending update, got %d: %+v", len(pending), pending)
	}
	if pending[0].Title != "Genuine update" {
		t.Errorf("pending game = %q, want %q", pending[0].Title, "Genuine update")
	}

	count, err := database.CountGamesNeedingUpdate()
	if err != nil {
		t.Fatalf("CountGamesNeedingUpdate: %v", err)
	}
	if count != len(pending) {
		t.Errorf("CountGamesNeedingUpdate = %d, list returned %d", count, len(pending))
	}
}

func TestGamesNeedingUpdate_QueuesUnorderableChange(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	// A date-form thread version replacing a numeric local one cannot be
	// ordered; the queue surfaces it for a person to decide.
	g := &Game{Title: "Unorderable", Engine: "RenPy", Path: "/games/unorderable",
		Version: "1.0", LatestVersion: "2026-10-01", Status: "active"}
	if _, err := database.InsertGame(g); err != nil {
		t.Fatal(err)
	}

	pending, err := database.GamesNeedingUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected the ambiguous change to be queued, got %d", len(pending))
	}
}
