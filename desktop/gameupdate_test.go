package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mili/moxie/internal/db"
)

// Each game has at most one update/install run: while one holds a game,
// every other entry point for that game must reject without spawning a
// second pipeline (two concurrent merges into the same game directory would
// corrupt it). Exercises the guard's reject path deterministically — the pipeline
// itself does real network IO and Wails event emission, so it is never
// started here.
func TestGameUpdateGuardRejectsConcurrentRuns(t *testing.T) {
	a := newTestApp(t)
	a.ctx = context.Background()
	id := addGame(t, a, "Test Game", "/games/test-game")

	// Simulate an in-flight pipeline for this game and a running batch.
	a.updateGate().claimGame(id)
	a.batchRunning.Store(true)

	t.Run("DownloadGameUpdate", func(t *testing.T) {
		err := a.DownloadGameUpdate(id)
		if err == nil {
			t.Fatal("expected error while update in progress, got nil")
		}
		if !strings.Contains(err.Error(), "already in progress") {
			t.Errorf("error = %q, want mention of in-progress guard", err)
		}
	})

	t.Run("DownloadAllUpdates", func(t *testing.T) {
		err := a.DownloadAllUpdates()
		if err == nil {
			t.Fatal("expected error while update in progress, got nil")
		}
		if !strings.Contains(err.Error(), "already in progress") {
			t.Errorf("error = %q, want mention of in-progress guard", err)
		}
	})

	t.Run("InstallGameSharesLock", func(t *testing.T) {
		err := a.InstallGame(id, t.TempDir())
		if err == nil {
			t.Fatal("expected error while update in progress, got nil")
		}
		if !strings.Contains(err.Error(), "in progress") {
			t.Errorf("error = %q, want mention of in-progress guard", err)
		}
	})

	t.Run("SelfUpdateExcluded", func(t *testing.T) {
		if a.updateGate().claimExclusive() {
			t.Fatal("self-update claimed the gate while a game update runs")
		}
	})

	t.Run("ProvideUpdateFileSharesLock", func(t *testing.T) {
		err := a.ProvideUpdateFile(id)
		if err == nil {
			t.Fatal("expected error while update in progress, got nil")
		}
		if !strings.Contains(err.Error(), "in progress") {
			t.Errorf("error = %q, want mention of in-progress guard", err)
		}
	})
}

// Parallel update workers sync their games concurrently, so the pipeline's
// internal sync must not take the global netBusy guard — routing it through
// netBusy is what made every game but the first fail with "another network
// request is already in progress". The exported binding keeps the guard for
// stacked UI clicks.
func TestSyncSingleGameNetBusyBoundary(t *testing.T) {
	a := newTestApp(t)
	id := addGame(t, a, "Test Game", "/games/test-game")

	// Simulate another blocking network binding already in flight.
	a.netBusy.Store(true)
	defer a.netBusy.Store(false)

	t.Run("internal sync bypasses netBusy", func(t *testing.T) {
		// A nonexistent id short-circuits at the DB lookup, before any network
		// IO, so the only way to see the netBusy error here is a guard leak.
		err := a.syncSingleGame(999999)
		if err == nil {
			t.Fatal("expected an error for a nonexistent game id")
		}
		if strings.Contains(err.Error(), "another network request is already in progress") {
			t.Fatalf("internal sync was blocked by netBusy: %v", err)
		}
	})

	t.Run("interactive binding rejects while netBusy", func(t *testing.T) {
		err := a.SyncSingleGame(id)
		if err == nil || !strings.Contains(err.Error(), "another network request is already in progress") {
			t.Fatalf("SyncSingleGame = %v, want netBusy rejection", err)
		}
	})
}

// A game with an in-flight update/install must not accept an interactive sync:
// both rewrite the same game row, so the binding rejects rather than racing.
func TestSyncSingleGameRejectsWhileUpdating(t *testing.T) {
	a := newTestApp(t)
	id := addGame(t, a, "Test Game", "/games/test-game")

	a.updateGate().claimGame(id)
	defer a.updateGate().release(id)

	err := a.SyncSingleGame(id)
	if err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("SyncSingleGame = %v, want rejection while the game is updating", err)
	}
}

// Manual scans and watcher rescans are single-flight: a second ScanDirectory
// while one is running must be rejected before any goroutine is spawned.
func TestScanDirectoryGuardRejectsConcurrent(t *testing.T) {
	a := newTestApp(t)

	a.scanRunning.Store(true)
	err := a.ScanDirectory(t.TempDir(), false)
	if err == nil {
		t.Fatal("expected error while scan in progress, got nil")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("error = %q, want mention of single-flight guard", err)
	}
}

// A completed update/install must survive the watcher's non-force rescan. The
// install directory keeps its old version in its name (e.g. ".../Condemned
// Bunker v0.16"), and the merge's file writes trip the watcher, which
// re-detects that stale version. recordAppliedVersion stamps the applied
// version source 'f95' so UpdateGameScanFields leaves it alone; without it the
// row silently reverts to the folder-name version and keeps prompting.
func TestRecordAppliedVersionSurvivesRescan(t *testing.T) {
	a := newTestApp(t)
	id := addGame(t, a, "Condemned Bunker", "/games/condemned-bunker")

	g, err := a.db.GetGame(id)
	if err != nil || g == nil {
		t.Fatalf("GetGame: %v", err)
	}
	g.LatestVersion = "0.18"
	g.Version = "0.16"
	g.VersionSource = "scanner"

	recordAppliedVersion(g)
	if g.Version != "0.18" || g.VersionSource != "f95" {
		t.Fatalf("recordAppliedVersion = version %q source %q, want 0.18/f95", g.Version, g.VersionSource)
	}
	if err := a.db.UpdateGame(g); err != nil {
		t.Fatalf("UpdateGame: %v", err)
	}

	// The watcher's non-force scan re-detects the stale folder-name version.
	now := time.Now().UTC()
	if err := a.db.UpdateGameScanFields(id, "0.16", "HTML", "", 0, now, now, false); err != nil {
		t.Fatalf("UpdateGameScanFields: %v", err)
	}

	got, err := a.db.GetGame(id)
	if err != nil || got == nil {
		t.Fatalf("GetGame after scan: %v", err)
	}
	if got.Version != "0.18" {
		t.Errorf("version = %q after rescan, want 0.18 (applied version must not revert to the folder name)", got.Version)
	}
}

// An empty latest version must not clear a known installed version: the
// updatable() gate guarantees a non-empty latest, but recordAppliedVersion is
// also reachable from install paths, so keep the guard.
func TestRecordAppliedVersionIgnoresEmptyLatest(t *testing.T) {
	g := &db.Game{Version: "0.16", VersionSource: "scanner", LatestVersion: ""}
	recordAppliedVersion(g)
	if g.Version != "0.16" || g.VersionSource != "scanner" {
		t.Errorf("recordAppliedVersion with empty latest = version %q source %q, want 0.16/scanner unchanged", g.Version, g.VersionSource)
	}
}
