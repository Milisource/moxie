package commands

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/mili/moxie/internal/util"
)

// Collections handles the "collections" command with subcommands for
// managing user-defined game collections.
//
// Usage:
//
//	moxie collections create <name>
//	moxie collections list
//	moxie collections show <id>
//	moxie collections add <collection-id> <game-id>
//	moxie collections remove <collection-id> <game-id>
//	moxie collections delete <id>
func Collections(args []string) {
	if len(args) < 1 {
		printCollectionsUsage()
		os.Exit(1)
	}

	sub := args[0]
	subArgs := args[1:]

	database := OpenDB()
	defer database.Close()

	switch sub {
	case "create":
		if len(subArgs) < 1 {
			fmt.Fprintf(os.Stderr, "Usage: moxie collections create <name>\n")
			os.Exit(1)
		}
		name := strings.Join(subArgs, " ")
		col, err := database.CreateCollection(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating collection: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Created collection [%d] %s\n", col.ID, col.Name)

	case "list":
		cols, err := database.ListCollections()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing collections: %v\n", err)
			os.Exit(1)
		}
		if len(cols) == 0 {
			fmt.Println("No collections found.")
			return
		}
		fmt.Printf("%-4s %-30s %s\n", "ID", "Name", "Created")
		fmt.Println(strings.Repeat("-", 60))
		for _, c := range cols {
			created := "-"
			if !c.CreatedAt.IsZero() {
				created = c.CreatedAt.Format("2006-01-02")
			}
			fmt.Printf("%-4d %-30s %s\n", c.ID, c.Name, created)
		}

	case "show":
		if len(subArgs) < 1 {
			fmt.Fprintf(os.Stderr, "Usage: moxie collections show <id>\n")
			os.Exit(1)
		}
		id, err := strconv.ParseInt(subArgs[0], 10, 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid collection ID: %s\n", subArgs[0])
			os.Exit(1)
		}
		col, err := database.GetCollection(id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if col == nil {
			fmt.Fprintf(os.Stderr, "Collection %d not found.\n", id)
			os.Exit(1)
		}
		fmt.Printf("Collection: %s\n\n", col.Name)

		games, err := database.GetGamesInCollection(id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if len(games) == 0 {
			fmt.Println("No games in this collection.")
			return
		}
		fmt.Printf("%-4s %-30s %-12s %-10s\n", "ID", "Title", "Engine", "Version")
		fmt.Println(strings.Repeat("-", 70))
		for _, g := range games {
			ver := g.Version
			if ver == "" {
				ver = "unknown"
			}
			fmt.Printf("%-4d %-30s %-12s %-10s\n", g.ID, util.Truncate(g.Title, 30), g.Engine, ver)
		}

	case "add":
		if len(subArgs) < 2 {
			fmt.Fprintf(os.Stderr, "Usage: moxie collections add <collection-id> <game-id>\n")
			os.Exit(1)
		}
		colID, err := strconv.ParseInt(subArgs[0], 10, 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid collection ID: %s\n", subArgs[0])
			os.Exit(1)
		}
		gameID, err := strconv.ParseInt(subArgs[1], 10, 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid game ID: %s\n", subArgs[1])
			os.Exit(1)
		}
		if err := database.AddGameToCollection(gameID, colID); err != nil {
			fmt.Fprintf(os.Stderr, "Error adding game to collection: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Added game %d to collection %d.\n", gameID, colID)

	case "remove":
		if len(subArgs) < 2 {
			fmt.Fprintf(os.Stderr, "Usage: moxie collections remove <collection-id> <game-id>\n")
			os.Exit(1)
		}
		colID, err := strconv.ParseInt(subArgs[0], 10, 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid collection ID: %s\n", subArgs[0])
			os.Exit(1)
		}
		gameID, err := strconv.ParseInt(subArgs[1], 10, 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid game ID: %s\n", subArgs[1])
			os.Exit(1)
		}
		if err := database.RemoveGameFromCollection(gameID, colID); err != nil {
			fmt.Fprintf(os.Stderr, "Error removing game from collection: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Removed game %d from collection %d.\n", gameID, colID)

	case "delete":
		if len(subArgs) < 1 {
			fmt.Fprintf(os.Stderr, "Usage: moxie collections delete <id>\n")
			os.Exit(1)
		}
		id, err := strconv.ParseInt(subArgs[0], 10, 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid collection ID: %s\n", subArgs[0])
			os.Exit(1)
		}
		if err := database.DeleteCollection(id); err != nil {
			fmt.Fprintf(os.Stderr, "Error deleting collection: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Deleted collection %d.\n", id)

	default:
		fmt.Fprintf(os.Stderr, "Unknown collection subcommand: %s\n", sub)
		printCollectionsUsage()
		os.Exit(1)
	}
}

func printCollectionsUsage() {
	fmt.Fprintf(os.Stderr, `Usage: moxie collections <subcommand> [args]

Subcommands:
  create <name>          Create a new collection
  list                   List all collections
  show <id>              Show games in a collection
  add <col-id> <game-id> Add a game to a collection
  remove <col-id> <game-id> Remove a game from a collection
  delete <id>            Delete a collection

`)
}
