package main

import (
	"context"
	"strings"
	"testing"
)

// The update pipeline is single-flight: while one run holds the lock, every
// other update/install entry point must reject without spawning a second
// pipeline (two concurrent merges into the same game directory would corrupt
// it). Exercises the guard's reject path deterministically — the pipeline
// itself does real network IO and Wails event emission, so it is never
// started here.
func TestGameUpdateGuardRejectsConcurrentRuns(t *testing.T) {
	a := newTestApp(t)
	a.ctx = context.Background()
	id := addGame(t, a, "Test Game", "/games/test-game")

	// Simulate an in-flight pipeline.
	a.updateRunning.Store(true)

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
}

// Manual scans and watcher rescans are single-flight: a second ScanDirectory
// while one is running must be rejected before any goroutine is spawned.
func TestScanDirectoryGuardRejectsConcurrent(t *testing.T) {
	a := newTestApp(t)

	a.scanRunning.Store(true)
	err := a.ScanDirectory(t.TempDir())
	if err == nil {
		t.Fatal("expected error while scan in progress, got nil")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("error = %q, want mention of single-flight guard", err)
	}
}
