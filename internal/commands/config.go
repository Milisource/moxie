package commands

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/mili/moxie/internal/config"
	"github.com/mili/moxie/internal/scraper"
)

// Config handles the config command and its subcommands.
func Config(args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie config <set|get|show|set-thread> [args...]\n")
		fmt.Fprintf(os.Stderr, "       moxie config set steamgriddb-key <key>\n")
		fmt.Fprintf(os.Stderr, "       moxie config get steamgriddb-key\n")
		fmt.Fprintf(os.Stderr, "       moxie config show\n")
		fmt.Fprintf(os.Stderr, "       moxie config set-thread <game-id> <thread-id>\n")
		os.Exit(1)
	}
	switch args[0] {
	case "set":
		ConfigSet(args[1:])
	case "get":
		ConfigGet(args[1:])
	case "show":
		ConfigShow()
	case "set-thread":
		ConfigSetThread(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown config subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

// ConfigSetThread sets the F95Zone thread ID for a game and persists it in
// the association cache so future auto-runs find the correct thread directly.
func ConfigSetThread(args []string) {
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: moxie config set-thread <id|name> <thread-id>\n")
		os.Exit(1)
	}

	threadID, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid thread ID: %s\n", args[1])
		os.Exit(1)
	}

	database := OpenDB()
	defer database.Close()

	game := ResolveGame(database, args[0])
	if game == nil {
		fmt.Fprintf(os.Stderr, "Cancelled.\n")
		os.Exit(1)
	}

	if !ConfirmDestructive("Setting F95Zone thread for", game, false) {
		fmt.Fprintf(os.Stderr, "Aborted.\n")
		os.Exit(1)
	}

	// Update the game record.
	game.F95ThreadID = threadID
	game.F95URL = scraper.ThreadURL(threadID)
	if err := database.UpdateGame(game); err != nil {
		fmt.Fprintf(os.Stderr, "Error updating game: %v\n", err)
		os.Exit(1)
	}

	// Persist to association cache so future auto-runs find it.
	scraper.LoadAssociationCache()
	sanitized := scraper.SanitizeTitle(game.Title)
	if sanitized == "" {
		sanitized = game.Title
	}
	scraper.SetCachedThreadID(sanitized, threadID)
	if err := scraper.SaveAssociationCache(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save association cache: %v\n", err)
	}

	fmt.Printf("Set F95Zone thread %d for %q\n", threadID, game.Title)
}

// ConfigSet sets a configuration value.
func ConfigSet(args []string) {
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: moxie config set <key> <value>\n")
		os.Exit(1)
	}
	key := args[0]
	value := strings.Join(args[1:], " ")

	cfg, err := config.ReadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading config: %v\n", err)
		os.Exit(1)
	}
	cfg[key] = value
	if err := config.WriteConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Set %s = %s\n", key, value)
}

// ConfigGet gets a configuration value.
func ConfigGet(args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie config get <key>\n")
		os.Exit(1)
	}
	cfg, err := config.ReadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading config: %v\n", err)
		os.Exit(1)
	}
	value, ok := cfg[args[0]]
	if !ok || value == "" {
		fmt.Fprintf(os.Stderr, "(not set)\n")
		return
	}
	fmt.Println(value)
}

// ConfigShow shows all configuration values.
func ConfigShow() {
	cfg, err := config.ReadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading config: %v\n", err)
		os.Exit(1)
	}
	if len(cfg) == 0 {
		fmt.Println("No configuration values set.")
		return
	}
	for k, v := range cfg {
		masked := v
		if strings.Contains(strings.ToLower(k), "key") && len(v) > 4 {
			masked = v[:4] + strings.Repeat("*", len(v)-4)
		}
		fmt.Printf("  %-25s %s\n", k, masked)
	}
}
