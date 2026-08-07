package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mili/moxie/internal/db"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.sqlite")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return &App{db: database}
}

func addGame(t *testing.T, a *App, title, path string) int64 {
	t.Helper()
	id, err := a.db.InsertGame(&db.Game{Title: title, Path: path, Engine: "Unknown", Status: "active"})
	if err != nil {
		t.Fatalf("InsertGame(%q): %v", title, err)
	}
	return id
}

func TestRemoveMissingUnderDeletesOnlyVanishedDirs(t *testing.T) {
	a := newTestApp(t)
	root := t.TempDir()

	present := filepath.Join(root, "present")
	if err := os.Mkdir(present, 0o755); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(root, "gone")

	presentID := addGame(t, a, "Present", present)
	goneID := addGame(t, a, "Gone", gone)

	if removed := a.removeMissingUnder(root); removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}

	stillHere, _ := a.db.GetGame(presentID)
	if stillHere == nil || !stillHere.DeletedAt.IsZero() {
		t.Error("game with an existing directory must not be soft-deleted")
	}
	deleted, _ := a.db.GetGame(goneID)
	if deleted == nil || deleted.DeletedAt.IsZero() {
		t.Error("game whose directory vanished should be soft-deleted")
	}
}

// A stat failure that is not "does not exist" — an unmounted drive, a
// permission error, ENOTDIR — must never be read as "the game is gone".
func TestRemoveMissingUnderIgnoresAmbiguousStatErrors(t *testing.T) {
	a := newTestApp(t)
	root := t.TempDir()

	// A regular file standing where a parent directory is expected makes
	// os.Stat return ENOTDIR rather than ENOENT.
	blocker := filepath.Join(root, "notadir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	unreachable := filepath.Join(blocker, "game")

	id := addGame(t, a, "Unreachable", unreachable)

	if removed := a.removeMissingUnder(root); removed != 0 {
		t.Errorf("removed = %d, want 0 — an ambiguous stat error must not delete", removed)
	}
	g, _ := a.db.GetGame(id)
	if g == nil || !g.DeletedAt.IsZero() {
		t.Error("game must survive a non-ErrNotExist stat failure")
	}
}

// Re-deleting an already-trashed game would reset deleted_at on every sweep,
// so the 30-day purge would never come due and the UI would report a phantom
// removal each time.
func TestRemoveMissingUnderSkipsAlreadyDeleted(t *testing.T) {
	a := newTestApp(t)
	root := t.TempDir()
	gone := filepath.Join(root, "gone")

	id := addGame(t, a, "Gone", gone)

	if removed := a.removeMissingUnder(root); removed != 1 {
		t.Fatalf("first sweep removed = %d, want 1", removed)
	}
	first, _ := a.db.GetGame(id)
	if first == nil || first.DeletedAt.IsZero() {
		t.Fatal("expected game to be soft-deleted")
	}

	if removed := a.removeMissingUnder(root); removed != 0 {
		t.Errorf("second sweep removed = %d, want 0", removed)
	}
	second, _ := a.db.GetGame(id)
	if second == nil {
		t.Fatal("game disappeared entirely")
	}
	if !second.DeletedAt.Equal(first.DeletedAt) {
		t.Errorf("deleted_at was reset: %v -> %v", first.DeletedAt, second.DeletedAt)
	}
}

func TestRemoveMissingUnderIgnoresOtherRoots(t *testing.T) {
	a := newTestApp(t)
	rootA := t.TempDir()
	rootB := t.TempDir()

	outside := addGame(t, a, "Outside", filepath.Join(rootB, "gone"))

	if removed := a.removeMissingUnder(rootA); removed != 0 {
		t.Errorf("removed = %d, want 0 — game lives under a different root", removed)
	}
	g, _ := a.db.GetGame(outside)
	if g == nil || !g.DeletedAt.IsZero() {
		t.Error("game outside the swept root must be untouched")
	}
}

func TestInstallableTitle(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Simple Game", "Simple Game"},
		{"Path/With/Slashes", "Path-With-Slashes"},
		{"Back\\Slash", "Back-Slash"},
		{"Colon: Subtitle", "Colon- Subtitle"},
		// Quotes and question marks are dropped; asterisks become a dash.
		{"Quote\"Star*Question?", "QuoteStar-Question"},
		{"Pipe|And<Angle>", "Pipe-AndAngle"},
		{"  padded  ", "padded"},
		{"...", "game"},
		{"", "game"},
	}
	for _, c := range cases {
		if got := installableTitle(c.in); got != c.want {
			t.Errorf("installableTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A title must never escape the chosen install directory.
func TestInstallableTitleStaysInsideParent(t *testing.T) {
	parent := t.TempDir()
	for _, evil := range []string{"../escape", "..", "../../etc", "a/../../b"} {
		got := filepath.Join(parent, installableTitle(evil))
		if !isPathUnder(parent, got) {
			t.Errorf("installableTitle(%q) produced %q, which escapes %q", evil, got, parent)
		}
	}
}
