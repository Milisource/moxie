package commands

import (
	"fmt"
	"os"

	"github.com/mili/moxie/internal/steam"
)

// Steam handles all steam subcommands.
func Steam(args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie steam <add|remove|list|proton-list|proton-set|fix-artwork>\n")
		os.Exit(1)
	}
	switch args[0] {
	case "add":
		SteamAdd(args[1:])
	case "remove":
		SteamRemove(args[1:])
	case "list":
		SteamList(args[1:])
	case "proton-list":
		SteamProtonList(args[1:])
	case "proton-set":
		SteamProtonSet(args[1:])
	case "fix-artwork":
		SteamFixArtwork(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown steam subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

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













