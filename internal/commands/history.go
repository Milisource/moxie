package commands

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// History shows recently played games.
// Usage: moxie history [count]
func History(args []string) {
	limit := 20
	if len(args) > 0 {
		n, err := strconv.Atoi(args[0])
		if err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	database := OpenDB()
	defer database.Close()

	entries, err := database.RecentPlays(limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading play history: %v\n", err)
		os.Exit(1)
	}

	if len(entries) == 0 {
		fmt.Println("No play history yet. Launch a game with 'moxie play' to start tracking.")
		return
	}

	fmt.Printf("%-4s %-30s %-20s %-10s\n", "ID", "Title", "Played At", "Platform")
	fmt.Println(strings.Repeat("-", 70))
	for _, e := range entries {
		played := e.PlayedAt.Format("2006-01-02 15:04")
		plat := e.Platform
		if plat == "" {
			plat = "unknown"
		}
		fmt.Printf("%-4d %-30s %-20s %-10s\n", e.GameID, truncate(e.GameTitle, 28), played, plat)
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
