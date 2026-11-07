package commands

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mili/moxie/internal/updater"
)

// TestRemoveExtractionIfUnused_AdoptedMergeKeepsLiveGame is the regression
// guard for the download command's deferred cleanup: when Merge adopts the
// extraction tree it renames it into the game dir, so the cleanup hook must
// not delete the staging path — that path no longer holds scratch data, it
// holds the install (or nothing at all).
func TestRemoveExtractionIfUnused_AdoptedMergeKeepsLiveGame(t *testing.T) {
	base := t.TempDir()
	gameDir := filepath.Join(base, "MyGame")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gameDir, "game.exe"), []byte("old-exe"), 0o644); err != nil {
		t.Fatal(err)
	}

	staging := filepath.Join(base, "staging")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "game.exe"), []byte("new-exe"), 0o644); err != nil {
		t.Fatal(err)
	}

	mergeResult, err := updater.Merge(context.Background(), gameDir, "RPGM", staging, true)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}
	if !mergeResult.Adopted {
		t.Fatal("expected the merge to adopt the staging root")
	}

	removeExtractionIfUnused(staging, mergeResult.Adopted)

	if _, err := os.Stat(filepath.Join(gameDir, "game.exe")); err != nil {
		t.Fatalf("live game dir was deleted by extraction cleanup: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(gameDir, "game.exe"))
	if err != nil || string(data) != "new-exe" {
		t.Fatalf("live game dir corrupted by extraction cleanup: %q, err=%v", data, err)
	}
}

// TestRemoveExtractionIfUnused_CopyMergeRemovesStaging guards the other side
// of the contract: a non-adopted merge leaves scratch data behind and the
// cleanup hook must still remove it.
func TestRemoveExtractionIfUnused_CopyMergeRemovesStaging(t *testing.T) {
	staging := filepath.Join(t.TempDir(), "staging")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "game.exe"), []byte("new-exe"), 0o644); err != nil {
		t.Fatal(err)
	}

	removeExtractionIfUnused(staging, false)

	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Errorf("staging dir should be removed, stat err = %v", err)
	}
}
