package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mili/moxie/internal/config"
	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/scanner"
	"github.com/mili/moxie/internal/scraper"
	"github.com/mili/moxie/internal/util"
)

// RunScanConfig holds parameters for RunScan.
type RunScanConfig struct {
	Dirs         []string
	JSONOut      bool
	NoSave       bool
	EngineFilter string
	Force        bool
	DoSync       bool
	DoScrape     bool
	CookieStr    string
	CookieFile   string
	Unsafe       bool
}

// RunScan scans one or more directories for games and saves results to the
// database. It returns an error if any directory fails to scan.
// This is the testable logic function — it returns errors instead of os.Exit.
func RunScan(database *db.Database, cfg RunScanConfig) error {
	for _, dir := range cfg.Dirs {
		if err := runScanDir(database, dir, cfg); err != nil {
			return err
		}
	}
	return nil
}

// Scan scans a directory for games and optionally saves them to the database.
// By default it performs an incremental scan, skipping directories already
// known to the database whose modification time hasn't changed. Use --force
// to rescan everything and re-detect all games.
//
// Flags:
//
//	--sync                After scanning, auto-associate F95Zone threads and check for updates
//	--scrape              After scanning, auto-associate F95Zone threads only (no update check)
//	--cookie <str>        Cookie header for F95Zone access (required with --sync/--scrape)
//	--cookie-file <path>  Read cookie from file
//	--unsafe              Skip rate limiting
//	--force               Full rescan
//	--no-save             Print detected games without saving to library
//	--engine <type>       Filter by engine
//	--json                Output as JSON
func Scan(args []string) {
	fs := flag.NewFlagSet("scan", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "Output as JSON")
	noSave := fs.Bool("no-save", false, "Don't save to database")
	engineFilter := fs.String("engine", "", "Filter by engine")
	force := fs.Bool("force", false, "Full rescan — detect all games, skip nothing")
	doSync := fs.Bool("sync", false, "After scan, auto-associate F95Zone threads and check for updates")
	doScrape := fs.Bool("scrape", false, "After scan, auto-associate F95Zone threads only")
	cookieStr := fs.String("cookie", "", "Cookie header (required with --sync/--scrape)")
	cookieFile := fs.String("cookie-file", "", "Read cookie from file")
	unsafe := fs.Bool("unsafe", false, "Skip rate limiting")
	fs.Parse(args)

	var dirs []string
	if fs.NArg() < 1 {
		// Try configured scan paths.
		cfg, err := config.ReadConfig()
		if err == nil && len(cfg.ScanPaths) > 0 {
			dirs = cfg.ScanPaths
		} else {
			fmt.Fprintf(os.Stderr, "Usage: moxie scan [flags] <directory>\n")
			fmt.Fprintf(os.Stderr, "Flags:\n")
			fs.PrintDefaults()
			os.Exit(1)
		}
	} else {
		dirs = fs.Args()
	}

	database := OpenDB()
	defer database.Close()

	runCfg := RunScanConfig{
		Dirs:         dirs,
		JSONOut:      *jsonOut,
		NoSave:       *noSave,
		EngineFilter: *engineFilter,
		Force:        *force,
		DoSync:       *doSync,
		DoScrape:     *doScrape,
		CookieStr:    *cookieStr,
		CookieFile:   *cookieFile,
		Unsafe:       *unsafe,
	}

	if err := RunScan(database, runCfg); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// runScanDir scans a single directory and optionally runs post-scan actions.
func runScanDir(database *db.Database, dir string, cfg RunScanConfig) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolving path %q: %w", dir, err)
	}

	// Default: incremental scan — skip known, unchanged game directories.
	// With --force: scan everything (skipPaths stays nil).
	var skipPaths map[string]bool
	skipped := 0

	if !cfg.Force {
		entries, err := database.AllGamePaths()
		if err != nil {
			return fmt.Errorf("querying known games: %w", err)
		}
		skipPaths = make(map[string]bool, len(entries))
		for _, e := range entries {
			// If directory mtime matches last scan, skip it (unchanged).
			// If no mtime recorded (legacy), skip by path alone — the
			// scanner will still re-detect and update tracking data.
			if e.DirMTime.IsZero() || mtimeMatches(e.Path, e.DirMTime) {
				skipPaths[e.Path] = true
				skipped++
			}
		}
	}

	fmt.Fprintf(os.Stderr, "Scanning %s...", absDir)
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, " (skipping %d unchanged)", skipped)
	}
	fmt.Fprintf(os.Stderr, "\n")
	scanStart := time.Now()
	games, err := scanner.ScanFiltered(absDir, skipPaths, func(dirsExamined, gamesFound int, phase string) {
		if phase == "walk" {
			fmt.Fprintf(os.Stderr, "\r  Directories examined: %d  •  Games found: %d  ", dirsExamined, gamesFound)
		} else {
			fmt.Fprintf(os.Stderr, "\r  Detecting engines: %d/%d games  ", gamesFound, dirsExamined)
		}
	})
	if err != nil {
		return fmt.Errorf("scanning %q: %w", absDir, err)
	}
	scanElapsed := time.Since(scanStart).Truncate(time.Second)
	fmt.Fprintf(os.Stderr, "\r  Scan complete: %d games found in %s  \n", len(games), scanElapsed)

	// Filter by engine if specified.
	if cfg.EngineFilter != "" {
		var filtered []scanner.DetectedGame
		for _, g := range games {
			if string(g.Engine) == cfg.EngineFilter {
				filtered = append(filtered, g)
			}
		}
		games = filtered
	}

	if cfg.JSONOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(games)
		return nil
	}

	fmt.Printf("Found %d games:\n\n", len(games))
	for _, g := range games {
		sizeStr := util.FormatSize(g.SizeBytes)
		fmt.Printf("  %-30s %-12s %8s", g.Title, g.Engine, sizeStr)
		if g.ExePath != "" {
			fmt.Printf("  %s", g.ExePath)
		}
		fmt.Println()
	}
	if skipped > 0 {
		fmt.Printf("  (%d known games skipped, use --force to rescan)\n", skipped)
	}

	if cfg.NoSave || (len(games) == 0 && !cfg.Force) {
		if len(games) == 0 && skipped > 0 {
			fmt.Println("No new games detected.")
		}
		return nil
	}

	fmt.Fprintf(os.Stderr, "\nSave to library? (y/N): ")
	var answer string
	fmt.Scanln(&answer)
	if strings.ToLower(answer) != "y" {
		fmt.Println("Not saved.")
		return nil
	}

	saved := 0
	updated := 0
	for _, g := range games {
		// Check if already exists by path.
		existing, err := database.GetGameByPath(g.Path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Error checking %s: %v\n", g.Path, err)
			return fmt.Errorf("checking game path %q: %w", g.Path, err)
		}
		if existing != nil {
			// Update existing record with newly detected scan data.
			// Only overwrite fields the user may have curated if they're
			// still empty or set to fallback values — preserves manual
			// corrections (engine re-classification, custom exe, etc.).
			if existing.Version == "" {
				existing.Version = g.Version
			}
			if existing.Engine == "" || existing.Engine == "Unknown" {
				existing.Engine = string(g.Engine)
			}
			if existing.ExePath == "" {
				existing.ExePath = g.ExePath
			}
			existing.SizeBytes = g.SizeBytes
			existing.LastScannedAt = time.Now().UTC()
			existing.DirMTime = dirModTime(g.Path)
			if err := database.UpdateGame(existing); err != nil {
				return fmt.Errorf("updating %q: %w", g.Title, err)
			}
			fmt.Fprintf(os.Stderr, "  Updated: %s (%s, v%s)\n", g.Title, g.Engine, g.Version)
			updated++
			continue
		}

		cleanTitle := scraper.SanitizeTitle(g.Title)
		if cleanTitle == "" {
			cleanTitle = g.Title
		}

		now := time.Now().UTC()
		newGame := &db.Game{
			Title:         cleanTitle,
			Engine:        string(g.Engine),
			Path:          g.Path,
			ExePath:       g.ExePath,
			Version:       g.Version,
			SizeBytes:     g.SizeBytes,
			Status:        "unknown",
			LastScannedAt: now,
			DirMTime:      dirModTime(g.Path),
		}
		if _, err := database.InsertGame(newGame); err != nil {
			fmt.Fprintf(os.Stderr, "  Error saving %s: %v\n", cleanTitle, err)
		} else {
			fmt.Fprintf(os.Stderr, "  Saved: %s (%s)\n", cleanTitle, g.Engine)
			saved++
		}
	}
	fmt.Fprintf(os.Stderr, "\nSaved %d games, updated %d.\n", saved, updated)

	// Post-scan action hooks: auto-sync or auto-scrape.
	if cfg.DoSync || cfg.DoScrape {
		cookie := ResolveCookie(cfg.CookieStr, cfg.CookieFile)
		if cookie == "" {
			return fmt.Errorf("cookie required for --sync/--scrape. Log into f95zone.to in Firefox")
		}

		var client *scraper.Client
		if cfg.Unsafe {
			client = scraper.NewUnsafeClient(cookie)
		} else {
			client = scraper.NewClient(cookie)
		}

		if cfg.DoSync {
			// Phase 1: Auto-associate.
			fmt.Fprintln(os.Stderr, "\n=== Post-scan: Auto-associating games with F95Zone threads ===")
			if err := RunScrapeAuto(database, client, false, 1); err != nil {
				return fmt.Errorf("auto-association: %w", err)
			}

			// Phase 2: Check for version updates.
			fmt.Fprintln(os.Stderr, "\n=== Post-scan: Checking for version updates ===")
			trackable, err := database.GamesWithF95URL()
			if err != nil {
				return fmt.Errorf("querying games with F95 URLs: %w", err)
			}
			if len(trackable) > 0 {
				updatesFound, _ := RunUpdateCheck(database, client, trackable, false)
				fmt.Fprintf(os.Stderr, "\n=== %d updates available ===\n", updatesFound)
			}
		} else {
			// Scrape only (no update check).
			fmt.Fprintln(os.Stderr, "\n=== Post-scan: Auto-associating games with F95Zone threads ===")
			if err := RunScrapeAuto(database, client, false, 1); err != nil {
				return fmt.Errorf("auto-association: %w", err)
			}
		}
	}
	return nil
}

// List lists all games in the library.
func List(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "Output as JSON")
	engineFilter := fs.String("engine", "", "Filter by engine")
	statusFilter := fs.String("status", "", "Filter by status")
	warnings := fs.Bool("warnings", false, "Show warnings column with detected issues (engine/exe mismatches)")
	showDeleted := fs.Bool("deleted", false, "Show soft-deleted games instead of active ones")
	fs.Parse(args)

	database := OpenDB()
	defer database.Close()

	var games []db.Game
	var err error
	if *showDeleted {
		games, err = database.ListDeletedGames()
	} else {
		games, err = database.ListActiveGames(*engineFilter, *statusFilter)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing games: %v\n", err)
		os.Exit(1)
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(games)
		return
	}

	if len(games) == 0 {
		if *showDeleted {
			fmt.Println("No deleted games found.")
		} else {
			fmt.Println("No games found. Use 'moxie scan <directory>' to scan for games.")
		}
		return
	}

	if *warnings {
		fmt.Printf("%-4s %-30s %-12s %-8s %-10s %s\n", "ID", "Title", "Engine", "Version", "Status", "Warnings")
		fmt.Println(strings.Repeat("-", 100))
		for _, g := range games {
			ver := g.Version
			if ver == "" {
				if g.LatestVersion != "" {
					ver = g.LatestVersion
				} else {
					ver = "unknown"
				}
			}
			// Collect compact warning indicators.
			var warns []string
			if s := CheckEngineMismatch(g); s != "" {
				warns = append(warns, "engine")
			}
			if s := CheckExeMismatch(g); s != "" {
				warns = append(warns, "exe")
			}
			warnStr := strings.Join(warns, "/")
			if warnStr == "" {
				warnStr = "-"
			}
			fmt.Printf("%-4d %-30s %-12s %-8s %-10s %s\n",
				g.ID, util.Truncate(g.Title, 30), g.Engine, ver, g.Status, warnStr)
		}
	} else {
		fmt.Printf("%-4s %-30s %-12s %-8s %-10s %s\n", "ID", "Title", "Engine", "Version", "Status", "Path")
		fmt.Println(strings.Repeat("-", 90))
		for _, g := range games {
			ver := g.Version
			if ver == "" {
				if g.LatestVersion != "" {
					ver = g.LatestVersion
				} else {
					ver = "unknown"
				}
			}
			fmt.Printf("%-4d %-30s %-12s %-8s %-10s %s\n",
				g.ID, util.Truncate(g.Title, 30), g.Engine, ver, g.Status, util.Truncate(g.Path, 30))
		}
	}

	count, err := database.GameCount()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to count games: %v\n", err)
	}
	size, err := database.TotalSize()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to compute total size: %v\n", err)
	}
	fmt.Fprintf(os.Stderr, "\n%d games, %s total.\n", count, util.FormatSize(size))
}

// Info shows detailed information about a game.
func Info(args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie info <id|name>\n")
		os.Exit(1)
	}

	database := OpenDB()
	defer database.Close()

	game := ResolveGame(database, args[0])
	if game == nil {
		fmt.Fprintf(os.Stderr, "Cancelled.\n")
		os.Exit(1)
	}

	fmt.Printf("ID:         %d\n", game.ID)
	fmt.Printf("Title:      %s\n", game.Title)
	fmt.Printf("Engine:     %s\n", game.Engine)
	fmt.Printf("Version:    %s\n", game.Version)
	fmt.Printf("Path:       %s\n", game.Path)
	fmt.Printf("Exe:        %s\n", game.ExePath)
	fmt.Printf("Size:       %s\n", util.FormatSize(game.SizeBytes))
	fmt.Printf("Status:     %s\n", game.Status)
	fmt.Printf("F95Zone:    %s\n", game.F95URL)
	fmt.Printf("Tags:       %s\n", strings.Join(game.Tags, ", "))
	fmt.Printf("Notes:      %s\n", game.Notes)
	fmt.Printf("Created:    %s\n", game.CreatedAt.Format("2006-01-02 15:04"))
	fmt.Printf("Updated:    %s\n", game.UpdatedAt.Format("2006-01-02 15:04"))

	// Show scraped metadata if available.
	meta, err := database.GetScrapedMeta(game.ID)
	if err == nil && meta != nil {
		fmt.Printf("\n--- F95Zone Metadata ---\n")
		fmt.Printf("Developer:  %s\n", meta.Developer)
		fmt.Printf("Cover:      %s\n", meta.CoverURL)
		if meta.Overview != "" {
			fmt.Printf("Overview:\n%s\n", util.WrapText(meta.Overview, 70))
		}
		fmt.Printf("Scraped:    %s\n", meta.LastScraped.Format("2006-01-02 15:04"))
	}
}

// Add manually adds a game to the library.
func Add(args []string) {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	title := fs.String("title", "", "Game title")
	engine := fs.String("engine", "", "Game engine")
	version := fs.String("version", "", "Game version")
	tags := fs.String("tags", "", "Comma-separated tags")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie add <path>\n")
		os.Exit(1)
	}
	path := fs.Arg(0)

	absPath, err := filepath.Abs(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving path: %v\n", err)
		os.Exit(1)
	}

	if *title == "" {
		*title = filepath.Base(absPath)
	}

	var tagList []string
	if *tags != "" {
		for _, t := range strings.Split(*tags, ",") {
			trimmed := strings.TrimSpace(t)
			if trimmed != "" {
				tagList = append(tagList, trimmed)
			}
		}
	}

	// If engine not specified, try auto-detection.
	if *engine == "" {
		detected := scanner.ScanSingle(absPath)
		if detected.Engine != "Unknown" {
			*engine = string(detected.Engine)
			fmt.Fprintf(os.Stderr, "Auto-detected engine: %s\n", *engine)
		}
	}

	database := OpenDB()
	defer database.Close()

	game := &db.Game{
		Title:   *title,
		Engine:  *engine,
		Path:    absPath,
		Version: *version,
		Tags:    tagList,
		Status:  "unknown",
	}

	id, err := database.InsertGame(game)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error adding game: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Added game with ID %d: %s (%s)\n", id, *title, *engine)
}

// SetExe sets the executable path for a game.
func SetExe(args []string) {
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: moxie set-exe <id|name> <exe-path>\n")
		os.Exit(1)
	}
	exePath := filepath.Clean(args[1])

	if _, err := os.Stat(exePath); err != nil {
		fmt.Fprintf(os.Stderr, "Executable does not exist: %s\n", exePath)
		os.Exit(1)
	}

	database := OpenDB()
	defer database.Close()

	game := ResolveGame(database, args[0])
	if game == nil {
		fmt.Fprintf(os.Stderr, "Cancelled.\n")
		os.Exit(1)
	}

	if !ConfirmDestructive("Setting executable for", game, false) {
		fmt.Fprintf(os.Stderr, "Aborted.\n")
		os.Exit(1)
	}

	game.ExePath = exePath
	if err := database.UpdateGame(game); err != nil {
		fmt.Fprintf(os.Stderr, "Error updating exe: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Updated %q\n  exe: %s\n", game.Title, exePath)
}

// SetPath sets the filesystem path for a game.
func SetPath(args []string) {
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: moxie set-path <id|name> <new-path>\n")
		os.Exit(1)
	}
	newPath := filepath.Clean(args[1])

	if _, err := os.Stat(newPath); err != nil {
		fmt.Fprintf(os.Stderr, "Path does not exist: %s\n", newPath)
		os.Exit(1)
	}

	database := OpenDB()
	defer database.Close()

	game := ResolveGame(database, args[0])
	if game == nil {
		fmt.Fprintf(os.Stderr, "Cancelled.\n")
		os.Exit(1)
	}

	if !ConfirmDestructive("Changing path for", game, false) {
		fmt.Fprintf(os.Stderr, "Aborted.\n")
		os.Exit(1)
	}

	old := game.Path
	game.Path = newPath
	if err := database.UpdateGame(game); err != nil {
		fmt.Fprintf(os.Stderr, "Error updating path: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Updated %q\n  %s\n  → %s\n", game.Title, old, newPath)
}

// Remove removes a game from the library.
func Remove(args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie remove <id|name>\n")
		os.Exit(1)
	}

	database := OpenDB()
	defer database.Close()

	game := ResolveGame(database, args[0])
	if game == nil {
		fmt.Fprintf(os.Stderr, "Cancelled.\n")
		os.Exit(1)
	}

	if !ConfirmDestructive("Removing", game, false) {
		fmt.Fprintf(os.Stderr, "Aborted.\n")
		os.Exit(1)
	}

	if err := database.DeleteGame(game.ID); err != nil {
		fmt.Fprintf(os.Stderr, "Error removing game: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Removed: %s (%s)\n", game.Title, game.Engine)
	fmt.Printf("  Use 'moxie restore %d' to undo.\n", game.ID)
}

// validStatuses lists the allowed game status values.
var validStatuses = []string{"active", "completed", "abandoned", "on_hold", "unknown"}

// isValidStatus checks whether a status string is one of the allowed values.
func isValidStatus(s string) bool {
	for _, vs := range validStatuses {
		if s == vs {
			return true
		}
	}
	return false
}

// SetStatus changes the status of one or more games.
//
// Usage:
//
//	moxie set-status <id|name> <status>      — single game
//	moxie set-status --engine <engine> <status> — batch by engine
//	moxie set-status --all <status>           — whole library
func SetStatus(args []string) {
	fs := flag.NewFlagSet("set-status", flag.ExitOnError)
	engineFilter := fs.String("engine", "", "Batch-update games with this engine")
	all := fs.Bool("all", false, "Update status for ALL games in library")
	assumeYes := fs.Bool("y", false, "Skip confirmation prompt")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie set-status [flags] <status>\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nValid statuses: %s\n", strings.Join(validStatuses, ", "))
		os.Exit(1)
	}

	status := fs.Arg(0)
	if !isValidStatus(status) {
		fmt.Fprintf(os.Stderr, "Invalid status %q. Valid: %s\n", status, strings.Join(validStatuses, ", "))
		os.Exit(1)
	}

	database := OpenDB()
	defer database.Close()

	hasEngine := *engineFilter != ""
	hasAll := *all

	// Single game: positional arg is ID or name.
	if !hasEngine && !hasAll {
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Usage: moxie set-status <id|name> <status>\n")
			os.Exit(1)
		}
		// Re-parse: first arg is the game identifier, second is status.
		game := ResolveGame(database, args[0])
		if game == nil {
			fmt.Fprintf(os.Stderr, "Cancelled.\n")
			os.Exit(1)
		}
		oldStatus := game.Status
		if err := database.UpdateGameStatus(game.ID, status); err != nil {
			fmt.Fprintf(os.Stderr, "Error updating status: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Updated %q: %s → %s\n", game.Title, oldStatus, status)
		return
	}

	// Batch operations require confirmation.
	if hasAll {
		count, _ := database.GameCount()
		if count == 0 {
			fmt.Println("No games in library.")
			return
		}
		if !*assumeYes && !isInteractive() {
			fmt.Fprintf(os.Stderr, "Non-interactive mode: use -y to confirm batch status update for all %d games.\n", count)
			os.Exit(1)
		}
		if !*assumeYes {
			fmt.Fprintf(os.Stderr, "Set status of ALL %d games to %q? (y/N): ", count, status)
			var answer string
			fmt.Scanln(&answer)
			if strings.ToLower(strings.TrimSpace(answer)) != "y" {
				fmt.Println("Cancelled.")
				return
			}
		}
		affected, err := database.BatchUpdateStatus("", status)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Updated %d games to %q.\n", affected, status)
		return
	}

	// Batch by engine.
	if hasEngine {
		matches, err := database.GamesByEngine(*engineFilter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if len(matches) == 0 {
			fmt.Fprintf(os.Stderr, "No games with engine %q found.\n", *engineFilter)
			return
		}
		if !*assumeYes && !isInteractive() {
			fmt.Fprintf(os.Stderr, "Non-interactive mode: use -y to confirm batch status update for %d %q games.\n",
				len(matches), *engineFilter)
			os.Exit(1)
		}
		if !*assumeYes {
			fmt.Fprintf(os.Stderr, "Set status of %d %q games to %q? (y/N): ", len(matches), *engineFilter, status)
			var answer string
			fmt.Scanln(&answer)
			if strings.ToLower(strings.TrimSpace(answer)) != "y" {
				fmt.Println("Cancelled.")
				return
			}
		}
		affected, err := database.BatchUpdateStatus(*engineFilter, status)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Updated %d %q games to %q.\n", affected, *engineFilter, status)
		return
	}
}

// Restore restores a soft-deleted game by clearing its deleted_at timestamp.
func Restore(args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie restore <id>\n")
		os.Exit(1)
	}

	database := OpenDB()
	defer database.Close()

	// Try to find the game — it's deleted, so GetGame won't find it.
	// We look up by ID directly.
	var id int64
	if n, err := fmt.Sscanf(args[0], "%d", &id); err != nil || n != 1 {
		// Try resolving by name from deleted games.
		deleted, err := database.ListDeletedGames()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		for _, g := range deleted {
			if strings.EqualFold(g.Title, args[0]) {
				id = g.ID
				break
			}
		}
		if id == 0 {
			fmt.Fprintf(os.Stderr, "No deleted game found matching %q.\n", args[0])
			os.Exit(1)
		}
	}

	if err := database.RestoreGame(id); err != nil {
		fmt.Fprintf(os.Stderr, "Error restoring game: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Restored game ID %d.\n", id)
}

// Purge permanently removes all soft-deleted games from the library.
func Purge(args []string) {
	database := OpenDB()
	defer database.Close()

	deleted, err := database.ListDeletedGames()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing deleted games: %v\n", err)
		os.Exit(1)
	}
	if len(deleted) == 0 {
		fmt.Println("No deleted games to purge.")
		return
	}

	fmt.Fprintf(os.Stderr, "Permanently delete %d soft-deleted games? (y/N): ", len(deleted))
	var answer string
	fmt.Scanln(&answer)
	if strings.ToLower(strings.TrimSpace(answer)) != "y" {
		fmt.Println("Cancelled.")
		return
	}

	count, err := database.PurgeDeleted()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error purging deleted games: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Purged %d games.\n", count)
}

// dirModTime returns the directory modification time as a UTC time.Time
// truncated to second precision (RFC3339 storage resolution).
// Returns the zero time if stat fails.
func dirModTime(dir string) time.Time {
	info, err := os.Stat(dir)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime().UTC().Truncate(time.Second)
}

// mtimeMatches checks whether the directory's current modification time
// matches the stored mtime from a previous scan. Compares at second
// precision because stored mtimes are serialized via RFC3339 which
// drops sub-second components. Returns true if stat fails (conservative
// — treats unreadable dirs as unchanged to avoid flooding errors).
func mtimeMatches(dir string, stored time.Time) bool {
	stored = stored.UTC().Truncate(time.Second)
	current := dirModTime(dir)
	if current.IsZero() {
		return true // can't stat, treat as unchanged
	}
	return current.Equal(stored)
}
