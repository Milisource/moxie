package commands

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/launcher"
)

// Play launches a game by ID or fuzzy name search.
// Usage: moxie play <id>  or  moxie play <name>
func Play(args []string) {
	fs := flag.NewFlagSet("play", flag.ExitOnError)
	winePrefixFlag := fs.String("wine-prefix", "", "Path to a custom Wine prefix (WINEPREFIX)")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie play <id>  or  moxie play <name>\n")
		fmt.Fprintf(os.Stderr, "  <id>    Game ID number (from `moxie list`)\n")
		fmt.Fprintf(os.Stderr, "  <name>  Fuzzy title search (e.g. \"Cyan Brain\" or \"Cyan\")\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fmt.Fprintf(os.Stderr, "  --wine-prefix <path>  Override Wine prefix for this launch\n")
		os.Exit(1)
	}

	database := OpenDB()
	defer database.Close()

	// Join all positional args so unquoted multi-word names still work.
	raw := strings.Join(fs.Args(), " ")
	game := ResolveGame(database, raw)
	if game == nil {
		fmt.Fprintf(os.Stderr, "Cancelled.\n")
		os.Exit(1)
	}

	// Use flag value if provided, otherwise fall back to DB-stored prefix.
	winePrefix := *winePrefixFlag
	if winePrefix == "" {
		winePrefix = game.WinePrefix
	}

	if err := RunPlay(database, game, winePrefix); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// RunPlay launches a game using the resolved game record.
// It finds the executable, launches it, and records play history.
// This is the testable logic function — it returns errors instead of os.Exit.
func RunPlay(database *db.Database, game *db.Game, winePrefix string) error {
	exe := launcher.ResolveExecutable(game.Path, game.ExePath)
	if exe == "" {
		// Check if it's a virtual game (added via browser, not downloaded yet).
		if strings.HasPrefix(game.Path, "/virtual/") {
			return fmt.Errorf("%q was added from F95Zone but not yet downloaded. Use 'moxie download %d' to install it", game.Title, game.ID)
		}
		return fmt.Errorf("no executable found for %q (path: %s)", game.Title, game.Path)
	}

	if err := launcher.Launch(exe, game.Path, winePrefix); err != nil {
		return fmt.Errorf("cannot launch %q: %w", exe, err)
	}
	fmt.Fprintf(os.Stderr, "Launching: %s\n", exe)

	// Record play history. Non-fatal if it fails.
	if err := database.RecordPlay(game.ID, runtime.GOOS); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to record play history: %v\n", err)
	}
	return nil
}
