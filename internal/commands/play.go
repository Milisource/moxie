package commands

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

	// Join all positional args so unquoted multi-word names still work.
	raw := strings.Join(fs.Args(), " ")
	game := ResolveGame(database, raw)
	if game == nil {
		fmt.Fprintf(os.Stderr, "Cancelled.\n")
		os.Exit(1)
	}

	exe := ResolveExecutable(*game)
	if exe == "" {
		fmt.Fprintf(os.Stderr, "No executable found for %q.\nPath: %s\n", game.Title, game.Path)
		os.Exit(1)
	}

	cmd := LaunchCommand(exe, game.Path)
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
// It skips known runtime engines and launchers, then picks by size
// (game executables are typically bigger than their launchers).
func SelectBestExe(paths []string) string {
	if len(paths) == 1 {
		return paths[0]
	}
	// Score-based selection: prefer "Game.exe"-like names, penalize known runtimes.
	type scored struct {
		path  string
		score int64
		size  int64
	}
	var candidates []scored
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		name := strings.ToLower(filepath.Base(p))
		base := strings.TrimSuffix(name, ".exe")

		// Skip known runtimes and installers — they're engine binaries, not the game.
		if base == "nwjc" || base == "nw" || base == "node" ||
			strings.Contains(name, "unitycrashhandler") ||
			strings.Contains(name, "unins") ||
			strings.Contains(name, "setup") {
			continue
		}

		// Score: size + bonus for common game executable names.
		s := info.Size()
		if base == "game" {
			s += 1 << 30 // +1 GB bonus — strongly prefer Game.exe
		}
		candidates = append(candidates, scored{p, s, info.Size()})
	}

	if len(candidates) == 0 {
		return ""
	}

	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.score > best.score {
			best = c
		}
	}
	return best.path
}

// LaunchCommand builds an exec.Cmd for the given executable, setting the
// working directory to the game's root path so relative asset paths resolve.
func LaunchCommand(exe, gameDir string) *exec.Cmd {
	ext := strings.ToLower(filepath.Ext(exe))
	setDir := func(cmd *exec.Cmd) *exec.Cmd {
		cmd.Dir = gameDir
		return cmd
	}
	switch {
	case ext == ".appimage":
		return setDir(exec.Command(exe))
	case ext == ".sh":
		return setDir(exec.Command("sh", exe))
	case ext == ".exe":
		if runtime.GOOS == "windows" {
			return setDir(exec.Command(exe))
		}
		// Check for wine availability.
		if winePath, err := exec.LookPath("wine"); err == nil {
			return setDir(exec.Command(winePath, exe))
		}
		// macOS: try CrossOver as fallback.
		if runtime.GOOS == "darwin" {
			crossoverWine := "/Applications/CrossOver.app/Contents/SharedSupport/CrossOver/bin/wine"
			if _, err := os.Stat(crossoverWine); err == nil {
				return setDir(exec.Command(crossoverWine, exe))
			}
		}
		fmt.Fprintf(os.Stderr, "⚠ wine not found — cannot launch .exe files on this platform.\n")
		return nil
	default:
		// Native Linux binary (.x86_64, .x86, no extension).
		return setDir(exec.Command(exe))
	}
}
