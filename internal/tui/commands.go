package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mili/moxie/internal/db"
)

// loadGames returns a command that fetches all games from the database.
func (m model) loadGames() tea.Cmd {
	return func() tea.Msg {
		games, err := m.db.ListGames("", "")
		if err != nil {
			return errMsg{err}
		}
		return gamesLoadedMsg{games}
	}
}

// loadMeta returns a command that fetches scraped metadata for a game.
func (m model) loadMeta(id int64) tea.Cmd {
	return func() tea.Msg {
		meta, err := m.db.GetScrapedMeta(id)
		if err != nil {
			return errMsg{err}
		}
		return metaLoadedMsg{meta}
	}
}

// deleteGame returns a command that deletes a game from the database and
// reloads the full game list. Both operations run asynchronously in a
// single goroutine to avoid blocking the UI.
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

// scrapeMeta returns a command that scrapes the given F95Zone URL for
// metadata, upserts it into the database, and returns the result.
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
