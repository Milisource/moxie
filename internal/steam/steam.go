package steam

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

// FindSteamRoot locates the Steam installation directory for the current platform.
// Returns ErrSteamNotFound if not found.
func FindSteamRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("steam: cannot determine home directory: %w", err)
	}

	var candidates []string
	switch runtime.GOOS {
	case "windows":
		candidates = []string{
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Steam"),
			filepath.Join("C:", "Program Files (x86)", "Steam"),
		}
	case "darwin":
		candidates = []string{
			filepath.Join(home, "Library", "Application Support", "Steam"),
		}
	default: // linux
		candidates = []string{
			filepath.Join(home, ".steam", "steam"),
			filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam"),
		}
	}

	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p, nil
		}
	}

	return "", ErrSteamNotFound
}

// FindSteamUsers scans <steamRoot>/userdata/ for numeric directory names
// and returns the sorted list of Steam ID3 values. Returns ErrNoUsers
// if no numeric user directories are found.
func FindSteamUsers(steamRoot string) ([]uint32, error) {
	userdataDir := filepath.Join(steamRoot, "userdata")
	entries, err := os.ReadDir(userdataDir)
	if err != nil {
		return nil, fmt.Errorf("steam: cannot read userdata directory %q: %w", userdataDir, err)
	}

	var users []uint32
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		n, err := strconv.ParseUint(e.Name(), 10, 32)
		if err != nil {
			continue // non-numeric directories (like "ac") are not user folders
		}
		if n == 0 {
			continue // user ID 0 is never a valid Steam account
		}

		// Validate it looks like a real Steam user directory: must contain
		// a config/ subdirectory with at least one file Steam would create.
		configDir := filepath.Join(userdataDir, e.Name(), "config")
		if info, err := os.Stat(configDir); err != nil || !info.IsDir() {
			continue
		}
		users = append(users, uint32(n))
	}

	if len(users) == 0 {
		return nil, ErrNoUsers
	}

	slices.Sort(users)
	return users, nil
}

// ResolveSteamPaths builds the full SteamPaths struct for a given user.
// Returns an error if the user directory doesn't exist.
func ResolveSteamPaths(steamRoot string, userID3 uint32) (*SteamPaths, error) {
	userDir := filepath.Join(steamRoot, "userdata", strconv.FormatUint(uint64(userID3), 10))
	if _, err := os.Stat(userDir); err != nil {
		return nil, fmt.Errorf("steam: user directory not found: %s", userDir)
	}

	return &SteamPaths{
		SteamRoot:    steamRoot,
		UserID3:      userID3,
		ShortcutsVDF: filepath.Join(userDir, "config", "shortcuts.vdf"),
		GridDir:      filepath.Join(userDir, "config", "grid"),
		ConfigVDF:    filepath.Join(steamRoot, "config", "config.vdf"),
	}, nil
}

// IsSteamRunning checks whether the Steam client process is currently running.
// Detection is platform-specific: /proc on Linux, pgrep on macOS, tasklist on Windows.
// Returns (true, nil) if Steam is running, (false, nil) if not.
func IsSteamRunning() (bool, error) {
	switch runtime.GOOS {
	case "linux":
		return isSteamRunningLinux()
	case "darwin":
		return isSteamRunningDarwin()
	case "windows":
		return isSteamRunningWindows()
	default:
		return false, nil
	}
}

// isSteamRunningLinux checks /proc for known Steam process names.
// Matches exact comm names, not substrings, to avoid false positives
// (e.g. on CI runners where unrelated process names may contain "steam").
func isSteamRunningLinux() (bool, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, nil
	}
	// Only exact comm names from the Steam client and its helpers.
	steamProcesses := map[string]bool{
		"steam":          true, // main client
		"steamwebhelper": true, // embedded Chromium helper
	}
	for _, e := range entries {
		if !e.IsDir() || !isNumeric(e.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", e.Name(), "comm"))
		if err != nil {
			continue
		}
		comm := strings.TrimSpace(string(data))
		if steamProcesses[comm] {
			return true, nil
		}
	}
	return false, nil
}

// isSteamRunningDarwin checks for a steam process via pgrep.
func isSteamRunningDarwin() (bool, error) {
	cmd := exec.Command("pgrep", "-q", "steam")
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil // not found
		}
		return false, nil // pgrep not available, assume not running
	}
	return true, nil
}

// isSteamRunningWindows checks for a steam.exe process via tasklist.
func isSteamRunningWindows() (bool, error) {
	cmd := exec.Command("tasklist", "/FI", "IMAGENAME eq steam.exe", "/NH")
	out, err := cmd.Output()
	if err != nil {
		return false, nil
	}
	return strings.Contains(string(out), "steam.exe"), nil
}

// isNumeric returns true when s consists only of digit characters.
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
