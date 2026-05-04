package commands

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mili/moxie/internal/archive"
	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/downloader"
	"github.com/mili/moxie/internal/scraper"
	"github.com/mili/moxie/internal/util"
)

// Download downloads a game from F95Zone download links with platform priority.
func Download(args []string) {
	fs := flag.NewFlagSet("download", flag.ExitOnError)
	downloadDir := fs.String("dir", "", "Download directory (default: game path)")
	extract := fs.Bool("extract", true, "Auto-extract archives after download")
	cookieStr := fs.String("cookie", "", "Cookie header from browser")
	cookieFile := fs.String("cookie-file", "", "File containing cookie header")
	platform := fs.String("platform", "", "Platform preference (linux/windows/macos/all, default: auto-detect)")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie download <game-id> [flags]\n")
		fmt.Fprintf(os.Stderr, "\nFlags:\n")
		fs.PrintDefaults()
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

	// Determine download destination
	destDir := *downloadDir
	if destDir == "" {
		destDir = filepath.Join(filepath.Dir(game.Path), "downloads")
	}

	// Ensure destination exists
	if err := os.MkdirAll(destDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating download directory: %v\n", err)
		os.Exit(1)
	}

	// Check for existing download
	existing, _ := database.GetDownloadByGameID(id)
	if existing != nil && existing.Status == db.DownloadStatusDownloading {
		fmt.Fprintf(os.Stderr, "Download already in progress for this game.\n")
		os.Exit(1)
	}

	// Determine target platform
	targetPlatform := downloader.Platform(*platform)
	if targetPlatform == "" {
		targetPlatform = downloader.CurrentPlatform()
	}

	fmt.Fprintf(os.Stderr, "Looking for %s downloads...\n", targetPlatform)

	// Get download links from database first
	var selectedLink *db.DownloadLink
	links, err := database.ListDownloadLinks(id, "", false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Could not list stored links: %v\n", err)
	}

	if len(links) > 0 {
		selectedLink = selectBestLinkByPlatform(links, targetPlatform)
	}

	// If no stored links, try scraping
	if selectedLink == nil && game.F95URL != "" {
		cookie := ResolveCookie(*cookieStr, *cookieFile)
		if cookie != "" {
			fmt.Fprintf(os.Stderr, "Fetching download links from F95Zone...\n")
			client := scraper.NewClient(cookie)
			data, err := client.ScrapeThread(game.F95URL)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Could not scrape F95Zone: %v\n", err)
			} else if len(data.DownloadLinks) > 0 {
				// Save and use the first link
				database.DeleteDownloadLinksByGameID(id)
				for _, dl := range data.DownloadLinks {
					linkPlatform := DetectPlatformFromLink(dl.Name, dl.URL)
					link := &db.DownloadLink{
						GameID:   id,
						URL:      dl.URL,
						Host:     dl.Host,
						Name:     dl.Name,
						Platform: db.Platform(linkPlatform),
					}
					database.CreateDownloadLink(link)
				}
				// Re-fetch with platform filter
				links, _ = database.ListDownloadLinks(id, "", false)
				selectedLink = selectBestLinkByPlatform(links, targetPlatform)
			}
		}
	}

	if selectedLink == nil {
		fmt.Fprintf(os.Stderr, "No download links found for this game.\n")
		fmt.Fprintf(os.Stderr, "Run 'moxie scrape %d' to fetch links from F95Zone.\n", id)
		os.Exit(1)
	}

	link := selectedLink
	fmt.Fprintf(os.Stderr, "Selected download: [%s] [%s] %s\n", link.Platform, link.Host, link.Name)

	// Create download record
	dlRecord := &db.Download{
		GameID:   id,
		URL:      link.URL,
		Host:     link.Host,
		Filename: link.Name,
		DestPath: destDir,
		Status:   db.DownloadStatusPending,
	}
	dlID, err := database.CreateDownload(dlRecord)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating download record: %v\n", err)
		os.Exit(1)
	}
	dlRecord.ID = dlID

	// Update status to downloading
	dlRecord.Status = db.DownloadStatusDownloading
	dlRecord.StartedAt = time.Now()
	database.UpdateDownload(dlRecord)

	// Perform download with progress
	fmt.Fprintf(os.Stderr, "\nDownloading to: %s\n\n", destDir)

	progressFn := func(p downloader.Progress) {
		dlRecord.BytesDownloaded = p.BytesDownloaded
		dlRecord.TotalBytes = p.TotalBytes
		dlRecord.SpeedBytesPerSec = p.SpeedBytesPerSec
		dlRecord.PercentComplete = p.Percent
		database.UpdateDownload(dlRecord)
		renderProgressBar(p)
	}

	err = downloader.Download(link.URL, destDir, 0, progressFn)

	if err != nil {
		dlRecord.Status = db.DownloadStatusFailed
		dlRecord.Error = err.Error()
		dlRecord.CompletedAt = time.Now()
		database.UpdateDownload(dlRecord)

		fmt.Fprintf(os.Stderr, "\n\nDownload failed: %v\n", err)
		os.Exit(1)
	}

	// Download completed
	dlRecord.Status = db.DownloadStatusCompleted
	dlRecord.PercentComplete = 100
	dlRecord.CompletedAt = time.Now()
	database.UpdateDownload(dlRecord)

	fmt.Fprintf(os.Stderr, "\n\nDownload completed!\n")

	// Auto-extract if enabled and it's an archive
	if *extract {
		downloadedFile := findDownloadedFile(destDir, link.Name)
		if downloadedFile != "" && archive.IsArchiveFile(downloadedFile) {
			fmt.Fprintf(os.Stderr, "\nExtracting archive...\n")
			dlRecord.Status = db.DownloadStatusExtracting
			database.UpdateDownload(dlRecord)

			result, err := archive.Extract(downloadedFile, destDir, archive.Options{
				OnProgress: func(totalFiles, extractedFiles int, currentFile string, bytesProcessed, bytesTotal int64) {
					if totalFiles > 0 {
						percent := float64(extractedFiles) / float64(totalFiles) * 100
						fmt.Fprintf(os.Stderr, "\r  Extracting: %d/%d files (%.1f%%) - %s", extractedFiles, totalFiles, percent, currentFile)
					}
				},
			})

			if err != nil {
				fmt.Fprintf(os.Stderr, "\nExtraction warning: %v\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "\n  Extracted %d files to: %s\n", result.FilesExtracted, result.Destination)
				os.Remove(downloadedFile)
				fmt.Fprintf(os.Stderr, "  Removed archive: %s\n", filepath.Base(downloadedFile))
			}
		}
	}

	fmt.Fprintf(os.Stderr, "\nGame ready at: %s\n", destDir)
}

// isOnlineOnlyLink returns true if the link text or URL indicates a browser-only version.
func isOnlineOnlyLink(name, url string) bool {
	lower := strings.ToLower(name + " " + url)
	return strings.Contains(lower, "online") || strings.Contains(lower, "gamejolt")
}

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
		if isOnlineOnlyLink(link.Name, link.URL) {
			continue
		}

		score := 0
		linkPlatform := downloader.Platform(link.Platform)

		if linkPlatform == targetPlatform {
			score = 100
		} else if linkPlatform == downloader.PlatformAll {
			score = 50
		} else if linkPlatform == downloader.PlatformUnknown {
			score = 25
		}

		host := strings.ToLower(link.Host)
		switch host {
		case "vikingfile", "buzzheavier", "pixeldrain", "mega", "gofile":
			score += 15
		case "mediafire", "workupload":
			score += 8
		case "krakenfiles", "googledrive":
			score += 5
		}

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

// renderProgressBar renders a terminal progress bar.
func renderProgressBar(p downloader.Progress) {
	width := 40
	filled := int(p.Percent / 100.0 * float64(width))
	if filled > width {
		filled = width
	}

	barStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("99"))
	emptyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	bar := strings.Repeat("█", filled)
	empty := strings.Repeat("░", width-filled)

	speed := formatSpeed(p.SpeedBytesPerSec)
	percent := fmt.Sprintf("%5.1f%%", p.Percent)
	size := fmt.Sprintf("%s / %s", formatBytes(p.BytesDownloaded), formatBytes(p.TotalBytes))

	fmt.Fprintf(os.Stderr, "\r%s%s %s %s %s",
		barStyle.Render(bar),
		emptyStyle.Render(empty),
		lipgloss.NewStyle().Bold(true).Render(percent),
		speed,
		size,
	)
}

func formatSpeed(bytesPerSec float64) string {
	if bytesPerSec < 1024 {
		return fmt.Sprintf("%.0f B/s", bytesPerSec)
	}
	if bytesPerSec < 1024*1024 {
		return fmt.Sprintf("%.1f KB/s", bytesPerSec/1024)
	}
	return fmt.Sprintf("%.1f MB/s", bytesPerSec/(1024*1024))
}

func formatBytes(b int64) string {
	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	}
	if b < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	}
	if b < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
	}
	return fmt.Sprintf("%.1f GB", float64(b)/(1024*1024*1024))
}

// findDownloadedFile attempts to find the downloaded file in the destination directory.
func findDownloadedFile(destDir, linkName string) string {
	path := filepath.Join(destDir, linkName)
	if _, err := os.Stat(path); err == nil {
		return path
	}

	if idx := strings.Index(linkName, "?"); idx > 0 {
		path = filepath.Join(destDir, linkName[:idx])
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	entries, err := os.ReadDir(destDir)
	if err != nil {
		return ""
	}

	var mostRecent os.DirEntry
	var mostRecentTime time.Time

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(mostRecentTime) {
			mostRecent = e
			mostRecentTime = info.ModTime()
		}
	}

	if mostRecent != nil {
		return filepath.Join(destDir, mostRecent.Name())
	}

	return ""
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

	var links []db.DownloadLink
	var err error

	if *gameID > 0 {
		links, err = database.ListDownloadLinks(*gameID, "", true)
	} else {
		// Get all games and their links
		games, _ := database.ListGames("", "")
		for _, g := range games {
			gameLinks, _ := database.ListDownloadLinks(g.ID, "", true)
			links = append(links, gameLinks...)
		}
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing links: %v\n", err)
		os.Exit(1)
	}

	if len(links) == 0 {
		fmt.Fprintln(os.Stderr, "No download links found.")
		return
	}

	fmt.Printf("Checking %d download links...\n\n", len(links))

	checked, dead := 0, 0
	for _, link := range links {
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
