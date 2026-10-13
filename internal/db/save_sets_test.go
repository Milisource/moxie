package db

import (
	"database/sql"
	"os"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func tempSQLitePath(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp("", "test-*.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	f.Close()
	t.Cleanup(func() { os.Remove(path) })
	return path
}

func assertTableExists(t *testing.T, db *Database, name string) {
	t.Helper()
	var n int
	err := db.conn.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", name).Scan(&n)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	if n == 0 {
		t.Fatalf("table %q does not exist", name)
	}
}

func insertSaveTestGame(t *testing.T, db *Database, title, path string) int64 {
	t.Helper()
	id, err := db.InsertGame(&Game{Title: title, Engine: "RenPy", Path: path})
	if err != nil {
		t.Fatalf("InsertGame: %v", err)
	}
	return id
}

// ---------------------------------------------------------------------------
// Migration v7
// ---------------------------------------------------------------------------

func TestMigrationV14_FreshDB(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	var version int
	if err := db.conn.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != currentSchemaVersion {
		t.Fatalf("user_version = %d, want %d", version, currentSchemaVersion)
	}

	assertTableExists(t, db, "save_sets")
	assertTableExists(t, db, "save_configs")

	// The partial unique index should be present on a fresh database.
	var n int
	err := db.conn.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_save_sets_one_active'").Scan(&n)
	if err != nil {
		t.Fatalf("query index: %v", err)
	}
	if n == 0 {
		t.Fatal("idx_save_sets_one_active missing on fresh DB")
	}
}

func TestMigrationV14_FromV6(t *testing.T) {
	path := tempSQLitePath(t)

	// Build a minimal v6-shaped database: a games table plus user_version = 6,
	// with none of the v7 tables.
	conn, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	if _, err := conn.Exec(`
		CREATE TABLE games (
			id INTEGER PRIMARY KEY,
			title TEXT NOT NULL,
			engine TEXT NOT NULL,
			path TEXT NOT NULL UNIQUE,
			tags TEXT,
			deleted_at TEXT
		)`); err != nil {
		conn.Close()
		t.Fatalf("create games: %v", err)
	}
	// The v6→current migration chain runs dev's later steps in between:
	// v8 recreates FTS triggers whose bodies read games.tags and
	// scraped_meta.developer/overview, and v9 alters download_links, so a
	// bare v6-era schema needs those tables and columns present.
	if _, err := conn.Exec(`
		CREATE TABLE IF NOT EXISTS scraped_meta (
			id INTEGER PRIMARY KEY,
			game_id INTEGER,
			developer TEXT,
			overview TEXT,
			cover_url TEXT
		);
		CREATE TABLE IF NOT EXISTS download_links (id INTEGER PRIMARY KEY);
		CREATE TABLE IF NOT EXISTS game_series (id INTEGER PRIMARY KEY)`); err != nil {
		conn.Close()
		t.Fatalf("create prerequisite tables: %v", err)
	}
	if _, err := conn.Exec("PRAGMA user_version = 6"); err != nil {
		conn.Close()
		t.Fatalf("set user_version: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}

	// Reopening through Open must run the v7 step.
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open on simulated v6 DB: %v", err)
	}

	var version int
	if err := db.conn.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		db.Close()
		t.Fatalf("read user_version: %v", err)
	}
	if version != currentSchemaVersion {
		db.Close()
		t.Fatalf("user_version = %d, want %d", version, currentSchemaVersion)
	}
	assertTableExists(t, db, "save_sets")
	assertTableExists(t, db, "save_configs")

	// The migrated tables must be usable.
	res, err := db.conn.Exec(
		"INSERT INTO games (title, engine, path) VALUES (?, ?, ?)",
		"Migrated", "RenPy", "/migrated")
	if err != nil {
		db.Close()
		t.Fatalf("insert game after migration: %v", err)
	}
	gameID, err := res.LastInsertId()
	if err != nil {
		db.Close()
		t.Fatalf("last insert id: %v", err)
	}
	if _, err := db.CreateSaveSet(&SaveSet{GameID: gameID, Name: "after-migrate"}); err != nil {
		db.Close()
		t.Fatalf("CreateSaveSet after migration: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close migrated DB: %v", err)
	}

	// Reopening again must be idempotent (v7 DDL re-run safely).
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen migrated DB: %v", err)
	}
	defer db2.Close()
	assertTableExists(t, db2, "save_sets")
}

// ---------------------------------------------------------------------------
// SaveSet CRUD
// ---------------------------------------------------------------------------

func TestSaveSetCRUD(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	gameID := insertSaveTestGame(t, db, "Save Game", "/save-game")

	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	set := &SaveSet{
		GameID:      gameID,
		Name:        "full-save",
		Label:       "Full Save",
		Source:      "import",
		SourceURL:   "https://f95zone.to/threads/1",
		GameVersion: "0.9.1",
		ApplyRoot:   "saves",
		SizeBytes:   4096,
		FileCount:   7,
		Active:      false,
		CreatedAt:   created,
	}
	id, err := db.CreateSaveSet(set)
	if err != nil {
		t.Fatalf("CreateSaveSet: %v", err)
	}
	if id == 0 || set.ID != id {
		t.Fatalf("CreateSaveSet id = %d, set.ID = %d", id, set.ID)
	}

	got, err := db.GetSaveSet(gameID, "full-save")
	if err != nil {
		t.Fatalf("GetSaveSet: %v", err)
	}
	if got == nil {
		t.Fatal("GetSaveSet returned nil")
	}
	if got.ID != id {
		t.Errorf("ID = %d, want %d", got.ID, id)
	}
	if got.GameID != gameID {
		t.Errorf("GameID = %d, want %d", got.GameID, gameID)
	}
	if got.Name != "full-save" {
		t.Errorf("Name = %q", got.Name)
	}
	if got.Label != "Full Save" {
		t.Errorf("Label = %q", got.Label)
	}
	if got.Source != "import" {
		t.Errorf("Source = %q", got.Source)
	}
	if got.SourceURL != "https://f95zone.to/threads/1" {
		t.Errorf("SourceURL = %q", got.SourceURL)
	}
	if got.GameVersion != "0.9.1" {
		t.Errorf("GameVersion = %q", got.GameVersion)
	}
	if got.ApplyRoot != "saves" {
		t.Errorf("ApplyRoot = %q", got.ApplyRoot)
	}
	if got.SizeBytes != 4096 {
		t.Errorf("SizeBytes = %d", got.SizeBytes)
	}
	if got.FileCount != 7 {
		t.Errorf("FileCount = %d", got.FileCount)
	}
	if got.Active {
		t.Error("Active = true, want false")
	}
	if !got.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, created)
	}

	byID, err := db.GetSaveSetByID(id)
	if err != nil {
		t.Fatalf("GetSaveSetByID: %v", err)
	}
	if byID == nil || byID.Name != "full-save" {
		t.Fatalf("GetSaveSetByID = %+v", byID)
	}

	// Delete + confirm.
	if err := db.DeleteSaveSet(id); err != nil {
		t.Fatalf("DeleteSaveSet: %v", err)
	}
	got, err = db.GetSaveSetByID(id)
	if err != nil {
		t.Fatalf("GetSaveSetByID after delete: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestSaveSetDefaults(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	gameID := insertSaveTestGame(t, db, "Defaults", "/defaults")

	// No Source, no CreatedAt: DB defaults apply.
	set := &SaveSet{GameID: gameID, Name: "plain"}
	if _, err := db.CreateSaveSet(set); err != nil {
		t.Fatalf("CreateSaveSet: %v", err)
	}
	if set.Source != "import" {
		t.Errorf("Source = %q, want import", set.Source)
	}
	if set.CreatedAt.IsZero() {
		t.Error("CreatedAt should be populated after insert")
	}

	got, err := db.GetSaveSet(gameID, "plain")
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "import" {
		t.Errorf("stored Source = %q, want import", got.Source)
	}
	if got.CreatedAt.IsZero() {
		t.Error("stored CreatedAt should not be zero")
	}
	if got.ApplyRoot != "" || got.SizeBytes != 0 || got.FileCount != 0 || got.Active {
		t.Errorf("unexpected defaults: %+v", got)
	}
}

func TestGetSaveSetNotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	gameID := insertSaveTestGame(t, db, "NotFound", "/not-found")

	got, err := db.GetSaveSet(gameID, "missing")
	if err != nil {
		t.Fatalf("GetSaveSet: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for missing name")
	}

	got, err = db.GetSaveSetByID(4242)
	if err != nil {
		t.Fatalf("GetSaveSetByID: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for missing ID")
	}
}

func TestSaveSetUniqueName(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	gameID := insertSaveTestGame(t, db, "Unique", "/unique")

	if _, err := db.CreateSaveSet(&SaveSet{GameID: gameID, Name: "dup"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateSaveSet(&SaveSet{GameID: gameID, Name: "dup"}); err == nil {
		t.Fatal("expected error for duplicate (game_id, name)")
	}

	// Same name under a different game is fine.
	otherID := insertSaveTestGame(t, db, "Other", "/other")
	if _, err := db.CreateSaveSet(&SaveSet{GameID: otherID, Name: "dup"}); err != nil {
		t.Fatalf("duplicate name across games should be allowed: %v", err)
	}
}

func TestListSaveSetsNewestFirst(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	gameID := insertSaveTestGame(t, db, "Listed", "/listed")
	otherID := insertSaveTestGame(t, db, "Unrelated", "/unrelated")

	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)

	if _, err := db.CreateSaveSet(&SaveSet{GameID: gameID, Name: "older", CreatedAt: older}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateSaveSet(&SaveSet{GameID: gameID, Name: "newer", CreatedAt: newer}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateSaveSet(&SaveSet{GameID: otherID, Name: "foreign", CreatedAt: newer}); err != nil {
		t.Fatal(err)
	}

	sets, err := db.ListSaveSets(gameID)
	if err != nil {
		t.Fatalf("ListSaveSets: %v", err)
	}
	if len(sets) != 2 {
		t.Fatalf("len = %d, want 2", len(sets))
	}
	if sets[0].Name != "newer" || sets[1].Name != "older" {
		t.Errorf("order = [%q, %q], want [newer, older]", sets[0].Name, sets[1].Name)
	}

	// No sets for an unknown game → empty, not an error.
	none, err := db.ListSaveSets(99999)
	if err != nil {
		t.Fatalf("ListSaveSets(unknown): %v", err)
	}
	if len(none) != 0 {
		t.Errorf("expected empty slice, got %d", len(none))
	}
}

// ---------------------------------------------------------------------------
// SetActiveSaveSet
// ---------------------------------------------------------------------------

func TestSetActiveSaveSet(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	gameID := insertSaveTestGame(t, db, "Activate", "/activate")

	first, err := db.CreateSaveSet(&SaveSet{GameID: gameID, Name: "mine"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.CreateSaveSet(&SaveSet{GameID: gameID, Name: "hundred"})
	if err != nil {
		t.Fatal(err)
	}

	// Activate the first set.
	if err := db.SetActiveSaveSet(gameID, first); err != nil {
		t.Fatalf("SetActiveSaveSet(first): %v", err)
	}
	sets, err := db.ListSaveSets(gameID)
	if err != nil {
		t.Fatal(err)
	}
	if !activeByName(sets, "mine") {
		t.Errorf("mine should be active, got %+v", sets)
	}
	if activeByName(sets, "hundred") {
		t.Errorf("hundred should not be active yet")
	}

	// Activate the second: the first must be deactivated, and the partial
	// unique index must not trip.
	if err := db.SetActiveSaveSet(gameID, second); err != nil {
		t.Fatalf("SetActiveSaveSet(second): %v", err)
	}
	sets, err = db.ListSaveSets(gameID)
	if err != nil {
		t.Fatal(err)
	}
	if activeByName(sets, "mine") {
		t.Errorf("mine should be deactivated after activating hundred")
	}
	if !activeByName(sets, "hundred") {
		t.Errorf("hundred should be active")
	}

	// Re-activating the same set is idempotent (no spurious unique violation).
	if err := db.SetActiveSaveSet(gameID, second); err != nil {
		t.Fatalf("SetActiveSaveSet(second) again: %v", err)
	}
	if n := countActive(t, db, gameID); n != 1 {
		t.Errorf("active rows = %d, want 1", n)
	}

	// A set that does not belong to the game is rejected.
	otherID := insertSaveTestGame(t, db, "Other", "/other")
	if err := db.SetActiveSaveSet(otherID, second); err == nil {
		t.Fatal("expected error activating a set from another game")
	}
	if err := db.SetActiveSaveSet(gameID, 9999); err == nil {
		t.Fatal("expected error activating a non-existent set")
	}
}

func activeByName(sets []SaveSet, name string) bool {
	for _, s := range sets {
		if s.Name == name {
			return s.Active
		}
	}
	return false
}

func countActive(t *testing.T, db *Database, gameID int64) int {
	t.Helper()
	var n int
	if err := db.conn.QueryRow(
		"SELECT COUNT(*) FROM save_sets WHERE game_id = ? AND active = 1", gameID).Scan(&n); err != nil {
		t.Fatalf("count active: %v", err)
	}
	return n
}

func TestPartialUniqueIndexRejectsTwoActive(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	gameID := insertSaveTestGame(t, db, "OneActive", "/one-active")

	if _, err := db.CreateSaveSet(&SaveSet{GameID: gameID, Name: "a", Active: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateSaveSet(&SaveSet{GameID: gameID, Name: "b", Active: true}); err == nil {
		t.Fatal("expected partial unique index to reject a second active set")
	}
}

// ---------------------------------------------------------------------------
// SaveConfig
// ---------------------------------------------------------------------------

func TestSaveConfigUpsertAndClear(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	gameID := insertSaveTestGame(t, db, "Config", "/config")

	// Not found initially.
	got, err := db.GetSaveConfig(gameID)
	if err != nil {
		t.Fatalf("GetSaveConfig: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil config before insert")
	}

	// Insert.
	if err := db.UpsertSaveConfig(&SaveConfig{
		GameID:       gameID,
		ManualPath:   "/manual/saves",
		ResolvedPath: "/auto/saves",
	}); err != nil {
		t.Fatalf("UpsertSaveConfig: %v", err)
	}
	got, err = db.GetSaveConfig(gameID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected config after upsert")
	}
	if got.ManualPath != "/manual/saves" || got.ResolvedPath != "/auto/saves" {
		t.Errorf("config = %+v", got)
	}
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be populated")
	}

	// Update (upsert replaces).
	if err := db.UpsertSaveConfig(&SaveConfig{
		GameID:       gameID,
		ManualPath:   "/manual/v2",
		ResolvedPath: "/auto/v2",
	}); err != nil {
		t.Fatalf("UpsertSaveConfig update: %v", err)
	}
	got, err = db.GetSaveConfig(gameID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ManualPath != "/manual/v2" || got.ResolvedPath != "/auto/v2" {
		t.Errorf("config after update = %+v", got)
	}

	// Only one row per game.
	var n int
	if err := db.conn.QueryRow("SELECT COUNT(*) FROM save_configs WHERE game_id = ?", gameID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("save_configs rows = %d, want 1", n)
	}

	// Clear.
	if err := db.ClearSaveConfig(gameID); err != nil {
		t.Fatalf("ClearSaveConfig: %v", err)
	}
	got, err = db.GetSaveConfig(gameID)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatal("expected nil config after clear")
	}

	// Clearing again is a no-op.
	if err := db.ClearSaveConfig(gameID); err != nil {
		t.Fatalf("ClearSaveConfig (again): %v", err)
	}
}

func TestSaveSetCascadeDeleteWithGame(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	gameID := insertSaveTestGame(t, db, "Cascade Sets", "/cascade-sets")
	if _, err := db.CreateSaveSet(&SaveSet{GameID: gameID, Name: "doomed"}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSaveConfig(&SaveConfig{GameID: gameID, ManualPath: "/x"}); err != nil {
		t.Fatal(err)
	}

	if err := db.DeleteGamePermanent(gameID); err != nil {
		t.Fatalf("DeleteGamePermanent: %v", err)
	}

	sets, err := db.ListSaveSets(gameID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sets) != 0 {
		t.Errorf("save sets should cascade-delete, got %d", len(sets))
	}
	cfg, err := db.GetSaveConfig(gameID)
	if err != nil {
		t.Fatal(err)
	}
	if cfg != nil {
		t.Error("save config should cascade-delete")
	}
}
