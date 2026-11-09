package commands

import (
	"fmt"
	"os"

	"github.com/mili/moxie/internal/config"
	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/launcher"
)

// OpenDB opens the SQLite database and exits on error.
func OpenDB() *db.Database {
	path := config.DbPath()
	database, err := db.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	return database
}

// resolveGameExe finds the best executable for a game using the shared launcher.
func resolveGameExe(game *db.Game) string {
	return launcher.ResolveExecutable(game.Path, game.ExePath)
}
