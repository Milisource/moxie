package saves

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mili/moxie/internal/archive"
	"github.com/mili/moxie/internal/db"
)

// maxSlugLen caps generated names/slugs so set directory names stay sane.
const maxSlugLen = 48

// ImportOptions tunes how Import names and records a new set. Every field is
// optional: zero values fall back to the label, the game's version, and the
// "import" source respectively.
type ImportOptions struct {
	// Name overrides the generated set name (before slugification/uniqueness).
	Name string
	// Label is the human-readable display label; also the default name source.
	Label string
	// Source records provenance: "snapshot" | "import" | "f95_attachment" |
	// "f95_link". Defaults to "import".
	Source string
	// SourceURL is the origin URL for fetched sets (optional).
	SourceURL string
	// GameVersion is the game version the set targets; defaults to game.Version.
	GameVersion string
	// ApplyRoot pins the in-set subpath to apply. When empty it is derived from
	// the staged tree (see computeApplyRoot).
	ApplyRoot string
	// Password is accepted for CLI compatibility but is NOT currently honored:
	// the extraction backend has no password support (see docs/saves.md).
	Password string
}

// maxExtractBytes and maxExtractEntries bound how far an imported archive may
// expand. Save data is a few MB, so an archive that unpacks to half a gigabyte
// (or tens of thousands of entries) is a decompression bomb, not a save; it is
// refused before it can fill the disk.
const (
	maxExtractBytes   = 512 << 20 // 512 MiB
	maxExtractEntries = 50000
)

// Import creates a new save set for game from src, an existing directory or a
// file (archive or single save file).
//
// The staged tree is copied verbatim into SetPath(game, name) — never moved,
// never reorganized — so the on-disk set always mirrors the source. The
// relative subpath to apply (SaveSet.ApplyRoot) is derived from the staged tree
// and stored, leaving the tree itself untouched; a later apply step copies the
// contents of that subpath out of the set.
//
// The set directory is reserved up front with an atomic name claim (see
// reserveSetDir) so concurrent imports cannot collide. Archives are extracted
// to a temporary directory first, under a size/entry budget. On any failure the
// directory reserved by this call is removed; directories owned by other
// callers are never touched.
func (e *Engine) Import(game *db.Game, src string, opts ImportOptions) (*db.SaveSet, error) {
	if e == nil || e.db == nil {
		return nil, fmt.Errorf("saves: engine is not initialized")
	}
	if game == nil {
		return nil, fmt.Errorf("saves: game is required")
	}
	if src == "" {
		return nil, fmt.Errorf("saves: source path is required")
	}

	info, err := os.Stat(src)
	if err != nil {
		return nil, fmt.Errorf("saves: stat source: %w", err)
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("saves: source must be a directory or a regular file")
	}

	name, setDir, err := e.reserveSetDir(game, opts)
	if err != nil {
		return nil, err
	}
	keepDir := false
	defer func() {
		if !keepDir {
			os.RemoveAll(setDir)
		}
	}()

	// Stage the source into a tree that can be copied verbatim. Directories
	// stage in place; archives are extracted because their bytes are not save
	// data; a lone non-archive file is copied directly.
	stageRoot := ""
	singleFile := ""
	switch {
	case info.IsDir():
		stageRoot = src
	case archive.IsArchiveFile(src):
		tempDir, err := os.MkdirTemp("", "moxie-saves-*")
		if err != nil {
			return nil, fmt.Errorf("saves: create staging dir: %w", err)
		}
		defer os.RemoveAll(tempDir)
		if _, err := archive.Extract(src, tempDir, archive.Options{
			MaxTotalBytes: maxExtractBytes,
			MaxEntries:    maxExtractEntries,
		}); err != nil {
			return nil, fmt.Errorf("saves: extract archive: %w", err)
		}
		// Stage the whole extraction tree: computeApplyRoot strips archive
		// wrapper directories itself, so the extractor's payload-root hint is
		// not needed here.
		stageRoot = tempDir
	default:
		singleFile = src
	}

	// apply_root is computed against the staged tree before copying; because
	// the copy is verbatim it describes the copied tree identically.
	applyRoot := ""
	if singleFile == "" {
		applyRoot, err = computeApplyRoot(stageRoot, opts.ApplyRoot)
		if err != nil {
			return nil, err
		}
	}

	if singleFile != "" {
		if err := copyFileContents(singleFile, filepath.Join(setDir, filepath.Base(singleFile))); err != nil {
			return nil, fmt.Errorf("saves: copy file: %w", err)
		}
	} else if err := copyTree(stageRoot, setDir); err != nil {
		return nil, fmt.Errorf("saves: copy set contents: %w", err)
	}

	sizeBytes, fileCount, err := treeStats(setDir)
	if err != nil {
		return nil, fmt.Errorf("saves: measure set: %w", err)
	}

	source := opts.Source
	if source == "" {
		source = "import"
	}
	version := opts.GameVersion
	if version == "" {
		version = game.Version
	}
	label := opts.Label
	if label == "" {
		label = name
	}

	set := &db.SaveSet{
		GameID:      game.ID,
		Name:        name,
		Label:       label,
		Source:      source,
		SourceURL:   opts.SourceURL,
		GameVersion: version,
		ApplyRoot:   applyRoot,
		SizeBytes:   sizeBytes,
		FileCount:   fileCount,
		Active:      false,
	}
	if _, err := e.db.CreateSaveSet(set); err != nil {
		return nil, fmt.Errorf("saves: record set: %w", err)
	}
	keepDir = true
	return set, nil
}

// List returns the save sets recorded for game, newest first.
func (e *Engine) List(game *db.Game) ([]db.SaveSet, error) {
	if game == nil {
		return nil, fmt.Errorf("saves: game is required")
	}
	return e.db.ListSaveSets(game.ID)
}

// Remove deletes the named set's directory and database row. It refuses to
// touch anything outside the engine root and errors on an unknown set.
//
// The game's ACTIVE set cannot be removed. Ownership of the live save data is
// tracked by the active flag: an active set is the one moxie last applied, and
// removing it would leave the live location holding that set's data with no
// record of where it came from — a later apply would then auto-snapshot the
// foreign data as "my saves". Apply a different set (which deactivates this
// one) or restore your own saves first.
func (e *Engine) Remove(game *db.Game, setName string) error {
	if game == nil {
		return fmt.Errorf("saves: game is required")
	}
	set, err := e.db.GetSaveSet(game.ID, setName)
	if err != nil {
		return err
	}
	if set == nil {
		return fmt.Errorf("saves: save set %q not found for game %d", setName, game.ID)
	}
	if set.Active {
		return fmt.Errorf("saves: cannot remove active save set %q; apply another set first", setName)
	}

	dir := e.SetPath(game, set.Name)
	if err := ensurePathWithin(e.root, dir); err != nil {
		return fmt.Errorf("saves: refusing to remove %q: %w", dir, err)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("saves: remove set dir: %w", err)
	}
	if err := e.db.DeleteSaveSet(set.ID); err != nil {
		return fmt.Errorf("saves: delete set row: %w", err)
	}
	return nil
}

// SetPath returns the on-disk directory for a named set:
// <root>/<gameID>/<setName>.
func (e *Engine) SetPath(game *db.Game, setName string) string {
	if e == nil || game == nil {
		return ""
	}
	return filepath.Join(e.gameDir(game), setName)
}

// gameDir returns the directory holding a game's set directories:
// <root>/<gameID>. Set directories are keyed by the immutable game ID because
// the game title is mutable — rename, scrape, and sync all change it — and a
// title-derived path would strand existing sets under the old name.
//
// A legacy directory named <gameID>-<slug> is migrated in place on first use.
// When the new path already exists it wins; when exactly one legacy directory
// matches it is renamed to the new path; otherwise the new path is returned
// and callers create it as usual.
func (e *Engine) gameDir(game *db.Game) string {
	id := fmt.Sprintf("%d", game.ID)
	dir := filepath.Join(e.root, id)
	if _, err := os.Stat(dir); err == nil {
		return dir
	}

	matches, err := filepath.Glob(filepath.Join(e.root, id+"-*"))
	if err == nil && len(matches) == 1 {
		if err := os.Rename(matches[0], dir); err == nil {
			return dir
		}
	}
	return dir
}

// reserveSetDir picks a filesystem-safe set name unique among the game's sets
// and atomically reserves its directory by creating it with os.Mkdir.
//
// Candidates are base, base-2, base-3, … where base is derived from opts (name,
// else label, else a timestamp). A candidate is skipped when the database
// already records it, and os.Mkdir itself rejects the rest: an existing
// directory makes Mkdir fail with EEXIST, which advances to the next candidate.
// This closes the check-then-create race that let two concurrent processes pick
// the same name — the loser would otherwise MkdirAll (a no-op on the winner's
// directory) and RemoveAll it on failure, destroying the winner's files.
//
// On success the returned directory was created by this call and is owned
// exclusively by it; callers MUST remove it if they fail before recording the
// set, and must not touch it otherwise.
func (e *Engine) reserveSetDir(game *db.Game, opts ImportOptions) (name, dir string, err error) {
	base := slugify(opts.Name, maxSlugLen)
	if base == "" {
		base = slugify(opts.Label, maxSlugLen)
	}
	if base == "" {
		base = "import-" + time.Now().Format("20060102-150405")
	}

	// The per-game parent directory must exist before a leaf Mkdir can succeed;
	// create it once up front (the same directory every candidate lives in).
	parent := e.gameDir(game)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", "", fmt.Errorf("saves: create game dir: %w", err)
	}

	for i := 1; i <= 10000; i++ {
		candidate := base
		if i > 1 {
			candidate = fmt.Sprintf("%s-%d", base, i)
		}

		existing, err := e.db.GetSaveSet(game.ID, candidate)
		if err != nil {
			return "", "", err
		}
		if existing != nil {
			continue
		}

		candidateDir := filepath.Join(parent, candidate)
		if err := os.Mkdir(candidateDir, 0o755); err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", "", fmt.Errorf("saves: reserve set dir: %w", err)
		}
		return candidate, candidateDir, nil
	}
	return "", "", fmt.Errorf("saves: could not find a unique set name for game %d", game.ID)
}

// ---------------------------------------------------------------------------
// apply_root heuristics
// ---------------------------------------------------------------------------

// computeApplyRoot derives the in-set subpath to apply from the staged tree
// root. The precedence is:
//
//	a. an explicit override (must be an existing directory under root);
//	b. strip single-wrapper chains (one-entry directories);
//	c. the deepest directory named "save"/"saves" (case-insensitive) that holds
//	   at least one file, else the deepest directory holding recognizable save
//	   files, else the directory left after (b).
//
// The result is a slash-separated path relative to root ("" for root itself).
// The set directory tree is deliberately left unmodified so a later apply can
// copy the apply_root subtree's contents out.
func computeApplyRoot(root, override string) (string, error) {
	if override != "" {
		rel := filepath.Clean(override)
		if rel == "." {
			return "", nil
		}
		abs := filepath.Join(root, rel)
		if err := ensurePathWithin(root, abs); err != nil {
			return "", fmt.Errorf("saves: apply_root %q is outside the set: %w", override, err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			return "", fmt.Errorf("saves: apply_root %q: %w", override, err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("saves: apply_root %q is not a directory", override)
		}
		return filepath.ToSlash(rel), nil
	}

	// Strip single-wrapper chains (e.g. archive payload nested one level deep).
	cur := root
	for {
		entries, err := os.ReadDir(cur)
		if err != nil {
			return "", fmt.Errorf("saves: read staging dir: %w", err)
		}
		if len(entries) != 1 || !entries[0].IsDir() {
			break
		}
		cur = filepath.Join(cur, entries[0].Name())
	}

	if d := findSaveDataDir(root); d != "" {
		return relativeUnder(root, d), nil
	}
	if d := findSaveFilesDir(root); d != "" {
		return relativeUnder(root, d), nil
	}
	return relativeUnder(root, cur), nil
}

// saveFilePatterns are lowercase globs matching recognizable save files across
// the supported engines.
var saveFilePatterns = []string{
	"*.save",
	"persistent",
	"save*.rvdata2",
	"global.rvdata2",
	"*.rpgsave",
	"*.rmmzsave",
	"global.rpgsave",
	"global.rmmzsave",
	"save*.rxdata",
	"*.rvdata",
	"*.rvdata2",
}

// findSaveDataDir returns the deepest directory named save/saves (case
// insensitive) that directly contains at least one file, or "".
func findSaveDataDir(root string) string {
	best, bestDepth := "", -1
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		switch strings.ToLower(d.Name()) {
		case "save", "saves":
		default:
			return nil
		}
		if !dirHasRegularFile(path) {
			return nil
		}
		if depth := dirDepth(root, path); depth > bestDepth {
			best, bestDepth = path, depth
		}
		return nil
	})
	return best
}

// findSaveFilesDir returns the deepest directory directly containing a
// recognizable save file, or "".
func findSaveFilesDir(root string) string {
	best, bestDepth := "", -1
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if !dirHasSaveFile(path) {
			return nil
		}
		if depth := dirDepth(root, path); depth > bestDepth {
			best, bestDepth = path, depth
		}
		return nil
	})
	return best
}

// dirHasRegularFile reports whether dir directly contains any non-directory
// entry.
func dirHasRegularFile(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			return true
		}
	}
	return false
}

// dirHasSaveFile reports whether dir directly contains any entry whose name
// matches a known save-file pattern.
func dirHasSaveFile(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && isSaveFileName(e.Name()) {
			return true
		}
	}
	return false
}

// isSaveFileName reports whether name matches a recognizable save-file pattern
// (case-insensitive).
func isSaveFileName(name string) bool {
	lower := strings.ToLower(name)
	for _, p := range saveFilePatterns {
		if ok, _ := filepath.Match(p, lower); ok {
			return true
		}
	}
	return false
}

// dirDepth returns the path depth of dir relative to root (root itself = 0).
func dirDepth(root, dir string) int {
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." {
		return 0
	}
	return strings.Count(rel, string(filepath.Separator))
}

// relativeUnder returns dir relative to root as a slash-separated path, or ""
// when dir is root itself.
func relativeUnder(root, dir string) string {
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." {
		return ""
	}
	return filepath.ToSlash(rel)
}

// ---------------------------------------------------------------------------
// Filesystem helpers
// ---------------------------------------------------------------------------

// slugify lowercases s, keeps [a-z0-9], collapses runs of other characters to
// single dashes, trims leading/trailing dashes, and caps the length.
func slugify(s string, maxLen int) string {
	var b strings.Builder
	pendingDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			if pendingDash && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingDash = false
			b.WriteRune(r)
		default:
			pendingDash = true
		}
	}
	out := b.String()
	if maxLen > 0 && len(out) > maxLen {
		out = strings.TrimRight(out[:maxLen], "-")
	}
	return out
}

// ensurePathWithin errors when target is not contained within root.
func ensurePathWithin(root, target string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("path %q escapes %q", target, root)
	}
	return nil
}

// copyTree copies src's entire tree into dst, creating directories with 0755
// and files with 0644. Symlinks are refused: save data must contain regular
// files only, so a hostile set cannot carry a link whose target the game would
// later follow and overwrite.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("refusing to copy symlink %q (symlinks are not allowed in save data)", path)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return copyFileContents(path, target)
	})
}

// copyFileContents copies a single file's bytes unchanged to dst, creating
// parent directories as needed.
func copyFileContents(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// treeStats totals the bytes and file count of a directory tree.
func treeStats(root string) (int64, int, error) {
	var size int64
	var count int
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		size += info.Size()
		count++
		return nil
	})
	return size, count, err
}
