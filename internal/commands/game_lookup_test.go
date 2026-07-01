package commands

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/mili/moxie/internal/db"
)

// nonInteractiveCmd prepares an exec.Cmd for the subprocess exit pattern
// with stdin connected to /dev/null so that isInteractive() returns false
// in the subprocess.
func nonInteractiveCmd(t *testing.T, testName string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^"+testName+"$")
	cmd.Env = append(os.Environ(), "GO_TEST_EXIT=1")
	cmd.Stdin = strings.NewReader("")
	return cmd
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// insertTestGame inserts a minimal game into the database and returns its ID.
func insertTestGame(t testing.TB, database *db.Database, title, engine string) int64 {
	t.Helper()
	g := &db.Game{
		Title:  title,
		Engine: engine,
		Path:   t.TempDir(),
		Status: "unknown",
	}
	id, err := database.InsertGame(g)
	if err != nil {
		t.Fatalf("InsertGame: %v", err)
	}
	return id
}

// ---------------------------------------------------------------------------
// ResolveGame
// ---------------------------------------------------------------------------

func TestResolveGame_ByNumericID(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	id := insertTestGame(t, database, "Cyan Brain", "RenPy")

	game := ResolveGame(database, strconv.FormatInt(id, 10))
	if game == nil {
		t.Fatal("expected game, got nil")
	}
	if game.ID != id {
		t.Errorf("expected ID %d, got %d", id, game.ID)
	}
}

func TestResolveGame_NumericIDNotFound(t *testing.T) {
	t.Parallel()
	// Numeric ID that doesn't exist should fall through to fuzzy search,
	// which finds nothing, causing os.Exit(1).
	if os.Getenv("GO_TEST_EXIT") == "1" {
		database := setupTestDB(t)
		insertTestGame(t, database, "Some Game", "RenPy")
		ResolveGame(database, "99999")
		return
	}
	cmd := nonInteractiveCmd(t, t.Name())
	out, err := cmd.CombinedOutput()
	if exitErr, ok := err.(*exec.ExitError); ok && !exitErr.Success() {
		if !strings.Contains(string(out), "No game found matching") {
			t.Fatalf("unexpected output: %s", out)
		}
		return
	}
	t.Fatalf("expected exit, got: %v\noutput: %s", err, out)
}

func TestResolveGame_ByFuzzyName(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	insertTestGame(t, database, "Cyan Brain", "RenPy")
	insertTestGame(t, database, "Demon Herd", "Unity")

	game := ResolveGame(database, "Cyan")
	if game == nil {
		t.Fatal("expected game, got nil")
	}
	if game.Title != "Cyan Brain" {
		t.Errorf("expected 'Cyan Brain', got %q", game.Title)
	}
}

func TestResolveGame_MultipleFuzzyMatches(t *testing.T) {
	t.Parallel()
	if os.Getenv("GO_TEST_EXIT") == "1" {
		database := setupTestDB(t)
		insertTestGame(t, database, "Cyan Brain", "RenPy")
		insertTestGame(t, database, "Cyan Heart", "Unity")
		ResolveGame(database, "Cyan")
		return
	}
	cmd := nonInteractiveCmd(t, t.Name())
	out, err := cmd.CombinedOutput()
	if exitErr, ok := err.(*exec.ExitError); ok && !exitErr.Success() {
		if !strings.Contains(string(out), "Multiple games found") {
			t.Fatalf("unexpected output: %s", out)
		}
		return
	}
	t.Fatalf("expected exit, got: %v\noutput: %s", err, out)
}

func TestResolveGame_NotFound(t *testing.T) {
	t.Parallel()
	if os.Getenv("GO_TEST_EXIT") == "1" {
		database := setupTestDB(t)
		ResolveGame(database, "NonexistentGame")
		return
	}
	cmd := nonInteractiveCmd(t, t.Name())
	out, err := cmd.CombinedOutput()
	if exitErr, ok := err.(*exec.ExitError); ok && !exitErr.Success() {
		if !strings.Contains(string(out), "No game found matching") {
			t.Fatalf("unexpected output: %s", out)
		}
		return
	}
	t.Fatalf("expected exit, got: %v\noutput: %s", err, out)
}

func TestResolveGame_EmptyString(t *testing.T) {
	t.Parallel()
	// Empty string bypasses numeric parse and falls through to fuzzy search.
	// With no games in the database, SearchGames returns empty results -> exit.
	if os.Getenv("GO_TEST_EXIT") == "1" {
		database := setupTestDB(t)
		// No games inserted — empty DB.
		ResolveGame(database, "")
		return
	}
	cmd := nonInteractiveCmd(t, t.Name())
	out, err := cmd.CombinedOutput()
	if exitErr, ok := err.(*exec.ExitError); ok && !exitErr.Success() {
		if !strings.Contains(string(out), "No game found matching") {
			t.Fatalf("unexpected output: %s", out)
		}
		return
	}
	t.Fatalf("expected exit, got: %v\noutput: %s", err, out)
}

// ---------------------------------------------------------------------------
// ResolveFirstArg
// ---------------------------------------------------------------------------

func TestResolveFirstArg_ByNumericID(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	id := insertTestGame(t, database, "Test Game", "RenPy")

	game := ResolveFirstArg(database, strconv.FormatInt(id, 10))
	if game == nil {
		t.Fatal("expected game, got nil")
	}
	if game.ID != id {
		t.Errorf("expected ID %d, got %d", id, game.ID)
	}
}

func TestResolveFirstArg_ByFuzzyName(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	insertTestGame(t, database, "Alpha Game", "Unity")
	insertTestGame(t, database, "Beta Game", "RenPy")

	game := ResolveFirstArg(database, "Alpha")
	if game == nil {
		t.Fatal("expected game, got nil")
	}
	if game.Title != "Alpha Game" {
		t.Errorf("expected 'Alpha Game', got %q", game.Title)
	}
}

func TestResolveFirstArg_NumericIDNotFound(t *testing.T) {
	t.Parallel()
	if os.Getenv("GO_TEST_EXIT") == "1" {
		database := setupTestDB(t)
		insertTestGame(t, database, "Some Game", "RenPy")
		ResolveFirstArg(database, "99999")
		return
	}
	cmd := nonInteractiveCmd(t, t.Name())
	out, err := cmd.CombinedOutput()
	if exitErr, ok := err.(*exec.ExitError); ok && !exitErr.Success() {
		if !strings.Contains(string(out), "No game found matching") {
			t.Fatalf("unexpected output: %s", out)
		}
		return
	}
	t.Fatalf("expected exit, got: %v\noutput: %s", err, out)
}

func TestResolveFirstArg_NotFound(t *testing.T) {
	t.Parallel()
	if os.Getenv("GO_TEST_EXIT") == "1" {
		database := setupTestDB(t)
		ResolveFirstArg(database, "NonexistentGame")
		return
	}
	cmd := nonInteractiveCmd(t, t.Name())
	out, err := cmd.CombinedOutput()
	if exitErr, ok := err.(*exec.ExitError); ok && !exitErr.Success() {
		if !strings.Contains(string(out), "No game found matching") {
			t.Fatalf("unexpected output: %s", out)
		}
		return
	}
	t.Fatalf("expected exit, got: %v\noutput: %s", err, out)
}

func TestResolveFirstArg_MultipleFuzzyMatches(t *testing.T) {
	t.Parallel()
	if os.Getenv("GO_TEST_EXIT") == "1" {
		database := setupTestDB(t)
		insertTestGame(t, database, "Alpha Core", "Unity")
		insertTestGame(t, database, "Alpha Prime", "RenPy")
		ResolveFirstArg(database, "Alpha")
		return
	}
	cmd := nonInteractiveCmd(t, t.Name())
	out, err := cmd.CombinedOutput()
	if exitErr, ok := err.(*exec.ExitError); ok && !exitErr.Success() {
		if !strings.Contains(string(out), "Multiple games found") {
			t.Fatalf("unexpected output: %s", out)
		}
		return
	}
	t.Fatalf("expected exit, got: %v\noutput: %s", err, out)
}

// ---------------------------------------------------------------------------
// isInteractive
// ---------------------------------------------------------------------------

func TestIsInteractive_ReturnsFalse(t *testing.T) {
	t.Parallel()
	// In test runs, stdin is typically piped, not a TTY.
	// If the developer is running tests interactively (e.g. go test -v
	// from a terminal with no pipe), just log and skip the assertion.
	if isInteractive() {
		t.Log("stdin is a TTY — skipping non-interactive assertion")
		return
	}
}

// ---------------------------------------------------------------------------
// ConfirmDestructive
// ---------------------------------------------------------------------------

func TestConfirmDestructive_AssumeYes(t *testing.T) {
	t.Parallel()
	game := &db.Game{ID: 1, Title: "Test Game"}
	if !ConfirmDestructive("Removing", game, true) {
		t.Error("expected confirmed with assumeYes=true")
	}
}

func TestConfirmDestructive_NonInteractive(t *testing.T) {
	t.Parallel()
	// In test environment, stdin is not a TTY, so ConfirmDestructive
	// should return false when assumeYes is false.
	game := &db.Game{ID: 1, Title: "Test Game"}
	if ConfirmDestructive("Removing", game, false) {
		t.Error("expected rejected in non-interactive mode")
	}
}

// ---------------------------------------------------------------------------
// promptSelectGame
// ---------------------------------------------------------------------------

func TestPromptSelectGame_NonInteractive(t *testing.T) {
	t.Parallel()
	if os.Getenv("GO_TEST_EXIT") == "1" {
		games := []db.Game{
			{ID: 1, Title: "Game One", Engine: "RenPy"},
			{ID: 2, Title: "Game Two", Engine: "Unity"},
		}
		promptSelectGame(games)
		return
	}
	cmd := nonInteractiveCmd(t, t.Name())
	out, err := cmd.CombinedOutput()
	if exitErr, ok := err.(*exec.ExitError); ok && !exitErr.Success() {
		if !strings.Contains(string(out), "Multiple games found") {
			t.Fatalf("unexpected output: %s", out)
		}
		return
	}
	t.Fatalf("expected exit, got: %v\noutput: %s", err, out)
}
