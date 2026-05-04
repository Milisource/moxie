package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/downloader"
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

// startDownloadCmd launches a download in a background goroutine.
// Progress is written to the model's activeDownloads map.
func (m model) startDownloadCmd(gameID int64, url, host, destDir string) tea.Cmd {
	// Create download record
	dl := &db.Download{
		GameID:   gameID,
		URL:      url,
		Host:     host,
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

	// Set up active download state
	ad := &activeDownload{
		gameID:  gameID,
		url:     url,
		host:    host,
		destDir: destDir,
		status:  db.DownloadStatusDownloading,
	}
	m.activeDownloads[gameID] = ad

	// Launch download in background
	go func() {
		err := downloader.DownloadWithHost(url, host, destDir, 0, func(p downloader.Progress) {
			ad.mu.Lock()
			ad.progress = p
			ad.mu.Unlock()
			dl.BytesDownloaded = p.BytesDownloaded
			dl.TotalBytes = p.TotalBytes
			dl.SpeedBytesPerSec = p.SpeedBytesPerSec
			dl.PercentComplete = p.Percent
			m.db.UpdateDownload(dl)
		})

		ad.mu.Lock()
		if err != nil {
			ad.status = db.DownloadStatusFailed
			ad.err = err.Error()
			dl.Status = db.DownloadStatusFailed
			dl.Error = err.Error()
		} else {
			ad.status = db.DownloadStatusCompleted
			ad.progress.Percent = 100
			dl.Status = db.DownloadStatusCompleted
			dl.PercentComplete = 100
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
func (m model) getDownloadProgress(gameID int64) (downloader.Progress, db.DownloadStatus, string) {
	ad, ok := m.activeDownloads[gameID]
	if !ok {
		return downloader.Progress{}, "", ""
	}
	ad.mu.Lock()
	defer ad.mu.Unlock()
	return ad.progress, ad.status, ad.err
}

// resolveDownloadLink finds a download URL+host for a game, checking DB then scraping.
func (m model) resolveDownloadLink(game *db.Game) (url, host string, err error) {
	links, listErr := m.db.ListDownloadLinks(game.ID, "", false)
	if listErr == nil && len(links) > 0 {
		targetPlatform := downloader.CurrentPlatform()
		best := selectBestLinkByPlatform(links, targetPlatform)
		if best != nil {
			return best.URL, best.Host, nil
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
			best := selectBestLinkByPlatform(links, targetPlatform)
			if best != nil {
				return best.URL, best.Host, nil
			}
		}
	}

	return "", "", fmt.Errorf("no download links found")
}

// isOnlineOnlyLink returns true if the link text or URL indicates a browser-only version.
func isOnlineOnlyLink(name, url string) bool {
	lower := strings.ToLower(name + " " + url)
	return strings.Contains(lower, "online") || strings.Contains(lower, "gamejolt")
}

// selectBestLinkByPlatform picks the best link based on platform + host reliability.
// Skips online-only links that aren't downloadable.
func selectBestLinkByPlatform(links []db.DownloadLink, targetPlatform downloader.Platform) *db.DownloadLink {
	if len(links) == 0 {
		return nil
	}
	type sl struct {
		link  db.DownloadLink
		score int
	}
	var scored []sl
	for _, link := range links {
		if isOnlineOnlyLink(link.Name, link.URL) {
			continue
		}
		score := downloader.PlatformPriority(downloader.Platform(link.Platform), targetPlatform)
		switch link.Host {
		case "vikingfile", "buzzheavier", "pixeldrain", "mega", "gofile":
			score += 15
		case "mediafire", "workupload":
			score += 8
		case "krakenfiles", "googledrive":
			score += 5
		}
		scored = append(scored, sl{link, score})
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
