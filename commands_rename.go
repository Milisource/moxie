package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/scraper"
)

func cmdRename(args []string) {
	fs := flag.NewFlagSet("rename", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "Preview renames without making changes")
	fs.Parse(args)

	database := openDB()
	defer database.Close()

	games, err := database.ListGames("", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading games: %v\n", err)
		os.Exit(1)
	}

	type renamePlan struct {
		game    db.Game
		oldPath string
		newPath string
		newName string
	}

	var plans []renamePlan
	for _, g := range games {
		newName := cleanGameTitle(g)
		if newName == "" || newName == filepath.Base(g.Path) {
			continue // no change needed
		}

		parent := filepath.Dir(g.Path)
		newPath := filepath.Join(parent, newName)

		// Skip if new path already exists (different game).
		if newPath != g.Path {
			if _, err := os.Stat(newPath); err == nil {
				fmt.Fprintf(os.Stderr, "  ⚠ Skipping %q — target already exists: %q\n",
					filepath.Base(g.Path), newName)
				continue
			}
		}

		plans = append(plans, renamePlan{
			game:    g,
			oldPath: g.Path,
			newPath: newPath,
			newName: newName,
		})
	}

	if len(plans) == 0 {
		fmt.Println("All game directories already have clean names.")
		return
	}

	// Show preview.
	fmt.Fprintf(os.Stderr, "=== %d directories to rename ===\n\n", len(plans))
	for _, p := range plans {
		fmt.Printf("  %s\n  → %s\n\n",
			filepath.Base(p.oldPath),
			p.newName)
	}

	if *dryRun {
		fmt.Fprintln(os.Stderr, "Dry run — no changes made. Remove --dry-run to apply.")
		return
	}

	fmt.Fprintf(os.Stderr, "Rename %d directories? (y/N): ", len(plans))
	var answer string
	fmt.Scanln(&answer)
	if strings.ToLower(answer) != "y" {
		fmt.Println("Cancelled.")
		return
	}

	renamed := 0
	for _, p := range plans {
		if err := os.Rename(p.oldPath, p.newPath); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ %s: %v\n", filepath.Base(p.oldPath), err)
			continue
		}
		// Update DB path.
		p.game.Path = p.newPath
		if err := database.UpdateGame(&p.game); err != nil {
			fmt.Fprintf(os.Stderr, "  ⚠ Renamed dir but failed to update DB for %s: %v\n",
				p.newName, err)
		}
		fmt.Fprintf(os.Stderr, "  ✓ %s → %s\n", filepath.Base(p.oldPath), p.newName)
		renamed++
	}
	fmt.Fprintf(os.Stderr, "\nRenamed %d directories.\n", renamed)
}

// cleanGameTitle produces a clean directory name for a game.
// Prefers the scraped title (stripped of engine/status prefixes), falls back
// to sanitizing the directory name.
func cleanGameTitle(g db.Game) string {
	title := g.Title

	// If we have a scraped F95Zone title, strip engine/status prefix tags.
	if g.F95URL != "" {
		title = stripThreadPrefix(title)
	}

	// Filesystem-safe: replace forbidden chars, limit length.
	title = filesystemSafe(title)
	return title
}

func stripThreadPrefix(title string) string {
	// Known engine/status/category prefix words.
	prefixWords := map[string]bool{
		"unity": true, "ren'py": true, "renpy": true, "rpgm": true,
		"vn": true, "html": true, "flash": true, "java": true,
		"godot": true, "electron": true, "unreal": true, "others": true, "html5": true,
		"completed": true, "abandoned": true, "onhold": true,
		"collection": true, "video": true, "mod": true, "cheat": true,
		"tool": true, "daz": true, "update": true, "req": true,
		"request": true, "seeking": true, "announcement": true,
	}

	words := strings.Fields(title)
	for len(words) > 0 && prefixWords[strings.ToLower(strings.TrimRight(words[0], "•"))] {
		words = words[1:]
	}

	result := strings.TrimSpace(strings.Join(words, " "))
	if result == "" {
		return title
	}
	return result
}

// filesystemSafe replaces characters that are illegal in directory names.
func filesystemSafe(name string) string {
	// Strip or replace problematic characters.
	replacer := strings.NewReplacer(
		"/", "-", "\\", "-", ":", "-", "*", "", "?", "",
		"\"", "'", "<", "", ">", "", "|", "-",
	)
	name = replacer.Replace(name)

	// Collapse multiple spaces/hyphens.
	name = multiSpaceRE.ReplaceAllString(name, " ")
	name = multiDashRE.ReplaceAllString(name, "-")

	// Remove version tags we don't need in the directory name.
	name = scraper.SanitizeTitle(name)

	// Don't start or end with space/dot/hyphen.
	name = strings.TrimSpace(name)
	name = strings.Trim(name, ".- ")

	// Limit length.
	if len(name) > 80 {
		name = name[:80]
	}

	// On Windows, avoid reserved filenames.
	if runtime.GOOS == "windows" {
		reserved := map[string]bool{
			"con": true, "prn": true, "aux": true, "nul": true,
			"com1": true, "com2": true, "com3": true, "com4": true,
			"com5": true, "com6": true, "com7": true, "com8": true, "com9": true,
			"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true,
			"lpt5": true, "lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
		}
		base := strings.ToLower(strings.SplitN(name, ".", 2)[0])
		if reserved[base] {
			name = "_" + name
		}
	}

	if name == "" {
		return "game"
	}
	return name
}
