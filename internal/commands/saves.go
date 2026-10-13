package commands

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/mili/moxie/internal/config"
	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/saves"
	"github.com/mili/moxie/internal/util"
)

// Saves handles the "saves" command and its subcommands for managing per-game
// save sets (snapshots, imports, apply/restore).
//
// Usage:
//
//	moxie saves list [id]
//	moxie saves snapshot <id> [--name N] [--label L]
//	moxie saves add <id> <archive|dir> [--label L] [--name N] [--root R]
//	moxie saves apply <id> <set>
//	moxie saves restore <id>
//	moxie saves path <id> [--set PATH|--auto]
//	moxie saves remove <id> <set>
//	moxie saves fetch <id> [--list] [--pick N] [--password P]
func Saves(args []string) {
	if len(args) < 1 {
		printSavesUsage()
		os.Exit(1)
	}

	switch args[0] {
	case "list":
		savesList(args[1:])
	case "snapshot":
		savesSnapshot(args[1:])
	case "add":
		savesAdd(args[1:])
	case "apply":
		savesApply(args[1:])
	case "restore":
		savesRestore(args[1:])
	case "path":
		savesPath(args[1:])
	case "remove":
		savesRemove(args[1:])
	case "fetch":
		savesFetch(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown saves subcommand: %s\n", args[0])
		printSavesUsage()
		os.Exit(1)
	}
}

// parseSavesArgs parses fs against args and returns the positional arguments.
// Unlike a bare fs.Parse, it allows flags to appear before, between, or after
// positionals (the standard flag package stops at the first positional), which
// keeps the documented "<id> <src> [--flag]" surface working.
func parseSavesArgs(fs *flag.FlagSet, args []string) []string {
	var positionals []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			return positionals
		}
		rest = fs.Args()
		if len(rest) == 0 {
			return positionals
		}
		positionals = append(positionals, rest[0])
		rest = rest[1:]
	}
}

// openSavesEngine builds a saves.Engine rooted at the configured saves_dir,
// falling back to config.DefaultSavesDir().
func openSavesEngine(database *db.Database) *saves.Engine {
	root := config.DefaultSavesDir()
	if cfg, err := config.ReadConfig(); err == nil {
		if v := cfg.Get("saves_dir"); v != "" {
			root = v
		}
	}
	engine, err := saves.NewEngine(database, root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing saves engine: %v\n", err)
		os.Exit(1)
	}
	return engine
}

// savesList prints the sets for one game, or every game's sets when no game is
// given (grouped by game).
func savesList(args []string) {
	fs := flag.NewFlagSet("saves-list", flag.ExitOnError)
	positionals := parseSavesArgs(fs, args)

	database := OpenDB()
	defer database.Close()

	titleFor := func(id int64) string {
		g, err := database.GetGame(id)
		if err != nil || g == nil {
			return fmt.Sprintf("game %d", id)
		}
		return g.Title
	}

	var sets []db.SaveSet
	var err error
	if len(positionals) >= 1 {
		game := ResolveGame(database, positionals[0])
		sets, err = database.ListSaveSets(game.ID)
	} else {
		sets, err = database.ListAllSaveSets()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing save sets: %v\n", err)
		os.Exit(1)
	}
	printSaveSets(sets, titleFor)
}

// formatSize renders a byte count for save-set output (e.g. "1.5 KB"). It lives
// here because the shared util.FormatSize helper this command used was dropped
// with the commands' table-rendering refactor.
func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// printSaveSets renders a set table. titleFor resolves a game title by ID.
func printSaveSets(sets []db.SaveSet, titleFor func(int64) string) {
	if len(sets) == 0 {
		fmt.Println("No save sets found.")
		return
	}
	fmt.Printf("%-6s %-28s %-26s %-8s %-10s %s\n", "GAME", "GAME TITLE", "SET", "SOURCE", "SIZE", "STATUS")
	fmt.Println(strings.Repeat("-", 92))
	for _, s := range sets {
		label := s.Name
		if s.Label != "" && s.Label != s.Name {
			label = fmt.Sprintf("%s (%s)", s.Name, s.Label)
		}
		status := ""
		if s.Active {
			status = "active"
		}
		fmt.Printf("%-6d %-28s %-26s %-8s %-10s %s\n",
			s.GameID, util.Truncate(titleFor(s.GameID), 28), util.Truncate(label, 26),
			s.Source, formatSize(s.SizeBytes), status)
	}
}

// savesSnapshot captures the game's live save data as a new set.
func savesSnapshot(args []string) {
	fs := flag.NewFlagSet("saves-snapshot", flag.ExitOnError)
	name := fs.String("name", "", "Set name (default mine-<timestamp>)")
	label := fs.String("label", "", "Display label (default \"my saves\")")
	positionals := parseSavesArgs(fs, args)

	if len(positionals) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie saves snapshot <id|name> [--name N] [--label L]\n")
		os.Exit(1)
	}

	database := OpenDB()
	defer database.Close()
	game := ResolveGame(database, positionals[0])
	engine := openSavesEngine(database)

	set, err := engine.Snapshot(game, *name, *label)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error snapshotting saves: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Snapshotted %q as %s (%d files, %s)\n",
		game.Title, set.Name, set.FileCount, formatSize(set.SizeBytes))
}

// savesAdd imports a save set from an archive or directory.
func savesAdd(args []string) {
	fs := flag.NewFlagSet("saves-add", flag.ExitOnError)
	label := fs.String("label", "", "Display label")
	name := fs.String("name", "", "Set name (default: derived from label/source)")
	root := fs.String("root", "", "In-set subpath to apply (default: detected)")
	positionals := parseSavesArgs(fs, args)

	if len(positionals) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: moxie saves add <id|name> <archive|dir> [--label L] [--name N] [--root R]\n")
		os.Exit(1)
	}

	database := OpenDB()
	defer database.Close()
	game := ResolveGame(database, positionals[0])
	engine := openSavesEngine(database)

	set, err := engine.Import(game, positionals[1], saves.ImportOptions{
		Name:      *name,
		Label:     *label,
		ApplyRoot: *root,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error importing save set: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Added set %s for %q (%d files, %s)\n",
		set.Name, game.Title, set.FileCount, formatSize(set.SizeBytes))
	if set.ApplyRoot != "" {
		fmt.Printf("  apply root: %s\n", set.ApplyRoot)
	}
}

// savesApply applies a named set to the game's live save location.
func savesApply(args []string) {
	fs := flag.NewFlagSet("saves-apply", flag.ExitOnError)
	positionals := parseSavesArgs(fs, args)

	if len(positionals) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: moxie saves apply <id|name> <set>\n")
		os.Exit(1)
	}

	database := OpenDB()
	defer database.Close()
	game := ResolveGame(database, positionals[0])
	engine := openSavesEngine(database)

	res, err := engine.Apply(game, positionals[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error applying save set: %v\n", err)
		os.Exit(1)
	}
	printApplyResult("Applied", res)
}

// savesRestore re-applies the newest snapshot ("my saves") for a game.
func savesRestore(args []string) {
	fs := flag.NewFlagSet("saves-restore", flag.ExitOnError)
	positionals := parseSavesArgs(fs, args)

	if len(positionals) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie saves restore <id|name>\n")
		os.Exit(1)
	}

	database := OpenDB()
	defer database.Close()
	game := ResolveGame(database, positionals[0])
	engine := openSavesEngine(database)

	res, err := engine.Restore(game)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error restoring saves: %v\n", err)
		os.Exit(1)
	}
	printApplyResult("Restored", res)
}

// printApplyResult reports a completed apply/restore.
func printApplyResult(verb string, res *saves.ApplyResult) {
	if res.AutoSnapshot != "" {
		fmt.Printf("Auto-snapshotted your current saves as %s\n", res.AutoSnapshot)
	}
	fmt.Printf("%s %s to %s (%d files copied)\n", verb, res.Set.Name, res.Target.Path, res.FilesCopied)
	fmt.Printf("  active set: %s\n", res.Set.Name)
}

// savesPath shows (and optionally pins or clears) the live save location.
func savesPath(args []string) {
	fs := flag.NewFlagSet("saves-path", flag.ExitOnError)
	setPath := fs.String("set", "", "Pin a manual save location (wins over auto-detection)")
	auto := fs.Bool("auto", false, "Clear the manual override and use auto-detection")
	positionals := parseSavesArgs(fs, args)

	if len(positionals) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie saves path <id|name> [--set PATH|--auto]\n")
		os.Exit(1)
	}
	if *setPath != "" && *auto {
		fmt.Fprintf(os.Stderr, "Cannot combine --set and --auto.\n")
		os.Exit(1)
	}

	database := OpenDB()
	defer database.Close()
	game := ResolveGame(database, positionals[0])
	engine := openSavesEngine(database)

	if *setPath != "" {
		if err := engine.SetManualPath(game, *setPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error pinning save path: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Pinned save location for %q: %s\n", game.Title, *setPath)
	}
	if *auto {
		if err := engine.ClearManualPath(game); err != nil {
			fmt.Fprintf(os.Stderr, "Error clearing save path: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Cleared manual save location for %q (using auto-detection).\n", game.Title)
	}

	resolved, candidates, err := engine.ResolveLocation(game)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving save location: %v\n", err)
		os.Exit(1)
	}

	if resolved.Path != "" {
		fmt.Printf("Resolved: %s\n", resolved.Path)
		fmt.Printf("  kind:       %s\n", resolved.Kind)
		fmt.Printf("  confidence: %.2f\n", resolved.Confidence)
		fmt.Printf("  note:       %s\n", resolved.Note)
	} else {
		fmt.Println("No save location resolved.")
	}

	if len(candidates) > 0 {
		fmt.Println("Candidates:")
		for _, c := range candidates {
			fmt.Printf("  %-6s %-55s conf %.2f  %s\n", c.Kind, c.Path, c.Confidence, c.Note)
		}
	}
}

// savesRemove deletes a named set and its stored files.
func savesRemove(args []string) {
	fs := flag.NewFlagSet("saves-remove", flag.ExitOnError)
	positionals := parseSavesArgs(fs, args)

	if len(positionals) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: moxie saves remove <id|name> <set>\n")
		os.Exit(1)
	}

	database := OpenDB()
	defer database.Close()
	game := ResolveGame(database, positionals[0])
	engine := openSavesEngine(database)

	if err := engine.Remove(game, positionals[1]); err != nil {
		fmt.Fprintf(os.Stderr, "Error removing save set: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Removed save set %s for %q.\n", positionals[1], game.Title)
}

func printSavesUsage() {
	fmt.Fprintf(os.Stderr, `Usage: moxie saves <subcommand> [args]

Manage per-game save sets: snapshots of your own saves, imported 100%% saves,
and non-destructive switching between them.

Subcommands:
  list [id]                            List save sets (all games when no id)
  snapshot <id> [--name N] [--label L] Snapshot the game's current live saves
  add <id> <archive|dir> [flags]       Import a save set from an archive or dir
                                       --label L  --name N  --root R
  apply <id> <set>                     Apply a set to the live save location
  restore <id>                         Re-apply the newest snapshot (my saves)
  path <id> [--set PATH|--auto]        Show/pin/clear the live save location
  remove <id> <set>                     Delete a save set
  fetch <id> [--list] [--pick N]        Download a save from the F95 thread OP
      [--password P] [--url URL]       --list to inspect, --pick N to choose

Storage root: saves_dir config key, default %s.

`, config.DefaultSavesDir())
}
