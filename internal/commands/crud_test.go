package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mili/moxie/internal/db"
)

// ---------------------------------------------------------------------------
// Flag ordering (Fix 1) — stdlib flag stops parsing at the first positional
// arg, so flags written after a directory/id must be hoisted ahead of it.
// ---------------------------------------------------------------------------

func TestHoistFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{"flags already first", []string{"--force", "/games"}, []string{"--force", "/games"}},
		{"bool flag after dir", []string{"/games", "--force"}, []string{"--force", "/games"}},
		{"value flag after dir keeps value", []string{"/games", "--engine", "RPGM"}, []string{"--engine", "RPGM", "/games"}},
		{"mixed order", []string{"/games/a", "/games/b", "--force", "--engine", "Unity"}, []string{"--force", "--engine", "Unity", "/games/a", "/games/b"}},
		{"value flag before dir unchanged", []string{"--cookie", "a=1;b=2", "/games"}, []string{"--cookie", "a=1;b=2", "/games"}},
		{"explicit terminator wins", []string{"/games", "--", "--force"}, []string{"/games", "--force"}},
		{"dangling value flag stays a flag", []string{"--engine"}, []string{"--engine"}},
		{"short flag hoisted", []string{"-v", "/games"}, []string{"-v", "/games"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := hoistFlags(c.args, scanValueFlags)
			if strings.Join(got, " ") != strings.Join(c.want, " ") {
				t.Errorf("hoistFlags(%v) = %v, want %v", c.args, got, c.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// RunScan / runScanDir logic (Fixes 2–4)
// ---------------------------------------------------------------------------

// withStdin feeds input to the CLI's "Save to library? (y/N):" prompt and
// silences the scan's stdout/stderr chatter so test output stays clean.
func withStdin(t *testing.T, input string) {
	t.Helper()
	oldIn, oldOut, oldErr := os.Stdin, os.Stdout, os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(input); err != nil {
		t.Fatal(err)
	}
	w.Close()
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	os.Stdout = null
	os.Stderr = null
	t.Cleanup(func() {
		os.Stdin, os.Stdout, os.Stderr = oldIn, oldOut, oldErr
		r.Close()
		null.Close()
	})
}

// mkGame creates a directory the scanner detects as a game (an .exe file).
func mkGame(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Game.exe"), []byte("MZ..."), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// A non-force scan skips known, unchanged directories and must not prompt,
// insert, or touch anything.
func TestRunScan_IncrementalSkipsUnchanged(t *testing.T) {
	withStdin(t, "n\n")
	root := t.TempDir()
	gameDir := mkGame(t, filepath.Join(root, "MyGame v1.0"))

	database := setupTestDB(t)
	defer database.Close()
	if _, err := database.InsertGame(&db.Game{
		Title:    "MyGame",
		Engine:   "RPGM",
		Path:     gameDir,
		Status:   "active",
		DirMTime: dirModTime(gameDir),
	}); err != nil {
		t.Fatal(err)
	}

	if err := RunScan(database, RunScanConfig{Dirs: []string{root}}); err != nil {
		t.Fatalf("RunScan: %v", err)
	}

	games, _ := database.ListActiveGames("", "")
	if len(games) != 1 {
		t.Fatalf("active games = %d, want 1 (unchanged dir skipped, nothing inserted)", len(games))
	}
}

// A renamed game dir + force rescan used to insert a duplicate row and leave
// the old record as a ghost. The shared relocation path must soft-delete the
// ghost and the forced upsert must refresh version/exe with fresh detection.
func TestRunScan_ForceRefreshesAndDedupesAfterRename(t *testing.T) {
	withStdin(t, "y\n")
	root := t.TempDir()
	oldDir := mkGame(t, filepath.Join(root, "MyGame v1.0"))

	database := setupTestDB(t)
	defer database.Close()
	if _, err := database.InsertGame(&db.Game{
		Title:    "MyGame",
		Engine:   "RPGM",
		Path:     oldDir,
		Version:  "1.0",
		Status:   "active",
		DirMTime: dirModTime(oldDir),
	}); err != nil {
		t.Fatal(err)
	}

	// The install/update renamed the directory on disk; the record is stale.
	newDir := filepath.Join(root, "MyGame v2.0")
	if err := os.Rename(oldDir, newDir); err != nil {
		t.Fatal(err)
	}

	if err := RunScan(database, RunScanConfig{Dirs: []string{root}, Force: true}); err != nil {
		t.Fatalf("RunScan: %v", err)
	}

	active, _ := database.ListActiveGames("", "")
	if len(active) != 1 {
		t.Fatalf("active games = %d, want 1 (no duplicate after rename)", len(active))
	}
	g := active[0]
	if g.Version != "2.0" {
		t.Errorf("version = %q, want 2.0 refreshed by force rescan", g.Version)
	}
	if g.ExePath == "" || !strings.HasSuffix(g.ExePath, "Game.exe") {
		t.Errorf("exePath = %q, want detected Game.exe", g.ExePath)
	}
	deleted, _ := database.ListDeletedGames()
	if len(deleted) != 1 || deleted[0].Path != oldDir {
		t.Errorf("deleted games = %d (%v), want 1 ghost at the old path", len(deleted), deleted)
	}
}

// Same-basename move within the scan root relocates the row in place — user
// curation survives, no duplicate, nothing soft-deleted.
func TestRunScan_MoveWithinRootRelocatesRow(t *testing.T) {
	withStdin(t, "y\n")
	root := t.TempDir()
	oldDir := mkGame(t, filepath.Join(root, "MyGame"))
	sub := filepath.Join(root, "Category")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	database := setupTestDB(t)
	defer database.Close()
	if _, err := database.InsertGame(&db.Game{
		Title:    "MyGame",
		Engine:   "RPGM",
		Path:     oldDir,
		Status:   "active",
		DirMTime: dirModTime(oldDir),
	}); err != nil {
		t.Fatal(err)
	}

	newDir := filepath.Join(sub, "MyGame")
	if err := os.Rename(oldDir, newDir); err != nil {
		t.Fatal(err)
	}

	if err := RunScan(database, RunScanConfig{Dirs: []string{root}, Force: true}); err != nil {
		t.Fatalf("RunScan: %v", err)
	}

	active, _ := database.ListActiveGames("", "")
	if len(active) != 1 {
		t.Fatalf("active games = %d, want exactly 1 (relocated, not duplicated)", len(active))
	}
	if active[0].Path != newDir {
		t.Errorf("path = %q, want relocated %q", active[0].Path, newDir)
	}
	if active[0].ExePath == "" {
		t.Errorf("exePath not refilled after relocation")
	}
	deleted, _ := database.ListDeletedGames()
	if len(deleted) != 0 {
		t.Errorf("deleted games = %d, want 0 (row relocated in place)", len(deleted))
	}
}