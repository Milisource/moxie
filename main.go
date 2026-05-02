package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mili/moxie/internal/browser"
	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/engine"
	"github.com/mili/moxie/internal/scanner"
	"github.com/mili/moxie/internal/scraper"
	"github.com/mili/moxie/internal/steam"
	"github.com/mili/moxie/internal/tui"
)

// Pre-compiled regexps used by filesystemSafe.
var (
	multiSpaceRE = regexp.MustCompile(`\s{2,}`)
	multiDashRE  = regexp.MustCompile(`-{2,}`)
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "scan":
		cmdScan(os.Args[2:])
	case "tui":
		cmdTUI()
	case "add":
		cmdAdd(os.Args[2:])
	case "info":
		cmdInfo(os.Args[2:])
	case "scrape":
		cmdScrape(os.Args[2:])
	case "scrape-batch":
		cmdScrapeBatch(os.Args[2:])
	case "set-path":
		cmdSetPath(os.Args[2:])
	case "set-exe":
		cmdSetExe(os.Args[2:])
	case "list":
		cmdList(os.Args[2:])
	case "remove":
		cmdRemove(os.Args[2:])
	case "rename":
		cmdRename(os.Args[2:])
	case "check-updates", "updates":
		cmdCheckUpdates(os.Args[2:])
	case "sync":
		cmdSync(os.Args[2:])
	case "play":
		cmdPlay(os.Args[2:])
	case "steam":
		cmdSteam(os.Args[2:])
	case "config":
		cmdConfig(os.Args[2:])
	case "cleanup":
		cmdCleanup(os.Args[2:])
	case "refresh-versions":
		cmdRefreshVersions(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `moxie - F95Zone Game Library Manager

Usage:
  moxie scan <directory> [flags]   Scan directory for games
  moxie tui                        Launch interactive TUI
  moxie add <path> [flags]         Manually add a game
  moxie info <id>                  Show game details
  moxie scrape <game-id> [flags]   Scrape F95Zone metadata
  moxie list [flags]               List all games in library
  moxie remove <id>                Remove a game from library
  moxie rename [flags]              Rename game directories using clean titles
  moxie check-updates [flags]       Check F95Zone for version updates
  moxie sync [game-id] [flags]      Full sync or sync a single game
  moxie play <id>                   Launch a game
  moxie steam add <id> [flags]        Add a game to Steam library
  moxie steam remove <id>              Remove a game from Steam library
  moxie steam list                     List games added to Steam
  moxie steam proton-list              List available Proton versions
  moxie steam proton-set <id>          Set Proton version for a Steam game
  moxie steam fix-artwork <id>         Re-download Steam artwork for a game
  moxie config <set|get|show>          Manage configuration settings
  moxie cleanup [flags]                Detect and fix wrong F95Zone thread associations
  moxie refresh-versions [flags]       Re-extract versions from directory names

Flags for 'scan':
  --json           Output results as JSON
  --no-save        Don't save to database (print only)
  --engine <type>  Filter by engine type

Flags for 'scrape':
  --cookie <str>   Cookie header string from browser DevTools
  --cookie-file <path>  Read cookie from file
  --url <url>      F95Zone thread URL (if game has no URL set)

Flags for 'list':
  --json           Output as JSON
  --engine <type>  Filter by engine
  --status <s>     Filter by status
  --warnings       Add warnings column (engine/exe mismatch indicators)

Flags for 'add':
  --title <name>   Game title
  --engine <type>  Game engine
  --version <ver>  Game version
  --tags <tags>    Comma-separated tags

Flags for 'cleanup':
  --dry-run        Preview issues without making changes
  --assume-yes     Auto-disassociate flagged games without prompting
  -y               Shorthand for --assume-yes

Flags for 'sync' and 'check-updates':
  --cookie <str>     Cookie header string from browser DevTools
  --cookie-file <path>  Read cookie from file
  --unsafe           ⚠ Skip rate limiting (risks IP ban)
  --force            Force re-check even if checked within 24h

`)
}

// configDir returns the platform-standard configuration directory for moxie.
func configDir() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "moxie")
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "moxie")
	default:
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "moxie")
	}
}

// dbPath returns the path to the games database.
func dbPath() string {
	dir := configDir()
	os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "games.db")
}

// configPath returns the path to the JSON config file.
func configPath() string {
	dir := configDir()
	os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "config.json")
}

// readConfig reads the JSON config file. Returns an empty map if the file
// does not exist.
func readConfig() (map[string]string, error) {
	path := configPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, err
	}
	var cfg map[string]string
	if err := json.Unmarshal(data, &cfg); err != nil {
		return make(map[string]string), nil // corrupted → start fresh
	}
	if cfg == nil {
		return make(map[string]string), nil
	}
	return cfg, nil
}

// writeConfig serializes the config map as JSON and writes it to the config
// file atomically.
func writeConfig(cfg map[string]string) error {
	path := configPath()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "config-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	return os.Rename(tmpName, path)
}

func openDB() *db.Database {
	path := dbPath()
	database, err := db.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	return database
}

func mustParseInt(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid ID: %s\n", s)
		os.Exit(1)
	}
	return n
}

// ---------------------------------------------------------------------------
// scan command
// ---------------------------------------------------------------------------

func cmdScan(args []string) {
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
		sizeStr := formatSize(g.SizeBytes)
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

	database := openDB()
	defer database.Close()

	saved := 0
	skipped := 0
	for _, g := range games {
		// Check if already exists by path.
		existing, err := database.GetGameByPath(g.Path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Error checking %s: %v\n", g.Path, err)
			continue
		}
		if existing != nil {
			fmt.Fprintf(os.Stderr, "  Skipping %s (already in library)\n", g.Title)
			skipped++
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
	fmt.Fprintf(os.Stderr, "\nSaved %d games, skipped %d (already in library).\n", saved, skipped)
}

// ---------------------------------------------------------------------------
// list command
// ---------------------------------------------------------------------------

func cmdList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "Output as JSON")
	engineFilter := fs.String("engine", "", "Filter by engine")
	statusFilter := fs.String("status", "", "Filter by status")
	warnings := fs.Bool("warnings", false, "Show warnings column with detected issues (engine/exe mismatches)")
	fs.Parse(args)

	database := openDB()
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
				ver = "-"
			}
			// Collect compact warning indicators.
			var warns []string
			if s := checkEngineMismatch(g); s != "" {
				warns = append(warns, "engine")
			}
			if s := checkExeMismatch(g); s != "" {
				warns = append(warns, "exe")
			}
			warnStr := strings.Join(warns, "/")
			if warnStr == "" {
				warnStr = "-"
			}
			fmt.Printf("%-4d %-30s %-12s %-8s %-10s %s\n",
				g.ID, truncate(g.Title, 30), g.Engine, ver, g.Status, warnStr)
		}
	} else {
		fmt.Printf("%-4s %-30s %-12s %-8s %-10s %s\n", "ID", "Title", "Engine", "Version", "Status", "Path")
		fmt.Println(strings.Repeat("-", 90))
		for _, g := range games {
			ver := g.Version
			if ver == "" {
				ver = "-"
			}
			fmt.Printf("%-4d %-30s %-12s %-8s %-10s %s\n",
				g.ID, truncate(g.Title, 30), g.Engine, ver, g.Status, truncate(g.Path, 30))
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
	fmt.Fprintf(os.Stderr, "\n%d games, %s total.\n", count, formatSize(size))
}

// ---------------------------------------------------------------------------
// info command
// ---------------------------------------------------------------------------

func cmdInfo(args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie info <id>\n")
		os.Exit(1)
	}
	id := mustParseInt(args[0])

	database := openDB()
	defer database.Close()

	game, err := database.GetGame(id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if game == nil {
		fmt.Fprintf(os.Stderr, "Game with ID %d not found.\n", id)
		os.Exit(1)
	}

	fmt.Printf("ID:         %d\n", game.ID)
	fmt.Printf("Title:      %s\n", game.Title)
	fmt.Printf("Engine:     %s\n", game.Engine)
	fmt.Printf("Version:    %s\n", game.Version)
	fmt.Printf("Path:       %s\n", game.Path)
	fmt.Printf("Exe:        %s\n", game.ExePath)
	fmt.Printf("Size:       %s\n", formatSize(game.SizeBytes))
	fmt.Printf("Status:     %s\n", game.Status)
	fmt.Printf("F95Zone:    %s\n", game.F95URL)
	fmt.Printf("Tags:       %s\n", strings.Join(game.Tags, ", "))
	fmt.Printf("Notes:      %s\n", game.Notes)
	fmt.Printf("Created:    %s\n", game.CreatedAt.Format("2006-01-02 15:04"))
	fmt.Printf("Updated:    %s\n", game.UpdatedAt.Format("2006-01-02 15:04"))

	// Show scraped metadata if available.
	meta, err := database.GetScrapedMeta(id)
	if err == nil && meta != nil {
		fmt.Printf("\n--- F95Zone Metadata ---\n")
		fmt.Printf("Developer:  %s\n", meta.Developer)
		fmt.Printf("Cover:      %s\n", meta.CoverURL)
		if meta.Overview != "" {
			fmt.Printf("Overview:\n%s\n", wrapText(meta.Overview, 70))
		}
		fmt.Printf("Scraped:    %s\n", meta.LastScraped.Format("2006-01-02 15:04"))
	}
}

// ---------------------------------------------------------------------------
// add command
// ---------------------------------------------------------------------------

func cmdAdd(args []string) {
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

	database := openDB()
	defer database.Close()

	game := &db.Game{
		Title:  *title,
		Engine: *engine,
		Path:   absPath,
		Version: *version,
		Tags:   tagList,
		Status: "unknown",
	}

	id, err := database.InsertGame(game)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error adding game: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Added game with ID %d: %s (%s)\n", id, *title, *engine)
}

// ---------------------------------------------------------------------------
// scrape command
// ---------------------------------------------------------------------------

func cmdScrape(args []string) {
	fs := flag.NewFlagSet("scrape", flag.ExitOnError)
	cookieStr := fs.String("cookie", "", "Cookie header from browser")
	cookieFile := fs.String("cookie-file", "", "File containing cookie header")
	threadURL := fs.String("url", "", "F95Zone thread URL")
	autoMode := fs.Bool("auto", false, "Auto-associate games using Firefox cookies + search")
	unsafe := fs.Bool("unsafe", false, "⚠ Skip rate limiting (fast but risky — may get IP banned)")
	fs.Parse(args)

	// Get cookie string — try Firefox auto-detect first.
	cookie := resolveCookie(*cookieStr, *cookieFile)

	if *autoMode {
		cmdScrapeAutoWrapper(cookie, *unsafe)
		return
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie scrape <game-id> [flags]\n")
		fmt.Fprintf(os.Stderr, "       moxie scrape --auto\n")
		os.Exit(1)
	}
	id := mustParseInt(fs.Arg(0))

	if cookie == "" {
		fmt.Fprintf(os.Stderr, "Cookie required. Use --cookie, --cookie-file, or log into f95zone.to in Firefox.\n")
		os.Exit(1)
	}

	database := openDB()
	defer database.Close()

	game, err := database.GetGame(id)
	if err != nil || game == nil {
		fmt.Fprintf(os.Stderr, "Game with ID %d not found.\n", id)
		os.Exit(1)
	}

	url := *threadURL
	if url == "" {
		url = game.F95URL
	}
	if url == "" {
		fmt.Fprintf(os.Stderr, "No F95Zone URL specified. Use --url or set it on the game first.\n")
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Scraping %s...\n", url)

	client := scraper.NewClient(cookie)
	data, err := client.ScrapeThread(url)
	if err != nil {
		if isBlocked(err) {
			fmt.Fprintf(os.Stderr, "\n⚠ BLOCKED: %v\n", err)
			fmt.Fprintf(os.Stderr, "Try refreshing your F95Zone session in Firefox and running again.\n")
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Error scraping: %v\n", err)
		os.Exit(1)
	}

	// Update game with scraped data.
	applyThreadData(game, data, url)

	if err := database.UpdateGame(game); err != nil {
		fmt.Fprintf(os.Stderr, "Error updating game: %v\n", err)
		os.Exit(1)
	}

	// Save scraped metadata.
	if data.Developer != "" || data.Overview != "" || data.CoverURL != "" {
		meta := &db.ScrapedMeta{
			GameID:    id,
			Developer: data.Developer,
			Overview:  data.Overview,
			CoverURL:  data.CoverURL,
		}
		if err := database.UpsertScrapedMeta(meta); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving metadata: %v\n", err)
		}
	}

	fmt.Printf("Scraped: %s", data.Title)
	if data.Version != "" {
		fmt.Printf(" [v%s]", data.Version)
	}
	fmt.Println()
	if data.Developer != "" {
		fmt.Printf("Developer: %s\n", data.Developer)
	}
	if len(data.Tags) > 0 {
		fmt.Printf("Tags: %s\n", strings.Join(data.Tags, ", "))
	}
	if len(data.DownloadLinks) > 0 {
		fmt.Printf("Download links: %d found\n", len(data.DownloadLinks))
		for _, dl := range data.DownloadLinks {
			fmt.Printf("  [%s] %s\n", dl.Host, dl.Name)
		}
	}
}

// cmdScrapeBatch scrapes F95Zone metadata for multiple games from a file.
// File format: one entry per line — "<id> <url>"
// Lines starting with # and blank lines are ignored.
func cmdScrapeBatch(args []string) {
	fs := flag.NewFlagSet("scrape-batch", flag.ExitOnError)
	cookieStr := fs.String("cookie", "", "Cookie header from browser")
	cookieFile := fs.String("cookie-file", "", "File containing cookie header")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie scrape-batch <file>\n")
		fmt.Fprintf(os.Stderr, "File format: one entry per line — \"<id> <url>\"\n")
		os.Exit(1)
	}

	cookie := resolveCookie(*cookieStr, *cookieFile)
	if cookie == "" {
		fmt.Fprintf(os.Stderr, "Cookie required. Log into f95zone.to in Firefox.\n")
		os.Exit(1)
	}

	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}

	type entry struct {
		id  int64
		url string
	}
	var entries []entry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			fmt.Fprintf(os.Stderr, "Skipping malformed line: %q\n", line)
			continue
		}
		id, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Skipping invalid ID %q: %v\n", parts[0], err)
			continue
		}
		entries = append(entries, entry{id: id, url: parts[1]})
	}

	if len(entries) == 0 {
		fmt.Fprintln(os.Stderr, "No valid entries found in file.")
		os.Exit(1)
	}

	database := openDB()
	defer database.Close()

	client := scraper.NewClient(cookie)
	ok, failed := 0, 0

	for i, e := range entries {
		fmt.Fprintf(os.Stderr, "[%d/%d] Scraping ID %d — %s\n", i+1, len(entries), e.id, e.url)

		game, err := database.GetGame(e.id)
		if err != nil || game == nil {
			fmt.Fprintf(os.Stderr, "  ✗ Game ID %d not found\n", e.id)
			failed++
			continue
		}

		td, err := client.ScrapeThread(e.url)
		if err != nil {
			if isBlocked(err) {
				fmt.Fprintf(os.Stderr, "\n⚠ BLOCKED: %v\nStopping batch.\n", err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "  ✗ %v\n", err)
			failed++
			continue
		}

		applyThreadData(game, td, e.url)
		if err := database.UpdateGame(game); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ Save error: %v\n", err)
			failed++
			continue
		}

		if td.Developer != "" || td.Overview != "" || td.CoverURL != "" {
			meta := &db.ScrapedMeta{
				GameID:    e.id,
				Developer: td.Developer,
				Overview:  td.Overview,
				CoverURL:  td.CoverURL,
			}
			_ = database.UpsertScrapedMeta(meta)
		}

		fmt.Fprintf(os.Stderr, "  ✓ %s", td.Title)
		if td.Version != "" {
			fmt.Fprintf(os.Stderr, " [v%s]", td.Version)
		}
		fmt.Fprintln(os.Stderr)
		ok++
	}

	fmt.Fprintf(os.Stderr, "\nDone: %d scraped, %d failed.\n", ok, failed)
}

// resolveCookie returns a cookie string from the most available source:
// 1. Explicit --cookie flag, 2. --cookie-file, 3. Firefox auto-detect.
func resolveCookie(explicit, file string) string {
	if explicit != "" {
		return explicit
	}
	if file != "" {
		if strings.HasSuffix(strings.ToLower(file), ".sqlite") {
			cookie, err := browser.GetF95CookiesFromSQLite(file)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Reading cookie database: %v\n", err)
				return ""
			}
			return cookie
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(data))
	}
	cookie, err := browser.GetF95Cookies()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Firefox cookie detection: %v\n", err)
		return ""
	}
	fmt.Fprintf(os.Stderr, "Using cookies from Firefox.\n")
	return cookie
}

// applyThreadData copies scraped ThreadData fields onto a Game.
// Non-empty fields in data overwrite the corresponding game fields.
func applyThreadData(game *db.Game, data *scraper.ThreadData, url string) {
	if data.Title != "" {
		game.Title = stripThreadPrefix(data.Title)
	}
	if data.Version != "" {
		// LatestVersion tracks the F95Zone version.
		// Version is the locally-installed version (from directory
		// name scan) — never overwrite it with F95Zone data.
		game.LatestVersion = data.Version
	}
	if data.ThreadID > 0 {
		game.F95ThreadID = data.ThreadID
	}
	game.F95URL = url
	if len(data.Tags) > 0 {
		game.Tags = data.Tags
	}
	if data.Status != "" {
		game.Status = data.Status
	}
}

// runScrapeAuto finds and associates F95Zone threads for unassociated games.
func runScrapeAuto(database *db.Database, cookie string, unsafe bool) {
	if cookie == "" {
		fmt.Fprintf(os.Stderr, "Cookie required for auto-association.\n")
		fmt.Fprintf(os.Stderr, "Log into f95zone.to in Firefox, or use --cookie/--cookie-file.\n")
		os.Exit(1)
	}

	if unsafe {
		fmt.Fprintln(os.Stderr, "⚠  --unsafe: rate limiting disabled. You may get IP-banned or Cloudflare-blocked.")
		fmt.Fprintln(os.Stderr)
	}



	allGames, err := database.ListGames("", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading games: %v\n", err)
		os.Exit(1)
	}

	// Filter to unassociated games (no F95Zone URL).
	var queue []db.Game
	for _, g := range allGames {
		if g.F95URL == "" {
			queue = append(queue, g)
		}
	}
	if len(queue) == 0 {
		fmt.Println("All games already have F95Zone URLs. Nothing to associate.")
		return
	}

	var client *scraper.Client
	if unsafe {
		client = scraper.NewUnsafeClient(cookie)
	} else {
		client = scraper.NewClient(cookie)
	}
	total := len(queue)
	associated := 0
	skipped := 0
	interrupted := false
	startTime := time.Now()

	// Estimate: each game = 1 search + maybe 1 thread read
	estSeconds := total * 8
	if unsafe {
		estSeconds = total * 2 // much faster without delays
	}
	estDuration := time.Duration(estSeconds) * time.Second
	fmt.Fprintf(os.Stderr, "\n=== Auto-Associating %d games ===\n", total)
	if unsafe {
		fmt.Fprintf(os.Stderr, "Estimated time: ~%s (unsafe mode — no rate limiting).\n", formatDuration(estDuration))
	} else {
		fmt.Fprintf(os.Stderr, "Estimated time: ~%s at current rate limits.\n", formatDuration(estDuration))
	}
	fmt.Fprintf(os.Stderr, "This is a background task — let it run. It'll pause occasionally to avoid rate limits.\n\n")

	searchCache := make(map[string][]scraper.SearchResult) // sanitized_title -> search results
	urlCache := make(map[string]string)                    // sanitized_title -> thread URL

	for i, game := range queue {
		elapsed := time.Since(startTime).Truncate(time.Second)
		fmt.Fprintf(os.Stderr, "[%d/%d] %s %q",
			i+1, total, elapsed, game.Title)

		// Search (with caching).
		query := scraper.SanitizeTitle(game.Title)
		if query == "" {
			query = game.Title
		}
		if query != game.Title {
			fmt.Fprintf(os.Stderr, "  (search: %q)", query)
		}
		fmt.Fprintln(os.Stderr)

		// Use caches to avoid redundant searches.
		var results []scraper.SearchResult
		if cachedURL, ok := urlCache[query]; ok {
			results = []scraper.SearchResult{{Title: game.Title, URL: cachedURL}}
		} else {
			var cached bool
			results, cached = searchCache[query]
			if !cached {
				var err error
				results, err = client.SearchF95Zone(query)
				if err != nil {
					if isBlocked(err) {
						fmt.Fprintf(os.Stderr, "  ⚠ BLOCKED: stopping auto-association\n    %v\n", err)
						fmt.Fprintf(os.Stderr, "  Try refreshing your F95Zone session in Firefox and running again.\n")
						interrupted = true
						break
					}
					fmt.Fprintf(os.Stderr, "  ✗ Search failed: %v\n\n", err)
					skipped++
					continue
				}
				searchCache[query] = results
			}
		}

		if len(results) == 0 {
			fmt.Fprintln(os.Stderr, "  ✗ No search results")
			skipped++
			continue
		}

		// Show results with scores.
		var best *scraper.SearchResult
		var bestScore float64
		for j, r := range results {
			score := scraper.ComputeMatchScore(game.Title, r.Title)
			marker := "  "
			if score > bestScore {
				bestScore = score
				best = &results[j]
				marker = "→ "
			}
			fmt.Fprintf(os.Stderr, "  %s[%.0f%%] %s\n", marker, score*100, truncate(r.Title, 55))
		}

		if best == nil || bestScore < 0.3 {
			fmt.Fprintf(os.Stderr, "  ✗ No good match (best: %.0f%%)\n\n", bestScore*100)
			skipped++
			continue
		}

		// Scrape the best match.
		fmt.Fprintf(os.Stderr, "  ⬇ Scraping %s...\n", best.URL)
		data, err := client.ScrapeThread(best.URL)
		if err != nil {
			if isBlocked(err) {
				fmt.Fprintf(os.Stderr, "  ⚠ BLOCKED: stopping auto-association\n    %v\n", err)
				fmt.Fprintf(os.Stderr, "  Try refreshing your F95Zone session in Firefox.\n")
				break
			}
			fmt.Fprintf(os.Stderr, "  ✗ Scrape failed: %v\n\n", err)
			skipped++
			continue
		}

		// Signal: prevent bad associations by checking engine consistency
		// BEFORE saving.  If the scanner-detected engine conflicts with
		// the F95Zone thread tags, skip this candidate — it's probably
		// the wrong thread.
		if len(data.Tags) > 0 {
			detEngine := engine.Detect(game.Path)
			if !engineMatchesTags(detEngine, data.Tags) {
				fmt.Fprintf(os.Stderr, "  ⚠ Engine mismatch (scanner: %s, thread tags: %s) — skipping\n",
					detEngine.Engine, formatTagsBrief(data.Tags, 4))
				skipped++
				continue
			}
		}

		applyThreadData(&game, data, best.URL)
		game.VersionCheckedAt = time.Now()

		if err := database.UpdateGame(&game); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ Save failed: %v\n\n", err)
			skipped++
			continue
		}

		// Cache the successful association so future runs return instantly.
		urlCache[query] = best.URL

		fmt.Fprintf(os.Stderr, "  ✓ Saved (%s)", game.Title)
		if data.Version != "" {
			fmt.Fprintf(os.Stderr, " v%s", data.Version)
		}
		if data.Developer != "" {
			fmt.Fprintf(os.Stderr, " • %s", data.Developer)
		}
		fmt.Fprintln(os.Stderr)

		// Save scraped metadata.
		if data.Developer != "" || data.Overview != "" || data.CoverURL != "" {
			meta := &db.ScrapedMeta{
				GameID:    game.ID,
				Developer: data.Developer,
				Overview:  data.Overview,
				CoverURL:  data.CoverURL,
			}
			_ = database.UpsertScrapedMeta(meta)
		}

		associated++
		fmt.Fprintln(os.Stderr)
	}

	if interrupted {
		fmt.Fprintf(os.Stderr, "=== INTERRUPTED (blocked by F95Zone) ===\n")
	}
	elapsed := time.Since(startTime).Truncate(time.Second)
	fmt.Fprintf(os.Stderr, "=== Done: %d associated, %d skipped, %d/%d total in %s ===\n",
		associated, skipped, associated+skipped, total, elapsed)
}

// cmdScrapeAutoWrapper opens the database and runs auto-association.
// This wrapper exists so cmdScrape can call it without already having a DB handle.
func cmdScrapeAutoWrapper(cookie string, unsafe bool) {
	database := openDB()
	defer database.Close()
	runScrapeAuto(database, cookie, unsafe)
}

// ---------------------------------------------------------------------------
// remove command
// ---------------------------------------------------------------------------

func cmdSetExe(args []string) {
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: moxie set-exe <id> <exe-path>\n")
		os.Exit(1)
	}
	id := mustParseInt(args[0])
	exePath := filepath.Clean(args[1])

	if _, err := os.Stat(exePath); err != nil {
		fmt.Fprintf(os.Stderr, "Executable does not exist: %s\n", exePath)
		os.Exit(1)
	}

	database := openDB()
	defer database.Close()

	game, err := database.GetGame(id)
	if err != nil || game == nil {
		fmt.Fprintf(os.Stderr, "Game with ID %d not found.\n", id)
		os.Exit(1)
	}

	game.ExePath = exePath
	if err := database.UpdateGame(game); err != nil {
		fmt.Fprintf(os.Stderr, "Error updating exe: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Updated %q\n  exe: %s\n", game.Title, exePath)
}

func cmdSetPath(args []string) {
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: moxie set-path <id> <new-path>\n")
		os.Exit(1)
	}
	id := mustParseInt(args[0])
	newPath := filepath.Clean(args[1])

	if _, err := os.Stat(newPath); err != nil {
		fmt.Fprintf(os.Stderr, "Path does not exist: %s\n", newPath)
		os.Exit(1)
	}

	database := openDB()
	defer database.Close()

	game, err := database.GetGame(id)
	if err != nil || game == nil {
		fmt.Fprintf(os.Stderr, "Game with ID %d not found.\n", id)
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

func cmdRemove(args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie remove <id>\n")
		os.Exit(1)
	}
	id := mustParseInt(args[0])

	database := openDB()
	defer database.Close()

	game, err := database.GetGame(id)
	if err != nil || game == nil {
		fmt.Fprintf(os.Stderr, "Game with ID %d not found.\n", id)
		os.Exit(1)
	}

	if err := database.DeleteGame(id); err != nil {
		fmt.Fprintf(os.Stderr, "Error removing game: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Removed: %s (%s)\n", game.Title, game.Engine)
}

// ---------------------------------------------------------------------------
// tui command
// ---------------------------------------------------------------------------

func cmdTUI() {
	if err := tui.Run(dbPath()); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// check-updates command
// ---------------------------------------------------------------------------

// checkUpdateResult holds the outcome of checking one game for updates.
type checkUpdateResult struct {
	Game    db.Game `json:"game"`
	Current string  `json:"current"`
	Latest  string  `json:"latest"`
	IsNew   bool    `json:"is_new"`
	Error   string  `json:"error,omitempty"`
}

const updateCheckCooldown = 24 * time.Hour

// runUpdateCheck scrapes each game's F95Zone thread, compares versions, and
// updates the database. It returns the count of games with new versions and
// a result for each game processed.
func runUpdateCheck(database *db.Database, client *scraper.Client, games []db.Game, force bool) (int, []checkUpdateResult) {
	// Skip games checked within the last 24 hours (unless --force).
	cooldownSkipped := 0
	var filtered []db.Game
	for _, g := range games {
		if g.F95URL == "" {
			continue
		}
		if !force && !g.VersionCheckedAt.IsZero() && time.Since(g.VersionCheckedAt) < updateCheckCooldown {
			cooldownSkipped++
			continue
		}
		filtered = append(filtered, g)
	}
	games = filtered

	if cooldownSkipped > 0 {
		fmt.Fprintf(os.Stderr, "  (skipped %d games checked within the last 24h; use --force to override)\n",
			cooldownSkipped)
	}

	if len(games) == 0 {
		if cooldownSkipped > 0 {
			fmt.Fprintln(os.Stderr, "  (all games are within the 24h cooldown — nothing to check)")
		} else {
			fmt.Fprintln(os.Stderr, "  (no games to check)")
		}
		return 0, nil
	}

	var results []checkUpdateResult
	updatesFound := 0

	// Worker pool for concurrent scraping.
	sem := make(chan struct{}, 3) // max 3 concurrent
	var mu sync.Mutex            // protects results slice and updatesFound counter
	var saveMu sync.Mutex        // serializes SQLite writes (prevents BUSY errors)
	var wg sync.WaitGroup

	for _, g := range games {
		sem <- struct{}{}
		wg.Add(1)
		go func(g db.Game) {
			defer wg.Done()
			defer func() { <-sem }()

			data, err := client.ScrapeThread(g.F95URL)
			if err != nil {
				mu.Lock()
				fmt.Fprintf(os.Stderr, "  %q ✗ %v\n", g.Title, err)
				results = append(results, checkUpdateResult{Game: g, Error: err.Error()})
				mu.Unlock()
				return
			}
			latest := data.Version
			knownVer := g.Version
			if knownVer == "" {
				knownVer = g.LatestVersion
			}
			isNew := latest != "" && knownVer != "" && normalizeVersion(latest) != normalizeVersion(knownVer)

			// Signal: check for engine mismatch between scanner and F95Zone tags.
			var engineWarn string
			if len(data.Tags) > 0 {
				detEngine := engine.Detect(g.Path)
				if !engineMatchesTags(detEngine, data.Tags) {
					engineWarn = fmt.Sprintf(" ⚠ engine mismatch (scanner: %s)",
						detEngine.Engine)
				}
			}

			g.LatestVersion = latest
			g.VersionCheckedAt = time.Now()
			saveMu.Lock()
			if err := database.UpdateGame(&g); err != nil {
				fmt.Fprintf(os.Stderr, "  ⚠ Failed to save version data for %q: %v\n", g.Title, err)
			}
			saveMu.Unlock()
			// Save scraped metadata (cover, developer, overview).
			if data.Developer != "" || data.Overview != "" || data.CoverURL != "" {
				_ = database.UpsertScrapedMeta(&db.ScrapedMeta{
					GameID:    g.ID,
					Developer: data.Developer,
					Overview:  data.Overview,
					CoverURL:  data.CoverURL,
				})
			}
			mu.Lock()
			if isNew {
				updatesFound++
				fmt.Fprintf(os.Stderr, "  %q 🔄 %s → %s%s\n", g.Title, knownVer, latest, engineWarn)
			} else if knownVer != "" {
				// Local version is known and matches F95Zone.
				fmt.Fprintf(os.Stderr, "  %q ✓ %s%s\n", g.Title, latest, engineWarn)
			} else if latest != "" {
				// F95Zone has a version but we don't know the local version.
				fmt.Fprintf(os.Stderr, "  %q ? %s (local version unknown)%s\n", g.Title, latest, engineWarn)
			} else {
				fmt.Fprintf(os.Stderr, "  %q ? no version detected%s\n", g.Title, engineWarn)
			}
			results = append(results, checkUpdateResult{Game: g, Current: knownVer, Latest: latest, IsNew: isNew})
			mu.Unlock()
		}(g)
	}
	wg.Wait()

	return updatesFound, results
}

// normalizeVersion strips trailing .0 segments and leading v/V prefix for comparison.
func normalizeVersion(v string) string {
	v = strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
	v = strings.TrimSpace(v)
	for strings.HasSuffix(v, ".0") {
		v = strings.TrimSuffix(v, ".0")
	}
	return v
}

func cmdCheckUpdates(args []string) {
	fs := flag.NewFlagSet("check-updates", flag.ExitOnError)
	cookieStr := fs.String("cookie", "", "Cookie header")
	cookieFile := fs.String("cookie-file", "", "Cookie file")
	jsonOut := fs.Bool("json", false, "JSON output")
	unsafe := fs.Bool("unsafe", false, "⚠ Skip rate limiting")
	force := fs.Bool("force", false, "Force re-check even if checked within 24h")
	fs.Parse(args)

	cookie := resolveCookie(*cookieStr, *cookieFile)
	if cookie == "" {
		fmt.Fprintf(os.Stderr, "Cookie required. Log into f95zone.to in Firefox.\n")
		os.Exit(1)
	}

	database := openDB()
	defer database.Close()

	allGames, err := database.ListGames("", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	var trackable []db.Game
	for _, g := range allGames {
		if g.F95URL != "" {
			trackable = append(trackable, g)
		}
	}
	if len(trackable) == 0 {
		fmt.Println("No games have F95Zone URLs. Run 'moxie sync' first.")
		return
	}

	var client *scraper.Client
	if *unsafe {
		client = scraper.NewUnsafeClient(cookie)
	} else {
		client = scraper.NewClient(cookie)
	}

	updatesFound, results := runUpdateCheck(database, client, trackable, *force)

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(results)
		return
	}

	fmt.Fprintf(os.Stderr, "\n=== %d updates available ===\n", updatesFound)
	for _, r := range results {
		if r.IsNew {
			fmt.Printf("  %s: %s → %s\n", r.Game.Title, r.Current, r.Latest)
		}
	}
}

// ---------------------------------------------------------------------------
// sync command — full library sync (associate + check updates)
// ---------------------------------------------------------------------------

// cmdSyncGame syncs a single game: associate it with F95Zone (if needed)
// and check for version updates.
func cmdSyncGame(id int64, cookie string, unsafe bool, force bool) {
	database := openDB()
	defer database.Close()

	game, err := database.GetGame(id)
	if err != nil || game == nil {
		fmt.Fprintf(os.Stderr, "Game with ID %d not found.\n", id)
		os.Exit(1)
	}

	var client *scraper.Client
	if unsafe {
		client = scraper.NewUnsafeClient(cookie)
	} else {
		client = scraper.NewClient(cookie)
	}

	fmt.Fprintf(os.Stderr, "Syncing: %s\n", game.Title)

	// Phase 1: Associate if needed.
	if game.F95URL == "" {
		fmt.Fprintf(os.Stderr, "  Searching F95Zone for %q...\n", game.Title)
		query := scraper.SanitizeTitle(game.Title)
		if query == "" {
			query = game.Title
		}
		results, err := client.SearchF95Zone(query)
		if err != nil {
			if isBlocked(err) {
				fmt.Fprintf(os.Stderr, "  ⚠ BLOCKED: %v\n", err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "  ✗ Search failed: %v\n", err)
			os.Exit(1)
		}
		if len(results) == 0 {
			fmt.Fprintf(os.Stderr, "  ✗ No F95Zone results found for %q.\n", game.Title)
			os.Exit(1)
		}

		// Show top results and pick best.
		var best *scraper.SearchResult
		var bestScore float64
		for i, r := range results {
			score := scraper.ComputeMatchScore(game.Title, r.Title)
			marker := "  "
			if score > bestScore {
				bestScore = score
				best = &results[i]
				marker = "→ "
			}
			fmt.Fprintf(os.Stderr, "  %s[%.0f%%] %s\n", marker, score*100, r.Title)
		}

		if best == nil || bestScore < 0.3 {
			fmt.Fprintf(os.Stderr, "  ✗ No good match found.\n")
			os.Exit(1)
		}

		fmt.Fprintf(os.Stderr, "  Scraping %s...\n", best.URL)
		data, err := client.ScrapeThread(best.URL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ Scrape failed: %v\n", err)
			os.Exit(1)
		}

		// Signal: check engine consistency before associating.
		if len(data.Tags) > 0 {
			detEngine := engine.Detect(game.Path)
			if !engineMatchesTags(detEngine, data.Tags) {
				fmt.Fprintf(os.Stderr, "  ⚠ Engine mismatch (scanner: %s, thread tags: %s)\n",
					detEngine.Engine, formatTagsBrief(data.Tags, 4))
				fmt.Fprintf(os.Stderr, "  Associate anyway? [y/N]: ")
				var answer string
				fmt.Scanln(&answer)
				if strings.ToLower(answer) != "y" {
					fmt.Fprintln(os.Stderr, "  Cancelled.")
					os.Exit(1)
				}
			}
		}

		applyThreadData(game, data, best.URL)
		game.VersionCheckedAt = time.Now() // prevent double-scrape in Phase 2
		if err := database.UpdateGame(game); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ Save failed: %v\n", err)
			os.Exit(1)
		}
		if data.Developer != "" || data.Overview != "" || data.CoverURL != "" {
			meta := &db.ScrapedMeta{
				GameID:    id,
				Developer: data.Developer,
				Overview:  data.Overview,
				CoverURL:  data.CoverURL,
			}
			_ = database.UpsertScrapedMeta(meta)
		}

		fmt.Fprintf(os.Stderr, "  ✓ Associated: %s", data.Title)
		if data.Version != "" {
			fmt.Fprintf(os.Stderr, " [v%s]", data.Version)
		}
		fmt.Fprintln(os.Stderr)
	}

	// Phase 2: Check for updates (skip if recently checked, unless --force).
	if !force && !game.VersionCheckedAt.IsZero() && time.Since(game.VersionCheckedAt) < updateCheckCooldown {
		fmt.Fprintf(os.Stderr, "  ✓ Skipped (checked within 24h; use --force to override)\n")
		return
	}
	fmt.Fprintf(os.Stderr, "  Checking for updates...\n")
	data, err := client.ScrapeThread(game.F95URL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ✗ %v\n", err)
		os.Exit(1)
	}

	// Signal: check engine consistency with freshly scraped tags.
	if len(data.Tags) > 0 {
		detEngine := engine.Detect(game.Path)
		if !engineMatchesTags(detEngine, data.Tags) {
			fmt.Fprintf(os.Stderr, "  ⚠ Engine mismatch (scanner: %s, thread tags: %s)\n",
				detEngine.Engine, formatTagsBrief(data.Tags, 4))
		}
	}

	latest := data.Version
	knownVer := game.Version

	game.LatestVersion = latest
	game.VersionCheckedAt = time.Now()
	if err := database.UpdateGame(game); err != nil {
		fmt.Fprintf(os.Stderr, "  ⚠ Failed to save version data: %v\n", err)
	}

	// Save scraped metadata (cover, developer, overview) from the scrape.
	if data.Developer != "" || data.Overview != "" || data.CoverURL != "" {
		_ = database.UpsertScrapedMeta(&db.ScrapedMeta{
			GameID:    id,
			Developer: data.Developer,
			Overview:  data.Overview,
			CoverURL:  data.CoverURL,
		})
	}

	if latest != "" && knownVer != "" && latest != knownVer {
		fmt.Fprintf(os.Stderr, "  🔄 Update available: %s → %s\n", knownVer, latest)
	} else if knownVer != "" {
		fmt.Fprintf(os.Stderr, "  ✓ Up to date: %s\n", latest)
	} else if latest != "" {
		fmt.Fprintf(os.Stderr, "  ? F95Zone has %s (local version unknown)\n", latest)
	} else {
		fmt.Fprintf(os.Stderr, "  ? No version detected\n")
	}

	// Also print scraped metadata if available.
	if data.Developer != "" {
		fmt.Fprintf(os.Stderr, "  Developer: %s\n", data.Developer)
	}
	if len(data.Tags) > 0 {
		fmt.Fprintf(os.Stderr, "  Tags: %s\n", strings.Join(data.Tags, ", "))
	}
}

func cmdSync(args []string) {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	cookieStr := fs.String("cookie", "", "Cookie header")
	cookieFile := fs.String("cookie-file", "", "Cookie file")
	unsafe := fs.Bool("unsafe", false, "⚠ Skip rate limiting")
	force := fs.Bool("force", false, "Force re-check even if checked within 24h")
	fs.Parse(args)

	cookie := resolveCookie(*cookieStr, *cookieFile)
	if cookie == "" {
		fmt.Fprintf(os.Stderr, "Cookie required. Log into f95zone.to in Firefox.\n")
		os.Exit(1)
	}

	// Single-game sync: moxie sync <game-id>
	if fs.NArg() >= 1 {
		id := mustParseInt(fs.Arg(0))
		cmdSyncGame(id, cookie, *unsafe, *force)
		return
	}

	database := openDB()
	defer database.Close()

	// Phase 1: Associate games with F95Zone threads.
	fmt.Fprintln(os.Stderr, "\n=== Phase 1/2: Associating games with F95Zone threads ===")
	runScrapeAuto(database, cookie, *unsafe)

	// Phase 2: Check for version updates.
	fmt.Fprintln(os.Stderr, "\n=== Phase 2/2: Checking for version updates ===")

	allGames, err := database.ListGames("", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}

	var trackable []db.Game
	for _, g := range allGames {
		if g.F95URL != "" {
			trackable = append(trackable, g)
		}
	}
	if len(trackable) == 0 {
		fmt.Fprintln(os.Stderr, "No games have F95Zone URLs. Nothing to check.")
		return
	}

	var client *scraper.Client
	if *unsafe {
		client = scraper.NewUnsafeClient(cookie)
	} else {
		client = scraper.NewClient(cookie)
	}

	updatesFound, _ := runUpdateCheck(database, client, trackable, *force)
	fmt.Fprintf(os.Stderr, "\n=== %d updates available ===\n", updatesFound)
}



// ---------------------------------------------------------------------------
// play command — launch games
// ---------------------------------------------------------------------------

func cmdPlay(args []string) {
	fs := flag.NewFlagSet("play", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie play <id>\n")
		os.Exit(1)
	}
	id := mustParseInt(fs.Arg(0))

	database := openDB()
	defer database.Close()

	game, err := database.GetGame(id)
	if err != nil || game == nil {
		fmt.Fprintf(os.Stderr, "Game %d not found.\n", id)
		os.Exit(1)
	}

	exe := resolveExecutable(*game)
	if exe == "" {
		fmt.Fprintf(os.Stderr, "No executable found for %q.\nPath: %s\n", game.Title, game.Path)
		os.Exit(1)
	}

	cmd := launchCommand(exe)
	fmt.Fprintf(os.Stderr, "Launching: %s\n", cmd)
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to launch: %v\n", err)
		os.Exit(1)
	}
	// Don't wait — let it run independently.
	go cmd.Wait()
}

// resolveExecutable finds the best executable to launch for a game.
func resolveExecutable(g db.Game) string {
	// If ExePath is set and exists, use it.
	if g.ExePath != "" {
		if _, err := os.Stat(g.ExePath); err == nil {
			return g.ExePath
		}
	}

	// Search the game directory for executables.
	entries, err := os.ReadDir(g.Path)
	if err != nil {
		return ""
	}

	// macOS: check for .app bundles (they're directories containing executables).
	if runtime.GOOS == "darwin" {
		appBundles, _ := filepath.Glob(filepath.Join(g.Path, "*.app"))
		for _, bundle := range appBundles {
			macExe := filepath.Join(bundle, "Contents", "MacOS")
			if dirEntries, err := os.ReadDir(macExe); err == nil {
				for _, de := range dirEntries {
					if !de.IsDir() {
						// Found an executable inside the .app bundle.
						return filepath.Join(macExe, de.Name())
					}
				}
			}
		}
	}

	var exes []string
	var scripts []string
	var appImages []string

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		switch {
		case strings.HasSuffix(name, ".AppImage"):
			appImages = append(appImages, filepath.Join(g.Path, name))
		case ext == ".x86_64" || ext == ".x86" || ext == ".sh":
			scripts = append(scripts, filepath.Join(g.Path, name))
		case ext == ".exe":
			exes = append(exes, filepath.Join(g.Path, name))
		}
	}

	// Filter out platform-incompatible executables.
	if runtime.GOOS != "linux" {
		appImages = nil // .AppImage is Linux-only
		scripts = nil   // .sh/.x86_64/.x86 are Linux-specific
	}

	// Prefer native over Wine.
	if len(appImages) > 0 {
		return selectBestExe(appImages)
	}
	if len(scripts) > 0 {
		return selectBestExe(scripts)
	}
	if len(exes) > 0 {
		return selectBestExe(exes)
	}
	return ""
}

// selectBestExe picks the most likely main executable from a list.
func selectBestExe(paths []string) string {
	if len(paths) == 1 {
		return paths[0]
	}
	// Pick the largest — game executables are typically bigger than launchers.
	var best string
	var bestSize int64
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		name := strings.ToLower(filepath.Base(p))
		if strings.Contains(name, "unitycrashhandler") ||
			strings.Contains(name, "unins") ||
			strings.Contains(name, "setup") {
			continue
		}
		if info.Size() > bestSize {
			bestSize = info.Size()
			best = p
		}
	}
	return best
}

// launchCommand builds an exec.Cmd for the given executable.
func launchCommand(exe string) *exec.Cmd {
	ext := strings.ToLower(filepath.Ext(exe))
	switch {
	case ext == ".appimage":
		return exec.Command(exe)
	case ext == ".sh":
		return exec.Command("sh", exe)
	case ext == ".exe":
		if runtime.GOOS == "windows" {
			return exec.Command(exe)
		}
		// Check for wine availability.
		if winePath, err := exec.LookPath("wine"); err == nil {
			return exec.Command(winePath, exe)
		}
		// macOS: try CrossOver as fallback.
		if runtime.GOOS == "darwin" {
			crossoverWine := "/Applications/CrossOver.app/Contents/SharedSupport/CrossOver/bin/wine"
			if _, err := os.Stat(crossoverWine); err == nil {
				return exec.Command(crossoverWine, exe)
			}
		}
		fmt.Fprintf(os.Stderr, "⚠ wine not found — cannot launch .exe files on this platform.\n")
		return nil
	default:
		// Native Linux binary (.x86_64, .x86, no extension).
		return exec.Command(exe)
	}
}

// ---------------------------------------------------------------------------
// steam command
// ---------------------------------------------------------------------------

func cmdSteam(args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie steam <add|remove|list|proton-list|proton-set|fix-artwork>\n")
		os.Exit(1)
	}
	switch args[0] {
	case "add":
		cmdSteamAdd(args[1:])
	case "remove":
		cmdSteamRemove(args[1:])
	case "list":
		cmdSteamList(args[1:])
	case "proton-list":
		cmdSteamProtonList(args[1:])
	case "proton-set":
		cmdSteamProtonSet(args[1:])
	case "fix-artwork":
		cmdSteamFixArtwork(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown steam subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

// pickSteamUser finds the first Steam user ID for single-user operations.
func pickSteamUser(steamRoot string) (uint32, error) {
	users, err := steam.FindSteamUsers(steamRoot)
	if err != nil {
		return 0, err
	}
	if len(users) == 0 {
		return 0, fmt.Errorf("no Steam users found")
	}
	return users[0], nil
}

func cmdSteamAdd(args []string) {
	fs := flag.NewFlagSet("steam-add", flag.ExitOnError)
	allUsers := fs.Bool("all-users", false, "Add to all Steam user accounts")
	noArtwork := fs.Bool("no-artwork", false, "Skip downloading cover artwork")
	protonVer := fs.String("proton", "proton_experimental", "Proton version (Linux only; use 'none' to skip)")
	displayName := fs.String("name", "", "Override display name in Steam")
	sgdbKey := fs.String("steamgriddb-key", "", "SteamGridDB API key for premium artwork")
	tagsFlag := fs.String("tags", "", "Additional comma-separated tags")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie steam add <game-id>\n")
		os.Exit(1)
	}
	id := mustParseInt(fs.Arg(0))

	// 1. Sanity checks for Steam.
	steamRoot, err := steam.FindSteamRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Steam not found: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Steam: %s\n", steamRoot)

	running, err := steam.IsSteamRunning()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error checking Steam: %v\n", err)
		os.Exit(1)
	}
	if running {
		fmt.Fprintf(os.Stderr, "\n⚠  Steam is running.\n")
		fmt.Fprintln(os.Stderr, "Please close Steam before adding games.")
		fmt.Fprintf(os.Stderr, "(Press Enter after closing Steam, or Ctrl+C to cancel): ")
		fmt.Scanln()

		// Re-verify Steam actually closed.
		running, err := steam.IsSteamRunning()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error checking Steam: %v\n", err)
			os.Exit(1)
		}
		if running {
			fmt.Fprintln(os.Stderr, "\n⚠  Steam is still running. Aborting.")
			fmt.Fprintln(os.Stderr, "Please fully close Steam and try again.")
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "✓ Steam is closed.\n")
	}

	// 2. Load game and scraped metadata from DB.
	database := openDB()
	defer database.Close()

	game, err := database.GetGame(id)
	if err != nil || game == nil {
		fmt.Fprintf(os.Stderr, "Game with ID %d not found.\n", id)
		os.Exit(1)
	}

	meta, _ := database.GetScrapedMeta(id)

	// 3. Resolve executable.
	exe := resolveExecutable(*game)
	if exe == "" {
		fmt.Fprintf(os.Stderr, "No executable found for %q. Scan or set one first.\n", game.Title)
		os.Exit(1)
	}

	// 4. Determine display name.
	name := game.Title
	if *displayName != "" {
		name = *displayName
	}


	// 5. Find Steam users.
	var userIDs []uint32
	if *allUsers {
		userIDs, err = steam.FindSteamUsers(steamRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "No Steam users found: %v\n", err)
			os.Exit(1)
		}
	} else {
		users, err := steam.FindSteamUsers(steamRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "No Steam users found: %v\n", err)
			os.Exit(1)
		}
		// Use the first (and typically only) user.
		if len(users) == 0 {
			fmt.Fprintln(os.Stderr, "No Steam users found.")
			os.Exit(1)
		}
		userIDs = users[:1]
	}

	// 6. For each user, add the game.
	for _, uid := range userIDs {
		paths, err := steam.ResolveSteamPaths(steamRoot, uid)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Skipping user %d: %v\n", uid, err)
			continue
		}

		shortcuts, err := steam.ReadShortcuts(paths.ShortcutsVDF)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Error reading shortcuts for user %d: %v\n", uid, err)
			continue
		}

		tags := []string{"F95Zone"}
		if game.Engine != "" && game.Engine != "Unknown" {
			tags = append(tags, game.Engine)
		}
		if *tagsFlag != "" {
			for _, t := range strings.Split(*tagsFlag, ",") {
				t = strings.TrimSpace(t)
				if t != "" && t != "F95Zone" {
					tags = append(tags, t)
				}
			}
		}

		entry := &steam.ShortcutEntry{
			AppName:            name,
			Exe:                exe,
			StartDir:           game.Path,
			LaunchOptions:      "",
			AllowDesktopConfig: true,
			AllowOverlay:       true,
			Tags:               tags,
		}

		if err := steam.AddGame(&shortcuts, entry); err != nil {
			if err == steam.ErrDuplicate {
				fmt.Fprintf(os.Stderr, "  Game %q is already in Steam shortcuts. Skipping.\n", name)
				continue
			}
			fmt.Fprintf(os.Stderr, "  Error adding game for user %d: %v\n", uid, err)
			continue
		}

		// Download artwork (best-effort) with priority chain.
		if !*noArtwork {
			artDone := false

			// Priority 1: SteamGridDB by name (always try if API key is configured).
			sgdbAPIKey := resolveSGDBKey(*sgdbKey)
			if sgdbAPIKey != "" {
				artDone = trySGDBArtworkByName(sgdbAPIKey, steamRoot, uid, entry.AppID, name)
			}

			// Priority 2: F95Zone cover image (fallback).
			if !artDone && meta != nil && meta.CoverURL != "" {
				fmt.Fprintf(os.Stderr, "  Downloading cover art from F95Zone...\n")
				if err := steam.SetAllArtwork(steamRoot, uid, entry.AppID, meta.CoverURL); err != nil {
					if errors.Is(err, steam.ErrUnsupportedFormat) {
						fmt.Fprintf(os.Stderr, "  ⚠ Artwork: unsupported format (SVG) — skipping\n")
					} else {
						fmt.Fprintf(os.Stderr, "  ⚠ Artwork: %v\n", err)
					}
				}
			}
		}

		// Set Proton for Windows EXEs on Linux.
		if steam.IsLinux() && *protonVer != "none" {
			ext := strings.ToLower(filepath.Ext(exe))
			if ext == ".exe" {
				fmt.Fprintf(os.Stderr, "  Setting Proton: %s\n", *protonVer)
				if err := steam.SetProtonVersion(steamRoot, entry.AppID, *protonVer); err != nil {
					fmt.Fprintf(os.Stderr, "  ⚠ Proton: %v\n", err)
				}
			}
		}

		// Write shortcuts back.
		if err := steam.WriteShortcuts(paths.ShortcutsVDF, shortcuts); err != nil {
			fmt.Fprintf(os.Stderr, "  Error writing shortcuts.vdf: %v\n", err)
			continue
		}

		fmt.Fprintf(os.Stderr, "  Backup: %s.backup-*\n", paths.ShortcutsVDF)

		fmt.Fprintf(os.Stderr, "  ✓ Added %q (AppID: %d / 0x%X) for user %d\n", name, entry.AppID, entry.AppID, uid)
	}

	fmt.Fprintln(os.Stderr, "\n⚠  Restart Steam to see the game in your library.")
}

func cmdSteamRemove(args []string) {
	fs := flag.NewFlagSet("steam-remove", flag.ExitOnError)
	userFlag := fs.Uint("user", 0, "Steam user ID (default: first user)")
	nameFlag := fs.String("name", "", "Override display name (must match the name used when adding)")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie steam remove <game-id>\n")
		os.Exit(1)
	}
	id := mustParseInt(fs.Arg(0))

	database := openDB()
	defer database.Close()

	game, err := database.GetGame(id)
	if err != nil || game == nil {
		fmt.Fprintf(os.Stderr, "Game with ID %d not found.\n", id)
		os.Exit(1)
	}

	exe := resolveExecutable(*game)
	if exe == "" {
		fmt.Fprintf(os.Stderr, "No executable found for %q.\n", game.Title)
		os.Exit(1)
	}

	name := game.Title
	if *nameFlag != "" {
		name = *nameFlag
	}
	appID := steam.GenerateAppID(exe, name)

	steamRoot, err := steam.FindSteamRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Steam not found: %v\n", err)
		os.Exit(1)
	}

	var uid uint32
	if *userFlag != 0 {
		uid = uint32(*userFlag)
	} else {
		uid, err = pickSteamUser(steamRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error finding Steam user: %v\n", err)
			os.Exit(1)
		}
	}

	paths, err := steam.ResolveSteamPaths(steamRoot, uid)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving Steam paths: %v\n", err)
		os.Exit(1)
	}

	shortcuts, err := steam.ReadShortcuts(paths.ShortcutsVDF)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading shortcuts: %v\n", err)
		os.Exit(1)
	}

	steam.RemoveGame(&shortcuts, appID)

	if err := steam.WriteShortcuts(paths.ShortcutsVDF, shortcuts); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing shortcuts.vdf: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Removed '%s' from Steam library\n", game.Title)
}

func cmdSteamList(args []string) {
	fs := flag.NewFlagSet("steam-list", flag.ExitOnError)
	userFlag := fs.Uint("user", 0, "Steam user ID (default: first user)")
	fs.Parse(args)

	steamRoot, err := steam.FindSteamRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Steam not found: %v\n", err)
		os.Exit(1)
	}

	var uid uint32
	if *userFlag != 0 {
		uid = uint32(*userFlag)
	} else {
		uid, err = pickSteamUser(steamRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error finding Steam user: %v\n", err)
			os.Exit(1)
		}
	}

	paths, err := steam.ResolveSteamPaths(steamRoot, uid)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving Steam paths: %v\n", err)
		os.Exit(1)
	}

	shortcuts, err := steam.ReadShortcuts(paths.ShortcutsVDF)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading shortcuts: %v\n", err)
		os.Exit(1)
	}

	// Filter for entries tagged with "F95Zone".
	var f95Entries []steam.ShortcutEntry
	for _, s := range shortcuts {
		for _, t := range s.Tags {
			if t == "F95Zone" {
				f95Entries = append(f95Entries, s)
				break
			}
		}
	}

	if len(f95Entries) == 0 {
		fmt.Println("No F95Zone games found in Steam library.")
		return
	}

	fmt.Printf("Steam user: %d\n", uid)
	fmt.Printf("%-30s %-12s %-12s %s\n", "Title", "AppID", "Engine", "Proton")
	fmt.Println(strings.Repeat("-", 70))
	for _, s := range f95Entries {
		title := truncate(s.AppName, 28)
		appIDHex := fmt.Sprintf("0x%08X", s.AppID)

		// Extract engine tag (first tag after "F95Zone").
		engine := ""
		for _, t := range s.Tags {
			if t != "F95Zone" {
				engine = t
				break
			}
		}

		protonVer, _ := steam.GetProtonVersion(steamRoot, s.AppID)
		if protonVer == "" {
			protonVer = "-"
		}

		fmt.Printf("%-30s %-12s %-12s %s\n", title, appIDHex, engine, protonVer)
	}
	fmt.Printf("\n%d F95Zone games in Steam library.\n", len(f95Entries))
}

func cmdSteamProtonList(args []string) {
	steamRoot, err := steam.FindSteamRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Steam not found: %v\n", err)
		os.Exit(1)
	}

	versions, err := steam.ListProtonVersions(steamRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing Proton versions: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Available Proton versions:")
	for _, v := range versions {
		if v == "proton_experimental" {
			fmt.Printf("  * %s (default)\n", v)
		} else {
			fmt.Printf("    %s\n", v)
		}
	}
}

func cmdSteamProtonSet(args []string) {
	fs := flag.NewFlagSet("steam-proton-set", flag.ExitOnError)
	versionFlag := fs.String("version", "", "Proton version (required)")
	userFlag := fs.Uint("user", 0, "Steam user ID (default: first user)")
	nameFlag := fs.String("name", "", "Override display name (must match the name used when adding)")
	fs.Parse(args)

	if *versionFlag == "" {
		fmt.Fprintf(os.Stderr, "Usage: moxie steam proton-set <game-id> --version <proton>\n")
		os.Exit(1)
	}
	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie steam proton-set <game-id> --version <proton>\n")
		os.Exit(1)
	}
	id := mustParseInt(fs.Arg(0))

	database := openDB()
	defer database.Close()

	game, err := database.GetGame(id)
	if err != nil || game == nil {
		fmt.Fprintf(os.Stderr, "Game with ID %d not found.\n", id)
		os.Exit(1)
	}

	exe := resolveExecutable(*game)
	if exe == "" {
		fmt.Fprintf(os.Stderr, "No executable found for %q.\n", game.Title)
		os.Exit(1)
	}

	name := game.Title
	if *nameFlag != "" {
		name = *nameFlag
	}
	appID := steam.GenerateAppID(exe, name)

	steamRoot, err := steam.FindSteamRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Steam not found: %v\n", err)
		os.Exit(1)
	}

	var uid uint32
	if *userFlag != 0 {
		uid = uint32(*userFlag)
	} else {
		uid, err = pickSteamUser(steamRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error finding Steam user: %v\n", err)
			os.Exit(1)
		}
	}

	// Validate the user path exists.
	if _, err := steam.ResolveSteamPaths(steamRoot, uid); err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving Steam paths: %v\n", err)
		os.Exit(1)
	}

	if err := steam.SetProtonVersion(steamRoot, appID, *versionFlag); err != nil {
		fmt.Fprintf(os.Stderr, "Error setting Proton version: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Set Proton for '%s' to %s\n", game.Title, *versionFlag)
}

func cmdSteamFixArtwork(args []string) {
	fs := flag.NewFlagSet("steam-fix-artwork", flag.ExitOnError)
	userFlag := fs.Uint("user", 0, "Steam user ID (default: first user)")
	nameFlag := fs.String("name", "", "Override display name (must match the name used when adding)")
	sgdbKey := fs.String("steamgriddb-key", "", "SteamGridDB API key for premium artwork")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie steam fix-artwork <game-id>\n")
		os.Exit(1)
	}
	id := mustParseInt(fs.Arg(0))

	database := openDB()
	defer database.Close()

	game, err := database.GetGame(id)
	if err != nil || game == nil {
		fmt.Fprintf(os.Stderr, "Game with ID %d not found.\n", id)
		os.Exit(1)
	}

	meta, _ := database.GetScrapedMeta(id)
	name := game.Title
	if *nameFlag != "" {
		name = *nameFlag
	}
	exe := resolveExecutable(*game)
	if exe == "" {
		fmt.Fprintf(os.Stderr, "No executable found for %q.\n", game.Title)
		os.Exit(1)
	}
	appID := steam.GenerateAppID(exe, name)

	steamRoot, err := steam.FindSteamRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Steam not found: %v\n", err)
		os.Exit(1)
	}

	var uid uint32
	if *userFlag != 0 {
		uid = uint32(*userFlag)
	} else {
		uid, err = pickSteamUser(steamRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error finding Steam user: %v\n", err)
			os.Exit(1)
		}
	}

	artDone := false

	apiKey := resolveSGDBKey(*sgdbKey)

	// Priority 1: SteamGridDB by name (if API key is configured).
	if apiKey != "" {
		artDone = trySGDBArtworkByName(apiKey, steamRoot, uid, appID, name)
	}

	// Priority 2: F95Zone cover URL (fallback, if available and valid).
	if !artDone && meta != nil && meta.CoverURL != "" {
		if err := steam.SetAllArtwork(steamRoot, uid, appID, meta.CoverURL); err == nil {
			artDone = true
		} else if !errors.Is(err, steam.ErrUnsupportedFormat) {
			fmt.Fprintf(os.Stderr, "Error setting artwork: %v\n", err)
			os.Exit(1)
		}
	}

	if artDone {
		fmt.Printf("Artwork updated for '%s'\n", game.Title)
	} else {
		fmt.Fprintf(os.Stderr, "No artwork found for this game.\n")
		fmt.Fprintf(os.Stderr, "Try setting a SteamGridDB API key: moxie config set steamgriddb-key <key>\n")
	}
}

// ---------------------------------------------------------------------------
// config command
// ---------------------------------------------------------------------------

func cmdConfig(args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie config <set|get|show> [key] [value]\n")
		fmt.Fprintf(os.Stderr, "       moxie config set steamgriddb-key <key>\n")
		fmt.Fprintf(os.Stderr, "       moxie config get steamgriddb-key\n")
		fmt.Fprintf(os.Stderr, "       moxie config show\n")
		os.Exit(1)
	}
	switch args[0] {
	case "set":
		cmdConfigSet(args[1:])
	case "get":
		cmdConfigGet(args[1:])
	case "show":
		cmdConfigShow()
	default:
		fmt.Fprintf(os.Stderr, "Unknown config subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func cmdConfigSet(args []string) {
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: moxie config set <key> <value>\n")
		os.Exit(1)
	}
	key := args[0]
	value := strings.Join(args[1:], " ")

	cfg, err := readConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading config: %v\n", err)
		os.Exit(1)
	}
	cfg[key] = value
	if err := writeConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Set %s = %s\n", key, value)
}

func cmdConfigGet(args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie config get <key>\n")
		os.Exit(1)
	}
	cfg, err := readConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading config: %v\n", err)
		os.Exit(1)
	}
	value, ok := cfg[args[0]]
	if !ok || value == "" {
		fmt.Fprintf(os.Stderr, "(not set)\n")
		return
	}
	fmt.Println(value)
}

func cmdConfigShow() {
	cfg, err := readConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading config: %v\n", err)
		os.Exit(1)
	}
	if len(cfg) == 0 {
		fmt.Println("No configuration values set.")
		return
	}
	for k, v := range cfg {
		masked := v
		if strings.Contains(strings.ToLower(k), "key") && len(v) > 4 {
			masked = v[:4] + strings.Repeat("*", len(v)-4)
		}
		fmt.Printf("  %-25s %s\n", k, masked)
	}
}

// ---------------------------------------------------------------------------
// refresh-versions command
// ---------------------------------------------------------------------------

func cmdRefreshVersions(args []string) {
	database := openDB()
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
			continue // no version in directory name
		}
		if g.Version == dirVer {
			continue // already matches
		}
		oldVer := g.Version
		g.Version = dirVer
		if err := database.UpdateGame(&g); err != nil {
			fmt.Fprintf(os.Stderr, "  ⚠ %q: failed to update version: %v\n", g.Title, err)
			continue
		}
		updated++
		fmt.Fprintf(os.Stderr, "  %-50s %s → %s\n",
			truncate(g.Title, 48), truncateVer(oldVer), dirVer)
	}

	if updated == 0 {
		fmt.Println("No version changes. All games are up to date.")
	} else {
		fmt.Fprintf(os.Stderr, "\nUpdated %d game(s).\n", updated)
	}
}

func truncateVer(v string) string {
	if v == "" {
		return "(none)"
	}
	return v
}

// ---------------------------------------------------------------------------
// cleanup command — detect and fix wrong F95Zone thread associations
// ---------------------------------------------------------------------------

func cmdCleanup(args []string) {
	fs := flag.NewFlagSet("cleanup", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "Preview issues without making changes")
	assumeYes := fs.Bool("assume-yes", false, "Auto-disassociate flagged games without prompting")
	yes := fs.Bool("y", false, "Auto-disassociate flagged games (shorthand for --assume-yes)")
	fs.Parse(args)

	database := openDB()
	defer database.Close()

	games, err := database.ListGames("", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading games: %v\n", err)
		os.Exit(1)
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
		if s := checkEngineMismatch(g); s != "" {
			issues = append(issues, s)
		}

		// Signal 2: Executable name mismatch.
		if s := checkExeMismatch(g); s != "" {
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
			disassociateGame(database, &g)
			disassociated++
			continue
		}

		// Interactive mode: prompt for each flagged game.
		fmt.Fprintf(os.Stderr, "  Disassociate? [y/N]: ")
		var answer string
		fmt.Scanln(&answer)
		if strings.ToLower(answer) == "y" {
			disassociateGame(database, &g)
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

// engineTagVariants maps canonical engine names to substrings that commonly
// appear in F95Zone thread tags.  A match is found when any variant appears
// within any tag (case-insensitive).
var engineTagVariants = map[string][]string{
	"RenPy":        {"ren'py", "renpy"},
	"Unity":        {"unity"},
	"RPGM":         {"rpg maker", "rpgm", "rmmv", "rmmz"},
	"HTML":         {"html", "html5"},
	"Flash":        {"flash"},
	"Java":         {"java"},
	"UnrealEngine": {"unreal", "unreal engine"},
	"WebGL":        {"webgl"},
	"WolfRPG":      {"wolf rpg", "wolfrpg"},
	"ADRIFT":       {"adrift"},
	"QSP":          {"qsp"},
	"RAGS":         {"rags"},
	"Tads":         {"tads", "tads"},
}

// engineMatchesTags checks whether the scanner-detected engine is consistent
// with the F95Zone thread tags.  Returns true when:
//   - There are no tags to compare (inconclusive)
//   - The detected engine is "Others" or empty (inconclusive)
//   - No tag variant mapping exists for the engine (inconclusive)
//   - At least one tag contains a variant of the detected engine (match)
//
// Returns false only when there is a clear engine mismatch (specific engine
// was detected, tags exist, and no variant matches any tag).
func engineMatchesTags(detected engine.Result, tags []string) bool {
	if len(tags) == 0 {
		return true // no metadata to compare against
	}
	if detected.Engine == "Others" || detected.Engine == "" {
		return true // detection inconclusive — don't flag
	}

	variants := engineTagVariants[string(detected.Engine)]
	if len(variants) == 0 {
		return true // no mapping known for this engine — don't flag
	}

	for _, tag := range tags {
		tagLower := strings.ToLower(tag)
		for _, variant := range variants {
			if strings.Contains(tagLower, variant) {
				return true // engine matches
			}
		}
	}

	return false
}

// checkEngineMismatch returns a description of the mismatch, or "" if no
// issue is found.  It compares the scanner-detected engine (via
// engine.Detect) against the F95Zone thread tags stored on the game.
func checkEngineMismatch(g db.Game) string {
	if len(g.Tags) == 0 {
		return "" // no F95Zone metadata to compare against
	}

	detected := engine.Detect(g.Path)
	if detected.Engine == "Others" || detected.Engine == "" {
		return "" // can't determine local engine
	}

	f95Engine := findF95Engine(g)
	if f95Engine == "" {
		return fmt.Sprintf("engine unverified — F95Zone tags don't mention an engine (scanner found: %s)", detected.Engine)
	}

	if engineMatchesTags(detected, g.Tags) {
		return ""
	}

	return fmt.Sprintf("engine mismatch (scanner: %s, F95Zone: %s)",
		detected.Engine, f95Engine)
}

// findF95Engine looks through F95Zone thread tags and the game title
// for an engine indicator.  Returns the engine name implied by the data,
// or "" if no engine is found.
func findF95Engine(g db.Game) string {
	// 1. Check tags (most reliable — explicitly tagged by thread author).
	for _, tag := range g.Tags {
		tagLower := strings.ToLower(tag)
		for engine, variants := range engineTagVariants {
			for _, variant := range variants {
				if strings.Contains(tagLower, variant) {
					return engine
				}
			}
		}
	}
	// 2. Fall back to title prefix (RPGM, Unity, RenPy, etc. in thread title).
	titleLower := strings.ToLower(g.Title)
	for engine, variants := range engineTagVariants {
		for _, variant := range variants {
			if strings.HasPrefix(titleLower, variant+" ") || strings.HasPrefix(titleLower, variant+"\t") {
				return engine
			}
		}
	}
	return ""
}

// checkExeMismatch returns a description when no executable in the game
// directory shares a word (case-insensitive) with the game title, or ""
// if at least one executable matches.
func checkExeMismatch(g db.Game) string {
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
	for _, exe := range exeNames {
		exeLower := strings.ToLower(exe)
		// Strip extension for a cleaner comparison.
		exeBase := strings.TrimSuffix(exeLower, filepath.Ext(exeLower))
		for word := range titleWords {
			if strings.Contains(exeBase, word) {
				return "" // at least one executable shares a title word
			}
		}
	}

	return fmt.Sprintf("unmatched executable (found: %s)", strings.Join(exeNames, ", "))
}

// disassociateGame clears the F95Zone URL and thread ID from a game and
// saves the change to the database.
func disassociateGame(database *db.Database, g *db.Game) {
	g.F95URL = ""
	g.F95ThreadID = 0
	if err := database.UpdateGame(g); err != nil {
		fmt.Fprintf(os.Stderr, "  ⚠ Failed to disassociate #%d: %v\n", g.ID, err)
	} else {
		fmt.Fprintf(os.Stderr, "  ✓ Disassociated #%d\n", g.ID)
	}
}

// formatTagsBrief returns a comma-separated tag string, limited to max tags
// to keep output readable.
func formatTagsBrief(tags []string, max int) string {
	if len(tags) == 0 {
		return ""
	}
	if len(tags) <= max {
		return strings.Join(tags, ", ")
	}
	return strings.Join(tags[:max], ", ") + fmt.Sprintf(" (+%d more)", len(tags)-max)
}

// ---------------------------------------------------------------------------
// Steam helpers
// ---------------------------------------------------------------------------

// resolveSGDBKey resolves the SteamGridDB API key from the flag, the
// STEAMGRIDDB_KEY environment variable, user config, or the legacy
// steamgriddb-key file (in that priority order). Returns "" if none is set.
func resolveSGDBKey(flagKey string) string {
	if flagKey != "" {
		return flagKey
	}
	if key := os.Getenv("STEAMGRIDDB_KEY"); key != "" {
		return key
	}
	// Check JSON config (new format).
	if cfg, err := readConfig(); err == nil {
		if key, ok := cfg["steamgriddb-key"]; ok && key != "" {
			return key
		}
	}
	// Fall back to legacy flat file.
	if data, err := os.ReadFile(filepath.Join(configDir(), "steamgriddb-key")); err == nil {
		return strings.TrimSpace(string(data))
	}
	return ""
}

// extractSteamAppID extracts the numeric Steam App ID from a
// store.steampowered.com URL.
// Example: "https://store.steampowered.com/app/12345/GameName/" → (12345, true)
func extractSteamAppID(storeURL string) (int, bool) {
	re := regexp.MustCompile(`/app/(\d+)/`)
	matches := re.FindStringSubmatch(storeURL)
	if len(matches) < 2 {
		return 0, false
	}
	id, err := strconv.Atoi(matches[1])
	return id, err == nil
}

// downloadSGDBArtwork fetches grid artwork from SteamGridDB for a real Steam
// App ID.  Downloads the best vertical grid (600×900), horizontal grid
// (460×215), and hero image (1920×620).  Returns true if at least the vertical
// grid succeeded.
func downloadSGDBArtwork(sgdb *steam.SGDBClient, steamRoot string, uid, appID uint32, realSteamAppID int) bool {
	// Vertical grid (600×900).
	grids, err := sgdb.GetGridsBySteamAppID(realSteamAppID, "600x900")
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ⚠ SGDB grids: %v\n", err)
		return false
	}
	if url, ok := steam.BestGridImage(grids); ok {
		dest := steam.GridFilePath(steamRoot, uid, appID, steam.ArtVertical)
		if err := sgdb.DownloadImage(url, dest); err != nil {
			fmt.Fprintf(os.Stderr, "  ⚠ SGDB vertical: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "  ✓ Vertical grid\n")
		}
	}

	// Horizontal grid (460×215).
	gridsH, _ := sgdb.GetGridsBySteamAppID(realSteamAppID, "460x215")
	if url, ok := steam.BestGridImage(gridsH); ok {
		dest := steam.GridFilePath(steamRoot, uid, appID, steam.ArtHorizontal)
		_ = sgdb.DownloadImage(url, dest)
	}

	// Hero (1920×620).
	heroes, _ := sgdb.GetHeroesBySteamAppID(realSteamAppID)
	if url, ok := steam.BestGridImage(heroes); ok {
		dest := steam.GridFilePath(steamRoot, uid, appID, steam.ArtHero)
		_ = sgdb.DownloadImage(url, dest)
	}

	return true
}

// sanitizeTitleForSGDB strips version numbers and engine prefixes from a title
// to produce a cleaner SteamGridDB search query.
func sanitizeTitleForSGDB(title string) string {
	s := regexp.MustCompile(`\s*\[?v?\d+\.\d+(?:\.\d+)?\]?\s*`).ReplaceAllString(title, " ")
	s = strings.TrimSpace(s)
	return s
}

// trySGDBArtworkByName searches SteamGridDB by game name and downloads the
// best grid artwork. Returns true if at least the vertical grid succeeded.
func trySGDBArtworkByName(apiKey, steamRoot string, uid, appID uint32, gameName string) bool {
	fmt.Fprintf(os.Stderr, "  Searching SteamGridDB for %q...\n", sanitizeTitleForSGDB(gameName))
	sgdb := steam.NewSGDBClient(apiKey)
	results, err := sgdb.SearchGame(sanitizeTitleForSGDB(gameName))
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ⚠ SGDB search: %v\n", err)
		return false
	}
	if len(results) == 0 {
		fmt.Fprintf(os.Stderr, "  No SteamGridDB results.\n")
		return false
	}

	gameID := results[0].ID
	fmt.Fprintf(os.Stderr, "  Best match: %s (SGDB #%d)\n", results[0].Name, gameID)

	// Vertical grid (600×900).
	grids, err := sgdb.GetGridsBySGDBGameID(gameID, "600x900")
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ⚠ SGDB grids: %v\n", err)
		return false
	}
	url, ok := steam.BestGridImage(grids)
	if !ok {
		fmt.Fprintf(os.Stderr, "  No suitable grids found.\n")
		return false
	}
	dest := steam.GridFilePath(steamRoot, uid, appID, steam.ArtVertical)
	if err := sgdb.DownloadImage(url, dest); err != nil {
		fmt.Fprintf(os.Stderr, "  ⚠ SGDB download: %v\n", err)
		return false
	}
	fmt.Fprintf(os.Stderr, "  ✓ Vertical grid\n")

	// Best-effort horizontal + hero.
	if gridsH, _ := sgdb.GetGridsBySGDBGameID(gameID, "460x215"); len(gridsH) > 0 {
		if urlH, ok := steam.BestGridImage(gridsH); ok {
			sgdb.DownloadImage(urlH, steam.GridFilePath(steamRoot, uid, appID, steam.ArtHorizontal))
		}
	}
	if heroes, _ := sgdb.GetHeroesBySGDBGameID(gameID); len(heroes) > 0 {
		if urlH, ok := steam.BestGridImage(heroes); ok {
			sgdb.DownloadImage(urlH, steam.GridFilePath(steamRoot, uid, appID, steam.ArtHero))
		}
	}

	return true
}

func cmdRename(args []string) {
	fs := flag.NewFlagSet("rename", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "Preview renames without making changes")
	fs.Parse(args)

	database := openDB()
	defer database.Close()

	games, err := database.ListGames("", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading games: %v\n", err)
		os.Exit(1)
	}

	type renamePlan struct {
		game    db.Game
		oldPath string
		newPath string
		newName string
	}

	var plans []renamePlan
	for _, g := range games {
		newName := cleanGameTitle(g)
		if newName == "" || newName == filepath.Base(g.Path) {
			continue // no change needed
		}

		parent := filepath.Dir(g.Path)
		newPath := filepath.Join(parent, newName)

		// Skip if new path already exists (different game).
		if newPath != g.Path {
			if _, err := os.Stat(newPath); err == nil {
				fmt.Fprintf(os.Stderr, "  ⚠ Skipping %q — target already exists: %q\n",
					filepath.Base(g.Path), newName)
				continue
			}
		}

		plans = append(plans, renamePlan{
			game:    g,
			oldPath: g.Path,
			newPath: newPath,
			newName: newName,
		})
	}

	if len(plans) == 0 {
		fmt.Println("All game directories already have clean names.")
		return
	}

	// Show preview.
	fmt.Fprintf(os.Stderr, "=== %d directories to rename ===\n\n", len(plans))
	for _, p := range plans {
		fmt.Printf("  %s\n  → %s\n\n",
			filepath.Base(p.oldPath),
			p.newName)
	}

	if *dryRun {
		fmt.Fprintln(os.Stderr, "Dry run — no changes made. Remove --dry-run to apply.")
		return
	}

	fmt.Fprintf(os.Stderr, "Rename %d directories? (y/N): ", len(plans))
	var answer string
	fmt.Scanln(&answer)
	if strings.ToLower(answer) != "y" {
		fmt.Println("Cancelled.")
		return
	}

	renamed := 0
	for _, p := range plans {
		if err := os.Rename(p.oldPath, p.newPath); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ %s: %v\n", filepath.Base(p.oldPath), err)
			continue
		}
		// Update DB path.
		p.game.Path = p.newPath
		if err := database.UpdateGame(&p.game); err != nil {
			fmt.Fprintf(os.Stderr, "  ⚠ Renamed dir but failed to update DB for %s: %v\n",
				p.newName, err)
		}
		fmt.Fprintf(os.Stderr, "  ✓ %s → %s\n", filepath.Base(p.oldPath), p.newName)
		renamed++
	}
	fmt.Fprintf(os.Stderr, "\nRenamed %d directories.\n", renamed)
}

// cleanGameTitle produces a clean directory name for a game.
// Prefers the scraped title (stripped of engine/status prefixes), falls back
// to sanitizing the directory name.
func cleanGameTitle(g db.Game) string {
	title := g.Title

	// If we have a scraped F95Zone title, strip engine/status prefix tags.
	if g.F95URL != "" {
		title = stripThreadPrefix(title)
	}

	// Filesystem-safe: replace forbidden chars, limit length.
	title = filesystemSafe(title)
	return title
}

// stripThreadPrefix removes engine and status prefix tags that XenForo
// threads often have prepended to titles.
//   "Unity Completed The Lewd Knight [v0.993]" → "The Lewd Knight"
func stripThreadPrefix(title string) string {
	// Known engine/status/category prefix words.
	prefixWords := map[string]bool{
		"unity": true, "ren'py": true, "renpy": true, "rpgm": true,
		"vn": true, "html": true, "flash": true, "java": true,
		"godot": true, "electron": true, "unreal": true, "others": true,
		"completed": true, "abandoned": true, "onhold": true,
		"collection": true, "video": true, "mod": true, "cheat": true,
		"tool": true, "daz": true, "update": true, "req": true,
		"request": true, "seeking": true, "announcement": true,
	}

	words := strings.Fields(title)
	for len(words) > 0 && prefixWords[strings.ToLower(strings.TrimRight(words[0], "•"))] {
		words = words[1:]
	}

	result := strings.TrimSpace(strings.Join(words, " "))
	if result == "" {
		return title
	}
	return result
}

// filesystemSafe replaces characters that are illegal in directory names.
func filesystemSafe(name string) string {
	// Strip or replace problematic characters.
	replacer := strings.NewReplacer(
		"/", "-", "\\", "-", ":", "-", "*", "", "?", "",
		"\"", "'", "<", "", ">", "", "|", "-",
	)
	name = replacer.Replace(name)

	// Collapse multiple spaces/hyphens.
	name = multiSpaceRE.ReplaceAllString(name, " ")
	name = multiDashRE.ReplaceAllString(name, "-")

	// Remove version tags we don't need in the directory name.
	name = scraper.SanitizeTitle(name)

	// Don't start or end with space/dot/hyphen.
	name = strings.TrimSpace(name)
	name = strings.Trim(name, ".- ")

	// Limit length.
	if len(name) > 80 {
		name = name[:80]
	}

	// On Windows, avoid reserved filenames.
	if runtime.GOOS == "windows" {
		reserved := map[string]bool{
			"con": true, "prn": true, "aux": true, "nul": true,
			"com1": true, "com2": true, "com3": true, "com4": true,
			"com5": true, "com6": true, "com7": true, "com8": true, "com9": true,
			"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true,
			"lpt5": true, "lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
		}
		base := strings.ToLower(strings.SplitN(name, ".", 2)[0])
		if reserved[base] {
			name = "_" + name
		}
	}

	if name == "" {
		return "game"
	}
	return name
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// formatDuration returns a human-readable duration string.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		if s == 0 {
			return fmt.Sprintf("%dm", m)
		}
		return fmt.Sprintf("%dm%ds", m, s)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%dm", h, m)
}

// isBlocked returns true if the error indicates we've been blocked/rate-limited.
func isBlocked(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "BlockedError") ||
		strings.Contains(msg, "blocked") ||
		strings.Contains(msg, "Cloudflare challenge")
}

func truncate(s string, maxLen int) string {
	// Strip newlines and truncate.
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func wrapText(s string, width int) string {
	var result strings.Builder
	words := strings.Fields(s)
	lineLen := 0
	for _, w := range words {
		if lineLen+len(w)+1 > width && lineLen > 0 {
			result.WriteByte('\n')
			lineLen = 0
		}
		if lineLen > 0 {
			result.WriteByte(' ')
			lineLen++
		}
		result.WriteString(w)
		lineLen += len(w)
	}
	return result.String()
}
