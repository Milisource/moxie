package launcher

import (
	"os/exec"
	"runtime"
)

// WinePath returns the path to a Wine or CrossOver binary and whether
// one was found. On Linux it checks PATH for "wine"; on macOS it also
// checks common CrossOver installation paths.
func WinePath() (string, bool) {
	if p, err := exec.LookPath("wine"); err == nil {
		return p, true
	}
	if runtime.GOOS == "darwin" {
		if p, err := findCrossOver(); err == nil {
			return p, true
		}
	}
	return "", false
}

// IsCrossOver returns true if the given path points to a CrossOver wine binary.
func IsCrossOver(path string) bool {
	// CrossOver is macOS-only.
	if runtime.GOOS != "darwin" {
		return false
	}
	// Check if any known CrossOver path matches.
	candidates := []string{
		"/Applications/CrossOver.app/Contents/SharedSupport/CrossOver/bin/wine",
		"/Applications/CrossOver.app/Contents/SharedSupport/crossover/bin/wine",
	}
	for _, c := range candidates {
		if path == c {
			return true
		}
	}
	return false
}

// NeedsWine returns true if the executable needs Wine to run on the current
// platform (i.e., a .exe on non-Windows).
func NeedsWine(exe string) bool {
	if runtime.GOOS == "windows" {
		return false
	}
	// Only .exe files need Wine.
	return len(exe) > 4 && exe[len(exe)-4:] == ".exe"
}
