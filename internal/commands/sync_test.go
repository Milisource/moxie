package commands

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/scraper"
)

// ---------------------------------------------------------------------------
// ApplyThreadData — version field preservation
// ---------------------------------------------------------------------------

func TestApplyThreadData_VersionPreservation(t *testing.T) {
	t.Parallel()

	// ApplyThreadData must set LatestVersion from thread data, but NEVER
	// overwrite the locally-scanned Version field.

	t.Run("sets LatestVersion when Version is empty", func(t *testing.T) {
		game := &db.Game{
			Title:         "My Game",
			Version:       "",
			LatestVersion: "",
		}
		data := &scraper.ThreadData{
			Title:    "RPGM My Game [v1.0] [Dev]",
			Version:  "1.0",
			ThreadID: 12345,
		}

		scraper.ApplyThreadData(game, data, "https://f95zone.to/threads/my-game.12345/")

		if game.LatestVersion != "1.0" {
			t.Errorf("LatestVersion = %q, want %q", game.LatestVersion, "1.0")
		}
		if game.Version != "" {
			t.Errorf("Version should remain empty, got %q", game.Version)
		}
	})

	t.Run("preserves existing Version when LatestVersion is set", func(t *testing.T) {
		game := &db.Game{
			Title:         "My Game",
			Version:       "0.9",
			LatestVersion: "",
		}
		data := &scraper.ThreadData{
			Title:    "RPGM My Game [v1.0] [Dev]",
			Version:  "1.0",
			ThreadID: 12345,
		}

		scraper.ApplyThreadData(game, data, "https://f95zone.to/threads/my-game.12345/")

		if game.LatestVersion != "1.0" {
			t.Errorf("LatestVersion = %q, want %q", game.LatestVersion, "1.0")
		}
		if game.Version != "0.9" {
			t.Errorf("Version should be preserved as %q, got %q", "0.9", game.Version)
		}
	})

	t.Run("does not clear LatestVersion when data.Version is empty", func(t *testing.T) {
		game := &db.Game{
			Title:         "My Game",
			Version:       "0.9",
			LatestVersion: "1.0",
		}
		data := &scraper.ThreadData{
			Title:    "My Game",
			Version:  "", // no version in thread
			ThreadID: 12345,
		}

		scraper.ApplyThreadData(game, data, "https://f95zone.to/threads/my-game.12345/")

		if game.LatestVersion != "1.0" {
			t.Errorf("LatestVersion should be unchanged, got %q", game.LatestVersion)
		}
		if game.Version != "0.9" {
			t.Errorf("Version should be unchanged, got %q", game.Version)
		}
	})
}

// ---------------------------------------------------------------------------
// ApplyThreadData — engine detection from title prefix
// ---------------------------------------------------------------------------

func TestApplyThreadData_SetsEngineFromF95Prefix(t *testing.T) {
	t.Parallel()

	// When the F95Zone thread title starts with a known engine prefix
	// and the game's engine is empty/Unknown/Others, ApplyThreadData
	// should set the engine from the prefix.

	t.Run("sets engine from RPGM prefix when engine is empty", func(t *testing.T) {
		game := &db.Game{Title: "Local Name", Engine: ""}
		data := &scraper.ThreadData{
			Title: "RPGM Local Name [v1.0] [Dev]",
		}
		scraper.ApplyThreadData(game, data, "https://f95zone.to/threads/game.12345/")

		if game.Engine != "RPGM" {
			t.Errorf("Engine = %q, want %q", game.Engine, "RPGM")
		}
	})

	t.Run("sets engine from Unity prefix when engine is Unknown", func(t *testing.T) {
		game := &db.Game{Title: "Local Name", Engine: "Unknown"}
		data := &scraper.ThreadData{
			Title: "Unity Local Name [v1.0] [Dev]",
		}
		scraper.ApplyThreadData(game, data, "https://f95zone.to/threads/game.12345/")

		if game.Engine != "Unity" {
			t.Errorf("Engine = %q, want %q", game.Engine, "Unity")
		}
	})

	t.Run("sets engine from RenPy prefix when engine is Others", func(t *testing.T) {
		game := &db.Game{Title: "Local Name", Engine: "Others"}
		data := &scraper.ThreadData{
			Title: "Ren'Py Local Name [v1.0] [Dev]",
		}
		scraper.ApplyThreadData(game, data, "https://f95zone.to/threads/game.12345/")

		if game.Engine != "RenPy" {
			t.Errorf("Engine = %q, want %q", game.Engine, "RenPy")
		}
	})

	t.Run("does NOT overwrite explicitly-set engine", func(t *testing.T) {
		game := &db.Game{Title: "Local Name", Engine: "Unity"}
		data := &scraper.ThreadData{
			Title: "RPGM Local Name [v1.0] [Dev]",
		}
		scraper.ApplyThreadData(game, data, "https://f95zone.to/threads/game.12345/")

		if game.Engine != "Unity" {
			t.Errorf("Engine should remain Unity, got %q", game.Engine)
		}
	})
}

// ---------------------------------------------------------------------------
// ApplyThreadData — URL, ThreadID, Tags, Status
// ---------------------------------------------------------------------------

func TestApplyThreadData_URLAndThreadID(t *testing.T) {
	t.Parallel()

	game := &db.Game{}
	data := &scraper.ThreadData{
		Title:    "Game Title",
		ThreadID: 54321,
	}
	url := "https://f95zone.to/threads/game-title.54321/"

	scraper.ApplyThreadData(game, data, url)

	if game.F95URL != url {
		t.Errorf("F95URL = %q, want %q", game.F95URL, url)
	}
	if game.F95ThreadID != 54321 {
		t.Errorf("F95ThreadID = %d, want %d", game.F95ThreadID, 54321)
	}
}

func TestApplyThreadData_TagsAndStatus(t *testing.T) {
	t.Parallel()

	game := &db.Game{}
	data := &scraper.ThreadData{
		Title:  "Game Title",
		Tags:   []string{"adult", "rpg", "parody"},
		Status: "completed",
	}
	scraper.ApplyThreadData(game, data, "https://f95zone.to/threads/game.54321/")

	if len(game.Tags) != 3 || game.Tags[0] != "adult" {
		t.Errorf("Tags = %v, want [adult rpg parody]", game.Tags)
	}
	if game.Status != "completed" {
		t.Errorf("Status = %q, want %q", game.Status, "completed")
	}
}

func TestApplyThreadData_StripsTitlePrefix(t *testing.T) {
	t.Parallel()

	game := &db.Game{}
	data := &scraper.ThreadData{
		Title: "RPGM Completed My Game [v1.0] [Dev]",
	}
	scraper.ApplyThreadData(game, data, "https://f95zone.to/threads/my-game.54321/")

	// The title should have the engine/status prefix stripped.
	want := "My Game [v1.0] [Dev]"
	if game.Title != want {
		t.Errorf("Title = %q, want %q", game.Title, want)
	}
}

// ---------------------------------------------------------------------------
// NormalizeVersion
// ---------------------------------------------------------------------------

func TestNormalizeVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"v1.0", "1"},
		{"V1.0.0", "1"},
		{"1.0.3", "1.0.3"},
		{"0.13.4", "0.13.4"},
		{"1.0", "1"},
		{"", ""},
		{"v0.5", "0.5"},
		{"V2.0", "2"},
		{"1.0.0.0", "1"},
		{"v1.2.3", "1.2.3"},
		{"  1.0  ", "1"},
		{"v1", "1"},
		{"0.0.0", "0"},
		{"V1.0.0.0.0", "1"},
		{"  v1.0  ", "1"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeVersion(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeVersion(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func setupTestDB(t testing.TB) *db.Database {
	t.Helper()
	f, err := os.CreateTemp("", "test-*.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	f.Close()
	t.Cleanup(func() { os.Remove(path) })
	database, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return database
}

// ---------------------------------------------------------------------------
// SyncGameLogic — business logic extraction tests
// ---------------------------------------------------------------------------

func TestSyncGameLogic_AlreadyAssociatedWithinCooldown(t *testing.T) {
	t.Parallel()

	database := setupTestDB(t)
	defer database.Close()

	game := &db.Game{
		Title:            "Test Game",
		Engine:           "Unity",
		Path:             "/test/game",
		F95URL:           "https://f95zone.to/threads/test.12345/",
		VersionCheckedAt: time.Now(),
	}
	id, err := database.InsertGame(game)
	if err != nil {
		t.Fatal(err)
	}

	result, err := SyncGameLogic(database, game, nil, false, false)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.CooldownSkipped {
		t.Error("expected CooldownSkipped=true for game within cooldown")
	}
	_ = id
}

func TestSyncGameLogic_NoURLWithNilClient(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	game := &db.Game{
		Title:  "Unassociated Game",
		Engine: "Unity",
		Path:   "/test/game",
		F95URL: "",
	}
	id, err := database.InsertGame(game)
	if err != nil {
		t.Fatal(err)
	}

	// With nil client, no URL, and force=false: the search should fail.
	result, err := SyncGameLogic(database, game, nil, false, false)
	if err == nil {
		t.Fatal("expected error when searching with nil client")
	}
	if result != nil {
		t.Error("expected nil result on error")
	}
	_ = id
}

func TestSyncGameLogic_GameWithURL_CooldownSkip(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	game := &db.Game{
		Title:            "Cooldown Game",
		Engine:           "Unity",
		Path:             "/test/cooldown",
		F95URL:           "https://f95zone.to/threads/test.12345/",
		VersionCheckedAt: time.Now(),
	}
	id, err := database.InsertGame(game)
	if err != nil {
		t.Fatal(err)
	}

	result, err := SyncGameLogic(database, game, nil, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.CooldownSkipped {
		t.Error("expected CooldownSkipped=true for recently checked game")
	}
	if result.Associated {
		t.Error("already-associated game should not be re-associated")
	}
	if !result.CooldownSkipped {
		t.Error("expected CooldownSkipped=true for recently checked game")
	}
	_ = id
}

// TestSyncGameLogic_ForceRerun_NoCooldown verifies that force=true bypasses
// the cooldown check so a recently-checked game is processed again.
func TestSyncGameLogic_ForceRerun_NoCooldown(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	game := &db.Game{
		Title:            "Force Game",
		Engine:           "RenPy",
		Path:             "/test/force",
		F95URL:           "https://f95zone.to/threads/test.12345/",
		VersionCheckedAt: time.Now(), // just checked — within cooldown
	}
	id, err := database.InsertGame(game)
	if err != nil {
		t.Fatal(err)
	}

	// With force=true, cooldown should be bypassed.
	// The scrape will fail (nil client) but CooldownSkipped must be false.
	result, err := SyncGameLogic(database, game, nil, true, false)
	if err == nil {
		t.Fatal("expected scrape error with nil client")
	}
	if result != nil && result.CooldownSkipped {
		t.Error("force=true should bypass cooldown, but CooldownSkipped was true")
	}
	_ = id
}

// TestSyncGameLogic_AlreadyAssociated_NoReassociation verifies that
// games with existing F95URL are not re-associated.
func TestSyncGameLogic_AlreadyAssociated_NoReassociation(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	game := &db.Game{
		Title:            "Associated Game",
		Engine:           "Unity",
		Path:             "/test/associated",
		F95URL:           "https://f95zone.to/threads/test.12345/",
		VersionCheckedAt: time.Now().Add(-48 * time.Hour), // checked 2 days ago — outside cooldown
	}
	id, err := database.InsertGame(game)
	if err != nil {
		t.Fatal(err)
	}

	result, err := SyncGameLogic(database, game, nil, false, false)
	// Game has URL + outside cooldown → should attempt Phase 2 scrape
	// Nil client will fail the scrape, which is expected.
	if err == nil {
		t.Fatal("expected scrape error with nil client (Phase 2)")
	}
	if result != nil && result.Associated {
		t.Error("game with existing URL should NOT be re-associated in Phase 1")
	}
	if result != nil && result.CooldownSkipped {
		t.Error("game outside cooldown should NOT be skipped")
	}
	_ = id
}

// ---------------------------------------------------------------------------
// RunUpdateCheck
// ---------------------------------------------------------------------------

func TestRunUpdateCheck_NoGames(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	updatesFound, results := RunUpdateCheck(database, nil, nil, false)
	if updatesFound != 0 {
		t.Errorf("expected 0 updates, got %d", updatesFound)
	}
	if results != nil {
		t.Errorf("expected nil results for no games, got %v", results)
	}
}

func TestRunUpdateCheck_NoGamesWithURL(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	// Insert a game without F95URL — should be filtered out.
	game := &db.Game{
		Title:  "No URL Game",
		Engine: "Unity",
		Path:   "/no-url-game",
		F95URL: "",
	}
	if _, err := database.InsertGame(game); err != nil {
		t.Fatal(err)
	}

	games := []db.Game{*game}
	updatesFound, results := RunUpdateCheck(database, nil, games, false)
	if updatesFound != 0 {
		t.Errorf("expected 0 updates (game has no F95URL), got %d", updatesFound)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestRunUpdateCheck_CooldownSkip(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	// Insert a game with F95URL, checked just now.
	game := &db.Game{
		Title:            "Cooldown Game",
		Engine:           "Unity",
		Path:             "/cooldown-game",
		F95URL:           "https://f95zone.to/threads/cooldown.12345/",
		VersionCheckedAt: time.Now(),
	}
	id, err := database.InsertGame(game)
	if err != nil {
		t.Fatal(err)
	}

	games := []db.Game{*game}
	updatesFound, results := RunUpdateCheck(database, nil, games, false)
	if updatesFound != 0 {
		t.Errorf("expected 0 updates (within cooldown), got %d", updatesFound)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
	_ = id
}

func TestRunUpdateCheck_ForceBypassCooldown(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	// Create a test server that returns an error to simulate a scrape failure.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := scraper.NewClientWithHTTP("", server.Client())

	// Insert a game with F95URL, checked just now, force=true.
	game := &db.Game{
		Title:            "Force Game",
		Engine:           "RenPy",
		Path:             "/force-game",
		F95URL:           server.URL + "/threads/force.12345/",
		VersionCheckedAt: time.Now(),
	}
	id, err := database.InsertGame(game)
	if err != nil {
		t.Fatal(err)
	}

	games := []db.Game{*game}
	// With force=true, cooldown is bypassed. Scrape will fail (500 response)
	// but the game should be processed (not skipped).
	updatesFound, results := RunUpdateCheck(database, client, games, true)
	if updatesFound != 0 {
		t.Errorf("expected 0 updates (scrape fails), got %d", updatesFound)
	}
	// With 500 response, scrape fails and result has error.
	if len(results) != 1 {
		t.Fatalf("expected 1 result (force bypasses cooldown), got %d", len(results))
	}
	if results[0].Error == "" {
		t.Error("expected error in result when scrape fails")
	}
	_ = id
}
