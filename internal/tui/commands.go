package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mili/moxie/internal/archive"
	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/downloader"
	"github.com/mili/moxie/internal/log"
	"github.com/mili/moxie/internal/scanner"
	"github.com/mili/moxie/internal/updater"
)

// loadGames fetches game summaries from the database, optionally filtered by
// the current collection.
func (m model) loadGames() tea.Cmd {
	return func() tea.Msg {
		var games []db.GameSummary
		var err error
		if m.collectionFilter != 0 {
			games, err = m.db.ListGameSummariesInCollection(m.collectionFilter)
		} else {
			games, err = m.db.ListGameSummaries("", "")
		}
		if err != nil {
			return errMsg{err}
		}
		return gamesLoadedMsg{games}
	}
}

// scanDirectory runs scanner.Scan on a directory and saves/updates games in the database.
func (m model) scanDirectory(dir string) tea.Cmd {
	return func() tea.Msg {
		games, err := scanner.Scan(context.Background(), dir)
		if err != nil {
			return errMsg{fmt.Errorf("scanning %s: %w", dir, err)}
		}

		var saved, updated int
		for _, g := range games {
			existing, err := m.db.GetGameByPath(g.Path)
			if err == nil && existing != nil {
				existing.Title = g.Title
				existing.Engine = string(g.Engine)
				existing.Version = orDefault(existing.Version, g.Version)
				existing.SizeBytes = g.SizeBytes
				if g.ExePath != "" {
					existing.ExePath = g.ExePath
				}
				if err := m.db.UpdateGame(existing); err == nil {
					updated++
				}
			} else if g.Path != "" {
				game := &db.Game{
					Title:     strings.TrimSpace(g.Title),
					Engine:    string(g.Engine),
					Path:      g.Path,
					ExePath:   g.ExePath,
					Version:   g.Version,
					SizeBytes: g.SizeBytes,
					Status:    "unknown",
				}
				if _, err := m.db.InsertGame(game); err == nil {
					saved++
				}
			}
		}

		return scanCompletedMsg{saved: saved, updated: updated, total: len(games)}
	}
}

// loadCollections fetches all collections from the database.
func (m model) loadCollections() tea.Cmd {
	return func() tea.Msg {
		collections, err := m.db.ListCollections()
		if err != nil {
			return errMsg{err}
		}
		return collectionsLoadedMsg{collections}
	}
}

// loadDetailGame fetches a single game by ID for the detail view.
func (m model) loadDetailGame(id int64) tea.Cmd {
	return func() tea.Msg {
		game, err := m.db.GetGame(id)
		if err != nil {
			return errMsg{err}
		}
		return detailGameLoadedMsg{game}
	}
}

// loadMeta fetches scraped metadata for a game.
func (m model) loadMeta(id int64) tea.Cmd {
	return func() tea.Msg {
		meta, err := m.db.GetScrapedMeta(id)
		if err != nil {
			return errMsg{err}
		}
		return metaLoadedMsg{meta}
	}
}

// deleteGame deletes a game and reloads the game list.
// Uses tea.Sequence to ensure delete completes before reload.
func (m model) deleteGame(id int64) tea.Cmd {
	return tea.Sequence(
		func() tea.Msg {
			if err := m.db.DeleteGame(id); err != nil {
				return gameDeletedMsg{err: err}
			}
			return gameDeletedMsg{}
		},
		func() tea.Msg {
			games, err := m.db.ListGameSummaries("", "")
			if err != nil {
				return errMsg{err}
			}
			return gamesLoadedMsg{games}
		},
	)
}

// scrapeMeta scrapes an F95Zone URL for metadata and saves it.
func (m model) scrapeMeta(gameID int64, url string) tea.Cmd {
	return func() tea.Msg {
		if m.scraperClient == nil {
			return metaScrapedMsg{err: fmt.Errorf("scraper not available")}
		}
		data, err := m.scraperClient.ScrapeThread(url)
		if err != nil {
			return metaScrapedMsg{err: fmt.Errorf("scrape failed: %w", err)}
		}
		meta := &db.ScrapedMeta{
			GameID:    gameID,
			Developer: data.Developer,
			Overview:  data.Overview,
			CoverURL:  data.CoverURL,
		}
		if err := m.db.UpsertScrapedMeta(meta); err != nil {
			return metaScrapedMsg{err: fmt.Errorf("save metadata failed: %w", err)}
		}
		return metaScrapedMsg{meta: meta}
	}
}

// startDownloadCmd launches a download in a background goroutine, trying links in priority order.
// Progress is written to the model's activeDownloads map.
// After successful download + extract, merges new files into gamePath, preserving saves/configs.
func (m model) startDownloadCmd(gameID int64, links []db.DownloadLink, destDir, gamePath, engine, f95Cookie string) tea.Cmd {
	firstLink := links[0]
	log.Info("tui download started", "game_id", gameID, "host", firstLink.Host, "total_links", len(links))

	// Attach the DB-backed masked-URL unwrap cache: repeated downloads of
	// the same /masked/ link resolve from the DB instead of re-hitting
	// F95Zone's rate-limited unwrap endpoint. Idempotent; every resolver
	// created afterwards (including the one inside DownloadWithHost)
	// picks it up.
	downloader.SetDefaultResolvedCache(
		m.db.GetResolvedURL,
		func(maskedURL, resolved, host string) {
			if err := m.db.PutResolvedURL(maskedURL, resolved, host); err != nil {
				log.Warn("failed to cache resolved masked URL", "error", err)
			}
		},
	)

	dl := &db.Download{
		GameID:   gameID,
		URL:      firstLink.URL,
		Host:     firstLink.Host,
		DestPath: destDir,
		Status:   db.DownloadStatusDownloading,
	}
	dl.StartedAt = time.Now()
	dlID, err := m.db.CreateDownload(dl)
	if err != nil {
		return func() tea.Msg {
			return downloadStartedMsg{gameID: gameID, err: fmt.Errorf("create download record: %w", err)}
		}
	}
	dl.ID = dlID

	ad := &activeDownload{
		gameID:  gameID,
		url:     firstLink.URL,
		host:    firstLink.Host,
		destDir: destDir,
		status:  db.DownloadStatusDownloading,
		stepMsg: "Finding suitable host...",
	}
	m.activeDownloadsMu.Lock()
	existing, ok := m.activeDownloads[gameID]
	if !ok {
		m.activeDownloads[gameID] = ad
	}
	m.activeDownloadsMu.Unlock()
	if ok {
		// Upgrade the Pending reservation placed by handleDownloadKey
		// instead of replacing it, keeping the entry's identity stable
		// across the resolve → download flow.
		existing.mu.Lock()
		existing.gameID = gameID
		existing.url = firstLink.URL
		existing.host = firstLink.Host
		existing.destDir = destDir
		existing.status = db.DownloadStatusDownloading
		existing.progress = downloader.Progress{}
		existing.err = ""
		existing.stepMsg = "Finding suitable host..."
		existing.mu.Unlock()
		ad = existing
	}

	go func() {
		var lastErr error
		var failures []string
		for i, link := range links {
			if i > 0 {
				ad.mu.Lock()
				ad.url = link.URL
				ad.host = link.Host
				ad.status = db.DownloadStatusDownloading
				ad.progress = downloader.Progress{}
				ad.err = ""
				ad.stepMsg = fmt.Sprintf("Trying next: %s...", link.Host)
				ad.mu.Unlock()

				dl.URL = link.URL
				dl.Host = link.Host
				dl.Status = db.DownloadStatusDownloading
				dl.BytesDownloaded = 0
				dl.TotalBytes = 0
				dl.SpeedBytesPerSec = 0
				dl.PercentComplete = 0
				dl.Error = ""
				m.db.UpdateDownload(dl)
				log.Info("tui download fallback", "game_id", gameID, "attempt", i+1, "host", link.Host)
			} else {
				ad.mu.Lock()
				ad.stepMsg = fmt.Sprintf("Trying: %s...", link.Host)
				ad.mu.Unlock()
			}

			err := downloader.DownloadWithHost(link.URL, link.Host, destDir, link.Size, func(p downloader.Progress) {
				ad.mu.Lock()
				if p.BytesDownloaded > 0 {
					ad.stepMsg = "Downloading..."
				}
				ad.progress = p
				ad.mu.Unlock()
				dl.BytesDownloaded = p.BytesDownloaded
				dl.TotalBytes = p.TotalBytes
				dl.SpeedBytesPerSec = p.SpeedBytesPerSec
				dl.PercentComplete = p.Percent
				m.db.UpdateDownload(dl)
			}, f95Cookie)

			if err == nil {
				downloadedFile := findMostRecentFile(destDir)
				if downloadedFile != "" && !downloader.IsValidGameFile(downloadedFile) {
					os.Remove(downloadedFile)
					err = fmt.Errorf("downloaded content is not a valid game file (interstitial page)")
					log.Warn("tui download validation failed", "game_id", gameID, "host", link.Host, "file", filepath.Base(downloadedFile))
				}
			}

			if err == nil {
				lastErr = nil
				break
			}
			lastErr = err
			failures = append(failures, fmt.Sprintf("[%s] ✗ %s", link.Host, err.Error()))
			ad.mu.Lock()
			ad.stepMsg = fmt.Sprintf("✗ Failed: %s", link.Host)
			ad.mu.Unlock()
			log.Warn("tui download attempt failed", "game_id", gameID, "host", link.Host, "error", err)
		}

		var summary string
		ad.mu.Lock()
		if lastErr != nil {
			summary = fmt.Sprintf("All %d download links failed:\n", len(links))
			for _, f := range failures {
				summary += "  " + f + "\n"
			}
			if len(links) > 0 {
				summary += "\n  → Press [y] to open the link in your browser, save the file,"
				summary += "\n    and moxie will detect and install it automatically."
			}
			ad.status = db.DownloadStatusFailed
			ad.err = summary
			ad.stepMsg = "✗ All links failed"
			dl.Status = db.DownloadStatusFailed
			dl.Error = lastErr.Error()
			log.Error("tui download failed (all links exhausted)", "game_id", gameID, "links_tried", len(links), "error", lastErr)
		} else {
			ad.status = db.DownloadStatusCompleted
			ad.progress.Percent = 100
			ad.stepMsg = "✓ Download succeeded!"
			dl.Status = db.DownloadStatusCompleted
			dl.PercentComplete = 100
			log.Info("tui download succeeded", "game_id", gameID, "host", dl.Host)

			// Auto-extract if the downloaded file is an archive
			downloadedFile := findMostRecentFile(destDir)
			if downloadedFile != "" && archive.IsArchiveFile(downloadedFile) {
				ad.mu.Unlock() // don't hold the lock across extraction/merge
				if errMsg := m.installArchive(ad, dl, downloadedFile, destDir, gamePath, engine); errMsg != "" {
					ad.mu.Lock()
					ad.status = db.DownloadStatusFailed
					ad.err = errMsg
					ad.stepMsg = "✗ Install failed"
					ad.mu.Unlock()
					dl.Status = db.DownloadStatusFailed
					dl.Error = errMsg
				}
				ad.mu.Lock() // rebalance: the final unlock below expects the lock held
			}
		}
		dl.CompletedAt = time.Now()
		m.db.UpdateDownload(dl)
		ad.mu.Unlock()

		if lastErr != nil {
			// Universal fallback: every resolver path failed (challenge,
			// captcha, dead host). Surface the best link so the TUI can
			// offer to open it in the user's real browser — the browser
			// has full clearance and can click through anything.
			m.watcherMsgCh <- downloadFinishedMsg{
				gameID:   gameID,
				url:      links[0].URL,
				summary:  summary,
				destDir:  destDir,
				gamePath: gamePath,
				engine:   engine,
			}
		}
	}()

	return func() tea.Msg {
		return downloadStartedMsg{gameID: gameID}
	}
}

// installArchive extracts a downloaded archive into destDir, merges the
// result into the game directory (preserving saves/configs) and removes
// the archive. Progress is reported through ad, and through dl when it is
// non-nil (the download-dir watcher path has no download record). The
// caller must NOT hold ad.mu. Returns "" on success or a user-facing error
// message.
func (m model) installArchive(ad *activeDownload, dl *db.Download, archivePath, destDir, gamePath, engine string) string {
	ad.mu.Lock()
	ad.stepMsg = "Extracting archive..."
	ad.status = db.DownloadStatusExtracting
	ad.mu.Unlock()
	if dl != nil {
		dl.Status = db.DownloadStatusExtracting
		dl.Error = ""
		m.db.UpdateDownload(dl)
	}

	log.Info("extracting archive", "file", filepath.Base(archivePath))
	result, extractErr := archive.Extract(archivePath, destDir, archive.Options{})
	if extractErr != nil {
		log.Warn("extraction failed", "file", filepath.Base(archivePath), "error", extractErr)
		return fmt.Sprintf("Download succeeded, but extraction failed: %v", extractErr)
	}
	log.Info("extraction complete", "files", result.FilesExtracted, "dest", result.Destination)
	os.Remove(archivePath)

	ad.mu.Lock()
	ad.stepMsg = "Merging into game directory..."
	ad.mu.Unlock()
	mergeResult, mergeErr := updater.Merge(gamePath, engine, result.Destination, true)
	if mergeErr != nil {
		log.Warn("merge failed", "game", gamePath, "error", mergeErr)
		return fmt.Sprintf("Download succeeded, but merging into the game directory failed: %v", mergeErr)
	}
	log.Info("merge complete", "game", gamePath, "copied", mergeResult.FilesCopied, "preserved", mergeResult.FilesPreserved)

	ad.mu.Lock()
	ad.status = db.DownloadStatusCompleted
	ad.progress.Percent = 100
	ad.mu.Unlock()
	return ""
}

// pollDownloads returns a Tick that triggers periodic re-renders while downloads are active.
func (m model) pollDownloads() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return downloadProgressMsg{}
	})
}

// releasePendingReservation removes the Pending-status reservation for
// gameID, if one exists. Reservations are placed synchronously by
// handleDownloadKey to guard against double-starts; they must be released
// whenever the resolve/download flow fails before startDownloadCmd had a
// chance to upgrade them into a real download entry.
func (m model) releasePendingReservation(gameID int64) {
	m.activeDownloadsMu.Lock()
	defer m.activeDownloadsMu.Unlock()
	ad, ok := m.activeDownloads[gameID]
	if !ok {
		return
	}
	ad.mu.Lock()
	status := ad.status
	ad.mu.Unlock()
	if status == db.DownloadStatusPending {
		delete(m.activeDownloads, gameID)
	}
}

// hasActiveDownloads checks if any downloads are in progress.
func (m model) hasActiveDownloads() bool {
	m.activeDownloadsMu.Lock()
	defer m.activeDownloadsMu.Unlock()
	for _, ad := range m.activeDownloads {
		ad.mu.Lock()
		status := ad.status
		ad.mu.Unlock()
		if status == db.DownloadStatusDownloading || status == db.DownloadStatusPending {
			return true
		}
	}
	return false
}

// getDownloadProgress returns a snapshot of a download's progress.
func (m model) getDownloadProgress(gameID int64) (downloader.Progress, db.DownloadStatus, string, string) {
	m.activeDownloadsMu.Lock()
	ad, ok := m.activeDownloads[gameID]
	m.activeDownloadsMu.Unlock()
	if !ok {
		return downloader.Progress{}, "", "", ""
	}
	ad.mu.Lock()
	defer ad.mu.Unlock()
	return ad.progress, ad.status, ad.err, ad.stepMsg
}

// resolveLinksCmd resolves download links in a background goroutine and
// emits a downloadLinksMsg. The fallback path scrapes F95Zone, so this
// must never run synchronously inside Update — a rate-limited scrape
// would stall the whole TUI for seconds.
func (m model) resolveLinksCmd(game *db.Game, destDir string) tea.Cmd {
	return func() tea.Msg {
		links, err := m.resolveDownloadLinks(game)
		return downloadLinksMsg{
			gameID:   game.ID,
			links:    links,
			destDir:  destDir,
			gamePath: game.Path,
			engine:   game.Engine,
			err:      err,
		}
	}
}

// resolveDownloadLinks finds all viable download links for a game, sorted by platform score.
func (m model) resolveDownloadLinks(game *db.Game) ([]db.DownloadLink, error) {
	links, listErr := m.db.ListDownloadLinks(game.ID, "", false)
	if listErr == nil && len(links) > 0 {
		targetPlatform := downloader.CurrentPlatform()
		sorted := sortLinksByPlatform(links, targetPlatform)
		if len(sorted) > 0 {
			return sorted, nil
		}
	}

	if game.F95URL != "" && m.scraperClient != nil {
		data, scrapeErr := m.scraperClient.ScrapeThread(game.F95URL)
		if scrapeErr == nil && len(data.DownloadLinks) > 0 {
			m.db.DeleteDownloadLinksByGameID(game.ID)
			for _, dl := range data.DownloadLinks {
				link := &db.DownloadLink{
					GameID: game.ID,
					URL:    dl.URL,
					Host:   dl.Host,
					Name:   dl.Name,
					Size:   dl.Size,
				}
				m.db.CreateDownloadLink(link)
			}
			links, _ = m.db.ListDownloadLinks(game.ID, "", false)
			targetPlatform := downloader.CurrentPlatform()
			sorted := sortLinksByPlatform(links, targetPlatform)
			if len(sorted) > 0 {
				return sorted, nil
			}
		}
	}

	return nil, fmt.Errorf("no download links found")
}

// sortLinksByPlatform returns all links sorted by platform + host reliability score (descending).
// Skips online-only links that aren't downloadable.
func sortLinksByPlatform(links []db.DownloadLink, targetPlatform downloader.Platform) []db.DownloadLink {
	type sl struct {
		link  db.DownloadLink
		score int
	}
	var scored []sl
	for _, link := range links {
		if downloader.IsOnlineOnly(link.Name, link.URL) {
			continue
		}
		score := downloader.ScoreDownloadLink(link, targetPlatform)
		scored = append(scored, sl{link, score})
	}
	if len(scored) == 0 {
		return nil
	}
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})
	result := make([]db.DownloadLink, len(scored))
	for i, s := range scored {
		result[i] = s.link
	}
	return result
}

// findMostRecentFile returns the path of the most recently modified regular file
// in a directory, or empty string if the directory is empty/unreadable.
func findMostRecentFile(dir string) string {
	return downloader.FindMostRecentFile(dir)
}

// ─── Browser fallback & download-dir watchers ───────────────────────────

// browserFallback tracks a game whose auto-download exhausted every link.
// The user is offered to open the best link in their real browser; a
// download-dir watcher then picks up the browser-saved archive and the
// existing validate → extract → merge pipeline installs it.
type browserFallback struct {
	gameID   int64
	url      string
	destDir  string
	gamePath string
	engine   string

	watching bool                     // browser opened + watcher started
	watcher  *downloader.ArchiveWatcher
}

// openInBrowser opens a URL in the user's default browser.
// Platform-specific: xdg-open (linux), open (macOS), cmd /c start (windows).
func openInBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("cmd", "/c", "start", "", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

// openBrowser launches the platform browser. It is a package variable so
// tests can stub it without spawning a real browser.
var openBrowser = openInBrowser

// confirmBrowserOpen opens the failed download's best link in the user's
// real browser and starts an ArchiveWatcher on the game's download dir so
// the browser-saved archive is detected and installed automatically.
func (m model) confirmBrowserOpen(fb *browserFallback) (tea.Model, tea.Cmd) {
	if err := openBrowser(fb.url); err != nil {
		m.err = fmt.Errorf("open in browser: %v", err)
		return m, nil
	}
	fb.watching = true

	w := downloader.NewArchiveWatcher(fb.destDir,
		downloader.WithDebounce(downloader.DefaultDebounce),
		downloader.WithOnArchive(func(path string) {
			m.watcherMsgCh <- watcherFoundMsg{
				gameID:   fb.gameID,
				path:     path,
				destDir:  fb.destDir,
				gamePath: fb.gamePath,
				engine:   fb.engine,
			}
		}),
	)
	if err := w.Start(); err != nil {
		fb.watching = false
		m.err = fmt.Errorf("start download watcher: %v", err)
		return m, nil
	}
	fb.watcher = w
	m.gameWatchers[fb.gameID] = w

	m.notice = fmt.Sprintf("Opened in browser — save the file into %s and moxie will install it automatically", fb.destDir)
	return m, m.armPump()
}

// hasWatchers reports whether any download-dir watcher is active.
func (m model) hasWatchers() bool {
	return len(m.gameWatchers) > 0
}

// watcherPumpIdle is how long the message pump waits for an event before
// re-evaluating whether it is still needed (downloads/watchers active).
const watcherPumpIdle = time.Second

// armPump returns the watcher-message pump cmd, or nil when a pump is
// already in flight. Exactly one pump may run at a time: it forwards
// download terminal messages and download-dir watcher events from
// watcherMsgCh into the Update loop. The armed flag is reset before a
// message is delivered so handlers can re-arm the pump.
func (m model) armPump() tea.Cmd {
	if !m.pumpArmed.CompareAndSwap(false, true) {
		return nil
	}
	return func() tea.Msg {
		defer m.pumpArmed.Store(false)
		t := time.NewTimer(watcherPumpIdle)
		defer t.Stop()
		select {
		case msg, ok := <-m.watcherMsgCh:
			if !ok {
				return nil
			}
			return msg
		case <-t.C:
			return watcherIdleMsg{}
		}
	}
}

// installBrowserArchiveCmd runs the validate → extract → merge pipeline for
// an archive that appeared in a game's download dir (a browser-saved
// download picked up by the ArchiveWatcher).
func (m model) installBrowserArchiveCmd(msg watcherFoundMsg) tea.Cmd {
	return func() tea.Msg {
		if !downloader.IsValidGameFile(msg.path) {
			log.Warn("watcher: picked-up file is not a valid game archive", "path", msg.path)
			return watcherInstalledMsg{gameID: msg.gameID, err: fmt.Errorf("file is not a valid game archive: %s", filepath.Base(msg.path))}
		}

		ad := m.ensureActiveDownload(msg.gameID, msg.destDir)
		if errMsg := m.installArchive(ad, nil, msg.path, msg.destDir, msg.gamePath, msg.engine); errMsg != "" {
			ad.mu.Lock()
			ad.status = db.DownloadStatusFailed
			ad.err = errMsg
			ad.stepMsg = "✗ Install failed"
			ad.mu.Unlock()
			return watcherInstalledMsg{gameID: msg.gameID, err: fmt.Errorf("%s", errMsg)}
		}
		ad.mu.Lock()
		ad.stepMsg = "✓ Browser download installed and merged"
		ad.mu.Unlock()
		return watcherInstalledMsg{gameID: msg.gameID}
	}
}

// ensureActiveDownload returns the active-download entry for gameID,
// creating a fresh one (so the detail view reports through it) if none
// exists.
func (m model) ensureActiveDownload(gameID int64, destDir string) *activeDownload {
	m.activeDownloadsMu.Lock()
	defer m.activeDownloadsMu.Unlock()
	if ad, ok := m.activeDownloads[gameID]; ok {
		return ad
	}
	ad := &activeDownload{
		gameID:  gameID,
		destDir: destDir,
		status:  db.DownloadStatusExtracting,
		stepMsg: "Browser download detected — installing...",
	}
	m.activeDownloads[gameID] = ad
	return ad
}
