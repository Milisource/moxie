package saves

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// saveScanChtimes stamps a file's modification time, failing the test when it
// cannot (save data recency is what the scan ranks on).
func saveScanChtimes(t *testing.T, path string, mod time.Time) {
	t.Helper()
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

// saveScanFind returns the candidate at path.
func saveScanFind(candidates []ScanCandidate, path string) (ScanCandidate, bool) {
	for _, c := range candidates {
		if filepath.Clean(c.Path) == filepath.Clean(path) {
			return c, true
		}
	}
	return ScanCandidate{}, false
}

// TestScanLiveSavesRanksByRecency scans a game directory holding save-ish
// files across nested directories: containing directories are reported with their
// match counts and ranked newest first, and a directory below the depth bound is
// ignored.
func TestScanLiveSavesRanksByRecency(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	engine, game := saveStoreEngineAt(t, base, "Scanned")
	game.Engine = "Others" // no external roots: the scan stays in the game dir
	game.Path = filepath.Join(base, "gametree")

	older := filepath.Join(game.Path, "saves")
	saveTestWrite(t, filepath.Join(older, "persistent"), "p")
	saveTestWrite(t, filepath.Join(older, "slot1.save"), "s")
	newer := filepath.Join(game.Path, "deep", "nested")
	saveTestWrite(t, filepath.Join(newer, "a.rpgsave"), "a")
	// Below the depth bound: the directory at depth 7 is not scanned.
	deep := filepath.Join(game.Path, "a", "b", "c", "d", "e", "f", "g", "h")
	saveTestWrite(t, filepath.Join(deep, "x.save"), "x")

	saveScanChtimes(t, filepath.Join(older, "persistent"), time.Now().Add(-3*time.Hour))
	saveScanChtimes(t, filepath.Join(older, "slot1.save"), time.Now().Add(-2*time.Hour))
	saveScanChtimes(t, filepath.Join(newer, "a.rpgsave"), time.Now().Add(-1*time.Hour))

	got, err := engine.scanLiveSaves(game, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("scanLiveSaves: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("candidates = %+v, want 2 (game dir only)", got)
	}

	if got[0].Path != newer {
		t.Errorf("candidates[0].Path = %q, want newest %q", got[0].Path, newer)
	}
	if got[0].Files != 1 {
		t.Errorf("candidates[0].Files = %d, want 1", got[0].Files)
	}
	if got[0].NewestFile != "a.rpgsave" {
		t.Errorf("candidates[0].NewestFile = %q, want %q", got[0].NewestFile, "a.rpgsave")
	}

	if got[1].Path != older {
		t.Errorf("candidates[1].Path = %q, want %q", got[1].Path, older)
	}
	if got[1].Files != 2 {
		t.Errorf("candidates[1].Files = %d, want 2", got[1].Files)
	}
	if got[1].NewestFile != "slot1.save" {
		t.Errorf("candidates[1].NewestFile = %q, want %q", got[1].NewestFile, "slot1.save")
	}
	if !got[0].NewestMod.After(got[1].NewestMod) {
		t.Errorf("candidates are not newest-first: %+v", got)
	}

	if _, ok := saveScanFind(got, deep); ok {
		t.Errorf("candidate below the depth bound was scanned: %+v", got)
	}
}

// TestScanLiveSavesCapsCandidates checks the scan returns at most ten
// candidates, keeping the ten most recent.
func TestScanLiveSavesCapsCandidates(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	engine, game := saveStoreEngineAt(t, base, "Many")
	game.Engine = "Others"
	game.Path = filepath.Join(base, "many")

	now := time.Now()
	for i := 1; i <= 12; i++ {
		dir := filepath.Join(game.Path, fmt.Sprintf("slot-%02d", i))
		saveTestWrite(t, filepath.Join(dir, "persistent"), "p")
		saveScanChtimes(t, filepath.Join(dir, "persistent"), now.Add(-time.Duration(i)*time.Minute))
	}

	got, err := engine.scanLiveSaves(game, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("scanLiveSaves: %v", err)
	}
	if len(got) != scanCandidateLimit {
		t.Fatalf("candidates = %d, want cap %d", len(got), scanCandidateLimit)
	}
	if want := filepath.Join(game.Path, "slot-01"); got[0].Path != want {
		t.Errorf("candidates[0].Path = %q, want newest %q", got[0].Path, want)
	}
	if want := filepath.Join(game.Path, "slot-10"); got[9].Path != want {
		t.Errorf("candidates[9].Path = %q, want %q (oldest two dropped)", got[9].Path, want)
	}
}

// TestScanLiveSavesRenpyExternalRoots scans the external Ren'Py save roots
// (the native ~/.renpy directory and the game's Wine prefix), and proves a
// root reachable both from the game directory and from the resolver's own roots
// is scanned once.
func TestScanLiveSavesRenpyExternalRoots(t *testing.T) {
	t.Parallel()

	t.Run("native and wine roots", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		engine, game := saveStoreEngineAt(t, base, "RenPy Scanned")
		game.Path = filepath.Join(base, "game")

		home := t.TempDir()
		native := filepath.Join(home, ".renpy", "SomeGame-123")
		saveTestWrite(t, filepath.Join(native, "persistent"), "native")

		game.WinePrefix = filepath.Join(base, "pfx")
		wine := filepath.Join(game.WinePrefix, "drive_c", "users", "steamuser",
			"AppData", "Roaming", "RenPy", "OtherGame-9")
		saveTestWrite(t, filepath.Join(wine, "persistent"), "wine")

		got, err := engine.scanLiveSaves(game, home, nil)
		if err != nil {
			t.Fatalf("scanLiveSaves: %v", err)
		}
		if _, ok := saveScanFind(got, native); !ok {
			t.Errorf("native Ren'Py root %s not scanned: %+v", native, got)
		}
		if _, ok := saveScanFind(got, wine); !ok {
			t.Errorf("Wine prefix root %s not scanned: %+v", wine, got)
		}
	})

	t.Run("dedupes a root reachable twice", func(t *testing.T) {
		t.Parallel()
		engine, game := saveStoreEngineAt(t, t.TempDir(), "Dedupe")
		home := t.TempDir()
		// The game directory IS the native Ren'Py root, so the subdirectory
		// below it is reachable through both root sources.
		game.Path = filepath.Join(home, ".renpy")
		shared := filepath.Join(game.Path, "SomeGame-1")
		saveTestWrite(t, filepath.Join(shared, "persistent"), "p")

		got, err := engine.scanLiveSaves(game, home, nil)
		if err != nil {
			t.Fatalf("scanLiveSaves: %v", err)
		}
		seen := 0
		for _, c := range got {
			if filepath.Clean(c.Path) == filepath.Clean(shared) {
				seen++
			}
		}
		if seen != 1 {
			t.Errorf("root shared by the game dir and the Ren'Py roots was scanned %d times: %+v", seen, got)
		}
	})

	t.Run("nested roots report a directory once", func(t *testing.T) {
		t.Parallel()
		engine, game := saveStoreEngineAt(t, t.TempDir(), "Nested")
		home := t.TempDir()
		// The game directory contains the native Ren'Py root, so the subdirectory
		// below it is reachable by walking either one.
		game.Path = home
		saveTestWrite(t, filepath.Join(home, ".renpy", "SomeGame-1", "persistent"), "p")

		got, err := engine.scanLiveSaves(game, home, nil)
		if err != nil {
			t.Fatalf("scanLiveSaves: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("candidates = %+v, want exactly one (nested roots)", got)
		}
	})

	t.Run("rpg maker wine roaming", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		engine, game := saveStoreEngineAt(t, base, "VX Scanned")
		game.Engine = "RPGMakerMV"
		game.Path = filepath.Join(base, "game")
		game.WinePrefix = filepath.Join(base, "pfx")
		saves := filepath.Join(game.WinePrefix, "drive_c", "users", "steamuser",
			"AppData", "Roaming", "MyGame")
		saveTestWrite(t, filepath.Join(saves, "global.rpgsave"), "g")

		got, err := engine.scanLiveSaves(game, t.TempDir(), nil)
		if err != nil {
			t.Fatalf("scanLiveSaves: %v", err)
		}
		if _, ok := saveScanFind(got, saves); !ok {
			t.Errorf("Wine Roaming root %s not scanned: %+v", saves, got)
		}
	})
}

// TestScanLiveSavesIgnoresExternalRootsForUnknownEngine pins that an
// unsupported engine only scans the game directory: stray save files outside the
// game never leak into its candidates.
func TestScanLiveSavesIgnoresExternalRootsForUnknownEngine(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	engine, game := saveStoreEngineAt(t, base, "Unknown")
	game.Engine = "Others"
	game.Path = filepath.Join(base, "game")
	saveTestMkdir(t, game.Path)

	home := t.TempDir()
	saveTestWrite(t, filepath.Join(home, ".renpy", "Stray-1", "persistent"), "p")

	got, err := engine.scanLiveSaves(game, home, []string{"~/.local/share/Steam"})
	if err != nil {
		t.Fatalf("scanLiveSaves: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("candidates = %+v, want none outside the game dir", got)
	}
}

// TestScanLiveSavesEmpty pins the empty cases: an empty game directory yields
// an empty slice without error, and a nil game yields nil.
func TestScanLiveSavesEmpty(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	engine, game := saveStoreEngineAt(t, base, "Empty")
	game.Engine = "Others"
	game.Path = filepath.Join(base, "game")
	saveTestMkdir(t, game.Path)

	got, err := engine.scanLiveSaves(game, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("scanLiveSaves on an empty game dir: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("candidates = %+v, want none", got)
	}

	got, err = engine.scanLiveSaves(nil, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("scanLiveSaves(nil): %v", err)
	}
	if got != nil {
		t.Errorf("candidates for a nil game = %+v, want nil", got)
	}
}
