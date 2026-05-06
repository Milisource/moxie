package commands

import (
	"flag"
	"fmt"
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
	log.Info("download command started", "game_id", id, "platform", targetPlatform)

	// Get download links from database first
	var selectedLink *db.DownloadLink
	links, err := database.ListDownloadLinks(id, "", false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Could not list stored links: %v\n", err)
	}

	if len(links) > 0 {
		selectedLink = selectBestLinkByPlatform(links, targetPlatform)
	}

	// Resolve F95Zone cookie for both scraping and download authentication
	cookie := ResolveCookie(*cookieStr, *cookieFile)

	// If no stored links, try scraping
	if selectedLink == nil && game.F95URL != "" {
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
					linkPlatform := downloader.DetectPlatformFromLink(dl.Name, dl.URL)
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

	if len(links) == 0 {
		fmt.Fprintf(os.Stderr, "No download links found for this game.\n")
		fmt.Fprintf(os.Stderr, "Run 'moxie scrape %d' to fetch links from F95Zone.\n", id)
		os.Exit(1)
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
		score := downloader.ScoreDownloadLink(link, targetPlatform)
		scored = append(scored, scoredLink{link, score})
	}
	if len(scored) == 0 {
		fmt.Fprintf(os.Stderr, "No download links found for this game.\n")
		fmt.Fprintf(os.Stderr, "Run 'moxie scrape %d' to fetch links from F95Zone.\n", id)
		os.Exit(1)
	}
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	log.Info("links scored for download", "game_id", id, "total_links", len(scored), "best_host", scored[0].link.Host, "best_score", scored[0].score)

	bestLink := scored[0].link
	fmt.Fprintf(os.Stderr, "Selected download: [%s] [%s] %s\n", bestLink.Platform, bestLink.Host, bestLink.Name)

	dlRecord := &db.Download{
		GameID:   id,
		URL:      bestLink.URL,
		Host:     bestLink.Host,
		Filename: bestLink.Name,
		DestPath: destDir,
		Status:   db.DownloadStatusPending,
	}
	dlID, err := database.CreateDownload(dlRecord)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating download record: %v\n", err)
		os.Exit(1)
	}
	dlRecord.ID = dlID

	fmt.Fprintf(os.Stderr, "\nDownloading to: %s\n\n", destDir)

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

			log.Info("download fallback", "game_id", id, "attempt", i+1, "total", len(scored), "host", link.Host)
			fmt.Fprintf(os.Stderr, "\nRetrying with [%s] [%s] %s\n", link.Platform, link.Host, link.Name)
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
			// Validate downloaded file is a real game file, not an interstitial page
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
		log.Warn("download attempt failed", "game_id", id, "host", link.Host, "error", err)
		fmt.Fprintf(os.Stderr, "\n  [%s] download failed: %v\n", link.Host, err)
	}

	if lastErr != nil {
		dlRecord.Status = db.DownloadStatusFailed
		dlRecord.Error = lastErr.Error()
		dlRecord.CompletedAt = time.Now()
		database.UpdateDownload(dlRecord)

		log.Error("download failed (all links exhausted)", "game_id", id, "links_tried", len(scored), "error", lastErr)

		// Show per-link failure summary
		fmt.Fprintf(os.Stderr, "\n\nDownload failed for all %d links:\n", len(scored))
		for i, sl := range scored {
			errMsg := "unknown error"
			if i < len(failures) {
				errMsg = failures[i]
			}
			fmt.Fprintf(os.Stderr, "  \u2717 [%s] %s \u2014 %s\n", sl.link.Host, sl.link.Name, errMsg)
		}
		fmt.Fprintf(os.Stderr, "\n  \u2192 Download manually and install with: moxie install %d <path-to-archive>\n", id)
		os.Exit(1)
	}

	// Download completed
	dlRecord.Status = db.DownloadStatusCompleted
	dlRecord.PercentComplete = 100
	dlRecord.CompletedAt = time.Now()
	database.UpdateDownload(dlRecord)

	log.Info("download succeeded", "game_id", id, "host", dlRecord.Host)
	fmt.Fprintf(os.Stderr, "\n\nDownload completed!\n")

	// Auto-extract if enabled and it's an archive
	if *extract {
		downloadedFile := findDownloadedFile(destDir, dlRecord.Filename)
		if downloadedFile != "" && archive.IsArchiveFile(downloadedFile) {
			fmt.Fprintf(os.Stderr, "\nExtracting archive...\n")
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
						fmt.Fprintf(os.Stderr, "\r  Extracting: %d/%d files (%.1f%%) - %-60s",
							extractedFiles, totalFiles, percent, displayFile)
					}
				},
			})

			if err != nil {
				fmt.Fprintf(os.Stderr, "\nExtraction warning: %v\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "\n  Extracted %d files to: %s\n", result.FilesExtracted, result.Destination)
				os.Remove(downloadedFile)
				fmt.Fprintf(os.Stderr, "  Removed archive: %s\n", filepath.Base(downloadedFile))

				// Merge extracted files into game directory, preserving saves/configs
				fmt.Fprintf(os.Stderr, "\nMerging update into %s...\n", game.Path)
				mergeResult, mergeErr := updater.Merge(game.Path, string(game.Engine), result.Destination, true)
				if mergeErr != nil {
					fmt.Fprintf(os.Stderr, "  Merge warning: %v\n", mergeErr)
				} else {
					fmt.Fprintf(os.Stderr, "  Files updated: %d  |  User files preserved: %d\n", mergeResult.FilesCopied, mergeResult.FilesPreserved)
					if mergeResult.BackupPath != "" {
						fmt.Fprintf(os.Stderr, "  Backup saved to: %s\n", mergeResult.BackupPath)
					}
				}
			}
		}
	}

	fmt.Fprintf(os.Stderr, "\nGame ready at: %s\n", destDir)
}


