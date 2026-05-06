package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mili/moxie/internal/archive"
	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/downloader"
	"github.com/mili/moxie/internal/log"
	"github.com/mili/moxie/internal/updater"
)

// loadGames fetches all games from the database.
func (m model) loadGames() tea.Cmd {
	return func() tea.Msg {
		games, err := m.db.ListGames("", "")
		if err != nil {
			return errMsg{err}
		}
		return gamesLoadedMsg{games}
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

// deleteGame deletes a game and reloads the full list.
func (m model) deleteGame(id int64) tea.Cmd {
	return func() tea.Msg {
		if err := m.db.DeleteGame(id); err != nil {
			return gameDeletedMsg{err: err}
		}
		games, err := m.db.ListGames("", "")
		if err != nil {
			return gameDeletedMsg{err: err}
		}
		return gameDeletedMsg{games: games}
	}
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
	m.activeDownloads[gameID] = ad

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

			err := downloader.DownloadWithHost(link.URL, link.Host, destDir, 0, func(p downloader.Progress) {
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
				// Validate the downloaded file — reject interstitial HTML pages
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

		ad.mu.Lock()
		if lastErr != nil {
			summary := fmt.Sprintf("All %d download links failed:\n", len(links))
			for _, f := range failures {
				summary += "  " + f + "\n"
			}
			if len(links) > 0 {
				summary += "\n  → Download manually and run:\n"
				summary += fmt.Sprintf("    moxie install %d <path-to-file>", gameID)
			}
			ad.status = db.DownloadStatusFailed
			ad.err = summary
			ad.stepMsg = "✗ All links failed"
			ad.completedAt = time.Now()
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
				ad.stepMsg = "Extracting archive..."
				ad.status = db.DownloadStatusExtracting
				dl.Status = db.DownloadStatusExtracting
				m.db.UpdateDownload(dl)
				ad.mu.Unlock()

				log.Info("extracting archive", "file", filepath.Base(downloadedFile))
				result, extractErr := archive.Extract(downloadedFile, destDir, archive.Options{})
				ad.mu.Lock()
				if extractErr != nil {
					log.Warn("extraction failed", "file", filepath.Base(downloadedFile), "error", extractErr)
				} else {
					log.Info("extraction complete", "files", result.FilesExtracted, "dest", result.Destination)
					os.Remove(downloadedFile)

					// Merge extracted files into game directory, preserving saves
					ad.mu.Unlock()
					ad.mu.Lock()
					ad.stepMsg = "Merging into game directory..."
					ad.mu.Unlock()
					mergeResult, mergeErr := updater.Merge(gamePath, engine, result.Destination, true)
					ad.mu.Lock()
					if mergeErr != nil {
						log.Warn("merge failed", "game", gamePath, "error", mergeErr)
					} else {
						log.Info("merge complete", "game", gamePath, "copied", mergeResult.FilesCopied, "preserved", mergeResult.FilesPreserved)
					}
				}
				ad.status = db.DownloadStatusCompleted
				ad.progress.Percent = 100
				ad.completedAt = time.Now()
				dl.Status = db.DownloadStatusCompleted
				dl.PercentComplete = 100
			}
		}
		dl.CompletedAt = time.Now()
		m.db.UpdateDownload(dl)
		ad.mu.Unlock()
	}()

	return func() tea.Msg {
		return downloadStartedMsg{gameID: gameID}
	}
}

// pollDownloads returns a Tick that triggers periodic re-renders while downloads are active.
func (m model) pollDownloads() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return downloadProgressMsg{}
	})
}

// hasActiveDownloads checks if any downloads are in progress.
func (m model) hasActiveDownloads() bool {
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
	ad, ok := m.activeDownloads[gameID]
	if !ok {
		return downloader.Progress{}, "", "", ""
	}
	ad.mu.Lock()
	defer ad.mu.Unlock()
	return ad.progress, ad.status, ad.err, ad.stepMsg
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
