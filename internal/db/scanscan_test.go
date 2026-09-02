package db

import (
	"testing"
	"time"
)

// UpdateGameScanFields must fill version/engine/exe_path only when unset (for
// background watcher upserts), always refresh size/scan time/mtime, and never
// touch user-edited columns (title, status, notes). The "unset" checks run
// inside the UPDATE, so a concurrent manual edit can never be clobbered with
// stale scanner data.
func TestUpdateGameScanFields_FillsOnlyUnsetFields(t *testing.T) {
	d := setupTestDB(t)
	id, err := d.InsertGame(&Game{
		Title:  "Test Game",
		Path:   "/games/test-game",
		Engine: "Unknown",
		Status: "active",
	})
	if err != nil {
		t.Fatalf("InsertGame: %v", err)
	}

	// A manual edit that lands between the scanner's read and write.
	if err := d.UpdateGameStatus(id, "completed"); err != nil {
		t.Fatalf("UpdateGameStatus: %v", err)
	}

	scanTime := time.Now().UTC().Add(-time.Minute)
	err = d.UpdateGameScanFields(id, "1.0", "RenPy", "/games/test-game/game.exe", 2048, scanTime, scanTime, false)
	if err != nil {
		t.Fatalf("UpdateGameScanFields: %v", err)
	}

	g, err := d.GetGame(id)
	if err != nil || g == nil {
		t.Fatalf("GetGame: %v", err)
	}
	if g.Version != "1.0" || g.Engine != "RenPy" || g.ExePath != "/games/test-game/game.exe" {
		t.Errorf("filled fields = version %q engine %q exe %q, want detected values", g.Version, g.Engine, g.ExePath)
	}
	if g.SizeBytes != 2048 {
		t.Errorf("sizeBytes = %d, want refreshed 2048", g.SizeBytes)
	}
	// The concurrent manual edit must survive.
	if g.Status != "completed" {
		t.Errorf("status = %q, want completed (concurrent edit preserved)", g.Status)
	}
	if g.Title != "Test Game" {
		t.Errorf("title = %q, want untouched", g.Title)
	}
}

func TestUpdateGameScanFields_PreservesCuratedValues(t *testing.T) {
	d := setupTestDB(t)
	id, err := d.InsertGame(&Game{
		Title:  "Curated Game",
		Path:   "/games/curated",
		Engine: "RenPy",
		Status: "active",
	})
	if err != nil {
		t.Fatalf("InsertGame: %v", err)
	}
	if err := d.UpdateGameExePath(id, "/custom/curated.exe"); err != nil {
		t.Fatalf("UpdateGameExePath: %v", err)
	}

	// Scanner detects a different engine/exe — the curated values win.
	err = d.UpdateGameScanFields(id, "2.0", "Unity", "/detected/other.exe", 4096, time.Now().UTC(), time.Now().UTC(), false)
	if err != nil {
		t.Fatalf("UpdateGameScanFields: %v", err)
	}

	g, err := d.GetGame(id)
	if err != nil || g == nil {
		t.Fatalf("GetGame: %v", err)
	}
	if g.Engine != "RenPy" {
		t.Errorf("engine = %q, want curated RenPy", g.Engine)
	}
	if g.ExePath != "/custom/curated.exe" {
		t.Errorf("exePath = %q, want curated path", g.ExePath)
	}
	if g.Version != "2.0" {
		t.Errorf("version = %q, want detected 2.0 (was unset)", g.Version)
	}
}

func TestUpdateGameScanFields_UnknownEngineFilled(t *testing.T) {
	d := setupTestDB(t)
	id, err := d.InsertGame(&Game{
		Title:  "Unknown Engine Game",
		Path:   "/games/unknown-engine",
		Engine: "Unknown",
		Status: "active",
	})
	if err != nil {
		t.Fatalf("InsertGame: %v", err)
	}

	if err := d.UpdateGameScanFields(id, "", "Java", "", 0, time.Now().UTC(), time.Now().UTC(), false); err != nil {
		t.Fatalf("UpdateGameScanFields: %v", err)
	}

	g, err := d.GetGame(id)
	if err != nil || g == nil {
		t.Fatalf("GetGame: %v", err)
	}
	if g.Engine != "Java" {
		t.Errorf("engine = %q, want Java (Unknown replaced)", g.Engine)
	}
}

// force=true is an explicit full rescan: the scanner's fresh detection wins
// for version/engine/exe_path even when the record already has values, while
// user-owned columns (title, status) still must never be touched.
func TestUpdateGameScanFields_ForceOverwritesSetFields(t *testing.T) {
	d := setupTestDB(t)
	id, err := d.InsertGame(&Game{
		Title:   "Forced Game",
		Path:    "/games/forced",
		Engine:  "RenPy",
		Version: "1.0",
		Status:  "active",
	})
	if err != nil {
		t.Fatalf("InsertGame: %v", err)
	}
	if err := d.UpdateGameExePath(id, "/custom/curated.exe"); err != nil {
		t.Fatalf("UpdateGameExePath: %v", err)
	}

	// Force rescan detects different engine/version/exe — detection wins.
	scanTime := time.Now().UTC().Add(-time.Minute)
	err = d.UpdateGameScanFields(id, "2.0", "Unity", "/detected/game.exe", 8192, scanTime, scanTime, true)
	if err != nil {
		t.Fatalf("UpdateGameScanFields(force): %v", err)
	}

	g, err := d.GetGame(id)
	if err != nil || g == nil {
		t.Fatalf("GetGame: %v", err)
	}
	if g.Engine != "Unity" {
		t.Errorf("engine = %q, want forced Unity (was curated RenPy)", g.Engine)
	}
	if g.ExePath != "/detected/game.exe" {
		t.Errorf("exePath = %q, want forced detection", g.ExePath)
	}
	if g.Version != "2.0" {
		t.Errorf("version = %q, want forced 2.0", g.Version)
	}
	if g.SizeBytes != 8192 {
		t.Errorf("sizeBytes = %d, want refreshed 8192", g.SizeBytes)
	}
	// User-owned columns survive a force upsert.
	if g.Title != "Forced Game" || g.Status != "active" {
		t.Errorf("user columns clobbered: title %q status %q", g.Title, g.Status)
	}
}

// force=true with an empty detection must clear a stale scan-owned value — an
// explicit rescan reflects what is on disk right now.
func TestUpdateGameScanFields_ForceClearsStale(t *testing.T) {
	d := setupTestDB(t)
	id, err := d.InsertGame(&Game{
		Title:   "Cleared Game",
		Path:    "/games/cleared",
		Engine:  "RenPy",
		Version: "1.0",
		Status:  "active",
	})
	if err != nil {
		t.Fatalf("InsertGame: %v", err)
	}
	if err := d.UpdateGameExePath(id, "/stale/game.exe"); err != nil {
		t.Fatalf("UpdateGameExePath: %v", err)
	}

	if err := d.UpdateGameScanFields(id, "", "Unknown", "", 0, time.Now().UTC(), time.Now().UTC(), true); err != nil {
		t.Fatalf("UpdateGameScanFields(force): %v", err)
	}

	g, err := d.GetGame(id)
	if err != nil || g == nil {
		t.Fatalf("GetGame: %v", err)
	}
	if g.Version != "" {
		t.Errorf("version = %q, want cleared by force rescan", g.Version)
	}
	if g.ExePath != "" {
		t.Errorf("exePath = %q, want cleared by force rescan", g.ExePath)
	}
}
