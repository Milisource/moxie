package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/mili/moxie/internal/archive"
	"github.com/mili/moxie/internal/browserresolve"
	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/downloader"
	"github.com/mili/moxie/internal/extensionbridge"
	"github.com/mili/moxie/internal/log"
	"github.com/mili/moxie/internal/scraper"
	"github.com/mili/moxie/internal/updater"
)

// prepareDownloader wires the process-wide downloader services used by every
// command that downloads: the DB-backed masked-URL cache and the browser
// fallback for challenge-graded links. Both are optional; when no browser is
// available the Go path still runs and reports the challenge.
func prepareDownloader(database *db.Database) {
	downloader.SetDefaultResolvedCache(
		database.GetResolvedURL,
		func(maskedURL, resolved, host string) {
			if err := database.PutResolvedURL(maskedURL, resolved, host); err != nil {
				log.Warn("failed to cache resolved masked URL", "error", err)
			}
		},
	)
	if installed, reason := browserresolve.InstallDownloaderFallback(); !installed {
		log.Debug("downloader browser fallback unavailable", "reason", reason)
	}
}

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
	extBridge := fs.Bool("extension", false, "Resolve /dl/ check pages in your browser via the moxie-bridge extension, then download automatically (implies browser-assisted flow)")
	watch := fs.Bool("watch", false, "Watch download directory for new files and auto-install (use with --open)")
	browserDir := fs.String("browser-dir", "", "Browser download directory to watch (default: ~/Downloads)")
	verbose := fs.Bool("verbose", false, "Verbose logging (debug level)")
	fs.Parse(args)

	if *verbose {
		log.SetLevel(slog.LevelDebug)
	}

	database := OpenDB()
	defer database.Close()

	prepareDownloader(database)

	// --extension: resolve F95Zone /dl/ check pages in the user's own browser
	// (where hCaptcha passes) instead of the automated browser. Overrides the
	// browser /dl/ solver installed above; the rest of the pipeline is
	// unchanged.
	if *extBridge {
		bridge, err := extensionbridge.New(extensionbridge.WithOpener(openURL))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error starting the extension bridge: %v\n", err)
			os.Exit(1)
		}
		defer bridge.Close()
		var extTimedOut bool
		downloader.SetDefaultDLSolver(func(ctx context.Context, dlURL string) (string, error) {
			if extTimedOut {
				return "", fmt.Errorf("extension check already timed out — re-run `moxie download --extension` when you can pass the check")
			}
			fmt.Fprintf(os.Stderr, "  Opening F95Zone's check in your browser (port %d) — pass it there and the download continues automatically...\n", bridge.Port())
			dest, err := bridge.Resolve(ctx, dlURL)
			if errors.Is(err, context.DeadlineExceeded) {
				extTimedOut = true
			}
			return dest, err
		})
		fmt.Fprintf(os.Stderr, "Extension bridge listening on 127.0.0.1:%d\n", bridge.Port())
	}

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
		downloadSingle(database, game, cookie, *downloadDir, string(targetPlatform), *extract, nil)
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
	var client *scraper.Client
	if cookie != "" {
		client = scraper.NewClient(cookie)
	}
	ok, failed := 0, 0
	for i := range queue {
		g := &queue[i]
		fmt.Fprintf(os.Stderr, "[%d/%d] %s: %s → %s\n", i+1, len(queue), g.Title, g.Version, g.LatestVersion)
		if err := downloadSingle(database, g, cookie, downloadDir, targetPlatform, extract, client); err != nil {
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
func downloadSingle(database *db.Database, game *db.Game, cookie, downloadDir, targetPlatform string, extract bool, client *scraper.Client) error {
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
		// Always refresh from the thread when a session is available: stored
		// links (masked or /dl/) point at the revision that was live when
		// they were scraped, so a game updated since the last sync would
		// otherwise download the previous build. On scrape failure the
		// stored links still fall through to the ranking below.
		if cookie == "" {
			selectedLink = selectBestLinkByPlatform(links, platform)
		}
	}

	// Fall back to scraping F95Zone
	if selectedLink == nil && game.F95URL != "" {
		if cookie != "" {
			fmt.Fprintf(os.Stderr, "  Fetching download links from F95Zone...\n")
			if client == nil {
				client = scraper.NewClient(cookie)
			}
			scrapeURL := scraper.ResolveScrapeURL(game.F95URL, game.F95ThreadID)
			data, err := client.ScrapeThread(scrapeURL)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: Could not scrape F95Zone: %v\n", err)
			} else if len(data.DownloadLinks) > 0 {
				if err := database.DeleteDownloadLinksByGameID(game.ID); err != nil {
					fmt.Fprintf(os.Stderr, "  ! Failed to clear stale download links: %v\n", err)
				}
				for _, dl := range data.DownloadLinks {
					linkPlatform := db.Platform(downloader.DetectPlatform(dl.Name, dl.URL))
					link := &db.DownloadLink{
						GameID:   game.ID,
						URL:      dl.URL,
						Host:     dl.Host,
						Name:     dl.Name,
						Size:     dl.Size,
						Platform: db.Platform(linkPlatform),
					}
					database.CreateDownloadLink(link)
				}
				// The thread page exposes only browser-checked /dl/ links;
				// add the cache API's still-unwrappable /masked/ links for
				// the same version so a failed check still has a path.
				if dataHasF95CheckLinks(data.DownloadLinks) {
					if added := addCacheDownloadLinks(database, game, data, cookie); added > 0 {
						fmt.Fprintf(os.Stderr, "  Added %d cache fallback link(s)\n", added)
					}
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

	progressFn := func(p downloader.Progress) {
		dlRecord.BytesDownloaded = p.BytesDownloaded
		dlRecord.TotalBytes = p.TotalBytes
		dlRecord.SpeedBytesPerSec = p.SpeedBytesPerSec
		dlRecord.PercentComplete = p.Percent
		database.UpdateDownload(dlRecord)
		renderProgressBar(p)
	}

	// Multi-part archives: download every part of the best group (with
	// per-part host fallback); on success skip the single-link loop and go
	// straight to install. On failure fall back to single links.
	var lastErr error
	var failures []string
	var multiPartFinal string
	groupLinks := make([]db.DownloadLink, 0, len(scored))
	for _, sl := range scored {
		groupLinks = append(groupLinks, sl.link)
	}
	if groups := downloader.GroupMultiPartLinks(groupLinks); len(groups) > 0 {
		group := groups[0]
		fmt.Fprintf(os.Stderr, "  Multi-part archive: %d parts (%s)\n", len(group.Parts), group.Prefix)
		finalFile, err := downloader.DownloadMultiPart(group, destDir, progressFn, cookie)
		if err == nil && downloader.IsValidGameFile(finalFile) {
			multiPartFinal = finalFile
			dlRecord.Filename = filepath.Base(finalFile)
			dlRecord.Status = db.DownloadStatusDownloading
			database.UpdateDownload(dlRecord)
		} else {
			if err == nil {
				os.Remove(finalFile)
				err = fmt.Errorf("downloaded content is not a valid game file")
			}
			lastErr = err
			failures = append(failures, err.Error())
			fmt.Fprintf(os.Stderr, "  ✗ Multi-part download failed: %v (trying single links)\n", err)
		}
	}

	if multiPartFinal == "" {
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

			err = downloader.Download(link.URL, destDir, link.Size, progressFn, cookie)
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
	} // end multiPartFinal == ""

	if lastErr != nil {
		dlRecord.Status = db.DownloadStatusFailed
		dlRecord.Error = lastErr.Error()
		dlRecord.CompletedAt = time.Now()
		database.UpdateDownload(dlRecord)

		log.Error("download failed (all links exhausted)", "game_id", game.ID, "links_tried", len(scored), "error", lastErr)
		fmt.Fprintf(os.Stderr, "  ✗ Failed: %s\n", lastErr.Error())
		if containsF95CheckLink(groupLinks) {
			fmt.Fprintf(os.Stderr, "  F95Zone now gates every download link behind a human hCaptcha check.\n")
			fmt.Fprintf(os.Stderr, "  Open the game's links in your browser instead: moxie download --open %d\n", game.ID)
		}
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
		downloadedFile := multiPartFinal
		if downloadedFile == "" {
			downloadedFile = findDownloadedFile(destDir, dlRecord.Filename)
		}
		if downloadedFile != "" && !archive.IsArchiveFile(downloadedFile) {
			// A plain executable/patch cannot be merged automatically. Report
			// it so auto-update does not claim success and re-queue forever.
			fmt.Fprintf(os.Stderr, "  Downloaded file is not an archive: %s\n", downloadedFile)
			fmt.Fprintf(os.Stderr, "  Install it manually: moxie install %d %s\n", game.ID, downloadedFile)
			return fmt.Errorf("downloaded file is not an archive; install manually")
		}
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
				dlRecord.Status = db.DownloadStatusFailed
				dlRecord.Error = err.Error()
				dlRecord.CompletedAt = time.Now()
				database.UpdateDownload(dlRecord)
				fmt.Fprintf(os.Stderr, "\n    Extraction failed: %v\n", err)
				fmt.Fprintf(os.Stderr, "    Archive kept: %s\n", downloadedFile)
				return fmt.Errorf("extract %s: %w", filepath.Base(downloadedFile), err)
			}
			fmt.Fprintf(os.Stderr, "\n    Extracted %d files\n", result.FilesExtracted)
			defer os.RemoveAll(result.Destination)

			fmt.Fprintf(os.Stderr, "  Merging update into %s...\n", game.Path)
			mergeResult, mergeErr := updater.Merge(context.Background(), game.Path, string(game.Engine), result.Destination, true)
			if mergeErr != nil {
				dlRecord.Status = db.DownloadStatusFailed
				dlRecord.Error = mergeErr.Error()
				dlRecord.CompletedAt = time.Now()
				database.UpdateDownload(dlRecord)
				fmt.Fprintf(os.Stderr, "    Merge failed: %v\n", mergeErr)
				fmt.Fprintf(os.Stderr, "    Version left at %q. Archive kept: %s\n", game.Version, downloadedFile)
				return fmt.Errorf("merge update: %w", mergeErr)
			}
			fmt.Fprintf(os.Stderr, "    Files updated: %d  |  User files preserved: %d\n", mergeResult.FilesCopied, mergeResult.FilesPreserved)
			if mergeResult.BackupPath != "" {
				fmt.Fprintf(os.Stderr, "    Backup: %s\n", mergeResult.BackupPath)
			}

			// Record the installed version so the game leaves the update
			// queue; only now is it safe to delete the archive.
			if installed, verr := recordInstalledVersion(database, game, downloadedFile); verr != nil {
				log.Warn("failed to record installed version", "game_id", game.ID, "error", verr)
				fmt.Fprintf(os.Stderr, "    Warning: could not record version: %v\n", verr)
			} else if installed != "" {
				fmt.Fprintf(os.Stderr, "    Version: %s\n", installed)
			}
			if rerr := os.Remove(downloadedFile); rerr != nil {
				fmt.Fprintf(os.Stderr, "    Warning: could not remove archive: %v\n", rerr)
			}
			dlRecord.Status = db.DownloadStatusCompleted
			dlRecord.PercentComplete = 100
			dlRecord.CompletedAt = time.Now()
			database.UpdateDownload(dlRecord)
		} else if downloadedFile != "" {
			// Single-file HTML games: the payload is the game, not an archive.
			if !applyHTMLGameInstall(database, game, downloadedFile) {
				// A plain executable/patch cannot be merged automatically.
				// Report it so auto-update does not claim success and requeue.
				fmt.Fprintf(os.Stderr, "  Downloaded file is not an archive or HTML game: %s\n", downloadedFile)
				fmt.Fprintf(os.Stderr, "  Install it manually: moxie install %d %s\n", game.ID, downloadedFile)
				return fmt.Errorf("downloaded file is not an archive or HTML game; install manually")
			}
		}
	}

	fmt.Fprintf(os.Stderr, "  ✓ Ready: %s\n", destDir)
	return nil
}
