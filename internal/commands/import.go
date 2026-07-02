package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Import reads an export JSON file and merges the games into the library.
// Games are matched by path (existing games are updated, new ones are inserted).
//
// Usage: moxie import <file.json>
func Import(args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie import <file.json>\n")
		os.Exit(1)
	}

	data, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading import file: %v\n", err)
		os.Exit(1)
	}

	var export ExportFile
	if err := json.Unmarshal(data, &export); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing import file: %v\n", err)
		os.Exit(1)
	}

	if export.Version != 1 {
		fmt.Fprintf(os.Stderr, "Unsupported export version: %d\n", export.Version)
		os.Exit(1)
	}

	database := OpenDB()
	defer database.Close()

	imported := 0
	updated := 0
	skipped := 0

	for _, eg := range export.Games {
		if eg.Game == nil {
			skipped++
			continue
		}

		// Ensure path is absolute (resolve relative to config dir if needed).
		path := eg.Game.Path
		if !filepath.IsAbs(path) {
			path = filepath.Clean(path)
		}

		// Check if game already exists by path.
		existing, err := database.GetGameByPath(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Error checking %q: %v\n", eg.Game.Title, err)
			skipped++
			continue
		}

		if existing != nil {
			// Update existing game — preserve created_at and other curation.
			existing.Title = eg.Game.Title
			existing.Engine = eg.Game.Engine
			existing.Version = eg.Game.Version
			if eg.Game.ExePath != "" {
				existing.ExePath = eg.Game.ExePath
			}
			existing.SizeBytes = eg.Game.SizeBytes
			existing.F95URL = eg.Game.F95URL
			existing.F95ThreadID = eg.Game.F95ThreadID
			existing.Tags = eg.Game.Tags
			existing.Status = eg.Game.Status
			existing.Notes = eg.Game.Notes
			existing.LatestVersion = eg.Game.LatestVersion
			existing.VersionCheckedAt = eg.Game.VersionCheckedAt
			existing.StoreLinks = eg.Game.StoreLinks
			existing.SteamAppID = eg.Game.SteamAppID
			if eg.Game.SeriesID != nil {
				existing.SeriesID = eg.Game.SeriesID
			}
			existing.SeriesOrder = eg.Game.SeriesOrder
			existing.UpdatedAt = time.Now().UTC()

			if err := database.UpdateGame(existing); err != nil {
				fmt.Fprintf(os.Stderr, "  Error updating %q: %v\n", eg.Game.Title, err)
				skipped++
				continue
			}

			// Import scraped metadata.
			if eg.ScrapedMeta != nil {
				eg.ScrapedMeta.GameID = existing.ID
				if upsertErr := database.UpsertScrapedMeta(eg.ScrapedMeta); upsertErr != nil {
					fmt.Fprintf(os.Stderr, "  Warning: failed to import metadata for %q: %v\n", eg.Game.Title, upsertErr)
				}
			}

			updated++
			continue
		}

		// Insert as new game.
		now := time.Now().UTC()
		eg.Game.CreatedAt = now
		eg.Game.UpdatedAt = now
		if eg.Game.Status == "" {
			eg.Game.Status = "unknown"
		}

		id, err := database.InsertGame(eg.Game)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Error importing %q: %v\n", eg.Game.Title, err)
			skipped++
			continue
		}

		// Import scraped metadata.
		if eg.ScrapedMeta != nil {
			eg.ScrapedMeta.GameID = id
			if upsertErr := database.UpsertScrapedMeta(eg.ScrapedMeta); upsertErr != nil {
				fmt.Fprintf(os.Stderr, "  Warning: failed to import metadata for %q: %v\n", eg.Game.Title, upsertErr)
			}
		}

		// Import download links.
		for _, link := range eg.DownloadLinks {
			link.GameID = id
			link.ID = 0 // reset ID so it gets auto-assigned
			if _, linkErr := database.CreateDownloadLink(&link); linkErr != nil {
				fmt.Fprintf(os.Stderr, "  Warning: failed to import download link for %q: %v\n", eg.Game.Title, linkErr)
			}
		}

		imported++
	}

	fmt.Printf("Import complete: %d imported, %d updated, %d skipped.\n", imported, updated, skipped)
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "  Check warnings above for details.\n")
	}

	// Summary stats.
	count, _ := database.GameCount()
	fmt.Printf("Library now has %d games.\n", count)
}


