package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mili/moxie/internal/archive"
	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/downloader"
	"github.com/mili/moxie/internal/log"
	"github.com/mili/moxie/internal/scanner"
	"github.com/mili/moxie/internal/scraper"
	"github.com/mili/moxie/internal/updater"
)

// resolveBestLink finds the best download link for a game by checking the
// database first, then falling back to scraping F95Zone. Returns nil if no
// link is found.
func resolveBestLink(database *db.Database, game db.Game, cookie, targetPlatform string) *watchedGame {
	platform := downloader.Platform(targetPlatform)

	links, err := database.ListDownloadLinks(game.ID, "", false)
	if err != nil {
		return nil
	}

	var selectedLink *db.DownloadLink
	if len(links) > 0 {
		selectedLink = selectBestLinkByPlatform(links, platform)
	}

	// Fall back to scraping F95Zone.
	if selectedLink == nil && game.F95URL != "" && cookie != "" {
		fmt.Fprintf(os.Stderr, "  Fetching download links...\n")
		client := scraper.NewClient(cookie)
		scrapeURL := scraper.ResolveScrapeURL(game.F95URL, game.F95ThreadID)
		data, err := client.ScrapeThread(scrapeURL)
		if err == nil && len(data.DownloadLinks) > 0 {
			database.DeleteDownloadLinksByGameID(game.ID)
			for _, dl := range data.DownloadLinks {
				linkPlatform := downloader.DetectPlatformFromLink(dl.Name, dl.URL)
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

	if selectedLink == nil {
		return nil
	}

	return &watchedGame{
		Game:     game,
		LinkURL:  selectedLink.URL,
		LinkName: selectedLink.Name,
	}
}

// downloadAllOpen finds all games with pending updates, opens their download
// links in the browser, and optionally watches for the downloaded archives.
func downloadAllOpen(database *db.Database, cookie, targetPlatform, watchDir string, extract, doWatch bool) {
	games, err := database.ListActiveGames("", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing games: %v\n", err)
		os.Exit(1)
	}

	var watched []watchedGame
	for _, g := range games {
		if g.LatestVersion != "" && g.Version != "" && g.Version != g.LatestVersion {
			if wg := resolveBestLink(database, g, cookie, targetPlatform); wg != nil {
				watched = append(watched, *wg)
				fmt.Fprintf(os.Stderr, "  %s: %s → %s  [%s]\n", g.Title, g.Version, g.LatestVersion, wg.LinkName)
			} else {
				fmt.Fprintf(os.Stderr, "  %s: %s → %s  (no download link)\n", g.Title, g.Version, g.LatestVersion)
			}
		}
	}

	if len(watched) == 0 {
		fmt.Fprintln(os.Stderr, "No games with pending updates and available links.")
		return
	}

	fmt.Fprintf(os.Stderr, "\nOpening %d download(s) in your browser...\n\n", len(watched))
	for i, wg := range watched {
		fmt.Fprintf(os.Stderr, "  [%d/%d] %s\n", i+1, len(watched), wg.Game.Title)
		if err := openURL(wg.LinkURL); err != nil {
			fmt.Fprintf(os.Stderr, "    ✗ Failed to open: %v\n", err)
		}
	}

	fmt.Fprintf(os.Stderr, "\nEach download opened in a new tab.\n")

	if doWatch {
		watchAndInstall(watchDir, watched, database, extract, 10*time.Minute)
	} else {
		fmt.Fprintf(os.Stderr, "Save the files and moxie will detect them.\n")
		fmt.Fprintf(os.Stderr, "  Run with --watch to auto-install: moxie download --all --open --watch\n")
	}
}

// downloadSingleOpen resolves the best link for a single game and opens it
// in the browser. Optionally watches for the downloaded archive.
func downloadSingleOpen(database *db.Database, game *db.Game, cookie, targetPlatform, watchDir string, extract, doWatch bool) {
	wg := resolveBestLink(database, *game, cookie, targetPlatform)
	if wg == nil {
		fmt.Fprintf(os.Stderr, "No download links found for %q.\n", game.Title)
		return
	}

	fmt.Fprintf(os.Stderr, "Opening download link in browser...\n")
	if err := openURL(wg.LinkURL); err != nil {
		fmt.Fprintf(os.Stderr, "✗ Failed to open: %v\n", err)
		return
	}
	fmt.Fprintf(os.Stderr, "  ✓ Opened in browser\n")

	if doWatch {
		watchAndInstall(watchDir, []watchedGame{*wg}, database, extract, 10*time.Minute)
	} else {
		fmt.Fprintf(os.Stderr, "Save the file to %s and moxie will detect it.\n", watchDir)
		fmt.Fprintf(os.Stderr, "  Run with --watch to auto-install.\n")
	}
}

// openURL opens a URL in the user's default browser.
// Platform-specific: xdg-open (linux), open (macOS), cmd /c start (windows).
func openURL(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("cmd", "/c", "start", "", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

// ---------------------------------------------------------------------------
// Download watcher — polls a directory for new archives and auto-installs
// them into matching games. Designed for the "open in browser" download mode
// where the user saves files from browser tabs and moxie picks them up.
// ---------------------------------------------------------------------------

// watchedGame tracks a game that's waiting for a browser-downloaded archive.
type watchedGame struct {
	Game       db.Game
	LinkURL    string
	LinkName   string
	Downloaded bool
}

// openDownloadsBatch opens all links in the browser and watches for the files
// to be saved by the user. Each link opens in a new browser tab.
func openDownloadsBatch(games []watchedGame) {
	fmt.Fprintf(os.Stderr, "Opening %d download links in your browser...\n", len(games))
	for i, g := range games {
		if g.Downloaded {
			continue
		}
		fmt.Fprintf(os.Stderr, "  [%d/%d] %s\n", i+1, len(games), g.Game.Title)
		if err := openURL(g.LinkURL); err != nil {
			fmt.Fprintf(os.Stderr, "    ✗ Failed to open: %v\n", err)
		}
	}
	fmt.Fprintf(os.Stderr, "\nEach link opened in a new browser tab.\n")
	fmt.Fprintf(os.Stderr, "Save the files and moxie will auto-install them.\n")
}

// watchAndInstall monitors a directory for new archive files and auto-installs
// them into the corresponding game. Returns after all games are installed,
// or after idleTimeout with no new files.
func watchAndInstall(watchDir string, games []watchedGame, database *db.Database, extract bool, idleTimeout time.Duration) {
	if len(games) == 0 {
		return
	}

	os.MkdirAll(watchDir, 0755)

	seen := make(map[string]bool)
	for _, g := range games {
		if g.Downloaded {
			seen[g.LinkName] = true
		}
	}

	pending := len(games)
	lastActivity := time.Now()
	absDir, _ := filepath.Abs(watchDir)

	fmt.Fprintf(os.Stderr, "\nWatching %s for downloaded files...\n", absDir)
	fmt.Fprintf(os.Stderr, "  (moxie watches for new archives and installs them automatically)\n")
	if idleTimeout > 0 {
		fmt.Fprintf(os.Stderr, "  Stops after %v with no activity.\n", idleTimeout)
	}

	poll := time.NewTicker(3 * time.Second)
	defer poll.Stop()

	inactivityCheck := time.NewTicker(30 * time.Second)
	defer inactivityCheck.Stop()

	for pending > 0 {
		select {
		case <-poll.C:
			entries, err := os.ReadDir(absDir)
			if err != nil {
				continue
			}

			for _, e := range entries {
				if e.IsDir() || seen[e.Name()] {
					continue
				}

				fullPath := filepath.Join(absDir, e.Name())
				if !archive.IsArchiveFile(fullPath) {
					seen[e.Name()] = true
					continue
				}

				// Found a new archive — match to a pending game.
				matched := matchArchiveToGame(e.Name(), games)
				if matched == nil {
					continue
				}

				seen[e.Name()] = true
				lastActivity = time.Now()

				fmt.Fprintf(os.Stderr, "\n  ✓ Downloaded: %s\n", e.Name())
				fmt.Fprintf(os.Stderr, "    → Installing %q...\n", matched.Game.Title)

				installDownloadedArchive(database, &matched.Game, fullPath, extract)
				matched.Downloaded = true
				pending--
			}

		case <-inactivityCheck.C:
			if idleTimeout > 0 && time.Since(lastActivity) > idleTimeout {
				fmt.Fprintf(os.Stderr, "\n  No activity for %v — stopping.\n", idleTimeout)
				fmt.Fprintf(os.Stderr, "  %d game(s) remaining. Run again when files are ready.\n", pending)
				return
			}
		}
	}

	fmt.Fprintf(os.Stderr, "\n  ✓ All %d game(s) installed!\n", len(games))
}

// matchArchiveToGame finds a pending game whose title or download link name
// matches the archive filename. Tries exact link name first, then title match.
func matchArchiveToGame(filename string, games []watchedGame) *watchedGame {
	lower := strings.ToLower(filename)

	// Pass 1: exact link name match.
	for i := range games {
		if games[i].Downloaded {
			continue
		}
		if strings.EqualFold(filename, games[i].LinkName) {
			return &games[i]
		}
	}

	// Pass 2: game title (underscored or original) appears in filename.
	for i := range games {
		if games[i].Downloaded {
			continue
		}
		titleUnderscored := strings.ToLower(strings.ReplaceAll(games[i].Game.Title, " ", "_"))
		if strings.Contains(lower, titleUnderscored) {
			return &games[i]
		}
		if strings.Contains(lower, strings.ToLower(games[i].Game.Title)) {
			return &games[i]
		}
	}

	return nil
}

// installDownloadedArchive handles the full install flow for a downloaded
// archive: extract → merge → update version.
func installDownloadedArchive(database *db.Database, game *db.Game, archivePath string, doExtract bool) {
	destDir := filepath.Join(filepath.Dir(game.Path), "downloads")

	if doExtract {
		fmt.Fprintf(os.Stderr, "    Extracting...\n")
		result, err := archive.Extract(archivePath, destDir, archive.Options{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "    ✗ Extraction failed: %v\n", err)
			return
		}

		fmt.Fprintf(os.Stderr, "    Merging into %s...\n", game.Path)
		mergeResult, mergeErr := updater.Merge(game.Path, string(game.Engine), result.Destination, true)
		if mergeErr != nil {
			fmt.Fprintf(os.Stderr, "    ⚠ Merge warning: %v\n", mergeErr)
		} else {
			fmt.Fprintf(os.Stderr, "    Files updated: %d  |  Preserved: %d\n", mergeResult.FilesCopied, mergeResult.FilesPreserved)
		}

		os.Remove(archivePath)
	}

	// Extract version from archive filename for the most accurate version.
	archiveVer := scanner.ExtractVersion(filepath.Base(archivePath))
	if archiveVer != "" && archiveVer != game.Version {
		game.Version = archiveVer
	} else if game.LatestVersion != "" && game.LatestVersion != game.Version {
		game.Version = game.LatestVersion
	}

	if err := database.UpdateGame(game); err != nil {
		log.Warn("failed to update version after install", "game_id", game.ID, "error", err)
	} else {
		fmt.Fprintf(os.Stderr, "    ✓ Version updated to %s\n", game.Version)
	}

	fmt.Fprintf(os.Stderr, "    ✓ %q is up to date.\n", game.Title)
}
