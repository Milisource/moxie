package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// makeV7DB builds a database with the pre-Godot schema (v7): the games
// table WITHOUT 'Godot' in the engine CHECK, a couple of rows, child rows
// in download_links/scraped_meta, and user_version=7. Open() must then
// run the v8 rebuild and leave everything intact.
func makeV7DB(t *testing.T, path string) {
	t.Helper()

	conn, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open v7 db: %v", err)
	}
	defer conn.Close()

	// The v7 schema — games CHECK without Godot.
	ddl := `
		CREATE TABLE games (
			id          INTEGER PRIMARY KEY,
			title       TEXT NOT NULL,
			engine      TEXT NOT NULL CHECK (engine IN ('ADRIFT','Flash','HTML','Java','Others','QSP','RAGS','RPGM','RenPy','Tads','Unity','UnrealEngine','WebGL','WolfRPG','Unknown')),
			path        TEXT NOT NULL UNIQUE,
			exe_path    TEXT,
			version     TEXT,
			size_bytes  INTEGER DEFAULT 0,
			f95_url     TEXT,
			f95_thread_id INTEGER,
			tags        TEXT DEFAULT '[]',
			status      TEXT DEFAULT 'unknown' CHECK (status IN ('active','completed','abandoned','on_hold','unknown')),
			notes       TEXT DEFAULT '',
			latest_version   TEXT,
			version_checked_at TEXT,
			store_links TEXT DEFAULT '{}',
			steam_app_id INTEGER,
			wine_prefix TEXT,
			last_scanned_at TEXT,
			dir_mtime TEXT,
			series_id    INTEGER,
			series_order INTEGER DEFAULT 0,
			deleted_at  TEXT,
			created_at  TEXT DEFAULT (datetime('now')),
			updated_at  TEXT DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS scraped_meta (
			game_id      INTEGER PRIMARY KEY REFERENCES games(id) ON DELETE CASCADE,
			developer    TEXT,
			overview     TEXT,
			cover_url    TEXT,
			last_scraped TEXT DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS download_links (
			id INTEGER PRIMARY KEY,
			game_id INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
			url TEXT NOT NULL,
			host TEXT,
			name TEXT,
			platform TEXT DEFAULT 'unknown',
			is_dead INTEGER DEFAULT 0,
			dead_reason TEXT,
			last_checked TEXT,
			created_at TEXT DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS game_series (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE
		);
		CREATE TABLE IF NOT EXISTS play_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			game_id INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
			played_at TEXT NOT NULL DEFAULT (datetime('now')),
			duration_s INTEGER DEFAULT 0,
			platform TEXT DEFAULT ''
		);
		CREATE TABLE IF NOT EXISTS collections (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			created_at TEXT DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS game_collections (
			game_id INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
			collection_id INTEGER NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
			PRIMARY KEY (game_id, collection_id)
		);
		CREATE TABLE IF NOT EXISTS downloads (
			id INTEGER PRIMARY KEY,
			game_id INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
			url TEXT NOT NULL,
			host TEXT,
			filename TEXT,
			dest_path TEXT,
			status TEXT DEFAULT 'pending',
			bytes_downloaded INTEGER DEFAULT 0,
			total_bytes INTEGER DEFAULT 0,
			speed_bytes_per_sec REAL DEFAULT 0,
			percent_complete REAL DEFAULT 0,
			error TEXT,
			started_at TEXT,
			completed_at TEXT,
			created_at TEXT DEFAULT (datetime('now'))
		);
		CREATE VIRTUAL TABLE games_fts USING fts5(
			title, tags, developer, overview
		);
		CREATE TRIGGER IF NOT EXISTS games_ai AFTER INSERT ON games BEGIN
			INSERT INTO games_fts(rowid, title, tags, developer, overview)
			VALUES (new.id, new.title, new.tags, COALESCE((SELECT sm.developer FROM scraped_meta sm WHERE sm.game_id = new.id), ''), COALESCE((SELECT sm.overview FROM scraped_meta sm WHERE sm.game_id = new.id), ''));
		END;
		CREATE TRIGGER IF NOT EXISTS games_ad AFTER DELETE ON games BEGIN
			DELETE FROM games_fts WHERE rowid = old.id;
		END;
		CREATE TRIGGER IF NOT EXISTS games_au AFTER UPDATE ON games BEGIN
			DELETE FROM games_fts WHERE rowid = old.id;
			INSERT INTO games_fts(rowid, title, tags, developer, overview)
			VALUES (new.id, new.title, new.tags, COALESCE((SELECT sm.developer FROM scraped_meta sm WHERE sm.game_id = new.id), ''), COALESCE((SELECT sm.overview FROM scraped_meta sm WHERE sm.game_id = new.id), ''));
		END;
		CREATE TRIGGER IF NOT EXISTS scraped_meta_ai AFTER INSERT ON scraped_meta BEGIN
			INSERT INTO games_fts(rowid, title, tags, developer, overview)
			VALUES (new.game_id, COALESCE((SELECT title FROM games WHERE id = new.game_id), ''), COALESCE((SELECT tags FROM games WHERE id = new.game_id), '[]'), new.developer, new.overview);
		END;
		CREATE TRIGGER IF NOT EXISTS scraped_meta_ad AFTER DELETE ON scraped_meta BEGIN
			DELETE FROM games_fts WHERE rowid = old.game_id;
		END;
		CREATE TRIGGER IF NOT EXISTS scraped_meta_au AFTER UPDATE ON scraped_meta BEGIN
			DELETE FROM games_fts WHERE rowid = old.game_id;
			INSERT INTO games_fts(rowid, title, tags, developer, overview)
			VALUES (new.game_id, COALESCE((SELECT title FROM games WHERE id = new.game_id), ''), COALESCE((SELECT tags FROM games WHERE id = new.game_id), '[]'), new.developer, new.overview);
		END;
		PRAGMA user_version = 7;
	`
	if _, err := conn.Exec(ddl); err != nil {
		t.Fatalf("create v7 schema: %v", err)
	}

	if _, err := conn.Exec(`
		INSERT INTO games (id, title, engine, path, version) VALUES
			(1, 'RenPy Game', 'RenPy', '/games/renpy', '1.0'),
			(2, 'Old Flash Game', 'Flash', '/games/flash', '0.5');
		INSERT OR REPLACE INTO scraped_meta (game_id, developer, overview) VALUES (1, 'Dev One', 'Overview');
		INSERT INTO download_links (game_id, url, host) VALUES (1, 'https://x/1', 'pixeldrain');
		INSERT INTO play_history (game_id) VALUES (1);
		INSERT INTO collections (id, name) VALUES (1, 'Collection A');
		INSERT INTO game_collections (game_id, collection_id) VALUES (1, 1);
	`); err != nil {
		t.Fatalf("seed v7 data: %v", err)
	}
}

// Open() on a v7 database must rebuild the games table (adding Godot to the
// CHECK), preserve all rows and FK relationships, keep FTS working, and
// accept Godot engines afterwards.
func TestMigrateV8AddsGodotEngine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v7.sqlite")
	makeV7DB(t, path)

	d, err := Open(path)
	if err != nil {
		t.Fatalf("Open on v7 db: %v", err)
	}
	defer d.Close()

	var userVersion int
	if err := d.conn.QueryRow("PRAGMA user_version").Scan(&userVersion); err != nil {
		t.Fatal(err)
	}
	if userVersion != currentSchemaVersion {
		t.Fatalf("user_version = %d, want %d", userVersion, currentSchemaVersion)
	}

	// Rows survived.
	if n, _ := d.GameCount(); n != 2 {
		t.Errorf("GameCount = %d, want 2 after migration", n)
	}
	g, err := d.GetGame(1)
	if err != nil || g == nil {
		t.Fatalf("GetGame(1): %v", err)
	}
	if g.Title != "RenPy Game" || g.Engine != "RenPy" || g.Version != "1.0" {
		t.Errorf("game 1 = %+v, want intact RenPy Game", g)
	}
	meta, err := d.GetScrapedMeta(1)
	if err != nil || meta == nil || meta.Developer != "Dev One" {
		t.Errorf("scraped meta = %+v, want intact", meta)
	}
	links, err := d.ListDownloadLinks(1, "", true)
	if err != nil || len(links) != 1 || links[0].Host != "pixeldrain" {
		t.Errorf("download links = %+v, want intact", links)
	}

	// No FK violations after the rebuild.
	rows, err := d.conn.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var table, parent, child string
		var rowID int64
		if scanErr := rows.Scan(&table, &rowID, &parent, &child); scanErr == nil {
			t.Errorf("foreign key violation after migration: table=%s row=%d parent=%s child=%s", table, rowID, parent, child)
		} else {
			t.Errorf("foreign key violation after migration (scan failed: %v)", scanErr)
		}
	}

	// The whole point: Godot engines are now accepted.
	godotID, err := d.InsertGame(&Game{Title: "Godot Game", Path: "/games/godot", Engine: "Godot", Status: "active"})
	if err != nil {
		t.Fatalf("insert Godot game: %v", err)
	}
	gg, err := d.GetGame(godotID)
	if err != nil || gg == nil || gg.Engine != "Godot" {
		t.Fatalf("Godot game = %+v, want engine Godot", gg)
	}

	// FTS triggers were recreated on the new table.
	results, err := d.SearchGames("Godot Game")
	if err != nil || len(results) == 0 {
		t.Errorf("FTS search after migration: %v / %d results", err, len(results))
	}

	// Cascade delete still works through the re-pointed FK.
	if err := d.DeleteGamePermanent(1); err != nil {
		t.Fatalf("DeleteGamePermanent: %v", err)
	}
	if m, _ := d.GetScrapedMeta(1); m != nil {
		t.Error("scraped_meta survived cascade delete of its game")
	}
	remaining, _ := d.ListDownloadLinks(1, "", true)
	if len(remaining) != 0 {
		t.Errorf("download links survived cascade delete: %+v", remaining)
	}
}

var _ = os.Exit // keep os import if unused paths change
