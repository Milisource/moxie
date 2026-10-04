package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Launch starts a game executable in the given game directory.
// It handles platform-specific launchers (Wine on Linux, CrossOver on macOS)
// and sets the working directory so relative asset paths resolve.
// The process is detached — Wait() runs in a background goroutine.
func Launch(exe, gameDir, winePrefix string) error {
	cmd, err := buildCommand(exe, gameDir, winePrefix)
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}
	// Reap the child process in the background.
	go cmd.Wait()
	return nil
}

// buildCommand constructs the appropriate *exec.Cmd for the given executable,
// setting the working directory to gameDir. It detects the file type and
// selects the appropriate launcher (wine for .exe on non-Windows, a browser
// for .html, sh for .sh, etc.).
func buildCommand(exe, gameDir, winePrefix string) (*exec.Cmd, error) {
	ext := strings.ToLower(filepath.Ext(exe))

	setDir := func(cmd *exec.Cmd) *exec.Cmd {
		cmd.Dir = gameDir
		return cmd
	}

	// Configure wine/proton env vars for a wine command.
	configureWine := func(cmd *exec.Cmd) *exec.Cmd {
		cmd.Dir = gameDir
		if winePrefix != "" {
			// cmd.Env == nil means "inherit os.Environ()"; once we set it we
			// must carry the parent environment ourselves, or wine loses
			// HOME/PATH/DISPLAY/XDG_RUNTIME_DIR and cannot start.
			cmd.Env = append(os.Environ(), "WINEPREFIX="+winePrefix)
		}
		return cmd
	}

	switch {
	case ext == ".appimage":
		return setDir(exec.Command(exe)), nil

	case ext == ".sh":
		return setDir(exec.Command("sh", exe)), nil

	case ext == ".html" || ext == ".htm":
		// HTML games have no executable — they are played by opening the page
		// in a browser. Exec'ing the file directly fails with EACCES.
		browser, err := browserCommand(exe)
		if err != nil {
			return nil, err
		}
		return setDir(browser), nil

	case ext == ".exe":
		if runtime.GOOS == "windows" {
			return setDir(exec.Command(exe)), nil
		}
		// Check for wine availability.
		if winePath, err := exec.LookPath("wine"); err == nil {
			return configureWine(exec.Command(winePath, exe)), nil
		}
		// macOS: try CrossOver as fallback.
		if runtime.GOOS == "darwin" {
			if crossover, err := findCrossOver(); err == nil {
				return configureWine(exec.Command(crossover, exe)), nil
			}
		}
		return nil, fmt.Errorf("wine not found — cannot launch .exe files on %s", runtime.GOOS)

	default:
		// Native binary (.x86_64, .x86, no extension).
		return setDir(exec.Command(exe)), nil
	}
}

// browserCommand builds a command that opens target (a local file path or URL)
// in the user's default browser. It is used for HTML games, whose entry point
// is a web page rather than a process.
func browserCommand(target string) (*exec.Cmd, error) {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", target), nil
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", target), nil
	default:
		if p, err := exec.LookPath("xdg-open"); err == nil {
			return exec.Command(p, target), nil
		}
		if p, err := exec.LookPath("gio"); err == nil {
			return exec.Command(p, "open", target), nil
		}
		return nil, fmt.Errorf("no browser opener found — install xdg-utils (xdg-open) to play HTML games")
	}
}

// findCrossOver locates the CrossOver wine binary on macOS.
func findCrossOver() (string, error) {
	candidates := []string{
		"/Applications/CrossOver.app/Contents/SharedSupport/CrossOver/bin/wine",
		"/Applications/CrossOver.app/Contents/SharedSupport/crossover/bin/wine",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("CrossOver not found")
}
