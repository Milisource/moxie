package commands

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeSizedFile writes count bytes at path, creating parents.
func writeSizedFile(t *testing.T, path string, count int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, make([]byte, count), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// agePath backdates a path's access and modification times by age.
func agePath(t *testing.T, path string, age time.Duration) {
	t.Helper()
	stamp := time.Now().Add(-age)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func pathExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	return err == nil
}

func categoryEmpty(c pruneCategory) bool {
	return c.Count == 0 && c.Bytes == 0 && len(c.Entries) == 0
}

func TestPruneBackupTrees(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		age        time.Duration
		createLive bool
		symlink    bool
		wantRemove bool
	}{
		{name: "old backup with live sibling is removed", age: 48 * time.Hour, createLive: true, wantRemove: true},
		{name: "young backup is kept", age: time.Hour, createLive: true},
		{name: "backup without live sibling is kept", age: 48 * time.Hour},
		{name: "symlinked backup is skipped", age: 48 * time.Hour, createLive: true, symlink: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			gameDir := filepath.Join(root, "Game")
			if tt.createLive {
				if err := os.MkdirAll(gameDir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			backup := gameDir + ".old"

			if tt.symlink {
				real := filepath.Join(root, "real")
				writeSizedFile(t, filepath.Join(real, "data.bin"), 1024)
				if err := os.Symlink(real, backup); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.MkdirAll(backup, 0o755); err != nil {
					t.Fatal(err)
				}
				writeSizedFile(t, filepath.Join(backup, "data.bin"), 1024)
				agePath(t, backup, tt.age)
			}

			report, err := pruneLibrary([]string{gameDir}, pruneOptions{
				Backups:   true,
				OlderThan: 24 * time.Hour,
			})
			if err != nil {
				t.Fatalf("pruneLibrary: %v", err)
			}

			removed := !pathExists(t, backup)
			if removed != tt.wantRemove {
				t.Fatalf("backup removed = %v, want %v", removed, tt.wantRemove)
			}
			if tt.wantRemove {
				if report.Backups.Count != 1 || report.Backups.Bytes != 1024 {
					t.Fatalf("report = %+v, want 1 backup / 1024 bytes", report.Backups)
				}
				if !categoryEmpty(report.Archives) {
					t.Fatalf("unexpected archive report: %+v", report.Archives)
				}
			} else if !categoryEmpty(report.Backups) {
				t.Fatalf("report = %+v, want no removals", report.Backups)
			}

			// A skipped symlink must never take its target with it.
			if tt.symlink && !pathExists(t, filepath.Join(root, "real", "data.bin")) {
				t.Fatal("symlink target was deleted")
			}
		})
	}
}

func TestPruneArchiveFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		fileName   string
		age        time.Duration
		nested     bool
		symlink    bool
		wantRemove bool
	}{
		{name: "old zip is removed", fileName: "Game_v1.zip", age: 48 * time.Hour, wantRemove: true},
		{name: "old rar is removed", fileName: "Game.part1.rar", age: 48 * time.Hour, wantRemove: true},
		{name: "young zip is kept", fileName: "Game_v1.zip", age: time.Hour},
		{name: "non-archive is kept", fileName: "notes.txt", age: 48 * time.Hour},
		{name: "split part is kept", fileName: "Game.7z.001", age: 48 * time.Hour},
		{name: "nested archive is kept", fileName: "Game_v1.zip", age: 48 * time.Hour, nested: true},
		{name: "symlinked archive is skipped", fileName: "Game_v1.zip", age: 48 * time.Hour, symlink: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			gameDir := filepath.Join(root, "Game")
			if err := os.MkdirAll(gameDir, 0o755); err != nil {
				t.Fatal(err)
			}
			downloads := filepath.Join(root, "downloads")

			target := filepath.Join(downloads, tt.fileName)
			if tt.symlink {
				real := filepath.Join(root, "outside.zip")
				writeSizedFile(t, real, 2048)
				if err := os.MkdirAll(downloads, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(real, target); err != nil {
					t.Fatal(err)
				}
			} else {
				if tt.nested {
					target = filepath.Join(downloads, "extracted", tt.fileName)
				}
				writeSizedFile(t, target, 2048)
				agePath(t, target, tt.age)
			}

			report, err := pruneLibrary([]string{gameDir}, pruneOptions{
				Archives:  true,
				OlderThan: 24 * time.Hour,
			})
			if err != nil {
				t.Fatalf("pruneLibrary: %v", err)
			}

			removed := !pathExists(t, target)
			if removed != tt.wantRemove {
				t.Fatalf("archive removed = %v, want %v", removed, tt.wantRemove)
			}
			if tt.wantRemove {
				if report.Archives.Count != 1 || report.Archives.Bytes != 2048 {
					t.Fatalf("report = %+v, want 1 archive / 2048 bytes", report.Archives)
				}
			} else if !categoryEmpty(report.Archives) {
				t.Fatalf("report = %+v, want no removals", report.Archives)
			}

			if tt.symlink && !pathExists(t, filepath.Join(root, "outside.zip")) {
				t.Fatal("symlink target was deleted")
			}
		})
	}
}

func TestPruneSkipsSymlinkedDownloadsDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	gameDir := filepath.Join(root, "Game")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	realDownloads := filepath.Join(root, "elsewhere")
	archive := filepath.Join(realDownloads, "Game.zip")
	writeSizedFile(t, archive, 512)
	agePath(t, archive, 48*time.Hour)
	if err := os.Symlink(realDownloads, filepath.Join(root, "downloads")); err != nil {
		t.Fatal(err)
	}

	report, err := pruneLibrary([]string{gameDir}, pruneOptions{Archives: true, OlderThan: 24 * time.Hour})
	if err != nil {
		t.Fatalf("pruneLibrary: %v", err)
	}
	if !categoryEmpty(report.Archives) {
		t.Fatalf("report = %+v, want no removals", report.Archives)
	}
	if !pathExists(t, archive) {
		t.Fatal("archive behind a symlinked downloads dir was deleted")
	}
}

func TestPruneDryRunChangesNothing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	gameDir := filepath.Join(root, "Game")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	backup := gameDir + ".old"
	writeSizedFile(t, filepath.Join(backup, "data.bin"), 1024)
	agePath(t, backup, 48*time.Hour)
	archive := filepath.Join(root, "downloads", "Game.zip")
	writeSizedFile(t, archive, 2048)
	agePath(t, archive, 48*time.Hour)

	report, err := pruneLibrary([]string{gameDir}, pruneOptions{
		Backups:   true,
		Archives:  true,
		OlderThan: 24 * time.Hour,
		DryRun:    true,
	})
	if err != nil {
		t.Fatalf("pruneLibrary: %v", err)
	}
	if report.Backups.Count != 1 || report.Backups.Bytes != 1024 {
		t.Fatalf("backup report = %+v, want 1 / 1024", report.Backups)
	}
	if len(report.Backups.Entries) != 1 || report.Backups.Entries[0].Path != backup {
		t.Fatalf("backup entries = %+v, want %s", report.Backups.Entries, backup)
	}
	if report.Archives.Count != 1 || report.Archives.Bytes != 2048 {
		t.Fatalf("archive report = %+v, want 1 / 2048", report.Archives)
	}
	if len(report.Archives.Entries) != 1 || report.Archives.Entries[0].Path != archive {
		t.Fatalf("archive entries = %+v, want %s", report.Archives.Entries, archive)
	}
	if !pathExists(t, backup) {
		t.Fatal("dry run deleted the backup")
	}
	if !pathExists(t, archive) {
		t.Fatal("dry run deleted the archive")
	}

	// Same inputs without --dry-run must remove both.
	report, err = pruneLibrary([]string{gameDir}, pruneOptions{
		Backups:   true,
		Archives:  true,
		OlderThan: 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("pruneLibrary: %v", err)
	}
	if report.Backups.Count != 1 || report.Archives.Count != 1 {
		t.Fatalf("report = %+v, want one removal per category", report)
	}
	if pathExists(t, backup) || pathExists(t, archive) {
		t.Fatal("expected both leftovers to be removed")
	}
}

func TestPruneLockBlocksPruning(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	gameDir := filepath.Join(root, "Game")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	backup := gameDir + ".old"
	writeSizedFile(t, filepath.Join(backup, "data.bin"), 1024)
	agePath(t, backup, 48*time.Hour)
	archive := filepath.Join(root, "downloads", "Game.zip")
	writeSizedFile(t, archive, 2048)
	agePath(t, archive, 48*time.Hour)

	lock := filepath.Join(root, "auto-update.lock")
	if err := os.WriteFile(lock, []byte("12345\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := pruneLibrary([]string{gameDir}, pruneOptions{
		Backups:   true,
		Archives:  true,
		OlderThan: 24 * time.Hour,
		LockPath:  lock,
	})
	if err == nil {
		t.Fatal("expected the lock file to block pruning")
	}
	if !categoryEmpty(report.Backups) || !categoryEmpty(report.Archives) {
		t.Fatalf("report = %+v, want zero", report)
	}
	if !pathExists(t, backup) || !pathExists(t, archive) {
		t.Fatal("pruning ran despite the lock")
	}
}

func TestPruneNothingToDo(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	gameDir := filepath.Join(root, "Game")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}

	report, err := pruneLibrary([]string{gameDir}, pruneOptions{
		Backups:   true,
		Archives:  true,
		OlderThan: 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("pruneLibrary: %v", err)
	}
	if !categoryEmpty(report.Backups) || !categoryEmpty(report.Archives) {
		t.Fatalf("report = %+v, want zero", report)
	}
}

func TestPruneIgnoresOtherOldNames(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	gameDir := filepath.Join(root, "Game")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Only the exact <gameDir>.old suffix is a valid target.
	other := filepath.Join(root, "Game.old-old")
	writeSizedFile(t, filepath.Join(other, "data.bin"), 1024)
	agePath(t, other, 48*time.Hour)

	report, err := pruneLibrary([]string{gameDir}, pruneOptions{Backups: true, OlderThan: 24 * time.Hour})
	if err != nil {
		t.Fatalf("pruneLibrary: %v", err)
	}
	if !categoryEmpty(report.Backups) {
		t.Fatalf("report = %+v, want no removals", report.Backups)
	}
	if !pathExists(t, other) {
		t.Fatal("a directory that is not exactly <gameDir>.old was deleted")
	}
}

func TestOwnedByCurrentUser(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "file.bin")
	writeSizedFile(t, path, 1)
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !ownedByCurrentUser(info) {
		t.Fatal("a file created by this process should be owned by the current user")
	}
}
