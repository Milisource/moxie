package commands

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/mili/moxie/internal/archive"
	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/downloader"
	"github.com/mili/moxie/internal/log"
	"github.com/mili/moxie/internal/scraper"
	"github.com/mili/moxie/internal/updater"
)

// Download downloads a game from F95Zone download links with platform priority.
func Download(args []string) {
	fs := flag.NewFlagSet("download", flag.ExitOnError)
	downloadDir := fs.String("dir", "", "Download directory (default: game path)")
	extract := fs.Bool("extract", true, "Auto-extract archives after download")
	cookieStr := fs.String("cookie", "", "Cookie header from browser")
	cookieFile := fs.String("cookie-file", "", "File containing cookie header")
	platform := fs.String("platform", "", "Platform preference (linux/windows/macos/all, default: auto-detect)")
	all := fs.Bool("all", false, "Download all games with pending updates")
	open := fs.Bool("open", false, "Open download links in browser instead of direct download (bypasses bot detection)")
	watch := fs.Bool("watch", false, "Watch download directory for new files and auto-install (use with --open)")
	browserDir := fs.String("browser-dir", "", "Browser download directory to watch (default: ~/Downloads)")
	verbose := fs.Bool("verbose", false, "Verbose logging (debug level)")
	fs.Parse(args)

	if *verbose {
		log.SetLevel(slog.LevelDebug)
	}

	database := OpenDB()
	defer database.Close()

	cookie := ResolveCookie(*cookieStr, *cookieFile)

	targetPlatform := downloader.Platform(*platform)
	if targetPlatform == "" {
		targetPlatform = downloader.CurrentPlatform()
	}

	// Resolve browser download directory for --open/--watch mode.
	dlWatchDir := *browserDir
	if dlWatchDir == "" && (*open || *watch) {
		home, _ := os.UserHomeDir()
		dlWatchDir = filepath.Join(home, "Downloads")
	}

	// ── Batch mode ──
	if *all {
		if *open {
			downloadAllOpen(database, cookie, string(targetPlatform), dlWatchDir, *extract, *watch)
		} else {
			downloadAll(database, cookie, *downloadDir, string(targetPlatform), *extract)
		}
		return
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie download [flags] <id|name>\n")
		fmt.Fprintf(os.Stderr, "       moxie download --all [flags]\n")
		fmt.Fprintf(os.Stderr, "\nNote: flags must come BEFORE the game ID.\n")
		fmt.Fprintf(os.Stderr, "\nFlags:\n")
		fs.PrintDefaults()
		os.Exit(1)
	}

	game := ResolveGame(database, fs.Arg(0))
	if game == nil {
		fmt.Fprintf(os.Stderr, "Cancelled.\n")
		os.Exit(1)
	}

	if *open {
		downloadSingleOpen(database, game, cookie, string(targetPlatform), dlWatchDir, *extract, *watch)
	} else {
		downloadSingle(database, game, cookie, *downloadDir, string(targetPlatform), *extract)
	}
}

// downloadAll downloads all games with pending version updates.
func downloadAll(database *db.Database, cookie, downloadDir, targetPlatform string, extract bool) {
	queue, err := database.GamesNeedingUpdate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing games: %v\n", err)
		os.Exit(1)
	}

	if len(queue) == 0 {
		fmt.Fprintln(os.Stderr, "No games have pending updates.")
		return
	}

	fmt.Fprintf(os.Stderr, "Downloading updates for %d games...\n\n", len(queue))
	ok, failed := 0, 0
	for i, g := range queue {
		fmt.Fprintf(os.Stderr, "[%d/%d] %s: %s → %s\n", i+1, len(queue), g.Title, g.Version, g.LatestVersion)
		if err := downloadSingle(database, &g, cookie, downloadDir, targetPlatform, extract); err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "  ✗ %v\n", err)
		} else {
			ok++
		}
	}

	fmt.Fprintf(os.Stderr, "\n=== Batch download complete: %d/%d games ===\n", ok, len(queue))
	if failed > 0 {
		fmt.Fprintf(os.Stderr, "  %d failed\n", failed)
	}
}

// downloadSingle handles the full download flow for one game.
// Returns an error when the download did not complete; nil on success or
// when skipped (e.g. download already in progress).
func downloadSingle(database *db.Database, game *db.Game, cookie, downloadDir, targetPlatform string, extract bool) error {
	destDir := downloadDir
	if destDir == "" {
		destDir = filepath.Join(filepath.Dir(game.Path), "downloads")
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "  ✗ Error creating download directory: %v\n", err)
		return fmt.Errorf("create download directory: %w", err)
	}

	existing, _ := database.GetDownloadByGameID(game.ID)
	if existing != nil && existing.Status == db.DownloadStatusDownloading {
		fmt.Fprintf(os.Stderr, "  – Download already in progress.\n")
		return nil
	}

	fmt.Fprintf(os.Stderr, "  Looking for %s downloads...\n", targetPlatform)
	log.Info("download command started", "game_id", game.ID, "platform", targetPlatform)

	platform := downloader.Platform(targetPlatform)

	// Get stored links first
	var selectedLink *db.DownloadLink
	links, err := database.ListDownloadLinks(game.ID, "", false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  Warning: Could not list stored links: %v\n", err)
	}

	if len(links) > 0 {
		selectedLink = selectBestLinkByPlatform(links, platform)
	}

	// Fall back to scraping F95Zone
	if selectedLink == nil && game.F95URL != "" {
		if cookie != "" {
			fmt.Fprintf(os.Stderr, "  Fetching download links from F95Zone...\n")
			client := scraper.NewClient(cookie)
			scrapeURL := scraper.ResolveScrapeURL(game.F95URL, game.F95ThreadID)
			data, err := client.ScrapeThread(scrapeURL)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: Could not scrape F95Zone: %v\n", err)
			} else if len(data.DownloadLinks) > 0 {
				if err := database.DeleteDownloadLinksByGameID(game.ID); err != nil {
					fmt.Fprintf(os.Stderr, "  ⚠ Failed to clear stale download links: %v\n", err)
				}
				for _, dl := range data.DownloadLinks {
					linkPlatform := db.Platform(downloader.DetectPlatform(dl.Name, dl.URL))
					link := &db.DownloadLink{
						GameID:   game.ID,
						URL:      dl.URL,
						Host:     dl.Host,
						Name:     dl.Name,
						Platform: db.Platform(linkPlatform),
					}
					database.CreateDownloadLink(link)
				}
				links, _ = database.ListDownloadLinks(game.ID, "", false)
				selectedLink = selectBestLinkByPlatform(links, platform)
			}
		}
	}

	if len(links) == 0 {
		fmt.Fprintf(os.Stderr, "  – No download links found.\n")
		return fmt.Errorf("no download links found")
	}

	type scoredLink struct {
		link  db.DownloadLink
		score int
	}
	var scored []scoredLink
	for _, link := range links {
		if downloader.IsOnlineOnly(link.Name, link.URL) {
			continue
		}
		score := downloader.ScoreDownloadLink(link, platform)
		scored = append(scored, scoredLink{link, score})
	}
	if len(scored) == 0 {
		fmt.Fprintf(os.Stderr, "  – No downloadable links (all online-only).\n")
		return fmt.Errorf("no downloadable links (all online-only)")
	}
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	log.Info("links scored for download", "game_id", game.ID, "total_links", len(scored), "best_host", scored[0].link.Host, "best_score", scored[0].score)

	bestLink := scored[0].link
	fmt.Fprintf(os.Stderr, "  Selected: [%s] [%s] %s\n", bestLink.Platform, bestLink.Host, bestLink.Name)

	dlRecord := &db.Download{
		GameID:   game.ID,
		URL:      bestLink.URL,
		Host:     bestLink.Host,
		Filename: bestLink.Name,
		DestPath: destDir,
		Status:   db.DownloadStatusPending,
	}
	dlID, err := database.CreateDownload(dlRecord)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ✗ Error creating download record: %v\n", err)
		return fmt.Errorf("create download record: %w", err)
	}
	dlRecord.ID = dlID

	var lastErr error
	var failures []string
	for i, sl := range scored {
		link := sl.link

		if i > 0 {
			dlRecord.URL = link.URL
			dlRecord.Host = link.Host
			dlRecord.Filename = link.Name
			dlRecord.Status = db.DownloadStatusDownloading
			dlRecord.StartedAt = time.Now()
			dlRecord.BytesDownloaded = 0
			dlRecord.TotalBytes = 0
			dlRecord.SpeedBytesPerSec = 0
			dlRecord.PercentComplete = 0
			dlRecord.Error = ""
			database.UpdateDownload(dlRecord)

			log.Info("download fallback", "game_id", game.ID, "attempt", i+1, "total", len(scored), "host", link.Host)
			fmt.Fprintf(os.Stderr, "  Retrying with [%s] [%s] %s\n", link.Platform, link.Host, link.Name)
		} else {
			dlRecord.Status = db.DownloadStatusDownloading
			dlRecord.StartedAt = time.Now()
			database.UpdateDownload(dlRecord)
		}

		progressFn := func(p downloader.Progress) {
			dlRecord.BytesDownloaded = p.BytesDownloaded
			dlRecord.TotalBytes = p.TotalBytes
			dlRecord.SpeedBytesPerSec = p.SpeedBytesPerSec
			dlRecord.PercentComplete = p.Percent
			database.UpdateDownload(dlRecord)
			renderProgressBar(p)
		}

		err = downloader.Download(link.URL, destDir, 0, progressFn, cookie)
		if err == nil {
			downloadedFile := findDownloadedFile(destDir, dlRecord.Filename)
			if downloadedFile != "" && !downloader.IsValidGameFile(downloadedFile) {
				fi, _ := os.Stat(downloadedFile)
				size := int64(0)
				if fi != nil {
					size = fi.Size()
				}
				log.Warn("downloaded file is not a valid game file (likely interstitial page)", "file", downloadedFile, "size", size)
				os.Remove(downloadedFile)
				err = fmt.Errorf("downloaded content is not a valid game file (%d bytes)", size)
			}
		}
		if err == nil {
			lastErr = nil
			break
		}
		lastErr = err
		failures = append(failures, err.Error())
		log.Warn("download attempt failed", "game_id", game.ID, "host", link.Host, "error", err)
		fmt.Fprintf(os.Stderr, "  ✗ [%s] download failed: %v\n", link.Host, err)
	}

	if lastErr != nil {
		dlRecord.Status = db.DownloadStatusFailed
		dlRecord.Error = lastErr.Error()
		dlRecord.CompletedAt = time.Now()
		database.UpdateDownload(dlRecord)

		log.Error("download failed (all links exhausted)", "game_id", game.ID, "links_tried", len(scored), "error", lastErr)
		fmt.Fprintf(os.Stderr, "  ✗ Failed: %s\n", lastErr.Error())
		return lastErr
	}

	dlRecord.Status = db.DownloadStatusCompleted
	dlRecord.PercentComplete = 100
	dlRecord.CompletedAt = time.Now()
	database.UpdateDownload(dlRecord)

	log.Info("download succeeded", "game_id", game.ID, "host", dlRecord.Host)
	fmt.Fprintf(os.Stderr, "  ✓ Download complete\n")

	// Auto-extract
	if extract {
		downloadedFile := findDownloadedFile(destDir, dlRecord.Filename)
		if downloadedFile != "" && archive.IsArchiveFile(downloadedFile) {
			fmt.Fprintf(os.Stderr, "  Extracting archive...\n")
			dlRecord.Status = db.DownloadStatusExtracting
			database.UpdateDownload(dlRecord)

			result, err := archive.Extract(downloadedFile, destDir, archive.Options{
				OnProgress: func(totalFiles, extractedFiles int, currentFile string, bytesProcessed, bytesTotal int64) {
					if totalFiles > 0 {
						percent := float64(extractedFiles) / float64(totalFiles) * 100
						displayFile := currentFile
						if len(displayFile) > 60 {
							displayFile = displayFile[:57] + "..."
						}
						fmt.Fprintf(os.Stderr, "\r    Extracting: %d/%d files (%.1f%%) - %-60s",
							extractedFiles, totalFiles, percent, displayFile)
					}
				},
			})

			if err != nil {
				fmt.Fprintf(os.Stderr, "\n    ⚠ Warning: %v\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "\n    Extracted %d files\n", result.FilesExtracted)
				os.Remove(downloadedFile)
				fmt.Fprintf(os.Stderr, "    Removed archive\n")

				fmt.Fprintf(os.Stderr, "  Merging update into %s...\n", game.Path)
				mergeResult, mergeErr := updater.Merge(game.Path, string(game.Engine), result.Destination, true)
				if mergeErr != nil {
					fmt.Fprintf(os.Stderr, "    ⚠ Warning: %v\n", mergeErr)
				} else {
					fmt.Fprintf(os.Stderr, "    Files updated: %d  |  User files preserved: %d\n", mergeResult.FilesCopied, mergeResult.FilesPreserved)
					if mergeResult.BackupPath != "" {
						fmt.Fprintf(os.Stderr, "    Backup: %s\n", mergeResult.BackupPath)
					}
				}
			}
		}
	}

	fmt.Fprintf(os.Stderr, "  ✓ Ready: %s\n", destDir)
	return nil
}
