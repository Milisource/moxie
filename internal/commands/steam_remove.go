package commands

import (
	"flag"
	"fmt"
	"os"

	"github.com/mili/moxie/internal/steam"
	"github.com/mili/moxie/internal/util"
)

// SteamRemove removes a game from the Steam library.
func SteamRemove(args []string) {
	fs := flag.NewFlagSet("steam-remove", flag.ExitOnError)
	userFlag := fs.Uint("user", 0, "Steam user ID (default: first user)")
	nameFlag := fs.String("name", "", "Override display name (must match the name used when adding)")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie steam remove <game-id>\n")
		os.Exit(1)
	}
	id := util.MustParseInt(fs.Arg(0))

	database := OpenDB()
	defer database.Close()

	game, err := database.GetGame(id)
	if err != nil || game == nil {
		fmt.Fprintf(os.Stderr, "Game with ID %d not found.\n", id)
		os.Exit(1)
	}

	exe := ResolveExecutable(*game)
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
