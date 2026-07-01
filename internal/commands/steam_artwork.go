package commands

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mili/moxie/internal/config"
	"github.com/mili/moxie/internal/steam"
)

// SteamFixArtwork re-downloads Steam artwork for a game.
func SteamFixArtwork(args []string) {
	fs := flag.NewFlagSet("steam-fix-artwork", flag.ExitOnError)
	userFlag := fs.Uint("user", 0, "Steam user ID (default: first user)")
	nameFlag := fs.String("name", "", "Override display name (must match the name used when adding)")
	sgdbKey := fs.String("steamgriddb-key", "", "SteamGridDB API key")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie steam fix-artwork <id|name>\n")
		os.Exit(1)
	}
	database := OpenDB()
	defer database.Close()

	game := ResolveGame(database, fs.Arg(0))
	if game == nil {
		fmt.Fprintf(os.Stderr, "Cancelled.\n")
		os.Exit(1)
	}

	meta, _ := database.GetScrapedMeta(game.ID)
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

const sgdbNoKeyLine = "  \U0001F4A1 No SteamGridDB API key configured. Set one for higher-quality artwork.\n"

const sgdbKeyHint = "  \U0001F4A1 Tip: Set a SteamGridDB API key for higher-quality artwork!\n" +
	"         moxie config set steamgriddb-key <key>\n" +
	"         (get one free at https://www.steamgriddb.com/profile/preferences)\n"

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
