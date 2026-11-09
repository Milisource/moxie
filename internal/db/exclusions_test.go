package db

import "testing"

// SoftDeleteAndExclude must both trash the row and record its path, so a
// scan cannot resurrect a resolved duplicate.
func TestSoftDeleteAndExclude(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	id := insertTestGame(t, d, "Dup", "/games/Dup", "active")

	if err := d.SoftDeleteAndExclude(id, "duplicate"); err != nil {
		t.Fatalf("SoftDeleteAndExclude: %v", err)
	}

	g, err := d.GetGameByPath("/games/Dup")
	if err != nil || g == nil {
		t.Fatalf("GetGameByPath: %v", err)
	}
	if g.DeletedAt.IsZero() {
		t.Error("game should be soft-deleted")
	}

	excluded, err := d.ExcludedPaths()
	if err != nil {
		t.Fatalf("ExcludedPaths: %v", err)
	}
	if !excluded["/games/Dup"] {
		t.Errorf("excluded = %v, want /games/Dup", excluded)
	}

	if games, err := d.ListActiveGames("", ""); err != nil || len(games) != 0 {
		t.Errorf("ListActiveGames = %d games (%v), want 0", len(games), err)
	}
}

func TestSoftDeleteAndExclude_UnknownID(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	if err := d.SoftDeleteAndExclude(999, "duplicate"); err == nil {
		t.Error("expected error for unknown game id")
	}
}

// Restoring a game must clear its scan exclusion so it is scannable again.
func TestRestoreGameClearsExclusion(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	id := insertTestGame(t, d, "Dup", "/games/Dup", "active")
	if err := d.SoftDeleteAndExclude(id, "duplicate"); err != nil {
		t.Fatalf("SoftDeleteAndExclude: %v", err)
	}

	if err := d.RestoreGame(id); err != nil {
		t.Fatalf("RestoreGame: %v", err)
	}

	g, err := d.GetGameByPath("/games/Dup")
	if err != nil || g == nil {
		t.Fatalf("GetGameByPath: %v", err)
	}
	if !g.DeletedAt.IsZero() {
		t.Error("game should be active after restore")
	}
	excluded, err := d.ExcludedPaths()
	if err != nil {
		t.Fatalf("ExcludedPaths: %v", err)
	}
	if excluded["/games/Dup"] {
		t.Errorf("exclusion for /games/Dup should be cleared, got %v", excluded)
	}
}

// ExcludePath is idempotent and UnexcludePath is a no-op when absent.
func TestExcludePath_IdempotentAndUnexclude(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	for i := 0; i < 2; i++ {
		if err := d.ExcludePath("/games/NoRow", "duplicate"); err != nil {
			t.Fatalf("ExcludePath #%d: %v", i, err)
		}
	}
	excluded, err := d.ExcludedPaths()
	if err != nil {
		t.Fatalf("ExcludedPaths: %v", err)
	}
	if !excluded["/games/NoRow"] {
		t.Fatalf("excluded = %v, want /games/NoRow", excluded)
	}

	if err := d.UnexcludePath("/games/NoRow"); err != nil {
		t.Fatalf("UnexcludePath: %v", err)
	}
	if err := d.UnexcludePath("/games/Absent"); err != nil {
		t.Fatalf("UnexcludePath(absent): %v", err)
	}
	excluded, err = d.ExcludedPaths()
	if err != nil {
		t.Fatalf("ExcludedPaths: %v", err)
	}
	if len(excluded) != 0 {
		t.Errorf("excluded = %v, want empty", excluded)
	}
}
