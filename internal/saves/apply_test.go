package saves

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mili/moxie/internal/db"
)

// saveActiveSet returns the named set when it exists and is active, else nil.
func saveActiveSet(t *testing.T, engine *Engine, game *db.Game, name string) *db.SaveSet {
	t.Helper()
	set, err := engine.DB().GetSaveSet(game.ID, name)
	if err != nil {
		t.Fatalf("GetSaveSet(%q): %v", name, err)
	}
	if set == nil || !set.Active {
		return nil
	}
	return set
}

// saveSnapshotCount counts sets with Source "snapshot" for a game.
func saveSnapshotCount(t *testing.T, engine *Engine, game *db.Game) int {
	t.Helper()
	sets, err := engine.List(game)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	n := 0
	for _, s := range sets {
		if s.Source == "snapshot" {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// KindDir round trip
// ---------------------------------------------------------------------------

func TestApplyKindDirRoundTrip(t *testing.T) {
	t.Parallel()
	engine, game := saveStoreEngine(t, "Round Trip")

	live := t.TempDir()
	if err := engine.SetManualPath(game, live); err != nil {
		t.Fatalf("SetManualPath: %v", err)
	}
	saveTestWrite(t, filepath.Join(live, "persistent"), "live-persistent")
	saveTestWrite(t, filepath.Join(live, "slot1.save"), "live-slot1")
	saveTestWrite(t, filepath.Join(live, "extra.txt"), "keep-me")

	snap, err := engine.Snapshot(game, "", "")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if !strings.HasPrefix(snap.Name, "mine-") {
		t.Errorf("snapshot Name = %q, want mine- prefix", snap.Name)
	}
	if snap.Label != "my saves" {
		t.Errorf("snapshot Label = %q, want %q", snap.Label, "my saves")
	}
	if snap.Source != "snapshot" {
		t.Errorf("snapshot Source = %q, want snapshot", snap.Source)
	}
	if snap.ApplyRoot != "" {
		t.Errorf("snapshot ApplyRoot = %q, want empty", snap.ApplyRoot)
	}
	if snap.Active {
		t.Error("snapshot should not be active")
	}

	// The set dir mirrors the live data verbatim.
	setDir := engine.SetPath(game, snap.Name)
	saveStoreAssertFile(t, filepath.Join(setDir, "persistent"), "live-persistent")
	saveStoreAssertFile(t, filepath.Join(setDir, "slot1.save"), "live-slot1")
	saveStoreAssertFile(t, filepath.Join(setDir, "extra.txt"), "keep-me")

	// Mark the snapshot active so the apply below does not auto-snapshot; that
	// path is covered by TestApplyAutoSnapshot.
	if err := engine.DB().SetActiveSaveSet(game.ID, snap.ID); err != nil {
		t.Fatalf("SetActiveSaveSet: %v", err)
	}

	// A distinct set to switch to.
	src := t.TempDir()
	saveTestWrite(t, filepath.Join(src, "persistent"), "hundred-persistent")
	saveTestWrite(t, filepath.Join(src, "slot2.save"), "hundred-slot2")
	other, err := engine.Import(game, src, ImportOptions{Name: "hundred"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	res, err := engine.Apply(game, other.Name)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.AutoSnapshot != "" {
		t.Errorf("AutoSnapshot = %q, want empty (active set already present)", res.AutoSnapshot)
	}
	if res.FilesCopied != 2 {
		t.Errorf("FilesCopied = %d, want 2", res.FilesCopied)
	}
	if res.Target.Path != live {
		t.Errorf("Target.Path = %q, want %q", res.Target.Path, live)
	}
	if !res.Set.Active {
		t.Error("applied set should be active in the result")
	}

	// Same-name overwrite; unmatched live files survive.
	saveStoreAssertFile(t, filepath.Join(live, "persistent"), "hundred-persistent")
	saveStoreAssertFile(t, filepath.Join(live, "slot2.save"), "hundred-slot2")
	saveStoreAssertFile(t, filepath.Join(live, "slot1.save"), "live-slot1")
	saveStoreAssertFile(t, filepath.Join(live, "extra.txt"), "keep-me")

	if saveActiveSet(t, engine, game, "hundred") == nil {
		t.Fatal("hundred should be the active set after apply")
	}
	if saveActiveSet(t, engine, game, snap.Name) != nil {
		t.Error("previous snapshot should no longer be active")
	}

	// Restore brings the original live data back.
	rres, err := engine.Restore(game)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if rres.Set.Name != snap.Name {
		t.Errorf("Restore applied %q, want %q", rres.Set.Name, snap.Name)
	}
	if !rres.Set.Active {
		t.Error("restored set should be active")
	}
	saveStoreAssertFile(t, filepath.Join(live, "persistent"), "live-persistent")
	saveStoreAssertFile(t, filepath.Join(live, "slot1.save"), "live-slot1")
	saveStoreAssertFile(t, filepath.Join(live, "extra.txt"), "keep-me")
	// slot2 came from the hundred apply and is unmatched by the snapshot, so it
	// survives the restore (copy-overwrite never deletes).
	saveStoreAssertFile(t, filepath.Join(live, "slot2.save"), "hundred-slot2")
}

// ---------------------------------------------------------------------------
// apply_root handling
// ---------------------------------------------------------------------------

func TestApplyHonorsApplyRoot(t *testing.T) {
	t.Parallel()
	engine, game := saveStoreEngine(t, "Rooted")

	live := t.TempDir()
	if err := engine.SetManualPath(game, live); err != nil {
		t.Fatalf("SetManualPath: %v", err)
	}

	src := t.TempDir()
	inner := filepath.Join(src, "CoolGame", "game", "saves")
	saveTestWrite(t, filepath.Join(inner, "persistent"), "rooted-p")
	saveTestWrite(t, filepath.Join(inner, "slot1.save"), "rooted-s")
	saveTestWrite(t, filepath.Join(src, "CoolGame", "readme.txt"), "not-a-save")

	set, err := engine.Import(game, src, ImportOptions{Name: "rooted"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if set.ApplyRoot != "CoolGame/game/saves" {
		t.Fatalf("ApplyRoot = %q, want CoolGame/game/saves", set.ApplyRoot)
	}

	res, err := engine.Apply(game, set.Name)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.AutoSnapshot != "" {
		t.Errorf("AutoSnapshot = %q, want empty (live dir was empty)", res.AutoSnapshot)
	}
	if res.FilesCopied != 2 {
		t.Errorf("FilesCopied = %d, want 2", res.FilesCopied)
	}

	// Only the apply_root subtree's contents land at the live root.
	saveStoreAssertFile(t, filepath.Join(live, "persistent"), "rooted-p")
	saveStoreAssertFile(t, filepath.Join(live, "slot1.save"), "rooted-s")
	saveStoreAssertAbsent(t, filepath.Join(live, "CoolGame"))
	saveStoreAssertAbsent(t, filepath.Join(live, "readme.txt"))
}

// ---------------------------------------------------------------------------
// Auto-snapshot
// ---------------------------------------------------------------------------

func TestApplyAutoSnapshot(t *testing.T) {
	t.Parallel()
	engine, game := saveStoreEngine(t, "Auto Snap")

	live := t.TempDir()
	if err := engine.SetManualPath(game, live); err != nil {
		t.Fatalf("SetManualPath: %v", err)
	}
	saveTestWrite(t, filepath.Join(live, "persistent"), "legit")

	src := t.TempDir()
	saveTestWrite(t, filepath.Join(src, "persistent"), "full")
	other, err := engine.Import(game, src, ImportOptions{Name: "full-save"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	res, err := engine.Apply(game, other.Name)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.AutoSnapshot == "" {
		t.Fatal("expected an auto-snapshot on first apply with untracked live data")
	}
	if !strings.HasPrefix(res.AutoSnapshot, "mine-") {
		t.Errorf("AutoSnapshot = %q, want mine- prefix", res.AutoSnapshot)
	}

	snap, err := engine.DB().GetSaveSet(game.ID, res.AutoSnapshot)
	if err != nil {
		t.Fatalf("GetSaveSet: %v", err)
	}
	if snap == nil {
		t.Fatalf("auto snapshot %q row missing", res.AutoSnapshot)
	}
	if snap.Source != "snapshot" {
		t.Errorf("auto snapshot Source = %q, want snapshot", snap.Source)
	}
	if snap.Label != "my saves (auto)" {
		t.Errorf("auto snapshot Label = %q, want %q", snap.Label, "my saves (auto)")
	}
	if snap.Active {
		t.Error("auto snapshot should not be active")
	}
	if saveActiveSet(t, engine, game, other.Name) == nil {
		t.Error("applied set should be active")
	}

	// A second apply must not auto-snapshot again: an active set now exists.
	res2, err := engine.Apply(game, other.Name)
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if res2.AutoSnapshot != "" {
		t.Errorf("second apply AutoSnapshot = %q, want empty", res2.AutoSnapshot)
	}
	if got := saveSnapshotCount(t, engine, game); got != 1 {
		t.Errorf("snapshot count = %d, want exactly 1", got)
	}
}

// ---------------------------------------------------------------------------
// Restore
// ---------------------------------------------------------------------------

func TestRestoreNewestSnapshot(t *testing.T) {
	t.Parallel()
	engine, game := saveStoreEngine(t, "Restore Newest")

	live := t.TempDir()
	if err := engine.SetManualPath(game, live); err != nil {
		t.Fatalf("SetManualPath: %v", err)
	}

	saveTestWrite(t, filepath.Join(live, "persistent"), "first")
	first, err := engine.Snapshot(game, "", "")
	if err != nil {
		t.Fatalf("Snapshot first: %v", err)
	}

	saveTestWrite(t, filepath.Join(live, "persistent"), "second")
	second, err := engine.Snapshot(game, "", "")
	if err != nil {
		t.Fatalf("Snapshot second: %v", err)
	}
	if second.Name == first.Name {
		t.Fatalf("expected distinct snapshot names, both %q", second.Name)
	}

	// Drift the live data, then restore: the newest snapshot must win.
	saveTestWrite(t, filepath.Join(live, "persistent"), "drifted")
	res, err := engine.Restore(game)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if res.Set.Name != second.Name {
		t.Errorf("Restore applied %q, want newest %q", res.Set.Name, second.Name)
	}
	saveStoreAssertFile(t, filepath.Join(live, "persistent"), "second")
}

func TestRestoreWithoutSnapshotErrors(t *testing.T) {
	t.Parallel()
	engine, game := saveStoreEngine(t, "No Snapshot")

	live := t.TempDir()
	if err := engine.SetManualPath(game, live); err != nil {
		t.Fatalf("SetManualPath: %v", err)
	}
	if _, err := engine.Restore(game); err == nil {
		t.Fatal("Restore without any snapshot should error")
	}
}

// ---------------------------------------------------------------------------
// KindFiles (RPG Maker VX Ace)
// ---------------------------------------------------------------------------

func TestApplyKindFiles(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	engine, game := saveStoreEngineAt(t, base, "VX Game")

	game.Engine = "RPGMakerVXAce"
	gameDir := filepath.Join(base, "vxgame")
	game.Path = gameDir
	saveTestWrite(t, filepath.Join(gameDir, "Save01.rvdata2"), "legit-save")
	saveTestWrite(t, filepath.Join(gameDir, "readme.txt"), "game-readme")

	loc, candidates, err := engine.ResolveLocation(game)
	if err != nil {
		t.Fatalf("ResolveLocation: %v", err)
	}
	if loc.Kind != KindFiles {
		t.Fatalf("Kind = %q (candidates %+v), want files", loc.Kind, candidates)
	}
	if loc.Path != gameDir {
		t.Errorf("Path = %q, want %q", loc.Path, gameDir)
	}

	// Snapshot captures only glob-matched files, never the readme.
	snap, err := engine.Snapshot(game, "", "")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	snapDir := engine.SetPath(game, snap.Name)
	saveStoreAssertFile(t, filepath.Join(snapDir, "Save01.rvdata2"), "legit-save")
	saveStoreAssertAbsent(t, filepath.Join(snapDir, "readme.txt"))

	// An imported set carrying a stray readme.txt must not leak it into the
	// game dir: only glob-matched files are applied.
	src := t.TempDir()
	saveTestWrite(t, filepath.Join(src, "Save01.rvdata2"), "full-save")
	saveTestWrite(t, filepath.Join(src, "readme.txt"), "junk")
	full, err := engine.Import(game, src, ImportOptions{Name: "vx-full"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if full.ApplyRoot != "" {
		t.Fatalf("ApplyRoot = %q, want empty", full.ApplyRoot)
	}

	res, err := engine.Apply(game, full.Name)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.FilesCopied != 1 {
		t.Errorf("FilesCopied = %d, want 1 (glob-matched only)", res.FilesCopied)
	}
	if res.AutoSnapshot == "" {
		t.Error("expected auto-snapshot of the untracked live save file")
	}
	saveStoreAssertFile(t, filepath.Join(gameDir, "Save01.rvdata2"), "full-save")
	// The game's own readme must survive untouched (junk never copied over it).
	saveStoreAssertFile(t, filepath.Join(gameDir, "readme.txt"), "game-readme")
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

func TestApplyErrors(t *testing.T) {
	t.Parallel()

	t.Run("unresolved location", func(t *testing.T) {
		t.Parallel()
		engine, game := saveStoreEngine(t, "Unresolvable")
		game.Engine = "Others"
		game.Path = filepath.Join(t.TempDir(), "missing")

		src := t.TempDir()
		saveTestWrite(t, filepath.Join(src, "persistent"), "x")
		set, err := engine.Import(game, src, ImportOptions{Name: "imported"})
		if err != nil {
			t.Fatalf("Import: %v", err)
		}

		_, err = engine.Apply(game, set.Name)
		if err == nil {
			t.Fatal("Apply with an unresolved location should error")
		}
		if !strings.Contains(err.Error(), "saves path") {
			t.Errorf("error = %q, want it to suggest `moxie saves path`", err)
		}
	})

	t.Run("missing set", func(t *testing.T) {
		t.Parallel()
		engine, game := saveStoreEngine(t, "Missing Set")
		live := t.TempDir()
		if err := engine.SetManualPath(game, live); err != nil {
			t.Fatalf("SetManualPath: %v", err)
		}
		if _, err := engine.Apply(game, "does-not-exist"); err == nil {
			t.Fatal("Apply of an unknown set should error")
		}
	})

	t.Run("empty set source", func(t *testing.T) {
		t.Parallel()
		engine, game := saveStoreEngine(t, "Empty Set")
		live := t.TempDir()
		if err := engine.SetManualPath(game, live); err != nil {
			t.Fatalf("SetManualPath: %v", err)
		}
		set, err := engine.Import(game, t.TempDir(), ImportOptions{Name: "empty"})
		if err != nil {
			t.Fatalf("Import: %v", err)
		}
		if _, err := engine.Apply(game, set.Name); err == nil {
			t.Fatal("Apply of a set with no files should error")
		}
	})

	t.Run("empty live dir snapshot", func(t *testing.T) {
		t.Parallel()
		engine, game := saveStoreEngine(t, "Empty Live")
		live := t.TempDir()
		if err := engine.SetManualPath(game, live); err != nil {
			t.Fatalf("SetManualPath: %v", err)
		}
		if _, err := engine.Snapshot(game, "", ""); err == nil {
			t.Fatal("Snapshot of an empty live dir should error")
		}
	})
}

// ---------------------------------------------------------------------------
// Atomic name reservation
// ---------------------------------------------------------------------------

// TestSnapshotReservesPreExistingDir proves Snapshot claims its directory the
// same atomic way Import does: an on-disk directory with no database row is
// skipped (suffixed name) and never written into.
func TestSnapshotReservesPreExistingDir(t *testing.T) {
	t.Parallel()
	engine, game := saveStoreEngine(t, "Snap Reserve")

	live := t.TempDir()
	if err := engine.SetManualPath(game, live); err != nil {
		t.Fatalf("SetManualPath: %v", err)
	}
	saveTestWrite(t, filepath.Join(live, "persistent"), "live")

	pre := engine.SetPath(game, "mine-fixed")
	saveTestMkdir(t, pre)
	saveTestWrite(t, filepath.Join(pre, "sentinel"), "keep")

	snap, err := engine.Snapshot(game, "mine-fixed", "my saves")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snap.Name != "mine-fixed-2" {
		t.Errorf("Name = %q, want %q (pre-existing dir must be skipped)", snap.Name, "mine-fixed-2")
	}
	saveStoreAssertFile(t, filepath.Join(pre, "sentinel"), "keep")
	saveStoreAssertAbsent(t, filepath.Join(pre, "persistent"))
	saveStoreAssertFile(t, filepath.Join(engine.SetPath(game, snap.Name), "persistent"), "live")
}

// ---------------------------------------------------------------------------
// Activation order
// ---------------------------------------------------------------------------

// TestApplyActivatesBeforeCopy pins the apply order: the set is marked active
// before the copy, so a copy failure leaves the set active and the error tells
// the user to re-run apply to retry.
func TestApplyActivatesBeforeCopy(t *testing.T) {
	t.Parallel()
	engine, game := saveStoreEngine(t, "Activate First")

	live := t.TempDir()
	if err := engine.SetManualPath(game, live); err != nil {
		t.Fatalf("SetManualPath: %v", err)
	}

	// An empty set: activation succeeds, but the copy step fails because the
	// set has nothing to copy.
	set, err := engine.Import(game, t.TempDir(), ImportOptions{Name: "empty"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	_, err = engine.Apply(game, set.Name)
	if err == nil {
		t.Fatal("Apply of an empty set should fail at the copy step")
	}
	if !strings.Contains(err.Error(), "re-run apply") {
		t.Errorf("error = %v, want it to note the set is active and retryable", err)
	}
	if saveActiveSet(t, engine, game, set.Name) == nil {
		t.Error("set should be marked active even though the copy failed")
	}
}

// ---------------------------------------------------------------------------
// ResolveLocation manual override
// ---------------------------------------------------------------------------

func TestResolveLocationManualOverride(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	engine, game := saveStoreEngineAt(t, base, "Manual Paths")

	// RPG Maker MV resolves to exactly one candidate, keeping the test
	// hermetic (no home-directory scanning).
	game.Engine = "RPGMakerMV"
	gameDir := filepath.Join(base, "mvgame")
	game.Path = gameDir
	saveTestMkdir(t, filepath.Join(gameDir, "www"))
	saveTestWrite(t, filepath.Join(gameDir, "www", "save", "persistent"), "auto")

	autoDir := filepath.Join(gameDir, "www", "save")
	auto, candidates, err := engine.ResolveLocation(game)
	if err != nil {
		t.Fatalf("ResolveLocation: %v", err)
	}
	if auto.Path != autoDir {
		t.Fatalf("auto Path = %q (candidates %+v), want %q", auto.Path, candidates, autoDir)
	}
	if auto.Note == "manual override" {
		t.Errorf("auto Note = %q, should not be a manual override", auto.Note)
	}

	// An existing resolved_path cache must survive SetManualPath.
	if err := engine.DB().UpsertSaveConfig(&db.SaveConfig{GameID: game.ID, ResolvedPath: "/cached/here"}); err != nil {
		t.Fatalf("UpsertSaveConfig: %v", err)
	}

	manual := t.TempDir()
	if err := engine.SetManualPath(game, manual); err != nil {
		t.Fatalf("SetManualPath: %v", err)
	}

	pinned, _, err := engine.ResolveLocation(game)
	if err != nil {
		t.Fatalf("ResolveLocation: %v", err)
	}
	if pinned.Path != manual {
		t.Errorf("pinned Path = %q, want %q", pinned.Path, manual)
	}
	if pinned.Note != "manual override" {
		t.Errorf("pinned Note = %q, want manual override", pinned.Note)
	}

	cfg, err := engine.DB().GetSaveConfig(game.ID)
	if err != nil {
		t.Fatalf("GetSaveConfig: %v", err)
	}
	if cfg == nil || cfg.ResolvedPath != "/cached/here" {
		t.Errorf("ResolvedPath = %+v, want /cached/here preserved", cfg)
	}

	// Clearing reverts to auto-detection.
	if err := engine.ClearManualPath(game); err != nil {
		t.Fatalf("ClearManualPath: %v", err)
	}
	back, _, err := engine.ResolveLocation(game)
	if err != nil {
		t.Fatalf("ResolveLocation: %v", err)
	}
	if back.Path != autoDir {
		t.Errorf("after clear Path = %q, want %q", back.Path, autoDir)
	}
}

// ---------------------------------------------------------------------------
// Replace applies
// ---------------------------------------------------------------------------

// TestApplyReplaceKindDir pins the KindDir replace flow: the live directory is
// cleared before the set is copied, so it mirrors the set exactly, and the live
// drift that the deletion would destroy is captured by a pre-replace snapshot
// (the active set suppresses the normal auto-snapshot).
func TestApplyReplaceKindDir(t *testing.T) {
	t.Parallel()
	engine, game := saveStoreEngine(t, "Replace Dir")

	live := t.TempDir()
	if err := engine.SetManualPath(game, live); err != nil {
		t.Fatalf("SetManualPath: %v", err)
	}

	src := t.TempDir()
	saveTestWrite(t, filepath.Join(src, "persistent"), "set-p")
	saveTestWrite(t, filepath.Join(src, "slot1.save"), "set-s")
	set, err := engine.Import(game, src, ImportOptions{Name: "replace-dir"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if err := engine.DB().SetActiveSaveSet(game.ID, set.ID); err != nil {
		t.Fatalf("SetActiveSaveSet: %v", err)
	}

	saveTestWrite(t, filepath.Join(live, "persistent"), "live-p")
	saveTestWrite(t, filepath.Join(live, "slot1.save"), "live-s")
	saveTestWrite(t, filepath.Join(live, "extra.txt"), "keep-until-replace")

	res, err := engine.ApplyWith(game, set.Name, ApplyOptions{Replace: true})
	if err != nil {
		t.Fatalf("ApplyWith(replace): %v", err)
	}
	if !res.Replaced {
		t.Error("Replaced = false, want true")
	}
	if res.FilesRemoved != 3 {
		t.Errorf("FilesRemoved = %d, want 3 (every live entry was cleared)", res.FilesRemoved)
	}
	if res.FilesCopied != 2 {
		t.Errorf("FilesCopied = %d, want 2", res.FilesCopied)
	}
	if res.AutoSnapshot == "" {
		t.Fatal("replace with live drift and an active set must snapshot first")
	}

	// The live location mirrors the set exactly: drift is gone, set files win.
	saveStoreAssertFile(t, filepath.Join(live, "persistent"), "set-p")
	saveStoreAssertFile(t, filepath.Join(live, "slot1.save"), "set-s")
	saveStoreAssertAbsent(t, filepath.Join(live, "extra.txt"))

	// The pre-replace snapshot kept the drift recoverable.
	snap, err := engine.DB().GetSaveSet(game.ID, res.AutoSnapshot)
	if err != nil {
		t.Fatalf("GetSaveSet: %v", err)
	}
	if snap == nil {
		t.Fatalf("pre-replace snapshot %q row missing", res.AutoSnapshot)
	}
	if snap.Source != "snapshot" || snap.Label != "pre-replace" {
		t.Errorf("pre-replace snapshot = %+v, want Source snapshot label pre-replace", snap)
	}
	snapDir := engine.SetPath(game, res.AutoSnapshot)
	saveStoreAssertFile(t, filepath.Join(snapDir, "persistent"), "live-p")
	saveStoreAssertFile(t, filepath.Join(snapDir, "slot1.save"), "live-s")
	saveStoreAssertFile(t, filepath.Join(snapDir, "extra.txt"), "keep-until-replace")
}

// TestApplyReplaceCreatesMissingLiveDir pins that a replace re-creates a live
// directory that does not exist yet, so the set still lands there.
func TestApplyReplaceCreatesMissingLiveDir(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	engine, game := saveStoreEngineAt(t, base, "Replace Missing")

	live := filepath.Join(base, "live") // deliberately never created
	if err := engine.SetManualPath(game, live); err != nil {
		t.Fatalf("SetManualPath: %v", err)
	}

	src := t.TempDir()
	saveTestWrite(t, filepath.Join(src, "persistent"), "set-p")
	set, err := engine.Import(game, src, ImportOptions{Name: "replace-missing"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	res, err := engine.ApplyWith(game, set.Name, ApplyOptions{Replace: true})
	if err != nil {
		t.Fatalf("ApplyWith(replace): %v", err)
	}
	if res.FilesRemoved != 0 {
		t.Errorf("FilesRemoved = %d, want 0 (nothing to delete)", res.FilesRemoved)
	}
	if res.FilesCopied != 1 {
		t.Errorf("FilesCopied = %d, want 1", res.FilesCopied)
	}
	if res.AutoSnapshot != "" {
		t.Errorf("AutoSnapshot = %q, want none (the live dir was empty)", res.AutoSnapshot)
	}
	saveStoreAssertFile(t, filepath.Join(live, "persistent"), "set-p")
}

// TestApplyReplaceKindFiles pins the KindFiles replace flow (RPG Maker VX Ace
// saves live in the game directory): only glob-matched files are deleted, and the
// game's own non-save files survive untouched. No active set exists here, so the
// ordinary auto-snapshot already protects the files the replace deletes.
func TestApplyReplaceKindFiles(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	engine, game := saveStoreEngineAt(t, base, "VX Replace")

	game.Engine = "RPGMakerVXAce"
	gameDir := filepath.Join(base, "vxgame")
	game.Path = gameDir
	saveTestWrite(t, filepath.Join(gameDir, "Save01.rvdata2"), "live-1")
	saveTestWrite(t, filepath.Join(gameDir, "Save02.rvdata2"), "live-2")
	saveTestWrite(t, filepath.Join(gameDir, "readme.txt"), "game-readme")

	src := t.TempDir()
	saveTestWrite(t, filepath.Join(src, "Save01.rvdata2"), "set-1")
	saveTestWrite(t, filepath.Join(src, "readme.txt"), "set-readme")
	set, err := engine.Import(game, src, ImportOptions{Name: "vx-replace"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	res, err := engine.ApplyWith(game, set.Name, ApplyOptions{Replace: true})
	if err != nil {
		t.Fatalf("ApplyWith(replace): %v", err)
	}
	if !res.Replaced {
		t.Error("Replaced = false, want true")
	}
	if res.FilesRemoved != 2 {
		t.Errorf("FilesRemoved = %d, want 2 (both live Save*.rvdata2 files)", res.FilesRemoved)
	}
	if res.FilesCopied != 1 {
		t.Errorf("FilesCopied = %d, want 1 (glob-matched set file only)", res.FilesCopied)
	}
	if !strings.HasPrefix(res.AutoSnapshot, "mine-") {
		t.Errorf("AutoSnapshot = %q, want a mine- snapshot of the live saves", res.AutoSnapshot)
	}

	// The set's Save01.rvdata2 replaced the live one; Save02 was deleted; the
	// game's readme survived and the set's readme was never copied over it.
	saveStoreAssertFile(t, filepath.Join(gameDir, "Save01.rvdata2"), "set-1")
	saveStoreAssertAbsent(t, filepath.Join(gameDir, "Save02.rvdata2"))
	saveStoreAssertFile(t, filepath.Join(gameDir, "readme.txt"), "game-readme")

	// The auto-snapshot holds the pre-replace live saves.
	snapDir := engine.SetPath(game, res.AutoSnapshot)
	saveStoreAssertFile(t, filepath.Join(snapDir, "Save01.rvdata2"), "live-1")
	saveStoreAssertFile(t, filepath.Join(snapDir, "Save02.rvdata2"), "live-2")
}

// TestApplyReplaceSnapshotGuardFailureAborts proves a replace never deletes
// before its snapshot guard succeeds: a live location that cannot be captured
// (here it holds a symlink, which save data must never carry) aborts the apply
// with the live data untouched.
func TestApplyReplaceSnapshotGuardFailureAborts(t *testing.T) {
	t.Parallel()
	engine, game := saveStoreEngine(t, "Guard Fail")

	live := t.TempDir()
	if err := engine.SetManualPath(game, live); err != nil {
		t.Fatalf("SetManualPath: %v", err)
	}
	saveTestWrite(t, filepath.Join(live, "persistent"), "live")

	src := t.TempDir()
	saveTestWrite(t, filepath.Join(src, "persistent"), "set-p")
	set, err := engine.Import(game, src, ImportOptions{Name: "guard-fail"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	// An active set suppresses the auto-snapshot, so the replace's own guard
	// is what must capture the live state.
	if err := engine.DB().SetActiveSaveSet(game.ID, set.ID); err != nil {
		t.Fatalf("SetActiveSaveSet: %v", err)
	}
	if err := os.Symlink("persistent", filepath.Join(live, "link.save")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := engine.ApplyWith(game, set.Name, ApplyOptions{Replace: true}); err == nil {
		t.Fatal("replace whose snapshot fails should abort")
	}

	// Nothing was deleted: the live data (and the hostile link) survived.
	saveStoreAssertFile(t, filepath.Join(live, "persistent"), "live")
	if _, statErr := os.Lstat(filepath.Join(live, "link.save")); statErr != nil {
		t.Errorf("live link should survive an aborted replace (lstat err = %v)", statErr)
	}
}

// TestCheckReplaceTarget pins the replace safety refusals directly: an empty
// path, a filesystem root, and (for KindDir) the game directory or an ancestor
// of it are refused; the game directory itself is the normal KindFiles layout.
func TestCheckReplaceTarget(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	game := &db.Game{Title: "Guarded", Engine: "RenPy", Path: filepath.Join(base, "game")}

	tests := []struct {
		name    string
		loc     Location
		wantErr bool
	}{
		{"empty path", Location{Kind: KindDir, Path: ""}, true},
		{"filesystem root", Location{Kind: KindDir, Path: "/"}, true},
		{"game dir as KindDir", Location{Kind: KindDir, Path: game.Path}, true},
		{"ancestor as KindDir", Location{Kind: KindDir, Path: base}, true},
		{"game dir as KindFiles", Location{Kind: KindFiles, Path: game.Path, Globs: []string{"save*.rvdata2"}}, false},
		{"ancestor as KindFiles", Location{Kind: KindFiles, Path: base, Globs: []string{"save*.rvdata2"}}, true},
		{"unrelated KindDir", Location{Kind: KindDir, Path: filepath.Join(base, "saves")}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := checkReplaceTarget(game, tc.loc)
			if tc.wantErr && err == nil {
				t.Error("checkReplaceTarget should refuse")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("checkReplaceTarget = %v, want nil", err)
			}
		})
	}
}

// TestApplyReplaceRefusesUnsafeTarget proves ApplyWith refuses a destructive
// replace before deleting anything when the resolved location holds the game.
func TestApplyReplaceRefusesUnsafeTarget(t *testing.T) {
	t.Parallel()

	t.Run("game directory", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		engine, game := saveStoreEngineAt(t, base, "Unsafe Dir")
		saveTestWrite(t, filepath.Join(game.Path, "keep.txt"), "keep-me")

		if err := engine.SetManualPath(game, game.Path); err != nil {
			t.Fatalf("SetManualPath: %v", err)
		}
		src := t.TempDir()
		saveTestWrite(t, filepath.Join(src, "persistent"), "set-p")
		set, err := engine.Import(game, src, ImportOptions{Name: "unsafe"})
		if err != nil {
			t.Fatalf("Import: %v", err)
		}

		if _, err := engine.ApplyWith(game, set.Name, ApplyOptions{Replace: true}); err == nil {
			t.Fatal("replace of the game directory should be refused")
		}
		saveStoreAssertFile(t, filepath.Join(game.Path, "keep.txt"), "keep-me")
	})

	t.Run("filesystem root", func(t *testing.T) {
		t.Parallel()
		engine, game := saveStoreEngine(t, "Unsafe Root")

		if err := engine.SetManualPath(game, "/"); err != nil {
			t.Fatalf("SetManualPath: %v", err)
		}
		src := t.TempDir()
		saveTestWrite(t, filepath.Join(src, "persistent"), "set-p")
		set, err := engine.Import(game, src, ImportOptions{Name: "unsafe-root"})
		if err != nil {
			t.Fatalf("Import: %v", err)
		}

		_, err = engine.ApplyWith(game, set.Name, ApplyOptions{Replace: true})
		if err == nil {
			t.Fatal("replace of a filesystem root should be refused")
		}
		if !strings.Contains(err.Error(), "filesystem root") {
			t.Errorf("error = %q, want it to mention the filesystem root", err)
		}
	})
}

// TestApplyWithoutReplacePreservesExtras pins that a plain Apply stays
// copy-overwrite: live files the set does not carry survive, no deletion runs
// and no replace snapshot is taken.
func TestApplyWithoutReplacePreservesExtras(t *testing.T) {
	t.Parallel()
	engine, game := saveStoreEngine(t, "Preserve Extras")

	live := t.TempDir()
	if err := engine.SetManualPath(game, live); err != nil {
		t.Fatalf("SetManualPath: %v", err)
	}
	saveTestWrite(t, filepath.Join(live, "persistent"), "live-p")
	saveTestWrite(t, filepath.Join(live, "extra.txt"), "keep-me")

	src := t.TempDir()
	saveTestWrite(t, filepath.Join(src, "persistent"), "set-p")
	set, err := engine.Import(game, src, ImportOptions{Name: "preserve"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	res, err := engine.Apply(game, set.Name)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Replaced {
		t.Error("Replaced = true, want false for a plain apply")
	}
	if res.FilesRemoved != 0 {
		t.Errorf("FilesRemoved = %d, want 0", res.FilesRemoved)
	}
	if res.FilesCopied != 1 {
		t.Errorf("FilesCopied = %d, want 1", res.FilesCopied)
	}
	if res.AutoSnapshot == "" {
		t.Error("expected the ordinary auto-snapshot of the untracked live data")
	}

	saveStoreAssertFile(t, filepath.Join(live, "persistent"), "set-p")
	saveStoreAssertFile(t, filepath.Join(live, "extra.txt"), "keep-me")
}
