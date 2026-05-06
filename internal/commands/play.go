package commands

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/mili/moxie/internal/db"
)

// Play launches a game by ID or fuzzy name search.
// Usage: moxie play <id>  or  moxie play <name>
func Play(args []string) {
	fs := flag.NewFlagSet("play", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie play <id>  or  moxie play <name>\n")
		fmt.Fprintf(os.Stderr, "  <id>    Game ID number (from `moxie list`)\n")
		fmt.Fprintf(os.Stderr, "  <name>  Fuzzy title search (e.g. \"Cyan Brain\" or \"Cyan\")\n")
		os.Exit(1)
	}

	database := OpenDB()
	defer database.Close()

	raw := fs.Arg(0)
	var game *db.Game

	// Try numeric ID first.
	if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
		g, gErr := database.GetGame(id)
		if gErr == nil && g != nil {
			game = g
		}
	}

	// Fall back to fuzzy name search if ID lookup failed or arg wasn't numeric.
	if game == nil {
		results, srchErr := database.SearchGames(raw)
		if srchErr != nil || len(results) == 0 {
			// Arg might contain spaces — try it as a multi-word query.
			results, srchErr = database.SearchGames(strings.Join(fs.Args(), " "))
		}
		if srchErr != nil || len(results) == 0 {
			fmt.Fprintf(os.Stderr, "No game found matching %q.\n", raw)
			os.Exit(1)
		}
		if len(results) == 1 {
			game = &results[0]
		} else {
			game = promptSelectGame(results)
		}
	}

	if game == nil {
		fmt.Fprintf(os.Stderr, "Game not found.\n")
		os.Exit(1)
	}

	exe := ResolveExecutable(*game)
	if exe == "" {
		fmt.Fprintf(os.Stderr, "No executable found for %q.\nPath: %s\n", game.Title, game.Path)
		os.Exit(1)
	}

	cmd := LaunchCommand(exe)
	if cmd == nil {
		fmt.Fprintf(os.Stderr, "Cannot launch %q: no launcher available for this file type on %s.\n", exe, runtime.GOOS)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Launching: %s\n", cmd)
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to launch: %v\n", err)
		os.Exit(1)
	}
	// Don't wait — let it run independently.
	go cmd.Wait()
}

// promptSelectGame shows a numbered list of games and asks the user to pick one.
func promptSelectGame(games []db.Game) *db.Game {
	fmt.Fprintf(os.Stderr, "\nMultiple games found:\n")
	for i, g := range games {
		fmt.Fprintf(os.Stderr, "  %2d. [%d] %s  (%s)\n", i+1, g.ID, g.Title, g.Engine)
	}
	fmt.Fprintf(os.Stderr, "\nEnter number or 0 to cancel: ")

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	n, err := strconv.Atoi(input)
	if err != nil || n < 1 || n > len(games) {
		return nil
	}
	return &games[n-1]
}

// ResolveExecutable finds the best executable to launch for a game.
func ResolveExecutable(g db.Game) string {
	// If ExePath is set and exists, use it.
	if g.ExePath != "" {
		if _, err := os.Stat(g.ExePath); err == nil {
			return g.ExePath
		}
	}

	// Search the game directory for executables.
	entries, err := os.ReadDir(g.Path)
	if err != nil {
		return ""
	}

	// macOS: check for .app bundles (they're directories containing executables).
	if runtime.GOOS == "darwin" {
		appBundles, _ := filepath.Glob(filepath.Join(g.Path, "*.app"))
		for _, bundle := range appBundles {
			macExe := filepath.Join(bundle, "Contents", "MacOS")
			if dirEntries, err := os.ReadDir(macExe); err == nil {
				for _, de := range dirEntries {
					if !de.IsDir() {
						// Found an executable inside the .app bundle.
						return filepath.Join(macExe, de.Name())
					}
				}
			}
		}
	}

	var exes []string
	var scripts []string
	var appImages []string
	var darwinNative []string

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		switch {
		case strings.HasSuffix(name, ".AppImage"):
			appImages = append(appImages, filepath.Join(g.Path, name))
		case ext == ".x86_64" || ext == ".x86" || ext == ".sh":
			scripts = append(scripts, filepath.Join(g.Path, name))
		case ext == ".exe":
			exes = append(exes, filepath.Join(g.Path, name))
		default:
			// macOS: detect native Mach-O executables (no recognized extension).
			if runtime.GOOS == "darwin" {
				info, err := e.Info()
				if err == nil && info.Mode().IsRegular() && info.Mode()&0111 != 0 {
					// Skip shebang scripts — only pick real binaries.
					f, fErr := os.Open(filepath.Join(g.Path, name))
					if fErr == nil {
						var header [2]byte
						n, _ := f.Read(header[:])
						f.Close()
						if n < 2 || header[0] != '#' || header[1] != '!' {
							darwinNative = append(darwinNative, filepath.Join(g.Path, name))
						}
					} else {
						// Can't read → still try it (might be a binary).
						darwinNative = append(darwinNative, filepath.Join(g.Path, name))
					}
				}
			}
		}
	}

	// Filter out platform-incompatible executables.
	if runtime.GOOS != "linux" {
		appImages = nil // .AppImage is Linux-only
		scripts = nil   // .sh/.x86_64/.x86 are Linux-specific
	}
	// darwinNative only populated on macOS, keep as-is on other platforms.

	// Prefer native over Wine.
	if len(appImages) > 0 {
		return SelectBestExe(appImages)
	}
	if len(scripts) > 0 {
		return SelectBestExe(scripts)
	}
	if len(darwinNative) > 0 {
		return SelectBestExe(darwinNative)
	}
	if len(exes) > 0 {
		return SelectBestExe(exes)
	}
	return ""
}

// SelectBestExe picks the most likely main executable from a list.
func SelectBestExe(paths []string) string {
	if len(paths) == 1 {
		return paths[0]
	}
	// Pick the largest — game executables are typically bigger than launchers.
	var best string
	var bestSize int64
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		name := strings.ToLower(filepath.Base(p))
		if strings.Contains(name, "unitycrashhandler") ||
			strings.Contains(name, "unins") ||
			strings.Contains(name, "setup") {
			continue
		}
		if info.Size() > bestSize {
			bestSize = info.Size()
			best = p
		}
	}
	return best
}

// LaunchCommand builds an exec.Cmd for the given executable.
func LaunchCommand(exe string) *exec.Cmd {
	ext := strings.ToLower(filepath.Ext(exe))
	switch {
	case ext == ".appimage":
		return exec.Command(exe)
	case ext == ".sh":
		return exec.Command("sh", exe)
	case ext == ".exe":
		if runtime.GOOS == "windows" {
			return exec.Command(exe)
		}
		// Check for wine availability.
		if winePath, err := exec.LookPath("wine"); err == nil {
			return exec.Command(winePath, exe)
		}
		// macOS: try CrossOver as fallback.
		if runtime.GOOS == "darwin" {
			crossoverWine := "/Applications/CrossOver.app/Contents/SharedSupport/CrossOver/bin/wine"
			if _, err := os.Stat(crossoverWine); err == nil {
				return exec.Command(crossoverWine, exe)
			}
		}
		fmt.Fprintf(os.Stderr, "⚠ wine not found — cannot launch .exe files on this platform.\n")
		return nil
	default:
		// Native Linux binary (.x86_64, .x86, no extension).
		return exec.Command(exe)
	}
}
