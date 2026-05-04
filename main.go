package main

import (
	"fmt"
	"os"

	"github.com/mili/moxie/internal/browser"
	"github.com/mili/moxie/internal/commands"
	"github.com/mili/moxie/internal/config"
	"github.com/mili/moxie/internal/scraper"
	"github.com/mili/moxie/internal/tui"
)

var version = "0.3.5-alpha"

func main() {
	if len(os.Args) < 2 {
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

	if os.Args[1] == "--version" || os.Args[1] == "-version" || os.Args[1] == "version" {
		fmt.Println("moxie", version)
		os.Exit(0)
	}

	switch os.Args[1] {
	case "scan":
		commands.Scan(os.Args[2:])
	case "tui":
		cmdTUI()
	case "add":
		commands.Add(os.Args[2:])
	case "info":
		commands.Info(os.Args[2:])
	case "scrape":
		commands.Scrape(os.Args[2:])
	case "scrape-batch":
		commands.ScrapeBatch(os.Args[2:])
	case "set-path":
		commands.SetPath(os.Args[2:])
	case "set-exe":
		commands.SetExe(os.Args[2:])
	case "list":
		commands.List(os.Args[2:])
	case "remove":
		commands.Remove(os.Args[2:])
	case "rename":
		commands.Rename(os.Args[2:])
	case "check-updates", "updates":
		commands.CheckUpdates(os.Args[2:])
	case "sync":
		commands.Sync(os.Args[2:])
	case "play":
		commands.Play(os.Args[2:])
	case "steam":
		commands.Steam(os.Args[2:])
	case "config":
		commands.Config(os.Args[2:])
	case "update":
		commands.Update(version)
	case "cleanup":
		commands.Cleanup(os.Args[2:])
	case "refresh-versions":
		commands.RefreshVersions(os.Args[2:])
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
  moxie update                           Check for and install moxie updates
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

Tip: Set a SteamGridDB API key for higher-quality artwork!
     Get one free at https://www.steamgriddb.com/profile/preferences
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

	if err := tui.Run(config.DbPath(), sc); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}
}
