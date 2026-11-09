package saves

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mili/moxie/internal/db"
)

// ApplyResult summarizes an Apply or Restore operation.
type ApplyResult struct {
	// Set is the set that was applied, with Active true.
	Set db.SaveSet
	// AutoSnapshot is the name of the snapshot taken before the apply to
	// protect live save data. It is set when the game had no active set and the
	// live location held data ("my saves (auto)"), or when a replace would
	// otherwise discard un-snapshotted drift ("pre-replace"); it is "" when no
	// snapshot was needed.
	AutoSnapshot string
	// Target is the live save location the set was copied into.
	Target Location
	// FilesCopied is the number of files copied into the live location.
	FilesCopied int
	// Replaced reports whether the apply ran in replace mode, i.e. whether the
	// live save data was deleted before the set was copied.
	Replaced bool
	// FilesRemoved is the number of entries the replace deleted from the live
	// location before copying: each top-level entry for KindDir (including
	// entries the set then re-copies), or each glob-matched file for KindFiles.
	// It is 0 when no replace ran.
	FilesRemoved int
}

// ApplyOptions tunes an Apply.
type ApplyOptions struct {
	// Replace mirrors: the live save data is deleted before the set is
	// copied, so files not present in the set do not survive. A snapshot
	// guard captures the live state first when it would otherwise be lost.
	Replace bool
}

// Snapshot captures the game's current live save data as a new set.
//
// KindDir locations have their whole directory tree copied into the set
// directory (apply_root ""), so the set mirrors the live data. KindFiles
// locations copy only files matching the location's globs into the set
// directory root. name defaults to "mine-<YYYYMMDD-HHMMSS>" and label to
// "my saves"; the resulting name is made unique per game. The set's Source is
// "snapshot" and the active flag is never modified.
func (e *Engine) Snapshot(game *db.Game, name, label string) (*db.SaveSet, error) {
	if e == nil || e.db == nil {
		return nil, fmt.Errorf("saves: engine is not initialized")
	}
	if game == nil {
		return nil, fmt.Errorf("saves: game is required")
	}

	loc, _, err := e.ResolveLocation(game)
	if err != nil {
		return nil, err
	}
	if loc.Path == "" {
		return nil, fmt.Errorf("saves: cannot resolve a live save location for %q", game.Title)
	}

	if name == "" {
		name = "mine-" + time.Now().Format("20060102-150405")
	}
	if label == "" {
		label = "my saves"
	}

	setName, setDir, err := e.reserveSetDir(game, ImportOptions{Name: name, Label: label})
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			os.RemoveAll(setDir)
		}
	}()

	if loc.Kind == KindFiles {
		copied, err := copySaveFiles(loc.Path, setDir, loc.Globs)
		if err != nil {
			return nil, fmt.Errorf("saves: snapshot save files: %w", err)
		}
		if copied == 0 {
			return nil, fmt.Errorf("saves: no save files match %v in %s", loc.Globs, loc.Path)
		}
	} else {
		entries, err := os.ReadDir(loc.Path)
		if err != nil {
			return nil, fmt.Errorf("saves: read live save dir: %w", err)
		}
		if len(entries) == 0 {
			return nil, fmt.Errorf("saves: live save dir %s is empty", loc.Path)
		}
		if err := copyTree(loc.Path, setDir); err != nil {
			return nil, fmt.Errorf("saves: snapshot save dir: %w", err)
		}
	}

	sizeBytes, fileCount, err := treeStats(setDir)
	if err != nil {
		return nil, fmt.Errorf("saves: measure snapshot: %w", err)
	}

	set := &db.SaveSet{
		GameID:      game.ID,
		Name:        setName,
		Label:       label,
		Source:      "snapshot",
		GameVersion: game.Version,
		ApplyRoot:   "",
		SizeBytes:   sizeBytes,
		FileCount:   fileCount,
		Active:      false,
	}
	if _, err := e.db.CreateSaveSet(set); err != nil {
		return nil, fmt.Errorf("saves: record snapshot: %w", err)
	}
	keep = true
	return set, nil
}

// Apply copies the named set's contents into the game's live save location.
// It is ApplyWith with default options, which preserve unmatched live files; see
// ApplyWith for the full semantics.
func (e *Engine) Apply(game *db.Game, setName string) (*ApplyResult, error) {
	return e.ApplyWith(game, setName, ApplyOptions{})
}

// ApplyWith copies the named set's contents into the game's live save location.
//
// If the game has no active set and the live location holds data, the live data
// is first auto-snapshotted (Source "snapshot", label "my saves (auto)") so the
// previous state can be restored. The set's apply_root subtree is the copy
// source: its whole tree for KindDir, or its glob-matched files for KindFiles.
// Copying is same-name-overwrite; unmatched live files are preserved and live
// user data is never deleted.
//
// With opts.Replace the live save data is deleted before the copy, so the live
// location ends up mirroring the set exactly: KindDir clears the directory
// contents (the directory itself survives, and is created when missing) and
// KindFiles removes only the glob-matched files. Deleting is destructive, so a
// replace snapshots the live state first whenever it holds data — including when
// an active set already exists, which suppresses the auto-snapshot above — and
// aborts before deleting anything if that snapshot fails. A replace also refuses
// to touch a location that holds the game itself (see checkReplaceTarget; the
// game directory itself is allowed for KindFiles, where the glob-scoped deletion
// is the engine's own layout).
//
// The order is: snapshot (auto and/or pre-replace), delete (replace only), then
// mark the set active, then copy. Activating before copying means a database
// failure leaves the live location untouched; a copy failure after activation is
// retryable because re-running apply simply copies again into the same live
// location, and the pre-apply live state is already safely captured.
func (e *Engine) ApplyWith(game *db.Game, setName string, opts ApplyOptions) (*ApplyResult, error) {
	if e == nil || e.db == nil {
		return nil, fmt.Errorf("saves: engine is not initialized")
	}
	if game == nil {
		return nil, fmt.Errorf("saves: game is required")
	}

	set, err := e.db.GetSaveSet(game.ID, setName)
	if err != nil {
		return nil, fmt.Errorf("saves: look up set: %w", err)
	}
	if set == nil {
		return nil, fmt.Errorf("saves: save set %q not found for game %q", setName, game.Title)
	}

	loc, _, err := e.ResolveLocation(game)
	if err != nil {
		return nil, err
	}
	if loc.Path == "" {
		return nil, fmt.Errorf(
			"saves: cannot resolve a live save location for %q; set one with `moxie saves path %d --set <path>`",
			game.Title, game.ID)
	}

	result := &ApplyResult{Target: loc}

	// Refuse a destructive replace before touching anything: the snapshot
	// guard below must never capture a location we would not delete.
	if opts.Replace {
		if err := checkReplaceTarget(game, loc); err != nil {
			return nil, err
		}
	}

	sets, err := e.db.ListSaveSets(game.ID)
	if err != nil {
		return nil, fmt.Errorf("saves: list sets: %w", err)
	}
	if !anyActive(sets) && liveDataCount(loc) > 0 {
		snap, err := e.Snapshot(game, "", "my saves (auto)")
		if err != nil {
			return nil, fmt.Errorf("saves: auto-snapshot before apply: %w", err)
		}
		result.AutoSnapshot = snap.Name
	} else if opts.Replace && liveDataCount(loc) > 0 {
		// An active set suppresses the auto-snapshot above, so deleting the
		// live data would lose drift that was never captured; snapshot it.
		snap, err := e.Snapshot(game, "", "pre-replace")
		if err != nil {
			return nil, fmt.Errorf("saves: snapshot before replace: %w", err)
		}
		result.AutoSnapshot = snap.Name
	}

	if opts.Replace {
		removed, err := removeLiveData(game, loc)
		if err != nil {
			return nil, fmt.Errorf("saves: replace live save data: %w", err)
		}
		result.Replaced = true
		result.FilesRemoved = removed
	}

	if err := e.activateAndCopy(game, set, loc, result); err != nil {
		return nil, err
	}
	return result, nil
}

// activateAndCopy marks set active and copies its contents into the live
// location loc, filling in result. Activation precedes the copy: an activation
// failure leaves the live location untouched, while a copy failure after
// activation is retryable (re-running apply re-copies into the same location).
func (e *Engine) activateAndCopy(game *db.Game, set *db.SaveSet, loc Location, result *ApplyResult) error {
	if err := e.db.SetActiveSaveSet(game.ID, set.ID); err != nil {
		return fmt.Errorf("saves: mark set active: %w", err)
	}
	set.Active = true
	result.Set = *set

	src := e.SetPath(game, set.Name)
	if set.ApplyRoot != "" {
		src = filepath.Join(src, filepath.FromSlash(set.ApplyRoot))
	}

	copied, err := e.copySetInto(set, loc, src)
	if err != nil {
		return fmt.Errorf(
			"saves: set %q is now marked active but copying it into the live location failed; re-run apply to retry the copy: %w",
			set.Name, err)
	}
	result.FilesCopied = copied
	return nil
}

// removeLiveData deletes the live save data at loc ahead of a replace apply,
// returning the number of entries removed. KindDir clears the contents of
// loc.Path, keeping the directory itself (creating it when missing) and refusing
// to touch a location that holds the game; KindFiles removes only the files
// matching loc.Globs, leaving everything else in place.
func removeLiveData(game *db.Game, loc Location) (int, error) {
	if err := checkReplaceTarget(game, loc); err != nil {
		return 0, err
	}
	if loc.Kind == KindFiles {
		return removeGlobFiles(loc.Path, loc.Globs)
	}
	return clearDirContents(loc.Path)
}

// checkReplaceTarget refuses a replace target whose contents must not be deleted:
// an empty path, a filesystem root, or a directory holding the game itself.
//
// For KindDir the location's whole tree is deleted, so pointing it at the game
// directory (or above) would destroy the game. For KindFiles only glob-matched
// save files are deleted, so the game directory itself is the legitimate VX Ace
// layout (the game's own Save*.rvdata2 files live there) and is allowed; a
// strict ancestor of the game directory is refused for both kinds.
func checkReplaceTarget(game *db.Game, loc Location) error {
	if loc.Path == "" {
		return fmt.Errorf("saves: refusing to replace an empty save location")
	}
	clean := filepath.Clean(loc.Path)
	if filepath.Dir(clean) == clean {
		return fmt.Errorf("saves: refusing to replace %q: it is a filesystem root", loc.Path)
	}
	if game == nil || game.Path == "" {
		return nil
	}
	gamePath := filepath.Clean(game.Path)
	if ensurePathWithin(clean, gamePath) != nil {
		return nil // the game directory is not inside the location
	}
	if clean != gamePath {
		return fmt.Errorf("saves: refusing to replace %q: it contains the game directory %q", loc.Path, game.Path)
	}
	if loc.Kind != KindFiles {
		return fmt.Errorf("saves: refusing to replace %q: it is the game directory", loc.Path)
	}
	return nil
}

// clearDirContents removes every entry inside dir, keeping dir itself and
// creating it first when missing so a following copy has a target.
func clearDirContents(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return 0, fmt.Errorf("create live save dir: %w", err)
			}
			return 0, nil
		}
		return 0, fmt.Errorf("read live save dir: %w", err)
	}

	removed := 0
	for _, ent := range entries {
		if err := os.RemoveAll(filepath.Join(dir, ent.Name())); err != nil {
			return removed, fmt.Errorf("remove %s: %w", ent.Name(), err)
		}
		removed++
	}
	return removed, nil
}

// removeGlobFiles removes the files in dir whose names match globs, returning
// the number removed. Directories are never removed: for KindFiles only the
// glob-matched files are save data, and a matching directory name is a
// coincidence, not a set of files to delete.
func removeGlobFiles(dir string, globs []string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read live save dir: %w", err)
	}

	removed := 0
	for _, ent := range entries {
		if ent.IsDir() || !saveGlobMatch(ent.Name(), globs) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, ent.Name())); err != nil {
			return removed, fmt.Errorf("remove %s: %w", ent.Name(), err)
		}
		removed++
	}
	return removed, nil
}

// Restore applies the newest snapshot set ("mine-*" by default), returning the
// player's own saves to the live location. It errors when the game has no
// snapshot yet.
func (e *Engine) Restore(game *db.Game) (*ApplyResult, error) {
	if e == nil || e.db == nil {
		return nil, fmt.Errorf("saves: engine is not initialized")
	}
	if game == nil {
		return nil, fmt.Errorf("saves: game is required")
	}

	sets, err := e.db.ListSaveSets(game.ID)
	if err != nil {
		return nil, fmt.Errorf("saves: list sets: %w", err)
	}
	for _, s := range sets {
		if s.Source == "snapshot" {
			return e.Apply(game, s.Name)
		}
	}
	return nil, fmt.Errorf("saves: no 'my saves' snapshot exists yet for %q", game.Title)
}

// copySetInto copies the contents of the set subtree at src into the live
// location loc, returning the number of files copied. It errors when src is
// missing, is not a directory, or holds no files to copy.
func (e *Engine) copySetInto(set *db.SaveSet, loc Location, src string) (int, error) {
	info, err := os.Stat(src)
	if err != nil {
		return 0, fmt.Errorf("saves: set contents missing at %s: %w", src, err)
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("saves: set contents %s is not a directory", src)
	}
	if err := os.MkdirAll(loc.Path, 0o755); err != nil {
		return 0, fmt.Errorf("saves: create live save dir: %w", err)
	}

	if loc.Kind == KindFiles {
		copied, err := copySaveFiles(src, loc.Path, loc.Globs)
		if err != nil {
			return copied, fmt.Errorf("saves: copy save files: %w", err)
		}
		if copied == 0 {
			return 0, fmt.Errorf("saves: set %q has no files matching %v to apply", set.Name, loc.Globs)
		}
		return copied, nil
	}

	_, count, err := treeStats(src)
	if err != nil {
		return 0, fmt.Errorf("saves: measure set contents: %w", err)
	}
	if count == 0 {
		return 0, fmt.Errorf("saves: set %q has no files to apply", set.Name)
	}
	if err := copyTree(src, loc.Path); err != nil {
		return 0, fmt.Errorf("saves: copy set contents: %w", err)
	}
	return count, nil
}

// copySaveFiles copies the top-level regular files of srcDir whose names match
// one of globs into dstDir, returning the number copied. Globs are matched
// case-insensitively against the file name (patterns are relative to srcDir).
func copySaveFiles(srcDir, dstDir string, globs []string) (int, error) {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return 0, err
	}
	copied := 0
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if !saveGlobMatch(name, globs) {
			continue
		}
		info, err := ent.Info()
		if err != nil {
			return copied, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if err := copyFileContents(filepath.Join(srcDir, name), filepath.Join(dstDir, name)); err != nil {
			return copied, err
		}
		copied++
	}
	return copied, nil
}

// saveGlobMatch reports whether name matches any of globs, comparing
// case-insensitively.
func saveGlobMatch(name string, globs []string) bool {
	lower := strings.ToLower(name)
	for _, g := range globs {
		if ok, _ := filepath.Match(strings.ToLower(g), lower); ok {
			return true
		}
	}
	return false
}

// liveDataCount returns how many files the location currently holds (0 when the
// location is missing or unreadable). For KindFiles only glob-matched files
// count, so it lines up with what Snapshot would capture.
func liveDataCount(loc Location) int {
	if loc.Path == "" {
		return 0
	}
	if loc.Kind == KindFiles {
		entries, err := os.ReadDir(loc.Path)
		if err != nil {
			return 0
		}
		n := 0
		for _, ent := range entries {
			if !ent.IsDir() && saveGlobMatch(ent.Name(), loc.Globs) {
				n++
			}
		}
		return n
	}
	_, count, err := treeStats(loc.Path)
	if err != nil {
		return 0
	}
	return count
}

// anyActive reports whether any set in sets is currently active.
func anyActive(sets []db.SaveSet) bool {
	for _, s := range sets {
		if s.Active {
			return true
		}
	}
	return false
}
