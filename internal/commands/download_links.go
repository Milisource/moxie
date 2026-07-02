package commands

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/downloader"
)

// selectBestLinkByPlatform selects the best download link based on platform priority.
// Skips online-only links that aren't downloadable.
func selectBestLinkByPlatform(links []db.DownloadLink, targetPlatform downloader.Platform) *db.DownloadLink {
	if len(links) == 0 {
		return nil
	}

	type scoredLink struct {
		link  db.DownloadLink
		score int
	}
	var scored []scoredLink

	for _, link := range links {
		// Skip online-only / browser-playable links
		if downloader.IsOnlineOnly(link.Name, link.URL) {
			continue
		}

		score := downloader.ScoreDownloadLink(link, targetPlatform)
		scored = append(scored, scoredLink{link, score})
	}

	if len(scored) == 0 {
		return nil
	}
	best := scored[0]
	for _, s := range scored[1:] {
		if s.score > best.score {
			best = s
		}
	}

	return &best.link
}

// ListDownloads shows all downloads and their status.
func ListDownloads(args []string) {
	fs := flag.NewFlagSet("downloads", flag.ExitOnError)
	filterStatus := fs.String("status", "", "Filter by status (pending, downloading, completed, failed)")
	fs.Parse(args)

	database := OpenDB()
	defer database.Close()

	downloads, err := database.ListDownloads(*filterStatus)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing downloads: %v\n", err)
		os.Exit(1)
	}

	if len(downloads) == 0 {
		fmt.Fprintln(os.Stderr, "No downloads found.")
		return
	}

	fmt.Printf("%-5s %-8s %-12s %-10s %-15s %s\n", "ID", "Game", "Status", "Progress", "Speed", "URL")
	fmt.Println(strings.Repeat("-", 100))

	for _, d := range downloads {
		progress := fmt.Sprintf("%.1f%%", d.PercentComplete)
		if d.Status == db.DownloadStatusCompleted {
			progress = "100%"
		}

		speed := formatSpeed(d.SpeedBytesPerSec)
		if d.Status != db.DownloadStatusDownloading {
			speed = "-"
		}

		url := d.URL
		if len(url) > 40 {
			url = url[:37] + "..."
		}

		fmt.Printf("%-5d %-8d %-12s %-10s %-15s %s\n",
			d.ID, d.GameID, d.Status, progress, speed, url)
	}
}

// CheckDeadLinks validates all download links and marks dead ones.
func CheckDeadLinks(args []string) {
	fs := flag.NewFlagSet("check-links", flag.ExitOnError)
	gameID := fs.Int64("game", 0, "Check links for specific game ID only")
	deleteDead := fs.Bool("delete", false, "Delete dead links instead of just marking them")
	fs.Parse(args)

	database := OpenDB()
	defer database.Close()

	var allLinks []db.DownloadLinkWithGame
	var err error

	if *gameID > 0 {
		// Single game: load links via per-game query
		gameLinks, listErr := database.ListDownloadLinks(*gameID, "", true)
		if listErr != nil {
			fmt.Fprintf(os.Stderr, "Error listing links: %v\n", listErr)
			os.Exit(1)
		}
		game, getErr := database.GetGame(*gameID)
		if getErr != nil {
			fmt.Fprintf(os.Stderr, "Error loading game: %v\n", getErr)
			os.Exit(1)
		}
		title := ""
		path := ""
		if game != nil {
			title = game.Title
			path = game.Path
		}
		for _, l := range gameLinks {
			allLinks = append(allLinks, db.DownloadLinkWithGame{
				DownloadLink: l,
				GameTitle:    title,
				GamePath:     path,
			})
		}
	} else {
		allLinks, err = database.AllDownloadLinks(true)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing links: %v\n", err)
			os.Exit(1)
		}
	}

	if len(allLinks) == 0 {
		fmt.Fprintln(os.Stderr, "No download links found.")
		return
	}

	fmt.Printf("Checking %d download links...\n\n", len(allLinks))

	checked, dead := 0, 0
	for _, link := range allLinks {
		if link.IsDead {
			fmt.Printf("[%s] Already marked dead: %s\n", link.Host, link.Name)
			dead++
			continue
		}

		fmt.Printf("Checking [%s] %s... ", link.Host, link.Name)
		if err := downloader.CheckLink(link.URL); err != nil {
			fmt.Printf("DEAD: %v\n", err)
			if *deleteDead {
				database.DeleteDownloadLink(link.ID)
				fmt.Printf("  -> Deleted\n")
			} else {
				database.MarkDownloadLinkDead(link.ID, err.Error())
				fmt.Printf("  -> Marked as dead\n")
			}
			dead++
		} else {
			fmt.Println("OK")
		}
		checked++
	}

	fmt.Printf("\nDone: %d checked, %d dead links found\n", checked, dead)
}
