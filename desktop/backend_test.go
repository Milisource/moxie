package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/engine"
	"github.com/mili/moxie/internal/scanner"
)

func detected(title, path string) scanner.DetectedGame {
	return scanner.DetectedGame{
		Title:     title,
		Path:      path,
		ExePath:   filepath.Join(path, title+".exe"),
		Engine:    engine.RenPy,
		Version:   "1.0",
		SizeBytes: 1024,
	}
}

func TestUpsertDetected_InsertsNew(t *testing.T) {
	a := newTestApp(t)
	dir := t.TempDir()
	g := detected("New Game", filepath.Join(dir, "New Game"))

	inserted, updated, errs := a.upsertDetected([]scanner.DetectedGame{g})
	if inserted != 1 || updated != 0 || len(errs) != 0 {
		t.Fatalf("upsert = inserted %d updated %d errs %v, want 1/0/none", inserted, updated, errs)
	}
	got, err := a.db.GetGameByPath(g.Path)
	if err != nil || got == nil {
		t.Fatalf("GetGameByPath: %v", err)
	}
	if got.Title != "New Game" || got.Engine != string(engine.RenPy) || got.Version != "1.0" {
		t.Errorf("row = %+v, want title/engine/version from detection", got)
	}
}

func TestUpsertDetected_UpdatePreservesCuratedFields(t *testing.T) {
	a := newTestApp(t)
	dir := t.TempDir()
	g := detected("Curated Game", filepath.Join(dir, "Curated Game"))
	a.upsertDetected([]scanner.DetectedGame{g})

	// User curates version + exe path; a rescan detects different ones.
	existing, err := a.db.GetGameByPath(g.Path)
	if err != nil || existing == nil {
		t.Fatalf("GetGameByPath: %v", err)
	}
	if err := a.db.UpdateGame(&db.Game{
		ID:      existing.ID,
		Title:   existing.Title,
		Path:    existing.Path,
		Engine:  existing.Engine,
		Version: "9.9",
		ExePath: "/custom/curated.exe",
		Status:  "active",
	}); err != nil {
		t.Fatalf("UpdateGame: %v", err)
	}

	re := detected("Curated Game", g.Path)
	re.Version = "2.0"
	re.ExePath = "/detected/other.exe"
	re.Engine = engine.Unity
	inserted, updated, errs := a.upsertDetected([]scanner.DetectedGame{re})
	if inserted != 0 || updated != 1 || len(errs) != 0 {
		t.Fatalf("upsert = inserted %d updated %d errs %v, want 0/1/none", inserted, updated, errs)
	}

	got, _ := a.db.GetGameByPath(g.Path)
	if got.Version != "9.9" {
		t.Errorf("version = %q, want curated 9.9 kept", got.Version)
	}
	if got.ExePath != "/custom/curated.exe" {
		t.Errorf("exePath = %q, want curated path kept", got.ExePath)
	}
	if got.Engine != string(engine.RenPy) {
		t.Errorf("engine = %q, want curated RenPy kept over detected Unity", got.Engine)
	}
	if got.SizeBytes != re.SizeBytes {
		t.Errorf("sizeBytes = %d, want detected %d", got.SizeBytes, re.SizeBytes)
	}
}

// A soft-deleted game found again on disk must come back to life — the
// UNIQUE index on path would otherwise block re-insertion while the row
// stays invisible in every listing.
func TestUpsertDetected_ResurrectsSoftDeleted(t *testing.T) {
	a := newTestApp(t)
	dir := t.TempDir()
	g := detected("Back From Dead", filepath.Join(dir, "Back From Dead"))
	a.upsertDetected([]scanner.DetectedGame{g})

	existing, _ := a.db.GetGameByPath(g.Path)
	if err := a.db.DeleteGame(existing.ID); err != nil {
		t.Fatalf("DeleteGame: %v", err)
	}
	// Deleted games are hidden from listings.
	if games, err := a.db.ListActiveGames("", ""); err != nil || len(games) != 0 {
		t.Fatalf("ListActiveGames after delete = %d games (%v), want 0", len(games), err)
	}

	inserted, updated, errs := a.upsertDetected([]scanner.DetectedGame{g})
	if inserted != 0 || updated != 1 || len(errs) != 0 {
		t.Fatalf("upsert = inserted %d updated %d errs %v, want 0/1/none", inserted, updated, errs)
	}

	games, err := a.db.ListActiveGames("", "")
	if err != nil || len(games) != 1 {
		t.Fatalf("ListActiveGames after rescan = %d games (%v), want 1 resurrected", len(games), err)
	}
	if games[0].ID != existing.ID {
		t.Errorf("resurrected row id = %d, want %d (same row, no duplicate)", games[0].ID, existing.ID)
	}
}

func TestSelectDownloadLink_ScoresAndFilters(t *testing.T) {
	mk := func(name, url, host, platform string, dead bool) DesktopDownloadLink {
		return DesktopDownloadLink{Name: name, URL: url, Host: host, Platform: platform, IsDead: dead}
	}
	any := "all"

	t.Run("empty links", func(t *testing.T) {
		if _, err := selectDownloadLink(nil); err == nil {
			t.Error("expected error for no links")
		}
	})

	t.Run("dead links skipped", func(t *testing.T) {
		links := []DesktopDownloadLink{
			mk("dead", "https://x/1", "mega", any, true),
			mk("live", "https://x/2", "pixeldrain", any, false),
		}
		got, err := selectDownloadLink(links)
		if err != nil || got == nil || got.Name != "live" {
			t.Errorf("select = %+v err %v, want live link", got, err)
		}
	})

	t.Run("all dead", func(t *testing.T) {
		links := []DesktopDownloadLink{mk("dead", "https://x/1", "mega", any, true)}
		if _, err := selectDownloadLink(links); err == nil {
			t.Error("expected error when every link is dead")
		}
	})
}

func TestNormalizeTitle(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  Corruption of Champions  ", "corruption of champions"},
		{"[RELEASE] Game Name", "game name"},
		{"[SOLVED][v0.5] Game", "game"},
		{"Game Name [v1.0]", "game name"},
		{"Game Name (STEAM)", "game name"},
		{"Game (Demo) [0.2]", "game"},
	}
	for _, c := range cases {
		if got := normalizeTitle(c.in); got != c.want {
			t.Errorf("normalizeTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The full manual-scan pipeline: scan the tree, upsert the results, remove
// vanished games — the same composition RescanDirectory performs (minus the
// Wails event emission, which needs a live runtime context).
func TestScanUpsertRemovePipeline(t *testing.T) {
	a := newTestApp(t)
	root := t.TempDir()

	gameDir := filepath.Join(root, "Good Game v1.0")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(gameDir, "Good Game.exe")
	if err := os.WriteFile(exe, []byte("MZ..."), 0o644); err != nil {
		t.Fatal(err)
	}

	// A stale DB row whose directory never existed on disk.
	a.upsertDetected([]scanner.DetectedGame{detected("Ghost Game", filepath.Join(root, "Ghost Game"))})

	detected, err := scanner.ScanFiltered(context.Background(), root, nil, nil)
	if err != nil {
		t.Fatalf("ScanFiltered: %v", err)
	}
	if len(detected) != 1 {
		t.Fatalf("ScanFiltered found %d games, want 1", len(detected))
	}
	if !strings.Contains(detected[0].Path, "Good Game") {
		t.Errorf("detected = %+v, want the Good Game dir", detected[0])
	}

	inserted, updated, errs := a.upsertDetected(detected)
	if inserted != 1 || updated != 0 || len(errs) != 0 {
		t.Fatalf("upsert = inserted %d updated %d errs %v, want 1/0/none", inserted, updated, errs)
	}

	// Rescan: same result, now an update.
	inserted, updated, errs = a.upsertDetected(detected)
	if inserted != 0 || updated != 1 || len(errs) != 0 {
		t.Fatalf("re-upsert = inserted %d updated %d errs %v, want 0/1/none", inserted, updated, errs)
	}

	removed := a.removeMissingUnder(root)
	if removed != 1 {
		t.Fatalf("removeMissingUnder removed %d, want 1 (Ghost Game)", removed)
	}
	games, err := a.db.ListActiveGames("", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 || !strings.Contains(games[0].Path, "Good Game") {
		t.Errorf("active games after pipeline = %+v, want only Good Game", games)
	}
}
