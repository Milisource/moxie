package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/mili/moxie/internal/db"
)

// ExportGameData holds a single game's complete data for export.
type ExportGameData struct {
	Game         *db.Game         `json:"game"`
	ScrapedMeta  *db.ScrapedMeta  `json:"scraped_meta,omitempty"`
	DownloadLinks []db.DownloadLink `json:"download_links,omitempty"`
}

// ExportFile is the top-level export JSON structure.
type ExportFile struct {
	Version    int              `json:"version"`
	ExportedAt string           `json:"exported_at"`
	Games      []ExportGameData `json:"games"`
}

// Export writes all games (with metadata and download links) to a JSON file.
//
// Usage: moxie export [--output file.json]
func Export(args []string) {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	output := fs.String("output", "", "Output file path (default: stdout)")
	fs.Parse(args)

	database := OpenDB()
	defer database.Close()

	games, err := database.ListActiveGames("", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing games: %v\n", err)
		os.Exit(1)
	}

	export := ExportFile{
		Version:    1,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Games:      make([]ExportGameData, 0, len(games)),
	}

	for _, g := range games {
		eg := ExportGameData{Game: &g}

		// Include scraped metadata if available.
		meta, metaErr := database.GetScrapedMeta(g.ID)
		if metaErr == nil && meta != nil {
			eg.ScrapedMeta = meta
		}

		// Include download links if available.
		links, linksErr := database.ListDownloadLinks(g.ID, "", true)
		if linksErr == nil && len(links) > 0 {
			eg.DownloadLinks = links
		}

		export.Games = append(export.Games, eg)
	}

	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshalling export: %v\n", err)
		os.Exit(1)
	}

	if *output != "" {
		if err := os.WriteFile(*output, data, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing export file: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Exported %d games to %s\n", len(export.Games), *output)
	} else {
		os.Stdout.Write(data)
		os.Stdout.Write([]byte("\n"))
	}
}
