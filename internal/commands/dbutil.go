package commands

import (
	"fmt"
	"os"

	"github.com/mili/moxie/internal/config"
	"github.com/mili/moxie/internal/db"
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
