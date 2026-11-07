package updater

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

// TestMerge_AdoptsStagingRoot covers the fast path: the extracted tree is
// moved into the game dir with a single rename, user files are restored from
// the backup, and the caller's cleanup of the (now consumed) staging path
// cannot delete the install.
func TestMerge_AdoptsStagingRoot(t *testing.T) {
	base := t.TempDir()
	gameDir := filepath.Join(base, "MyGame")
	write(t, filepath.Join(gameDir, "game.exe"), "old-exe")
	write(t, filepath.Join(gameDir, "saves", "save01.sav"), "user-save-data")

	staging := filepath.Join(base, "staging")
	write(t, filepath.Join(staging, "game.exe"), "new-exe")
	write(t, filepath.Join(staging, "data.rpa"), "new-data")

	result, err := Merge(context.Background(), gameDir, "RenPy", staging, true)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}
	if !result.Adopted {
		t.Fatal("expected the extracted tree to be adopted by rename")
	}
	if result.FilesCopied != 2 {
		t.Errorf("FilesCopied = %d, want 2", result.FilesCopied)
	}

	// The staging root moved: its old path is gone.
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Errorf("staging root still exists after adoption, stat err = %v", err)
	}
	if got := read(t, filepath.Join(gameDir, "game.exe")); got != "new-exe" {
		t.Errorf("game.exe = %q, want new-exe", got)
	}
	if got := read(t, filepath.Join(gameDir, "data.rpa")); got != "new-data" {
		t.Errorf("data.rpa = %q, want new-data", got)
	}
	if got := read(t, filepath.Join(gameDir, "saves", "save01.sav")); got != "user-save-data" {
		t.Errorf("save file = %q, want user-save-data", got)
	}
	if got := read(t, filepath.Join(gameDir+".old", "game.exe")); got != "old-exe" {
		t.Errorf("backup game.exe = %q, want old-exe", got)
	}

	// The download command's deferred cleanup targets the extraction
	// destination. Simulate it: the live install must survive.
	removeExtractionDestination(t, staging)
	if got := read(t, filepath.Join(gameDir, "game.exe")); got != "new-exe" {
		t.Fatalf("live install deleted by staging cleanup: game.exe = %q", got)
	}
	if _, err := os.Stat(filepath.Join(gameDir, "data.rpa")); err != nil {
		t.Fatalf("live install deleted by staging cleanup: %v", err)
	}
}

// TestMerge_AdoptsNestedRootAndCleansStagingParent covers the wrapped
// extraction shape: findGameRoot unwraps the single game folder, which is
// adopted while the emptied staging parent is removed.
func TestMerge_AdoptsNestedRootAndCleansStagingParent(t *testing.T) {
	base := t.TempDir()
	gameDir := filepath.Join(base, "MyGame")
	write(t, filepath.Join(gameDir, "game.exe"), "old-exe")

	staging := filepath.Join(base, "staging")
	write(t, filepath.Join(staging, "Release", "game.exe"), "new-exe")
	write(t, filepath.Join(staging, "Release", "data.rpa"), "new-data")

	result, err := Merge(context.Background(), gameDir, "RenPy", staging, true)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}
	if !result.Adopted {
		t.Fatal("expected the nested extracted root to be adopted")
	}
	if got := read(t, filepath.Join(gameDir, "game.exe")); got != "new-exe" {
		t.Errorf("game.exe = %q, want new-exe", got)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Errorf("emptied staging parent left behind, stat err = %v", err)
	}
}

// TestMerge_AdoptionFailureRollsBack forces a failure after the tree has been
// adopted (the preserved save cannot be placed because the new tree ships a
// regular file named "saves") and verifies the previous install is restored
// and the stale .old chain is put back exactly as before.
func TestMerge_AdoptionFailureRollsBack(t *testing.T) {
	base := t.TempDir()
	gameDir := filepath.Join(base, "MyGame")
	write(t, filepath.Join(gameDir, "game.exe"), "old-exe")
	write(t, filepath.Join(gameDir, "saves", "save01.sav"), "user-save-data")

	staleBackup := gameDir + ".old"
	write(t, filepath.Join(staleBackup, "game.exe"), "prev-exe")

	staging := filepath.Join(base, "staging")
	write(t, filepath.Join(staging, "game.exe"), "new-exe")
	// A file where restorePreserved must create the "saves" directory.
	write(t, filepath.Join(staging, "saves"), "not-a-directory")

	result, err := Merge(context.Background(), gameDir, "RPGM", staging, true)
	if err == nil {
		t.Fatal("expected merge to fail while restoring preserved files")
	}
	if result.Adopted {
		t.Error("Adopted should be cleared after rollback")
	}
	if result.BackupPath != "" {
		t.Errorf("BackupPath should be cleared after rollback, got %q", result.BackupPath)
	}

	// The previous install is back in place.
	if got := read(t, filepath.Join(gameDir, "game.exe")); got != "old-exe" {
		t.Errorf("game.exe after rollback = %q, want old-exe", got)
	}
	if got := read(t, filepath.Join(gameDir, "saves", "save01.sav")); got != "user-save-data" {
		t.Errorf("save file after rollback = %q, want user-save-data", got)
	}

	// The stale backup was restored to gameDir.old; no staging chain remains.
	if got := read(t, filepath.Join(staleBackup, "game.exe")); got != "prev-exe" {
		t.Errorf("stale backup after rollback = %q, want prev-exe", got)
	}
	if _, err := os.Stat(staleBackup + ".old"); !os.IsNotExist(err) {
		t.Errorf("stale staging dir left behind after rollback, stat err = %v", err)
	}

	// The adopted tree was discarded during rollback; the staging path is
	// gone, so the caller's cleanup is a no-op.
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Errorf("adopted staging tree survived rollback, stat err = %v", err)
	}
}

// TestMerge_CrossDeviceFallsBackToCopy simulates EXDEV from the adoption
// rename and verifies the copy path still runs with the backup rollback net.
func TestMerge_CrossDeviceFallsBackToCopy(t *testing.T) {
	original := renameAdopt
	renameAdopt = func(oldpath, newpath string) error {
		return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: syscall.EXDEV}
	}
	t.Cleanup(func() { renameAdopt = original })

	base := t.TempDir()
	gameDir := filepath.Join(base, "MyGame")
	write(t, filepath.Join(gameDir, "game.exe"), "old-exe")

	staging := filepath.Join(base, "staging")
	write(t, filepath.Join(staging, "game.exe"), "new-exe")

	result, err := Merge(context.Background(), gameDir, "RPGM", staging, true)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}
	if result.Adopted {
		t.Error("Adopted must be false when the rename is cross-device")
	}
	if result.FilesCopied != 1 {
		t.Errorf("FilesCopied = %d, want 1", result.FilesCopied)
	}
	if got := read(t, filepath.Join(gameDir, "game.exe")); got != "new-exe" {
		t.Errorf("game.exe = %q, want new-exe", got)
	}
	if got := read(t, filepath.Join(gameDir+".old", "game.exe")); got != "old-exe" {
		t.Errorf("backup game.exe = %q, want old-exe", got)
	}
	// The copy path leaves staging for the caller to delete.
	if _, err := os.Stat(filepath.Join(staging, "game.exe")); err != nil {
		t.Errorf("staging should still exist after copy fallback: %v", err)
	}
}

// TestMerge_RefusesAdoptionWithEscapingSymlink verifies that a tree the copy
// path would sanitise is never adopted, and the escaping link is still skipped.
func TestMerge_RefusesAdoptionWithEscapingSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	base := t.TempDir()
	gameDir := filepath.Join(base, "MyGame")
	write(t, filepath.Join(gameDir, "game.exe"), "old-exe")

	staging := filepath.Join(base, "staging")
	write(t, filepath.Join(staging, "game.exe"), "new-exe")
	if err := os.Symlink("/etc/passwd", filepath.Join(staging, "escape")); err != nil {
		t.Fatal(err)
	}

	result, err := Merge(context.Background(), gameDir, "RPGM", staging, true)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}
	if result.Adopted {
		t.Fatal("tree with an escaping symlink must not be adopted")
	}
	if _, err := os.Lstat(filepath.Join(gameDir, "escape")); !os.IsNotExist(err) {
		t.Errorf("escaping symlink was installed, stat err = %v", err)
	}
	if got := read(t, filepath.Join(gameDir, "game.exe")); got != "new-exe" {
		t.Errorf("game.exe = %q, want new-exe", got)
	}
}

// A flat archive is extracted straight into the caller's download directory,
// so findGameRoot hands back that shared container. Renaming it into the game
// would move the download directory itself; the merge must use the copy path.
func TestMerge_FlatExtractionWithArchiveIsNotAdopted(t *testing.T) {
	base := t.TempDir()
	gameDir := filepath.Join(base, "MyGame")
	write(t, filepath.Join(gameDir, "game.exe"), "old-exe")

	container := filepath.Join(base, "downloads")
	write(t, filepath.Join(container, "game.exe"), "new-exe")
	write(t, filepath.Join(container, "Game_v1.1.rar"), "archive-bytes")

	result, err := Merge(context.Background(), gameDir, "RenPy", container, true)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}
	if result.Adopted {
		t.Fatal("download container must not be adopted by rename")
	}
	if _, err := os.Stat(filepath.Join(container, "Game_v1.1.rar")); err != nil {
		t.Errorf("download container was moved: %v", err)
	}
	if got := read(t, filepath.Join(gameDir, "game.exe")); got != "new-exe" {
		t.Errorf("game.exe = %q, want new-exe", got)
	}
}

// removeExtractionDestination mirrors the download command's deferred cleanup
// so the test proves the real ownership contract, not a simplified one.
func removeExtractionDestination(t *testing.T, destination string) {
	t.Helper()
	if destination == "" {
		t.Fatal("extraction destination should not be empty")
	}
	if err := os.RemoveAll(destination); err != nil {
		t.Fatalf("remove extraction destination: %v", err)
	}
}
