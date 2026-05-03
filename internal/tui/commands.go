package tui

import tea "github.com/charmbracelet/bubbletea"

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
