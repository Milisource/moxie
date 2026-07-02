package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/mili/moxie/internal/browser"
	"github.com/mili/moxie/internal/commands"
	"github.com/mili/moxie/internal/config"
	"github.com/mili/moxie/internal/log"
	"github.com/mili/moxie/internal/scraper"
	"github.com/mili/moxie/internal/tui"
)

var version = "0.4.0-alpha"

func main() {
	log.Init(config.LogDir())

	// Parse global flags before command dispatch.
	// --verbose / -v enables debug logging. The flag is consumed here and
	// stripped from args so subcommand parsers never see it.
	args := os.Args[1:]
	if len(args) > 0 {
		verbose := false
		filtered := make([]string, 0, len(args))
		for _, a := range args {
			switch a {
			case "--verbose", "-v", "--debug", "-d":
				verbose = true
			default:
				filtered = append(filtered, a)
			}
		}
		args = filtered
		if verbose {
			log.SetLevel(slog.LevelDebug)
			log.Debug("verbose logging enabled")
		}
	}

	log.Info("moxie started", "version", version, "args", args)

	if len(args) < 1 {
		// First-run welcome if no database exists yet
		dbPath := config.DbPath()
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "👋 Welcome to moxie! It looks like this is your first run.")
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, "   Quick start:")
			fmt.Fprintln(os.Stderr, "     moxie scan ~/Downloads       Scan a directory for games")
			fmt.Fprintln(os.Stderr, "     moxie scrape --auto          Auto-associate F95Zone threads")
			fmt.Fprintln(os.Stderr, "     moxie tui                    Interactive library browser")
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, "   Steam integration (optional):")
			fmt.Fprintln(os.Stderr, "     moxie config set steamgriddb-key <key>   Set API key (get one free at")
			fmt.Fprintln(os.Stderr, "                                                https://www.steamgriddb.com/profile/preferences)")
			fmt.Fprintln(os.Stderr, "     moxie steam fix-artwork <id>             Download higher-quality artwork")
			fmt.Fprintln(os.Stderr, "     moxie steam add <id>                     Add a game to Steam library")
			fmt.Fprintln(os.Stderr, "     moxie steam fix-artwork <id>             Re-download Steam artwork")
			fmt.Fprintln(os.Stderr, "     moxie steam list                         List games added to Steam")
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, "   To check for updates:")
			fmt.Fprintln(os.Stderr, "     moxie check-updates   Check all games for new versions on F95Zone")
			fmt.Fprintln(os.Stderr, "     moxie sync            Full sync: associate + check updates")
			fmt.Fprintln(os.Stderr, "     moxie update           Check for and install moxie updates")
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, "   For F95Zone scraping, log into F95Zone in Firefox first —")
			fmt.Fprintln(os.Stderr, "   moxie reads your browser cookies automatically.")
			fmt.Fprintln(os.Stderr)
		}
		printUsage()
		os.Exit(1)
	}

	if len(args) >= 1 && (args[0] == "--version" || args[0] == "-version" || args[0] == "version") {
		fmt.Println("moxie", version)
		os.Exit(0)
	}

	if len(args) >= 1 {
		switch args[0] {
		case "scan":
			commands.Scan(args[1:])
		case "tui":
			cmdTUI()
		case "add":
			commands.Add(args[1:])
		case "info":
			commands.Info(args[1:])
		case "scrape":
			commands.Scrape(args[1:])
		case "scrape-batch":
			commands.ScrapeBatch(args[1:])
		case "set-path":
			commands.SetPath(args[1:])
		case "set-exe":
			commands.SetExe(args[1:])
		case "list":
			commands.List(args[1:])
		case "remove":
			commands.Remove(args[1:])
		case "rename":
			commands.Rename(args[1:])
		case "check-updates", "updates":
			commands.CheckUpdates(args[1:])
		case "sync":
			commands.Sync(args[1:])
		case "play":
			commands.Play(args[1:])
		case "steam":
			commands.Steam(args[1:])
		case "config":
			commands.Config(args[1:])
		case "history":
			commands.History(args[1:])
		case "update":
			commands.Update(version)
		case "cleanup":
			commands.Cleanup(args[1:])
		case "refresh-versions":
			commands.RefreshVersions(args[1:])
		case "download":
			commands.Download(args[1:])
		case "install":
			commands.Install(args[1:])
		case "downloads":
			commands.ListDownloads(args[1:])
		case "check-links":
			commands.CheckDeadLinks(args[1:])
		case "restore":
			commands.Restore(args[1:])
		case "purge":
			commands.Purge(args[1:])
		case "set-status":
			commands.SetStatus(args[1:])
		case "collections", "collection":
			commands.Collections(args[1:])
		case "export":
			commands.Export(args[1:])
		case "import":
			commands.Import(args[1:])
		default:
			fmt.Fprintf(os.Stderr, "Unknown command: %s\n", args[0])
			printUsage()
			os.Exit(1)
		}
		return
	}

	// If we get here, no args — already handled above, but keep as safeguard.
	printUsage()
	os.Exit(1)
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `moxie - F95Zone Game Library Manager

USAGE
  moxie <command> [args] [flags]

CORE
  scan <dir> [flags]          Scan directory for new games (incremental by default)
  list [flags]                List all games in library
  tui                         Launch interactive terminal UI
  info <id|name>              Show detailed game info
  play <id|name>              Launch a game
  history [count]             Show recently played games
  add <path> [flags]          Manually add a game to library
  remove <id|name>            Remove a game from library (soft delete)
  restore <id|name>           Restore a soft-deleted game
  purge                       Permanently delete all soft-deleted games
  rename [flags]              Rename game directories to clean titles
  set-status [flags] <status> Update game status (active/completed/abandoned/on_hold/unknown)
  collections [sub] [args]    Manage game collections
  export [--output file]      Export library as JSON
  import <file>               Import games from JSON export

F95ZONE
  sync [id] [flags]           Full sync: auto-associate + check version updates
  scrape <id> [flags]         Scrape F95Zone metadata for a game
  check-updates [flags]       Check all games for newer versions on F95Zone
  refresh-versions [flags]    Re-extract version strings from directory names

DOWNLOADS
  download <id> [flags]       Download a game from F95Zone links
  install <id> <archive>      Install a downloaded archive into game directory
  downloads [flags]           List download history
  check-links [flags]         Validate download links (detect dead URLs)

STEAM
  steam add <id> [flags]      Add a game to Steam library
  steam remove <id>           Remove from Steam
  steam list                  List games added to Steam
  steam proton-list           List available Proton versions
  steam proton-set <id>       Set Proton version for a game
  steam fix-artwork <id>      Re-download Steam artwork

ADMIN
  cleanup [flags]             Detect and fix wrong F95Zone thread associations
  config <set|get|show>       Manage configuration settings
  update                      Check for and install moxie updates

GLOBAL FLAGS
  --help, -h                  Show help for any command
  --verbose, -v               Enable debug logging

COMMON FLAGS

  scan:
    --force                   Full rescan (skip nothing, re-detect all games)
    --no-save                 Print detected games without saving to library
    --engine <type>           Filter by engine (Unity, RenPy, RPGM, etc.)
    --json                    Output as JSON

  list:
    --engine <type>           Filter by engine
    --status <s>              Filter by status (active, completed, etc.)
    --deleted                 Show soft-deleted games instead of active ones
    --warnings                Show engine/exe mismatch warnings
    --json                    Output as JSON

  sync, check-updates:
    --cookie <str>            Cookie header from browser DevTools
    --cookie-file <path>      Read cookie from file
    --unsafe                  ⚠ Skip rate limiting (may get IP-banned)
    --force                   Re-check even if checked within 24h

  scrape:
    --cookie <str>            Cookie header from browser DevTools
    --cookie-file <path>      Read cookie from file
    --url <url>               F95Zone thread URL (if game has no URL)

  add:
    --title <name>            Game title (defaults to directory name)
    --engine <type>           Game engine (auto-detected if omitted)
    --version <ver>           Game version
    --tags <tags>             Comma-separated tags

  set-status:
    --engine <type>           Batch-update games with this engine
    --all                     Update status for ALL games in library
    -y                        Skip confirmation prompt

  cleanup:
    --dry-run                 Preview issues without making changes
    --assume-yes, -y          Auto-disassociate flagged games

TIP
  Set a SteamGridDB API key for higher-quality artwork!
    Get one free: https://www.steamgriddb.com/profile/preferences
    moxie config set steamgriddb-key <key>
    moxie steam fix-artwork <id>

`)
}

func cmdTUI() {
	// Try loading cookies for F95Zone access so the TUI can scrape metadata
	// on URL changes. A nil client is fine — the TUI will gracefully fall back.
	var sc *scraper.Client
	cookieStr, err := browser.GetF95Cookies()
	if err == nil && cookieStr != "" {
		sc = scraper.NewClient(cookieStr)
	}

	if err := tui.Run(config.DbPath(), sc, cookieStr); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}
}
