package commands

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// SetWinePrefix sets the Wine prefix for a game.
// Usage: moxie set-wine-prefix <id|name> <prefix>
func SetWinePrefix(args []string) {
	fs := flag.NewFlagSet("set-wine-prefix", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() < 2 {
		fmt.Fprintf(os.Stderr, "Usage: moxie set-wine-prefix <id|name> <prefix>\n")
		fmt.Fprintf(os.Stderr, "  <id|name>  Game ID number or fuzzy title\n")
		fmt.Fprintf(os.Stderr, "  <prefix>   Absolute path to Wine prefix directory\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "  Use an empty string \"\" to clear the prefix.\n")
		os.Exit(1)
	}

	database := OpenDB()
	defer database.Close()

	raw := strings.Join(fs.Args()[:1], " ")
	game := ResolveGame(database, raw)
	if game == nil {
		fmt.Fprintf(os.Stderr, "Cancelled.\n")
		os.Exit(1)
	}

	prefix := strings.Join(fs.Args()[1:], " ")

	if err := database.UpdateGameWinePrefix(game.ID, prefix); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if prefix == "" {
		fmt.Fprintf(os.Stderr, "Cleared Wine prefix for %q.\n", game.Title)
	} else {
		fmt.Fprintf(os.Stderr, "Set Wine prefix for %q to: %s\n", game.Title, prefix)
	}
}
