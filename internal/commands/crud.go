package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/scanner"
	"github.com/mili/moxie/internal/scraper"
	"github.com/mili/moxie/internal/util"
)

// Scan scans a directory for games and optionally saves them to the database.
func Scan(args []string) {
	fs := flag.NewFlagSet("scan", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "Output as JSON")
	noSave := fs.Bool("no-save", false, "Don't save to database")
	engineFilter := fs.String("engine", "", "Filter by engine")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie scan <directory>\n")
		os.Exit(1)
	}
	dir := fs.Arg(0)

	absDir, err := filepath.Abs(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving path: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Scanning %s...\n", absDir)
	games, err := scanner.Scan(absDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error scanning: %v\n", err)
		os.Exit(1)
	}

	// Filter by engine if specified.
	if *engineFilter != "" {
		var filtered []scanner.DetectedGame
		for _, g := range games {
			if string(g.Engine) == *engineFilter {
				filtered = append(filtered, g)
			}
		}
		games = filtered
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(games)
		return
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

	if *noSave || len(games) == 0 {
		return
	}

	fmt.Fprintf(os.Stderr, "\nSave to library? (y/N): ")
	var answer string
	fmt.Scanln(&answer)
	if strings.ToLower(answer) != "y" {
		fmt.Println("Not saved.")
		return
	}

	database := OpenDB()
	defer database.Close()

	saved := 0
	updated := 0
	for _, g := range games {
		// Check if already exists by path.
		existing, err := database.GetGameByPath(g.Path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Error checking %s: %v\n", g.Path, err)
			continue
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
			if err := database.UpdateGame(existing); err != nil {
				fmt.Fprintf(os.Stderr, "  Error updating %s: %v\n", g.Title, err)
				continue
			}
			fmt.Fprintf(os.Stderr, "  Updated: %s (%s, v%s)\n", g.Title, g.Engine, g.Version)
			updated++
			continue
		}

		cleanTitle := scraper.SanitizeTitle(g.Title)
		if cleanTitle == "" {
			cleanTitle = g.Title
		}

		game := &db.Game{
			Title:     cleanTitle,
			Engine:    string(g.Engine),
			Path:      g.Path,
			ExePath:   g.ExePath,
			Version:   g.Version,
			SizeBytes: g.SizeBytes,
			Status:    "unknown",
		}
		if _, err := database.InsertGame(game); err != nil {
			fmt.Fprintf(os.Stderr, "  Error saving %s: %v\n", cleanTitle, err)
		} else {
			fmt.Fprintf(os.Stderr, "  Saved: %s (%s)\n", cleanTitle, g.Engine)
			saved++
		}
	}
	fmt.Fprintf(os.Stderr, "\nSaved %d games, updated %d.\n", saved, updated)
}

// List lists all games in the library.
func List(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "Output as JSON")
	engineFilter := fs.String("engine", "", "Filter by engine")
	statusFilter := fs.String("status", "", "Filter by status")
	warnings := fs.Bool("warnings", false, "Show warnings column with detected issues (engine/exe mismatches)")
	fs.Parse(args)

	database := OpenDB()
	defer database.Close()

	games, err := database.ListGames(*engineFilter, *statusFilter)
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
		fmt.Println("No games found. Use 'moxie scan <directory>' to scan for games.")
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
		detected, err := scanner.ScanSingle(absPath)
		if err == nil && detected.Engine != "Unknown" {
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
}
