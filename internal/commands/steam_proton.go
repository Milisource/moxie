package commands

import (
	"flag"
	"fmt"
	"os"

	"github.com/mili/moxie/internal/steam"
)

// SteamProtonSet sets the Proton version for a Steam game.
func SteamProtonSet(args []string) {
	fs := flag.NewFlagSet("steam-proton-set", flag.ExitOnError)
	versionFlag := fs.String("version", "", "Proton version (required)")
	userFlag := fs.Uint("user", 0, "Steam user ID (default: first user)")
	nameFlag := fs.String("name", "", "Override display name (must match the name used when adding)")
	fs.Parse(args)

	if *versionFlag == "" {
		fmt.Fprintf(os.Stderr, "Usage: moxie steam proton-set <id|name> --version <proton>\n")
		os.Exit(1)
	}
	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie steam proton-set <id|name> --version <proton>\n")
		os.Exit(1)
	}
	database := OpenDB()
	defer database.Close()

	game := ResolveGame(database, fs.Arg(0))
	if game == nil {
		fmt.Fprintf(os.Stderr, "Cancelled.\n")
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
