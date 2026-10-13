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
	// AutoSnapshot is the name of the snapshot auto-created before the apply
	// (when the game had no active set and live save data to protect), or ""
	// when no snapshot was needed.
	AutoSnapshot string
	// Target is the live save location the set was copied into.
	Target Location
	// FilesCopied is the number of files copied into the live location.
	FilesCopied int
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
//
// If the game has no active set and the live location holds data, the live data
// is first auto-snapshotted (Source "snapshot", label "my saves (auto)") so the
// previous state can be restored. The set's apply_root subtree is the copy
// source: its whole tree for KindDir, or its glob-matched files for KindFiles.
// Copying is same-name-overwrite; unmatched live files are preserved and live
// user data is never deleted.
//
// The order is: auto-snapshot (when needed), then mark the set active, then
// copy. Activating first means a database failure leaves the live location
// completely untouched; a copy failure after activation is retryable because
// re-running apply simply copies again into the same live location, and when an
// auto-snapshot was taken the pre-apply live state is already safely captured.
func (e *Engine) Apply(game *db.Game, setName string) (*ApplyResult, error) {
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
	}

	// Activate before copying: an activation failure leaves the live location
	// untouched, while a copy failure after activation is retryable (re-running
	// apply re-copies into the same live location).
	if err := e.db.SetActiveSaveSet(game.ID, set.ID); err != nil {
		return nil, fmt.Errorf("saves: mark set active: %w", err)
	}
	set.Active = true
	result.Set = *set

	src := e.SetPath(game, set.Name)
	if set.ApplyRoot != "" {
		src = filepath.Join(src, filepath.FromSlash(set.ApplyRoot))
	}

	copied, err := e.copySetInto(set, loc, src)
	if err != nil {
		return nil, fmt.Errorf(
			"saves: set %q is now marked active but copying it into the live location failed; re-run apply to retry the copy: %w",
			set.Name, err)
	}
	result.FilesCopied = copied
	return result, nil
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
