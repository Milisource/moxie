// Package commands implements CLI subcommands for the moxie game manager.
//
// TTY detection behavior: Functions that prompt for user input check whether
// stdin is a terminal before prompting. When stdin is not a TTY (e.g., piped
// input, scripted execution), interactive prompts are skipped. Game selection
// prints matches to stderr and exits; destructive operations are rejected
// with a message telling the user to use a numeric ID instead.
package commands

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/mili/moxie/internal/db"
)

// ResolveGame resolves a game argument that may be a numeric ID or fuzzy title.
// Tries numeric ID first (backward compatible), then falls back to SearchGames
// for SQL LIKE matching. If multiple matches, prompts user to select one.
// Exits with error if no match found.
func ResolveGame(database *db.Database, raw string) *db.Game {
	// 1. Try numeric ID first.
	if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
		g, gErr := database.GetGame(id)
		if gErr != nil {
			fmt.Fprintf(os.Stderr, "Database error: %v\n", gErr)
			os.Exit(1)
		}
		if g != nil {
			return g
		}
		// No game with this ID — fall through to fuzzy search.
	}

	// 2. Fall back to fuzzy name search.
	results, err := database.SearchGames(raw)
	if err != nil || len(results) == 0 {
		fmt.Fprintf(os.Stderr, "No game found matching %q.\n", raw)
		os.Exit(1)
	}

	if len(results) == 1 {
		return &results[0]
	}

	return promptSelectGame(results)
}

// ResolveFirstArg resolves a game using only the first token for name search,
// for commands that take multiple positional args (e.g. `install <game-id> <archive-path>`).
// This avoids consuming subsequent args as part of the game name query.
//
// Currently unused; will be needed by the install command (F95-3bu).
func ResolveFirstArg(database *db.Database, raw string) *db.Game {
	// Try numeric ID first.
	if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
		g, gErr := database.GetGame(id)
		if gErr != nil {
			fmt.Fprintf(os.Stderr, "Database error: %v\n", gErr)
			os.Exit(1)
		}
		if g != nil {
			return g
		}
	}

	// Only search the first token for fuzzy match.
	results, err := database.SearchGames(raw)
	if err != nil || len(results) == 0 {
		fmt.Fprintf(os.Stderr, "No game found matching %q.\n", raw)
		os.Exit(1)
	}

	if len(results) == 1 {
		return &results[0]
	}

	return promptSelectGame(results)
}

// isInteractive returns true when stdin is connected to a terminal (TTY).
// When false, stdin is a pipe or redirect and interactive prompts would block.
func isInteractive() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return stat.Mode()&os.ModeCharDevice != 0
}

// ConfirmDestructive prompts the user to confirm a destructive action on a game
// that was resolved by fuzzy name match. Returns true if confirmed.
// Skips prompting when --assume-yes/-y flag is set (checked via assumeYes param)
// or when stdin is not a TTY (non-interactive mode always rejects).
func ConfirmDestructive(action string, game *db.Game, assumeYes bool) bool {
	if assumeYes {
		return true
	}
	if !isInteractive() {
		fmt.Fprintf(os.Stderr, "Non-interactive mode: use numeric ID for destructive operations.\n")
		return false
	}
	fmt.Fprintf(os.Stderr, "[Name Match] %s '%s' [ID %d] — confirm? (y/N): ", action, game.Title, game.ID)
	var answer string
	fmt.Scanln(&answer)
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "y" || answer == "yes"
}

// promptSelectGame shows a numbered list of games and asks the user to pick one.
// In non-interactive mode (piped stdin), it prints matches to stderr and exits.
func promptSelectGame(games []db.Game) *db.Game {
	if !isInteractive() {
		fmt.Fprintf(os.Stderr, "Multiple games found:\n")
		for _, g := range games {
			fmt.Fprintf(os.Stderr, "  [%d] %s\n", g.ID, g.Title)
		}
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "\nMultiple games found:\n")
	for i, g := range games {
		fmt.Fprintf(os.Stderr, "  %2d. [%d] %s  (%s)\n", i+1, g.ID, g.Title, g.Engine)
	}
	fmt.Fprintf(os.Stderr, "\nEnter number or 0 to cancel: ")

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		// EOF or stdin error: treat as cancellation.
		return nil
	}
	input = strings.TrimSpace(input)

	n, err := strconv.Atoi(input)
	if err != nil || n < 1 || n > len(games) {
		return nil
	}
	return &games[n-1]
}
