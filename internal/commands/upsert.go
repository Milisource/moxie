package commands

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/scanner"
	"github.com/mili/moxie/internal/scraper"
)

// UpsertDetected inserts new games and updates existing records from a scan,
// preserving fields the user may have curated. It is the single source of
// truth for the scan save path, shared by the CLI scan, the desktop manual
// scan, and the desktop directory watcher.
//
// With force set — an explicit full rescan — the scanner's fresh detection
// wins for the scanner-owned fields version/engine/exe_path; otherwise those
// fields are only filled when currently unset so manual corrections survive.
// size, scan time, and directory mtime are always refreshed, and user-owned
// columns (title, status, notes, f95_url, ...) are never touched. A
// soft-deleted game found again on disk comes back to life — otherwise the
// UNIQUE index on path keeps blocking re-insertion.
func UpsertDetected(database *db.Database, detected []scanner.DetectedGame, force bool) (inserted, updated int, errs []string) {
	detectedPaths := make(map[string]bool, len(detected))
	for _, g := range detected {
		detectedPaths[g.Path] = true
	}
	// Snapshot rows before this scan: used to merge an existing inner-directory
	// row into a newly-registered release wrapper instead of duplicating it.
	priorEntries, perr := database.AllGamePaths()
	if perr != nil {
		priorEntries = nil
	}
	prior := make(map[string]db.GamePathEntry, len(priorEntries))
	for _, e := range priorEntries {
		prior[e.Path] = e
	}

	for _, g := range detected {
		existing, err := database.GetGameByPath(g.Path)
		if err != nil {
			errs = append(errs, g.Title+": "+err.Error())
			continue
		}

		now := time.Now().UTC()
		if existing != nil {
			// A soft-deleted game found again on disk comes back to life —
			// otherwise the UNIQUE index on path keeps blocking re-insertion
			// while the row stays invisible in every listing.
			if !existing.DeletedAt.IsZero() {
				if err := database.RestoreGame(existing.ID); err != nil {
					errs = append(errs, existing.Title+": "+err.Error())
					continue
				}
			}
			// Narrow, atomic update: only the scanner-owned fields are
			// written, and the source checks live inside the UPDATE, so a user
			// edit landing between our read and write can never be clobbered
			// with the stale record we loaded.
			if err := database.UpdateGameScanFields(
				existing.ID, g.Version, string(g.Engine), g.ExePath,
				g.SizeBytes, now, dirModTime(g.Path), force,
			); err != nil {
				errs = append(errs, existing.Title+": "+err.Error())
				continue
			}
			updated++
			continue
		}

		// A collapsed release wrapper supersedes a row that pointed at an
		// inner directory (e.g. "Brothel King" now supersedes
		// "Brothel King/Brothel King"). Relocate that row instead of
		// inserting a duplicate, preserving user curation. Only rows that are
		// not themselves detected games are merged, so container children and
		// sibling games are untouched. Remaining stale rows are pruned by
		// PruneSuperseded after this pass.
		if inner := shallowestUndetectedDescendant(prior, detectedPaths, g.Path); inner != nil {
			if ig, gerr := database.GetGameByPath(inner.Path); gerr == nil && ig != nil && ig.DeletedAt.IsZero() {
				if rerr := relocateGameRow(database, ig, ig.Path, g.Path); rerr == nil {
					_ = database.UpdateGameScanFields(ig.ID, g.Version, string(g.Engine), g.ExePath, g.SizeBytes, now, dirModTime(g.Path), false)
					updated++
					continue
				}
			}
		}

		title := scraper.SanitizeTitle(g.Title)
		if title == "" {
			title = g.Title
		}
		versionSource := ""
		if g.Version != "" {
			versionSource = "scanner"
		}
		newGame := &db.Game{
			Title:         title,
			Engine:        string(g.Engine),
			Path:          g.Path,
			ExePath:       g.ExePath,
			Version:       g.Version,
			SizeBytes:     g.SizeBytes,
			Status:        "unknown",
			EngineSource:  "scanner",
			VersionSource: versionSource,
			LastScannedAt: now,
			DirMTime:      dirModTime(g.Path),
		}
		if _, err := database.InsertGame(newGame); err != nil {
			errs = append(errs, title+": "+err.Error())
			continue
		}
		inserted++
	}
	return inserted, updated, errs
}

// shallowestUndetectedDescendant returns the nearest prior row whose path is a
// strict descendant of wrapper and that the current scan did not detect. Used
// to merge a superseded inner game row into its collapsed wrapper.
func shallowestUndetectedDescendant(prior map[string]db.GamePathEntry, detected map[string]bool, wrapper string) *db.GamePathEntry {
	var best *db.GamePathEntry
	for p, e := range prior {
		if p == wrapper || detected[p] || !isPathUnder(wrapper, p) {
			continue
		}
		if best == nil || len(p) < len(best.Path) {
			ec := e
			best = &ec
		}
	}
	return best
}

// PruneSuperseded soft-deletes active rows under root that the latest scan
// proved are no longer games: a row nested inside a detected game (a stale
// inner directory after wrapper collapse), a stale ancestor container, or
// anything under a downloads/ directory. Rows outside root are never touched.
// Called after UpsertDetected; returns the number pruned.
func PruneSuperseded(database *db.Database, root string, detected []scanner.DetectedGame) int {
	detectedPaths := make(map[string]bool, len(detected))
	for _, g := range detected {
		detectedPaths[g.Path] = true
	}
	entries, err := database.AllGamePaths()
	if err != nil {
		return 0
	}
	pruned := 0
	for _, e := range entries {
		if !isPathUnder(root, e.Path) || detectedPaths[e.Path] {
			continue
		}
		if !isSupersededPath(e.Path, detectedPaths) {
			continue
		}
		g, gerr := database.GetGameByPath(e.Path)
		if gerr != nil || g == nil || !g.DeletedAt.IsZero() {
			continue
		}
		if database.DeleteGame(e.ID) == nil {
			pruned++
		}
	}
	return pruned
}

// isSupersededPath reports whether path is no longer a standalone game: it
// lives under a downloads/ directory, or is an ancestor/descendant of a
// detected game path.
func isSupersededPath(path string, detected map[string]bool) bool {
	if hasDownloadsComponent(path) {
		return true
	}
	for d := range detected {
		if d == path {
			continue
		}
		if isPathUnder(d, path) || isPathUnder(path, d) {
			return true
		}
	}
	return false
}

// hasDownloadsComponent reports whether any path segment is "downloads".
func hasDownloadsComponent(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == "downloads" {
			return true
		}
	}
	return false
}

// vanishedGame is a game row whose directory is definitively gone from disk,
// collected by RemoveMissingUnder.
type vanishedGame struct {
	path string
	game *db.Game
}

// RemoveMissingUnder soft-deletes game rows under root whose directory no
// longer exists on disk, after first giving vanished directories a chance to
// be a move-within-root: a game folder renamed inside the same scan root keeps
// its row (path and dir mtime updated in place) so user curation — status,
// title, tags — survives. Returns the number of rows soft-deleted. Shared by
// the CLI scan and the desktop directory watcher.
func RemoveMissingUnder(database *db.Database, root string) int {
	entries, err := database.AllGamePaths()
	if err != nil {
		return 0
	}

	// Collect the rows whose directories are definitively gone. Only a
	// definitive "not there" justifies action; any other stat failure — an
	// unmounted drive, EACCES on a parent, EMFILE — means we do not know,
	// and guessing "gone" would trash the whole library on the next scan.
	var gone []vanishedGame
	for _, e := range entries {
		if !isPathUnder(root, e.Path) {
			continue
		}
		if _, serr := os.Stat(e.Path); !errors.Is(serr, fs.ErrNotExist) {
			continue
		}
		game, gerr := database.GetGameByPath(e.Path)
		if gerr != nil || game == nil {
			continue
		}
		// Already in the trash — re-deleting would reset deleted_at on every
		// scan, so the auto-purge would never come due.
		if !game.DeletedAt.IsZero() {
			continue
		}
		gone = append(gone, vanishedGame{path: e.Path, game: game})
	}
	if len(gone) == 0 {
		return 0
	}

	// Match vanished directories to same-basename directories that now exist
	// under the root — the signature of a move-within-root.
	moved := matchMovedWithinRoot(database, root, gone, entries)

	removed := 0
	for _, v := range gone {
		if newPath, ok := moved[v.path]; ok {
			if err := relocateGameRow(database, v.game, v.path, newPath); err != nil {
				slog.Warn("could not relocate moved game",
					"from", v.path, "to", newPath, "error", err)
				removed++
			}
			continue
		}
		if derr := database.DeleteGame(v.game.ID); derr == nil {
			removed++
		}
	}
	return removed
}

// matchMovedWithinRoot finds, for each vanished game directory, a directory
// under the same root with the same basename — the signature of a
// move-within-root. Candidates nested inside another known game's directory
// are rejected: the scanner treats those as part of the parent game, so they
// cannot be the new home of a standalone game row. Returns vanished path ->
// new path.
func matchMovedWithinRoot(database *db.Database, root string, gone []vanishedGame, allRows []db.GamePathEntry) map[string]string {
	wantBase := make(map[string]string) // basename -> vanished path
	for _, v := range gone {
		wantBase[filepath.Base(v.path)] = v.path
	}
	found := make(map[string]string)

	nestedInRow := func(p string) bool {
		for _, r := range allRows {
			if p != r.Path && isPathUnder(r.Path, p) {
				return true
			}
		}
		return false
	}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() || path == root {
			return nil
		}
		old, ok := wantBase[d.Name()]
		if !ok || path == old {
			return nil
		}
		if nestedInRow(path) {
			return nil
		}
		if _, taken := found[old]; !taken {
			found[old] = path
		}
		return nil
	})
	if err != nil {
		slog.Warn("move-match walk failed", "root", root, "error", err)
	}
	return found
}

// relocateGameRow updates a game row whose directory moved within the scan
// root: path and dir mtime are refreshed in place, and a scanner-owned
// exe_path that pointed into the old directory is cleared so the following
// upsert pass refills it from detection. Everything the user curated (title,
// status, tags, notes, f95 URL) is preserved — the row is never deleted and
// re-inserted.
func relocateGameRow(database *db.Database, game *db.Game, oldPath, newPath string) error {
	// A row already exists at the new path (a previous scan inserted it
	// before the move was recognised): the old row is a duplicate and the
	// caller falls back to soft-deleting it.
	if existing, err := database.GetGameByPath(newPath); err == nil && existing != nil {
		return fmt.Errorf("a row already exists at the moved path")
	}
	game.Path = newPath
	game.DirMTime = dirModTime(newPath)
	game.LastScannedAt = time.Now().UTC()
	if game.ExePath != "" && isPathUnder(oldPath, game.ExePath) {
		// Points into the old directory — stale after the move. Clear it so
		// the upsert pass refills it from the new location.
		game.ExePath = ""
	}
	return database.UpdateGame(game)
}

// isPathUnder reports whether p equals root or lives underneath it, using a
// separator-aware comparison to avoid path-prefix collisions
// (e.g. /games/foo must not match /games/foobar).
func isPathUnder(root, p string) bool {
	root = filepath.Clean(root)
	p = filepath.Clean(p)
	if p == root {
		return true
	}
	return strings.HasPrefix(p, root+string(filepath.Separator))
}