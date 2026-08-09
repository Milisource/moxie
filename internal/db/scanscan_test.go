package db

import (
	"testing"
	"time"
)

// UpdateGameScanFields must fill version/engine/exe_path only when unset,
// always refresh size/scan time/mtime, and never touch user-edited columns
// (title, status, notes). The "unset" checks run inside the UPDATE, so a
// concurrent manual edit can never be clobbered with stale scanner data.
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
	err = d.UpdateGameScanFields(id, "1.0", "RenPy", "/games/test-game/game.exe", 2048, scanTime, scanTime)
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
	err = d.UpdateGameScanFields(id, "2.0", "Unity", "/detected/other.exe", 4096, time.Now().UTC(), time.Now().UTC())
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

	if err := d.UpdateGameScanFields(id, "", "Java", "", 0, time.Now().UTC(), time.Now().UTC()); err != nil {
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
