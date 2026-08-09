package updater

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// matchPath
// ---------------------------------------------------------------------------

func TestMatchPath_ExactBasename(t *testing.T) {
	t.Parallel()
	if !matchPath("Game.ini", "Game.ini") {
		t.Error("exact match should work")
	}
	if !matchPath("Game.ini", "sub/Game.ini") {
		t.Error("basename match should work")
	}
	if matchPath("Game.ini", "Game.exe") {
		t.Error("different name should not match")
	}
}

func TestMatchPath_GlobPattern(t *testing.T) {
	t.Parallel()
	if !matchPath("*.sav", "file01.sav") {
		t.Error("glob *.sav should match file01.sav")
	}
	if !matchPath("*.sav", "saves/file01.sav") {
		t.Error("glob *.sav should match saves/file01.sav by basename")
	}
	if matchPath("*.sav", "file01.png") {
		t.Error("glob *.sav should not match .png")
	}
}

func TestMatchPath_DirPrefix(t *testing.T) {
	t.Parallel()
	if !matchPath("saves/*", "saves/file01.sav") {
		t.Error("saves/* should match saves/file01.sav")
	}
	if !matchPath("saves/*", "saves/subdir/data.sav") {
		t.Error("saves/* should match nested saves/subdir/data.sav")
	}
	if matchPath("saves/*", "other/file01.sav") {
		t.Error("saves/* should NOT match other/file01.sav")
	}
}

func TestMatchPath_DeepDirPrefix(t *testing.T) {
	t.Parallel()
	if !matchPath("game/saves/*", "game/saves/file.sav") {
		t.Error("game/saves/* should match game/saves/file.sav")
	}
	if matchPath("game/saves/*", "saves/file.sav") {
		t.Error("game/saves/* should NOT match saves/file.sav")
	}
}

// ---------------------------------------------------------------------------
// patterns
// ---------------------------------------------------------------------------

func TestPatterns_UnknownEngineReturnsDefault(t *testing.T) {
	t.Parallel()
	p := patterns("UnknownFakeEngine")
	if len(p) == 0 {
		t.Fatal("expected non-empty default patterns")
	}
	found := false
	for _, pat := range p {
		if pat == "saves/*" {
			found = true
			break
		}
	}
	if !found {
		t.Error("default patterns should include saves/*")
	}
}

func TestPatterns_RenPyHasSpecific(t *testing.T) {
	t.Parallel()
	p := patterns("RenPy")
	hasSaves := false
	hasOptions := false
	for _, pat := range p {
		if pat == "game/saves/*" {
			hasSaves = true
		}
		if pat == "game/options.rpy" {
			hasOptions = true
		}
	}
	if !hasSaves {
		t.Error("RenPy should preserve game/saves/*")
	}
	if !hasOptions {
		t.Error("RenPy should preserve game/options.rpy")
	}
}

func TestPatterns_CommonPatternsIncluded(t *testing.T) {
	t.Parallel()
	p := patterns("RPGM")
	hasCommon := false
	for _, pat := range p {
		if pat == "*.sav" {
			hasCommon = true
			break
		}
	}
	if !hasCommon {
		t.Error("all engines should include *.sav (common pattern)")
	}
}

// ---------------------------------------------------------------------------
// shouldPreserve
// ---------------------------------------------------------------------------

func TestShouldPreserve_ReturnsFalseWhenNoFile(t *testing.T) {
	preserve := []string{"saves/*"}
	// No file at destPath → should NOT preserve
	if shouldPreserve("saves/data.sav", "/nonexistent/path", preserve) {
		t.Error("should not preserve when dest file doesn't exist")
	}
}

func TestShouldPreserve_ReturnsFalseWhenNoMatch(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "game.exe")
	os.WriteFile(f, []byte("data"), 0644)

	preserve := []string{"saves/*"}
	if shouldPreserve("game.exe", f, preserve) {
		t.Error("should not preserve game.exe (not in save patterns)")
	}
}

func TestShouldPreserve_ReturnsTrueWhenMatchAndExists(t *testing.T) {
	tmp := t.TempDir()
	saveDir := filepath.Join(tmp, "saves")
	os.MkdirAll(saveDir, 0755)
	saveFile := filepath.Join(saveDir, "file01.sav")
	os.WriteFile(saveFile, []byte("savedata"), 0644)

	preserve := []string{"saves/*"}
	if !shouldPreserve("saves/file01.sav", saveFile, preserve) {
		t.Error("should preserve saves/file01.sav")
	}
}

// ---------------------------------------------------------------------------
// findGameRoot
// ---------------------------------------------------------------------------

func TestFindGameRoot_SingleSubdir(t *testing.T) {
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, "MyGame", "data"), 0755)
	os.WriteFile(filepath.Join(tmp, "MyGame", "game.exe"), []byte("exe"), 0644)
	os.WriteFile(filepath.Join(tmp, "README.txt"), []byte("readme"), 0644)

	got := findGameRoot(tmp)
	if filepath.Base(got) != "MyGame" {
		t.Errorf("expected MyGame, got %s", filepath.Base(got))
	}
}

func TestFindGameRoot_MultipleSubdirs(t *testing.T) {
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, "GameA"), 0755)
	os.MkdirAll(filepath.Join(tmp, "GameB"), 0755)

	got := findGameRoot(tmp)
	if got != tmp {
		t.Errorf("expected %s, got %s", tmp, got)
	}
}

func TestFindGameRoot_NoSubdirs(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "game.exe"), []byte("exe"), 0644)

	got := findGameRoot(tmp)
	if got != tmp {
		t.Errorf("expected %s, got %s", tmp, got)
	}
}

// ---------------------------------------------------------------------------
// Merge (integration)
// ---------------------------------------------------------------------------

func TestMerge_PreservesSaves(t *testing.T) {
	// Setup: existing game with user save
	gameDir := t.TempDir()
	gameDir = filepath.Join(gameDir, "MyGame")
	os.MkdirAll(filepath.Join(gameDir, "saves"), 0755)
	os.WriteFile(filepath.Join(gameDir, "saves", "save01.sav"), []byte("user-save-data"), 0644)
	os.WriteFile(filepath.Join(gameDir, "game.exe"), []byte("old-exe"), 0644)

	// Setup: extracted updated version with new exe + default save
	extractDir := t.TempDir()
	extractDir = filepath.Join(extractDir, "MyGame")
	os.MkdirAll(filepath.Join(extractDir, "saves"), 0755)
	os.WriteFile(filepath.Join(extractDir, "saves", "save01.sav"), []byte("default-save-data"), 0644)
	os.WriteFile(filepath.Join(extractDir, "game.exe"), []byte("new-exe"), 0644)
	os.WriteFile(filepath.Join(extractDir, "new-asset.dat"), []byte("new-content"), 0644)

	// Merge
	parent := filepath.Dir(extractDir)
	result, err := Merge(gameDir, "RPGM", parent, false)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}

	// Verify: user save preserved
	data, _ := os.ReadFile(filepath.Join(gameDir, "saves", "save01.sav"))
	if string(data) != "user-save-data" {
		t.Errorf("save file was overwritten! got %q", string(data))
	}

	// Verify: new exe was copied
	data, _ = os.ReadFile(filepath.Join(gameDir, "game.exe"))
	if string(data) != "new-exe" {
		t.Errorf("game.exe was not updated! got %q", string(data))
	}

	// Verify: new asset was copied
	data, _ = os.ReadFile(filepath.Join(gameDir, "new-asset.dat"))
	if string(data) != "new-content" {
		t.Errorf("new-asset.dat was not copied! got %q", string(data))
	}

	// Verify: counts make sense
	if result.FilesCopied < 2 {
		t.Errorf("expected at least 2 files copied, got %d", result.FilesCopied)
	}
	if result.FilesPreserved < 1 {
		t.Errorf("expected at least 1 file preserved, got %d", result.FilesPreserved)
	}
}

func TestMerge_FirstTimeNoGame(t *testing.T) {
	tmp := t.TempDir()
	noGame := filepath.Join(tmp, "NoGame")
	result, err := Merge(noGame, "RPGM", tmp, false)
	if err == nil {
		t.Errorf("expected error for nonexistent game dir")
	}
	_ = result
}

func TestMerge_BackupCreatesOldDir(t *testing.T) {
	gameDir := t.TempDir()
	os.MkdirAll(gameDir, 0755)
	os.WriteFile(filepath.Join(gameDir, "game.exe"), []byte("old-exe"), 0644)

	extractDir := t.TempDir()
	os.WriteFile(filepath.Join(extractDir, "game.exe"), []byte("new-exe"), 0644)

	result, err := Merge(gameDir, "RPGM", extractDir, true)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}
	if result.BackupPath == "" {
		t.Error("expected backup path, got empty")
	}
	if _, err := os.Stat(result.BackupPath); os.IsNotExist(err) {
		t.Errorf("backup directory %s should exist", result.BackupPath)
	}
}

// TestMerge_FailedCopyRollsBack verifies that a copy failure mid-merge
// restores the original game dir from the backup, leaving the game intact.
// The copy is forced to fail with a dangling symlink in the extracted dir:
// os.Open on it fails with ENOENT regardless of uid, so the test does not
// depend on permissions.
func TestMerge_FailedCopyRollsBack(t *testing.T) {
	gameDir := t.TempDir()
	os.MkdirAll(filepath.Join(gameDir, "saves"), 0755)
	os.WriteFile(filepath.Join(gameDir, "saves", "save01.sav"), []byte("user-save-data"), 0644)
	os.WriteFile(filepath.Join(gameDir, "game.exe"), []byte("old-exe"), 0644)

	extractDir := t.TempDir()
	os.WriteFile(filepath.Join(extractDir, "game.exe"), []byte("new-exe"), 0644)
	// "broken-link.dat" sorts before "game.exe", so the copy fails before
	// any file is written.
	if err := os.Symlink("nonexistent-target", filepath.Join(extractDir, "broken-link.dat")); err != nil {
		t.Fatal(err)
	}

	result, err := Merge(gameDir, "RPGM", extractDir, true)
	if err == nil {
		t.Fatal("expected merge to fail on broken symlink, got nil")
	}
	if result.BackupPath != "" {
		t.Errorf("BackupPath should be cleared after rollback, got %q", result.BackupPath)
	}

	// The live game dir must be restored to its original contents.
	data, err := os.ReadFile(filepath.Join(gameDir, "game.exe"))
	if err != nil {
		t.Fatalf("game.exe missing after rollback: %v", err)
	}
	if string(data) != "old-exe" {
		t.Errorf("game.exe not restored after failed merge: got %q, want %q", string(data), "old-exe")
	}
	data, err = os.ReadFile(filepath.Join(gameDir, "saves", "save01.sav"))
	if err != nil {
		t.Fatalf("save file missing after rollback: %v", err)
	}
	if string(data) != "user-save-data" {
		t.Errorf("save file not restored after failed merge: got %q", string(data))
	}

	// The rollback consumed the backup: no gameDir.old may remain.
	if _, err := os.Stat(gameDir + ".old"); !os.IsNotExist(err) {
		t.Errorf("backup dir should be consumed by rollback, stat err = %v", err)
	}
}

// TestMerge_FailedCopyPreservesStaleBackup verifies that a previous merge's
// backup (gameDir.old) survives a failed merge — it must not be deleted
// before the new merge completes, or a retry would have nothing to restore.
func TestMerge_FailedCopyPreservesStaleBackup(t *testing.T) {
	gameDir := t.TempDir()
	os.WriteFile(filepath.Join(gameDir, "game.exe"), []byte("old-exe"), 0644)

	// A stale backup from a previous merge holds the only intact
	// pre-merge copy — the failed merge must not destroy it.
	staleBackup := gameDir + ".old"
	os.MkdirAll(filepath.Join(staleBackup, "saves"), 0755)
	os.WriteFile(filepath.Join(staleBackup, "game.exe"), []byte("prev-exe"), 0644)
	os.WriteFile(filepath.Join(staleBackup, "saves", "save01.sav"), []byte("prev-save"), 0644)

	extractDir := t.TempDir()
	os.WriteFile(filepath.Join(extractDir, "game.exe"), []byte("new-exe"), 0644)
	if err := os.Symlink("nonexistent-target", filepath.Join(extractDir, "broken-link.dat")); err != nil {
		t.Fatal(err)
	}

	if _, err := Merge(gameDir, "RPGM", extractDir, true); err == nil {
		t.Fatal("expected merge to fail on broken symlink, got nil")
	}

	// Live dir restored.
	data, err := os.ReadFile(filepath.Join(gameDir, "game.exe"))
	if err != nil {
		t.Fatalf("game.exe missing after rollback: %v", err)
	}
	if string(data) != "old-exe" {
		t.Errorf("game.exe not restored after failed merge: got %q", string(data))
	}

	// Stale backup preserved at its original name with its contents.
	data, err = os.ReadFile(filepath.Join(staleBackup, "game.exe"))
	if err != nil {
		t.Fatalf("stale backup game.exe missing: %v", err)
	}
	if string(data) != "prev-exe" {
		t.Errorf("stale backup destroyed by failed merge: got %q, want %q", string(data), "prev-exe")
	}
	data, err = os.ReadFile(filepath.Join(staleBackup, "saves", "save01.sav"))
	if err != nil {
		t.Fatalf("stale backup save missing: %v", err)
	}
	if string(data) != "prev-save" {
		t.Errorf("stale backup save destroyed by failed merge: got %q", string(data))
	}

	// No leftover staging dir (gameDir.old.old).
	if _, err := os.Stat(staleBackup + ".old"); !os.IsNotExist(err) {
		t.Errorf("stale staging dir left behind, stat err = %v", err)
	}
}

// TestMerge_StaleBackupReplacedOnSuccess verifies that a stale backup is
// discarded only after the new merge has completed successfully, and the
// backup then holds the pre-merge game version.
func TestMerge_StaleBackupReplacedOnSuccess(t *testing.T) {
	gameDir := t.TempDir()
	os.WriteFile(filepath.Join(gameDir, "game.exe"), []byte("old-exe"), 0644)

	staleBackup := gameDir + ".old"
	os.MkdirAll(staleBackup, 0755)
	os.WriteFile(filepath.Join(staleBackup, "game.exe"), []byte("prev-exe"), 0644)

	extractDir := t.TempDir()
	os.WriteFile(filepath.Join(extractDir, "game.exe"), []byte("new-exe"), 0644)

	result, err := Merge(gameDir, "RPGM", extractDir, true)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}
	if result.BackupPath != staleBackup {
		t.Errorf("expected backup at %s, got %q", staleBackup, result.BackupPath)
	}

	// The backup now holds the pre-merge version; the stale copy is gone.
	data, err := os.ReadFile(filepath.Join(staleBackup, "game.exe"))
	if err != nil {
		t.Fatalf("backup game.exe missing after merge: %v", err)
	}
	if string(data) != "old-exe" {
		t.Errorf("backup does not hold pre-merge version: got %q, want %q", string(data), "old-exe")
	}
	if _, err := os.Stat(staleBackup + ".old"); !os.IsNotExist(err) {
		t.Errorf("stale staging dir left behind after success, stat err = %v", err)
	}
}

// TestMerge_PreservesExecutableBits verifies launcher executables keep
// their +x permission after a merge — os.Create would otherwise drop it
// to 0666 & umask and the game would stop launching.
func TestMerge_PreservesExecutableBits(t *testing.T) {
	gameDir := t.TempDir()
	os.MkdirAll(gameDir, 0755)
	os.WriteFile(filepath.Join(gameDir, "game.sh"), []byte("old-launcher"), 0755)
	os.WriteFile(filepath.Join(gameDir, "data.rpa"), []byte("old-data"), 0644)

	extractDir := t.TempDir()
	os.WriteFile(filepath.Join(extractDir, "game.sh"), []byte("new-launcher"), 0755)
	os.WriteFile(filepath.Join(extractDir, "data.rpa"), []byte("new-data"), 0644)

	_, err := Merge(gameDir, "RenPy", extractDir, true)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}

	fi, err := os.Stat(filepath.Join(gameDir, "game.sh"))
	if err != nil {
		t.Fatalf("stat merged launcher: %v", err)
	}
	if fi.Mode().Perm()&0111 == 0 {
		t.Errorf("launcher lost executable bit after merge: %v", fi.Mode().Perm())
	}

	fi, err = os.Stat(filepath.Join(gameDir, "data.rpa"))
	if err != nil {
		t.Fatalf("stat merged data file: %v", err)
	}
	if fi.Mode().Perm()&0111 != 0 {
		t.Errorf("data file unexpectedly executable after merge: %v", fi.Mode().Perm())
	}
}
