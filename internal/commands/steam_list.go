package commands

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/mili/moxie/internal/steam"
	"github.com/mili/moxie/internal/util"
)

// SteamList lists games added to the Steam library.
func SteamList(args []string) {
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
		title := util.Truncate(s.AppName, 28)
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

// SteamProtonList lists available Proton versions.
func SteamProtonList(args []string) {
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
