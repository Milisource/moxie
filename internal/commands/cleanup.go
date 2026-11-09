package commands

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/engine"
	"github.com/mili/moxie/internal/scanner"
	"github.com/mili/moxie/internal/util"
)

// archiveExtensions are the archive suffixes the downloader accepts. Pruning
// only ever deletes files ending in one of these; nothing else in a game's
// downloads directory is touched.
var archiveExtensions = map[string]bool{
	".zip": true,
	".7z":  true,
	".rar": true,
	".tar": true,
	".gz":  true,
	".tgz": true,
	".bz2": true,
	".xz":  true,
}

// Cleanup detects and fixes wrong F95Zone thread associations. With --backups
// or --archives it instead prunes update leftovers: `<gameDir>.old` trees and
// stale archives in each game's `<parent>/downloads` directory. The pruning
// modes are opt-in and never run the association sweep.
func Cleanup(args []string) {
	fs := flag.NewFlagSet("cleanup", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "Preview issues without making changes")
	assumeYes := fs.Bool("assume-yes", false, "Auto-disassociate flagged games without prompting")
	yes := fs.Bool("y", false, "Auto-disassociate flagged games (shorthand for --assume-yes)")
	backups := fs.Bool("backups", false, "Remove <gameDir>.old trees left behind by updates")
	archives := fs.Bool("archives", false, "Remove leftover archives in each game's downloads directory")
	olderThan := fs.Duration("older-than", 168*time.Hour, "Only prune entries older than this age")
	fs.Parse(args)

	database := OpenDB()
	defer database.Close()

	games, err := database.ListGames("", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading games: %v\n", err)
		os.Exit(1)
	}

	// Pruning is a separate opt-in mode: when requested, it replaces the
	// association sweep so a maintenance run never prompts for disassociation.
	if *backups || *archives {
		gameDirs := make([]string, 0, len(games))
		for _, g := range games {
			if g.Path != "" {
				gameDirs = append(gameDirs, g.Path)
			}
		}
		report, prErr := pruneLibrary(gameDirs, pruneOptions{
			Backups:   *backups,
			Archives:  *archives,
			OlderThan: *olderThan,
			DryRun:    *dryRun,
			LockPath:  autoUpdateLockPath(),
		})
		if prErr != nil {
			fmt.Fprintf(os.Stderr, "Error pruning: %v\n", prErr)
			os.Exit(1)
		}
		printPruneReport(report, *dryRun)
		return
	}

	// Only check games that have an F95Zone URL.
	var toCheck []db.Game
	for _, g := range games {
		if g.F95URL != "" {
			toCheck = append(toCheck, g)
		}
	}

	if len(toCheck) == 0 {
		fmt.Println("No games have F95Zone URLs. Nothing to clean up.")
		return
	}

	autoDisassociate := *assumeYes || *yes

	flagged := 0
	disassociated := 0
	for _, g := range toCheck {
		var issues []string

		// Signal 1: Engine mismatch.
		if s := CheckEngineMismatch(g); s != "" {
			issues = append(issues, s)
		}

		// Signal 2: Executable name mismatch.
		if s := CheckExeMismatch(g); s != "" {
			issues = append(issues, s)
		}

		if len(issues) == 0 {
			continue
		}

		flagged++
		for _, issue := range issues {
			fmt.Printf("  #%d %q — %s\n", g.ID, g.Title, issue)
		}

		if *dryRun {
			continue
		}

		// Only prompt for disassociation on clear mismatches,
		// not "unverified" issues where F95Zone simply lacks engine tags.
		hasHardMismatch := false
		for _, s := range issues {
			if strings.Contains(s, "mismatch") && !strings.Contains(s, "unverified") {
				hasHardMismatch = true
				break
			}
		}
		if !hasHardMismatch {
			continue // just flag, don't prompt
		}

		if autoDisassociate {
			DisassociateGame(database, &g)
			disassociated++
			continue
		}

		// Interactive mode: prompt for each flagged game.
		fmt.Fprintf(os.Stderr, "  Disassociate? [y/N]: ")
		var answer string
		fmt.Scanln(&answer)
		if strings.ToLower(answer) == "y" {
			DisassociateGame(database, &g)
			disassociated++
		}
	}

	if *dryRun && flagged > 0 {
		fmt.Fprintf(os.Stderr, "\n%d game(s) flagged. Use --assume-yes or -y to auto-disassociate, or run without --dry-run for interactive mode.\n", flagged)
	}
	if flagged == 0 {
		fmt.Println("No issues found. All F95Zone associations look correct.")
	}
	if disassociated > 0 {
		fmt.Fprintf(os.Stderr, "\nDisassociated %d game(s).\n", disassociated)
	}
}

// pruneOptions controls one pruning pass. The zero value is safe: no category
// is enabled, a 7-day age floor is used, and nothing is deleted.
type pruneOptions struct {
	Backups   bool
	Archives  bool
	OlderThan time.Duration
	DryRun    bool
	// LockPath is the auto-update lock file. When it exists, pruning is
	// refused: a merge may hold a `.old` tree that is its rollback copy.
	LockPath string
	// Now is the reference time for --older-than (overridable in tests).
	Now time.Time
}

// pruneEntry is one leftover selected for removal.
type pruneEntry struct {
	Path  string
	Bytes int64
}

// pruneCategory summarizes what a category removed (or would remove).
type pruneCategory struct {
	Count   int
	Bytes   int64
	Entries []pruneEntry
}

// pruneReport is the per-category result of a pruning pass.
type pruneReport struct {
	Backups  pruneCategory
	Archives pruneCategory
}

const defaultPruneAge = 168 * time.Hour

// pruneLibrary removes update leftovers for the given game directories.
// Backups are exactly `<gameDir>.old`; archives live in the game directory's
// sibling `downloads` directory and are matched by extension only.
func pruneLibrary(gameDirs []string, opts pruneOptions) (pruneReport, error) {
	var report pruneReport
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if opts.OlderThan == 0 {
		opts.OlderThan = defaultPruneAge
	}
	if opts.OlderThan < 0 {
		return report, fmt.Errorf("--older-than must not be negative")
	}
	if opts.LockPath != "" && !opts.DryRun {
		if _, err := os.Lstat(opts.LockPath); err == nil {
			return report, fmt.Errorf("an update is in progress (lock: %s); wait for it to finish, then retry", opts.LockPath)
		}
	}

	cutoff := opts.Now.Add(-opts.OlderThan)
	seenDownloads := make(map[string]bool)
	for _, dir := range gameDirs {
		dir = filepath.Clean(dir)
		if dir == "" || dir == "." {
			continue
		}
		if opts.Backups {
			addCategory(&report.Backups, pruneBackupTree(dir, cutoff, opts.DryRun))
		}
		if opts.Archives {
			downloads := filepath.Join(filepath.Dir(dir), "downloads")
			if !seenDownloads[downloads] {
				seenDownloads[downloads] = true
				addCategory(&report.Archives, pruneArchiveFiles(downloads, cutoff, opts.DryRun))
			}
		}
	}
	return report, nil
}

func addCategory(dst *pruneCategory, src pruneCategory) {
	dst.Count += src.Count
	dst.Bytes += src.Bytes
	dst.Entries = append(dst.Entries, src.Entries...)
}

// pruneBackupTree removes `<gameDir>.old` when it is a real directory owned by
// the current user, is older than cutoff, and its live sibling still exists.
// The sibling guard protects the case where the backup is the only copy.
func pruneBackupTree(gameDir string, cutoff time.Time, dryRun bool) pruneCategory {
	var result pruneCategory
	backup := gameDir + ".old"

	info, err := os.Lstat(backup)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return result
	}
	if !ownedByCurrentUser(info) {
		return result
	}
	if info.ModTime().After(cutoff) {
		return result
	}
	// Only a real live directory beside it makes the backup expendable.
	live, err := os.Lstat(gameDir)
	if err != nil || !live.IsDir() || live.Mode()&os.ModeSymlink != 0 {
		return result
	}

	size, err := treeSize(backup)
	if err != nil {
		return result // unreadable tree: do not guess at its size or delete it
	}
	if !dryRun {
		if err := os.RemoveAll(backup); err != nil {
			return result
		}
	}
	result.Count = 1
	result.Bytes = size
	result.Entries = []pruneEntry{{Path: backup, Bytes: size}}
	return result
}

// treeSize sums regular-file sizes. Symlinks are counted neither as files nor
// traversed: filepath.WalkDir never follows them, and ModeSymlink entries are
// skipped explicitly.
func treeSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	return total, err
}

// pruneArchiveFiles removes old top-level archive files from a downloads
// directory. Subdirectories are not recursed into: extraction trees live there
// and may legitimately contain archives.
// isGameDownloadDir reports whether name is a per-game download directory
// (the game ID).
func isGameDownloadDir(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func pruneArchiveFiles(dir string, cutoff time.Time, dryRun bool) pruneCategory {
	var result pruneCategory

	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return result
	}
	if !ownedByCurrentUser(info) {
		return result
	}

	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if path == dir {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil // never delete or measure through a symlink
		}
		if d.IsDir() {
			// Downloads live in one numeric subdirectory per game (concurrent
			// updates must not share a path). Descend into those, never
			// deeper and never into arbitrary subdirectories.
			if filepath.Dir(path) == dir && isGameDownloadDir(d.Name()) {
				return nil
			}
			return filepath.SkipDir
		}
		if !archiveExtensions[strings.ToLower(filepath.Ext(d.Name()))] {
			return nil
		}
		fi, err := d.Info()
		if err != nil || !fi.Mode().IsRegular() || !ownedByCurrentUser(fi) {
			return nil
		}
		if fi.ModTime().After(cutoff) {
			return nil
		}
		if !dryRun {
			if err := os.Remove(path); err != nil {
				return nil
			}
		}
		result.Count++
		result.Bytes += fi.Size()
		result.Entries = append(result.Entries, pruneEntry{Path: path, Bytes: fi.Size()})
		return nil
	})
	if err != nil {
		return pruneCategory{}
	}
	return result
}

// ownedByCurrentUser reports whether the file is owned by the invoking user.
// Platform file info exposes the owner as a Uid field on Unix; where that
// field does not exist (Windows, synthetic FileInfo in tests) ownership cannot
// be checked and the target is accepted, matching os.Remove's own permission
// enforcement.
func ownedByCurrentUser(info os.FileInfo) bool {
	if info == nil {
		return false
	}
	value := reflect.ValueOf(info.Sys())
	if !value.IsValid() {
		return true
	}
	if value.Kind() == reflect.Ptr {
		if value.IsNil() {
			return true
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return true
	}
	field := value.FieldByName("Uid")
	if !field.IsValid() {
		return true
	}
	var uid uint64
	switch field.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		uid = field.Uint()
	default:
		return true
	}
	current := os.Getuid()
	if current < 0 {
		return true
	}
	return uid == uint64(current)
}

// printPruneReport prints per-category reclaimed bytes and the total.
func printPruneReport(report pruneReport, dryRun bool) {
	total := report.Backups.Bytes + report.Archives.Bytes
	if report.Backups.Count == 0 && report.Archives.Count == 0 {
		fmt.Println("Nothing to prune.")
		return
	}

	verb := "Removed"
	reclaimed := "Reclaimed"
	if dryRun {
		verb = "Would remove"
		reclaimed = "Would reclaim"
		for _, entry := range report.Backups.Entries {
			fmt.Printf("%s backup: %s (%s)\n", verb, entry.Path, formatBytes(entry.Bytes))
		}
		for _, entry := range report.Archives.Entries {
			fmt.Printf("%s archive: %s (%s)\n", verb, entry.Path, formatBytes(entry.Bytes))
		}
	}
	if report.Backups.Count > 0 {
		fmt.Printf("%s %d backup director%s (%s)\n", verb, report.Backups.Count,
			pluralY(report.Backups.Count), formatBytes(report.Backups.Bytes))
	}
	if report.Archives.Count > 0 {
		fmt.Printf("%s %d archive%s (%s)\n", verb, report.Archives.Count,
			pluralS(report.Archives.Count), formatBytes(report.Archives.Bytes))
	}
	fmt.Printf("%s %s total.\n", reclaimed, formatBytes(total))
}

func pluralY(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// CheckEngineMismatch returns a description of the mismatch, or "" if no
// issue is found.  It compares the scanner-detected engine (via
// engine.Detect) against the F95Zone thread tags stored on the game.
func CheckEngineMismatch(g db.Game) string {
	if len(g.Tags) == 0 {
		return "" // no F95Zone metadata to compare against
	}

	detected := engine.Detect(g.Path)
	if detected.Engine == "Others" || detected.Engine == "" {
		return "" // can't determine local engine
	}

	f95Engine := engine.FindF95Engine(g)
	if f95Engine == "" {
		return fmt.Sprintf("engine unverified — no engine info in F95Zone tags/title (scanner found: %s)", detected.Engine)
	}

	if f95Engine == string(detected.Engine) {
		return "" // both agree on the engine
	}

	// Check compatibility: RPGM games use HTML runtime (NW.js),
	// WolfRPG uses HTML, WebGL is compatible with HTML, etc.
	if compat, ok := engine.EngineCompat[f95Engine]; ok && compat[string(detected.Engine)] {
		return "" // compatible engines — not a mismatch
	}

	return fmt.Sprintf("engine mismatch (scanner: %s, F95Zone: %s)",
		detected.Engine, f95Engine)
}

// CheckExeMismatch returns a description when no executable in the game
// directory shares a word (case-insensitive) with the game title, or ""
// if at least one executable matches.
func CheckExeMismatch(g db.Game) string {
	entries, err := os.ReadDir(g.Path)
	if err != nil {
		return "" // directory inaccessible — skip
	}

	// Build a set of lowercase meaningful words from the title.
	titleWords := make(map[string]bool)
	for _, w := range strings.Fields(strings.ToLower(g.Title)) {
		w = strings.Trim(w, ".,!?-:;\"'()[]{}")
		if w != "" {
			titleWords[w] = true
		}
	}

	if len(titleWords) == 0 {
		return ""
	}

	// Collect executable filenames.
	var exeNames []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		lower := strings.ToLower(name)
		if strings.HasSuffix(lower, ".exe") ||
			strings.HasSuffix(lower, ".sh") ||
			strings.HasSuffix(lower, ".x86_64") ||
			strings.HasSuffix(lower, ".appimage") {
			exeNames = append(exeNames, name)
		}
	}

	if len(exeNames) == 0 {
		return "" // no executables to check
	}

	// Check if any exe name contains any title word.
	// Skip generic short executable names (Game.exe, LT.exe, etc.)
	// which are ubiquitous across game engines and produce false positives.
	genericExeNames := map[string]bool{
		"game": true, "launcher": true, "start": true, "run": true,
		"setup": true, "install": true, "config": true, "patch": true,
		"update": true, "unins": true,
	}
	var meaningfulExes []string
	for _, exe := range exeNames {
		exeLower := strings.ToLower(exe)
		exeBase := strings.TrimSuffix(exeLower, filepath.Ext(exeLower))
		if len(exeBase) <= 4 || genericExeNames[exeBase] {
			continue // too generic to be diagnostic
		}
		meaningfulExes = append(meaningfulExes, exe)
	}
	if len(meaningfulExes) == 0 {
		return "" // only generic executables — not diagnostic
	}

	for _, exe := range meaningfulExes {
		exeLower := strings.ToLower(exe)
		// Strip extension for a cleaner comparison.
		exeBase := strings.TrimSuffix(exeLower, filepath.Ext(exeLower))
		for word := range titleWords {
			if strings.Contains(exeBase, word) {
				return "" // at least one executable shares a title word
			}
		}
	}

	return fmt.Sprintf("unmatched executable (found: %s)", strings.Join(meaningfulExes, ", "))
}

// DisassociateGame clears the F95Zone URL and thread ID from a game and
// saves the change to the database.
func DisassociateGame(database *db.Database, g *db.Game) {
	g.F95URL = ""
	g.F95ThreadID = 0
	if err := database.UpdateGame(g); err != nil {
		fmt.Fprintf(os.Stderr, "  ! Failed to disassociate #%d: %v\n", g.ID, err)
	} else {
		fmt.Fprintf(os.Stderr, "  ✓ Disassociated #%d\n", g.ID)
	}
}

// RefreshVersions re-extracts versions from directory names.
func RefreshVersions(args []string) {
	fs := flag.NewFlagSet("refresh-versions", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "Show the version changes without writing them")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: moxie refresh-versions [--dry-run]\n\n")
		fmt.Fprintf(os.Stderr, "Re-detect installed versions from folder names and game files\n(Ren'Py options/.rpa, RPG Maker System.json, Game.ini, package.json).\n\nFlags:\n")
		fs.PrintDefaults()
	}
	fs.Parse(hoistFlags(args, nil))
	if fs.NArg() > 0 {
		fs.Usage()
		os.Exit(2)
	}

	database := OpenDB()
	defer database.Close()

	games, err := database.ListGames("", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading games: %v\n", err)
		os.Exit(1)
	}

	updated := 0
	for _, g := range games {
		dirVer := scanner.ExtractVersion(filepath.Base(g.Path))
		if dirVer == "" {
			dirVer = scanner.ExtractVersionFromDir(g.Path)
		}
		if dirVer == "" {
			continue // no version in directory name or files
		}
		if g.Version == dirVer {
			continue // already matches
		}
		oldVer := g.Version
		if *dryRun {
			updated++
			fmt.Fprintf(os.Stderr, "  %-50s %s → %s\n",
				util.Truncate(g.Title, 48), util.TruncateVer(oldVer), dirVer)
			continue
		}
		g.Version = dirVer
		if err := database.UpdateGame(&g); err != nil {
			fmt.Fprintf(os.Stderr, "  ! %q: failed to update version: %v\n", g.Title, err)
			continue
		}
		updated++
		fmt.Fprintf(os.Stderr, "  %-50s %s → %s\n",
			util.Truncate(g.Title, 48), util.TruncateVer(oldVer), dirVer)
	}

	if updated == 0 {
		fmt.Println("No version changes. All games are up to date.")
	} else if *dryRun {
		fmt.Fprintf(os.Stderr, "\n%d game(s) would change (dry run, nothing written).\n", updated)
	} else {
		fmt.Fprintf(os.Stderr, "\nUpdated %d game(s).\n", updated)
	}
}
