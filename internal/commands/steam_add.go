package commands

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mili/moxie/internal/steam"
)

// SteamAdd adds a game to the Steam library.
func SteamAdd(args []string) {
	fs := flag.NewFlagSet("steam-add", flag.ExitOnError)
	allUsers := fs.Bool("all-users", false, "Add to all Steam user accounts")
	noArtwork := fs.Bool("no-artwork", false, "Skip downloading cover artwork")
	protonVer := fs.String("proton", "proton_experimental", "Proton version (Linux only; use 'none' to skip)")
	displayName := fs.String("name", "", "Override display name in Steam")
	sgdbKey := fs.String("steamgriddb-key", "", "SteamGridDB API key")
	tagsFlag := fs.String("tags", "", "Additional comma-separated tags")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie steam add <id|name>\n")
		os.Exit(1)
	}
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
	database := OpenDB()
	defer database.Close()

	game := ResolveGame(database, fs.Arg(0))
	if game == nil {
		fmt.Fprintf(os.Stderr, "Cancelled.\n")
		os.Exit(1)
	}

	meta, _ := database.GetScrapedMeta(game.ID)

	// 3. Resolve executable.
	exe := resolveGameExe(game)
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

			sgdbAPIKey := ResolveSGDBKey(*sgdbKey)
			if sgdbAPIKey != "" {
				sgdbClient := steam.NewSGDBClient(sgdbAPIKey)
				// Priority 1: SteamGridDB by real Steam App ID (precise artwork).
				if game.SteamAppID > 0 {
					artDone = DownloadSGDBArtwork(sgdbClient, steamRoot, uid, entry.AppID, int(game.SteamAppID))
				}
				// Priority 2: SteamGridDB by name (fuzzy search).
				if !artDone {
					artDone = TrySGDBArtworkByName(sgdbClient, steamRoot, uid, entry.AppID, name)
				}
			} else {
				fmt.Fprintf(os.Stderr, sgdbNoKeyLine)
			}
			// Priority 3: F95Zone cover image (fallback).
			if !artDone && meta != nil && meta.CoverURL != "" {
				if err := steam.SetAllArtwork(steamRoot, uid, entry.AppID, meta.CoverURL); err == nil {
					artDone = true
				} else if !errors.Is(err, steam.ErrUnsupportedFormat) {
					fmt.Fprintf(os.Stderr, "  ⚠ Artwork: %v\n", err)
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
