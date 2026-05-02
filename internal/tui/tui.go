// Package tui provides a Bubble Tea terminal UI for browsing and managing
// the F95 game library. It presents an interactive table view of all games
// with filtering, sorting, detail inspection, and game management.
package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mili/moxie/internal/db"
)

// Run opens the database at dbPath, creates the Bubble Tea program, and runs
// the interactive terminal UI. The database is closed when the program exits.
func Run(dbPath string) error {
	database, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	p := tea.NewProgram(initialModel(database), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}
