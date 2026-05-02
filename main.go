package main

import (
	"fmt"
	"os"

	"github.com/mili/moxie/internal/tui"
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

func cmdTUI() {
	if err := tui.Run(dbPath()); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}
}
