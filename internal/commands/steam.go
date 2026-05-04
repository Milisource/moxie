package commands

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/mili/moxie/internal/config"
	"github.com/mili/moxie/internal/steam"
	"github.com/mili/moxie/internal/util"
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
		fmt.Fprintf(os.Stderr, "Usage: moxie steam add <game-id>\n")
		os.Exit(1)
	}
	id := util.MustParseInt(fs.Arg(0))

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

	game, err := database.GetGame(id)
	if err != nil || game == nil {
		fmt.Fprintf(os.Stderr, "Game with ID %d not found.\n", id)
		os.Exit(1)
	}

	meta, _ := database.GetScrapedMeta(id)

	// 3. Resolve executable.
	exe := ResolveExecutable(*game)
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

// SteamProtonSet sets the Proton version for a Steam game.
func SteamProtonSet(args []string) {
	fs := flag.NewFlagSet("steam-proton-set", flag.ExitOnError)
	versionFlag := fs.String("version", "", "Proton version (required)")
	userFlag := fs.Uint("user", 0, "Steam user ID (default: first user)")
	nameFlag := fs.String("name", "", "Override display name (must match the name used when adding)")
	fs.Parse(args)

	if *versionFlag == "" {
		fmt.Fprintf(os.Stderr, "Usage: moxie steam proton-set <game-id> --version <proton>\n")
		os.Exit(1)
	}
	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie steam proton-set <game-id> --version <proton>\n")
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

// SteamFixArtwork re-downloads Steam artwork for a game.
func SteamFixArtwork(args []string) {
	fs := flag.NewFlagSet("steam-fix-artwork", flag.ExitOnError)
	userFlag := fs.Uint("user", 0, "Steam user ID (default: first user)")
	nameFlag := fs.String("name", "", "Override display name (must match the name used when adding)")
	sgdbKey := fs.String("steamgriddb-key", "", "SteamGridDB API key")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie steam fix-artwork <game-id>\n")
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

	meta, _ := database.GetScrapedMeta(id)
	name := game.Title
	if *nameFlag != "" {
		name = *nameFlag
	}
	exe := ResolveExecutable(*game)
	if exe == "" {
		fmt.Fprintf(os.Stderr, "No executable found for %q.\n", game.Title)
		os.Exit(1)
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

	artDone := false

	apiKey := ResolveSGDBKey(*sgdbKey)

	if apiKey != "" {
		sgdbClient := steam.NewSGDBClient(apiKey)
		// Priority 1: SteamGridDB by real Steam App ID.
		if game.SteamAppID > 0 {
			artDone = DownloadSGDBArtwork(sgdbClient, steamRoot, uid, appID, int(game.SteamAppID))
		}
		// Priority 2: SteamGridDB by name.
		if !artDone {
			artDone = TrySGDBArtworkByName(sgdbClient, steamRoot, uid, appID, name)
		}
	} else {
		fmt.Fprintf(os.Stderr, sgdbNoKeyLine)
	}
	// Priority 3: F95Zone cover URL (fallback, if available and valid).
	if !artDone && meta != nil && meta.CoverURL != "" {
		if err := steam.SetAllArtwork(steamRoot, uid, appID, meta.CoverURL); err == nil {
			artDone = true
		} else if !errors.Is(err, steam.ErrUnsupportedFormat) {
			fmt.Fprintf(os.Stderr, "  ⚠ Error setting artwork: %v\n", err)
			// Continue — artwork is best-effort, the game is still handled.
		}
	}

	if artDone {
		fmt.Printf("Artwork updated for '%s'\n", game.Title)
	} else {
		fmt.Fprintf(os.Stderr, "No artwork found for this game.\n")
		fmt.Fprintf(os.Stderr, sgdbKeyHint)
	}
}

// ResolveSGDBKey returns a SteamGridDB API key from the most available source.
func ResolveSGDBKey(flagKey string) string {
	if flagKey != "" {
		return flagKey
	}
	if key := os.Getenv("STEAMGRIDDB_KEY"); key != "" {
		return key
	}
	// Check JSON config (new format).
	if cfg, err := config.ReadConfig(); err == nil {
		if key, ok := cfg["steamgriddb-key"]; ok && key != "" {
			return key
		}
	}
	// Fall back to legacy flat file.
	if data, err := os.ReadFile(filepath.Join(config.ConfigDir(), "steamgriddb-key")); err == nil {
		return strings.TrimSpace(string(data))
	}
	return ""
}

var steamAppIDRe = regexp.MustCompile(`/app/(\d+)(?:/|$)`)

const sgdbNoKeyLine = "  \U0001F4A1 No SteamGridDB API key configured. Set one for higher-quality artwork.\n"

const sgdbKeyHint = "  \U0001F4A1 Tip: Set a SteamGridDB API key for higher-quality artwork!\n" +
	"         moxie config set steamgriddb-key <key>\n" +
	"         (get one free at https://www.steamgriddb.com/profile/preferences)\n"

// ExtractSteamAppID extracts a Steam App ID from a store URL.
// Example: "https://store.steampowered.com/app/12345/GameName/" → (12345, true)
func ExtractSteamAppID(storeURL string) (int, bool) {
	matches := steamAppIDRe.FindStringSubmatch(storeURL)
	if len(matches) < 2 {
		return 0, false
	}
	id, err := strconv.Atoi(matches[1])
	return id, err == nil
}

// DownloadSGDBArtwork downloads SteamGridDB artwork for a real Steam App ID.
func DownloadSGDBArtwork(sgdb *steam.SGDBClient, steamRoot string, uid, appID uint32, realSteamAppID int) bool {
	// Vertical grid (600×900).
	grids, err := sgdb.GetGridsBySteamAppID(realSteamAppID, "600x900")
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ⚠ SGDB grids: %v\n", err)
		return false
	}
	if url, ok := steam.BestGridImage(grids); ok {
		dest := steam.GridFilePath(steamRoot, uid, appID, steam.ArtVertical)
		if err := sgdb.DownloadImage(url, dest); err != nil {
			fmt.Fprintf(os.Stderr, "  ⚠ SGDB vertical: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "  ✓ Vertical grid\n")
		}
	}

	// Horizontal grid (460×215).
	gridsH, _ := sgdb.GetGridsBySteamAppID(realSteamAppID, "460x215")
	if url, ok := steam.BestGridImage(gridsH); ok {
		dest := steam.GridFilePath(steamRoot, uid, appID, steam.ArtHorizontal)
		_ = sgdb.DownloadImage(url, dest)
		fmt.Fprintf(os.Stderr, "  ✓ Horizontal grid\n")
	}

	// Hero (1920×620).
	heroes, _ := sgdb.GetHeroesBySteamAppID(realSteamAppID)
	if url, ok := steam.BestGridImage(heroes); ok {
		dest := steam.GridFilePath(steamRoot, uid, appID, steam.ArtHero)
		_ = sgdb.DownloadImage(url, dest)
		fmt.Fprintf(os.Stderr, "  ✓ Hero banner\n")
	}

	// Icon (best-effort).
	icons, _ := sgdb.GetIconsBySteamAppID(realSteamAppID)
	if urlI, ok := steam.BestGridImage(icons); ok {
		_ = sgdb.DownloadImage(urlI, steam.GridFilePath(steamRoot, uid, appID, steam.ArtIcon))
		fmt.Fprintf(os.Stderr, "  ✓ Icon\n")
	}

	// Logo (best-effort).
	logos, _ := sgdb.GetLogosBySteamAppID(realSteamAppID)
	if urlL, ok := steam.BestGridImage(logos); ok {
		_ = sgdb.DownloadImage(urlL, steam.GridFilePath(steamRoot, uid, appID, steam.ArtLogo))
		fmt.Fprintf(os.Stderr, "  ✓ Logo\n")
	}

	return true
}

// SanitizeTitleForSGDB cleans a game title for SteamGridDB search.
func SanitizeTitleForSGDB(title string) string {
	s := regexp.MustCompile(`\s*\[?v?\d+\.\d+(?:\.\d+)?\]?\s*`).ReplaceAllString(title, " ")
	s = strings.TrimSpace(s)
	return s
}

// TrySGDBArtworkByName searches SteamGridDB by game name and downloads artwork.
func TrySGDBArtworkByName(sgdb *steam.SGDBClient, steamRoot string, uid, appID uint32, gameName string) bool {
	fmt.Fprintf(os.Stderr, "  Searching SteamGridDB for %q...\n", SanitizeTitleForSGDB(gameName))
	results, err := sgdb.SearchGame(SanitizeTitleForSGDB(gameName))
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ⚠ SGDB search: %v\n", err)
		return false
	}
	if len(results) == 0 {
		fmt.Fprintf(os.Stderr, "  No SteamGridDB results.\n")
		return false
	}

	gameID := results[0].ID
	fmt.Fprintf(os.Stderr, "  Best match: %s (SGDB #%d)\n", results[0].Name, gameID)

	// Vertical grid (600×900).
	grids, err := sgdb.GetGridsBySGDBGameID(gameID, "600x900")
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ⚠ SGDB grids: %v\n", err)
		return false
	}
	url, ok := steam.BestGridImage(grids)
	if !ok {
		fmt.Fprintf(os.Stderr, "  No suitable grids found.\n")
		return false
	}
	dest := steam.GridFilePath(steamRoot, uid, appID, steam.ArtVertical)
	if err := sgdb.DownloadImage(url, dest); err != nil {
		fmt.Fprintf(os.Stderr, "  ⚠ SGDB download: %v\n", err)
		return false
	}
	fmt.Fprintf(os.Stderr, "  ✓ Vertical grid\n")

	// Best-effort horizontal + hero + icon + logo.
	if gridsH, _ := sgdb.GetGridsBySGDBGameID(gameID, "460x215"); len(gridsH) > 0 {
		if urlH, ok := steam.BestGridImage(gridsH); ok {
			sgdb.DownloadImage(urlH, steam.GridFilePath(steamRoot, uid, appID, steam.ArtHorizontal))
		}
	}
	if heroes, _ := sgdb.GetHeroesBySGDBGameID(gameID); len(heroes) > 0 {
		if urlH, ok := steam.BestGridImage(heroes); ok {
			sgdb.DownloadImage(urlH, steam.GridFilePath(steamRoot, uid, appID, steam.ArtHero))
			fmt.Fprintf(os.Stderr, "  ✓ Hero banner\n")
		}
	}
	if icons, _ := sgdb.GetIconsBySGDBGameID(gameID); len(icons) > 0 {
		if urlI, ok := steam.BestGridImage(icons); ok {
			sgdb.DownloadImage(urlI, steam.GridFilePath(steamRoot, uid, appID, steam.ArtIcon))
			fmt.Fprintf(os.Stderr, "  ✓ Icon\n")
		}
	}
	if logos, _ := sgdb.GetLogosBySGDBGameID(gameID); len(logos) > 0 {
		if urlL, ok := steam.BestGridImage(logos); ok {
			sgdb.DownloadImage(urlL, steam.GridFilePath(steamRoot, uid, appID, steam.ArtLogo))
			fmt.Fprintf(os.Stderr, "  ✓ Logo\n")
		}
	}

	return true
}
