package downloader

import (
	"runtime"
	"strings"
)

type Platform string

const (
	PlatformLinux   Platform = "linux"
	PlatformWindows Platform = "windows"
	PlatformMacOS   Platform = "macos"
	PlatformAll     Platform = "all"
	PlatformUnknown Platform = "unknown"
)

// DetectPlatform attempts to determine the target platform from a download link's name/URL.
func DetectPlatform(name, url string) Platform {
	text := strings.ToLower(name + " " + url)

	// Linux indicators (check first to avoid "darwin" matching windows via "win")
	linuxTerms := []string{"linux", "ubuntu", "debian", "fedora", "arch", "manjaro", "opensuse"}
	for _, term := range linuxTerms {
		if strings.Contains(text, term) {
			return PlatformLinux
		}
	}

	// Binary/shell formats that are platform-specific
	if strings.Contains(text, ".appimage") {
		return PlatformLinux
	}
	if strings.Contains(text, ".sh") && !strings.Contains(text, ".sh.") && !strings.Contains(text, ".sh?") {
		return PlatformLinux
	}
	if strings.Contains(text, "tar.gz") || strings.Contains(text, ".tgz") {
		return PlatformLinux
	}

	// Windows indicators
	windowsTerms := []string{"windows", ".exe", ".msi", "setup", "installer"}
	for _, term := range windowsTerms {
		if strings.Contains(text, term) {
			return PlatformWindows
		}
	}
	// "win" standalone (not part of "windows", "linux", etc.)
	words := strings.Fields(text)
	for _, w := range words {
		w = strings.Trim(w, " .-")
		if w == "win" {
			return PlatformWindows
		}
	}

	// MacOS indicators
	macTerms := []string{"macos", ".dmg", ".pkg", "osx"}
	for _, term := range macTerms {
		if strings.Contains(text, term) {
			return PlatformMacOS
		}
	}
	if strings.Contains(text, "mac ") || strings.Contains(text, "-mac") || strings.Contains(text, "_mac") {
		return PlatformMacOS
	}

	// Check "darwin" but after "win" detection to avoid false positives
	if strings.Contains(text, "darwin") {
		return PlatformMacOS
	}

	// Web/HTML is cross-platform
	if strings.Contains(text, "html") || strings.Contains(text, "web") {
		return PlatformAll
	}

	return PlatformUnknown
}

// CurrentPlatform returns the current runtime platform.
func CurrentPlatform() Platform {
	switch runtime.GOOS {
	case "linux":
		return PlatformLinux
	case "windows":
		return PlatformWindows
	case "darwin":
		return PlatformMacOS
	default:
		return PlatformUnknown
	}
}

// PlatformMatches returns true if the download platform could reasonably run
// on the current platform (with emulation/compatibility layers).
func PlatformMatches(downloadPlatform, current Platform) bool {
	if downloadPlatform == PlatformAll || current == PlatformAll {
		return true
	}
	if downloadPlatform == PlatformUnknown {
		return true
	}
	if downloadPlatform == current {
		return true
	}
	// Windows binaries run on Linux via Wine/Proton
	if current == PlatformLinux && downloadPlatform == PlatformWindows {
		return true
	}
	return false
}

// PlatformPriority returns a priority score for platform matching.
// Higher is better. Accounts for compatibility layers like Wine/Proton.
//
// Priority chain for each platform:
//
//	Linux:  native (100) > Windows via Wine (70) > cross-platform (50) > unknown (25) > Mac (0)
//	Windows: native (100) > cross-platform (50) > unknown (25) > Linux/Mac (0)
//	Mac:     native (100) > cross-platform (50) > unknown (25) > Linux/Windows (0)
func PlatformPriority(downloadPlatform, current Platform) int {
	if downloadPlatform == current {
		return 100
	}
	if downloadPlatform == PlatformAll {
		return 50
	}
	if downloadPlatform == PlatformUnknown {
		return 25
	}
	// Windows binaries run on Linux via Wine/Proton — next best thing
	if current == PlatformLinux && downloadPlatform == PlatformWindows {
		return 70
	}
	return 0
}
