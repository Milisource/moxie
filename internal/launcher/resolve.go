// Package launcher provides game executable resolution and launching.
//
// It consolidates logic that was previously duplicated between
// the CLI (commands/play.go) and TUI (tui/helpers.go), ensuring
// identical behavior from both entry points.
package launcher

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mili/moxie/internal/engine"
)

// ResolveExecutable finds the best executable to launch for a game.
// It checks the known exePath first, then searches the game directory.
// On macOS it also inspects .app bundles and native Mach-O binaries.
func ResolveExecutable(gameDir, exePath string) string {
	// Virtual paths (browser-added games not downloaded yet) have no executable.
	if strings.HasPrefix(gameDir, "/virtual/") {
		return ""
	}

	// If we know the exe and it exists, use it.
	if exePath != "" {
		if _, err := os.Stat(exePath); err == nil {
			return exePath
		}
	}

	// Search the game directory for executables.
	entries, err := os.ReadDir(gameDir)
	if err != nil {
		return ""
	}

	// macOS: check for .app bundles (they're directories containing executables).
	if runtime.GOOS == "darwin" {
		appBundles, _ := filepath.Glob(filepath.Join(gameDir, "*.app"))
		for _, bundle := range appBundles {
			macExe := filepath.Join(bundle, "Contents", "MacOS")
			if dirEntries, err := os.ReadDir(macExe); err == nil {
				for _, de := range dirEntries {
					if !de.IsDir() {
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
			appImages = append(appImages, filepath.Join(gameDir, name))
		case ext == ".x86_64" || ext == ".x86" || ext == ".sh":
			scripts = append(scripts, filepath.Join(gameDir, name))
		case ext == ".exe":
			exes = append(exes, filepath.Join(gameDir, name))
		default:
			// macOS: detect native Mach-O executables (no recognized extension).
			if runtime.GOOS == "darwin" {
				info, err := e.Info()
				if err == nil && info.Mode().IsRegular() && info.Mode()&0111 != 0 {
					// Skip shebang scripts — only pick real binaries.
					f, fErr := os.Open(filepath.Join(gameDir, name))
					if fErr == nil {
						var header [2]byte
						n, _ := f.Read(header[:])
						f.Close()
						if n < 2 || header[0] != '#' || header[1] != '!' {
							darwinNative = append(darwinNative, filepath.Join(gameDir, name))
						}
					}
					// On open failure the file is unknown (EACCES on the
					// candidate itself) — skip it rather than promoting a
					// permission-denied file to launch candidate.
				}
			}
		}
	}

	// Filter out platform-incompatible executables.
	if runtime.GOOS != "linux" {
		appImages = nil // .AppImage is Linux-only
		scripts = nil   // .sh/.x86_64/.x86 are Linux-specific
	}

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
	// Browser-played HTML games have no native executable — their entry point
	// is an HTML page opened in the browser.
	if entry := engine.FindHTMLEntry(gameDir); entry != "" {
		return entry
	}

	// Nothing launchable at the root. Some archives nest the playable build a
	// level or two down (release wrappers the scanner did not collapse, source
	// repos). Descend a bounded distance so those installs still get an entry
	// point instead of an empty exe_path.
	return resolveNested(gameDir)
}

// nestedSearchDepth bounds the fallback descent in resolveNested.
const nestedSearchDepth = 2

// nestedSkipDirs are directory names that never hold a game's launchable entry
// point — engine runtimes, asset trees and tooling. They are skipped during the
// nested search so a bundled JRE or Unity data folder cannot masquerade as the
// game.
var nestedSkipDirs = map[string]bool{
	"game": true, "renpy": true, "www": true, "jre": true,
	"runtime": true, "lib": true, "libs": true, "node_modules": true,
	"__macosx": true, ".git": true,
}

// resolveNested looks for the best launchable entry point within
// nestedSearchDepth levels below gameDir, preferring a native executable over
// an HTML entry page. Returns "" when nothing is found.
func resolveNested(gameDir string) string {
	if exe := findNestedExe(gameDir, 1); exe != "" {
		return exe
	}
	return engine.FindHTMLEntryShallow(gameDir)
}

// findNestedExe returns the best native executable within nestedSearchDepth
// levels below dir, or "". Known runtimes/installers are skipped and the same
// platform filtering as the top-level search applies (.sh/.x86_64/AppImage are
// Linux-only).
func findNestedExe(dir string, depth int) string {
	if depth > nestedSearchDepth {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	var exes []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		lower := strings.ToLower(name)
		ext := strings.ToLower(filepath.Ext(name))
		if strings.Contains(lower, "unitycrashhandler") ||
			strings.Contains(lower, "unins") ||
			strings.Contains(lower, "setup") {
			continue
		}
		linuxOnly := ext == ".sh" || ext == ".x86_64" || ext == ".x86" ||
			strings.HasSuffix(name, ".AppImage")
		switch {
		case ext == ".exe":
			exes = append(exes, filepath.Join(dir, name))
		case linuxOnly && runtime.GOOS == "linux":
			exes = append(exes, filepath.Join(dir, name))
		}
	}
	if len(exes) > 0 {
		return SelectBestExe(exes)
	}

	for _, e := range entries {
		if !e.IsDir() || nestedSkipDirs[strings.ToLower(e.Name())] {
			continue
		}
		if exe := findNestedExe(filepath.Join(dir, e.Name()), depth+1); exe != "" {
			return exe
		}
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
		candidates = append(candidates, scored{p, s})
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

// ListExecutables returns all playable entry points in a directory
// (non-recursive): native executables, launcher scripts, AppImages, and HTML
// entry pages for browser-played games. Skips known non-game files
// (uninstallers, crash handlers, setup programs). Used by the TUI to show
// available executables when editing exe_path.
func ListExecutables(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var exes []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))

		// Skip known non-game executables.
		lower := strings.ToLower(name)
		if strings.Contains(lower, "unitycrashhandler") ||
			strings.Contains(lower, "unins") ||
			strings.Contains(lower, "setup") {
			continue
		}

		switch {
		case ext == ".exe" || ext == ".sh" || ext == ".x86_64" || ext == ".x86":
			exes = append(exes, filepath.Join(dir, name))
		case ext == ".html" || ext == ".htm":
			exes = append(exes, filepath.Join(dir, name))
		case strings.HasSuffix(name, ".AppImage"):
			exes = append(exes, filepath.Join(dir, name))
		}
	}
	return exes
}
