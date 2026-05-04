// Package downloader provides HTTP download functionality with resume support
// and progress reporting for game files.
package downloader

import (
	"runtime"
	"strings"
)

// Platform represents a target operating system.
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
	// Combine name and URL for detection
	text := strings.ToLower(name + " " + url)

	// Linux indicators
	linuxTerms := []string{"linux", "ubuntu", "debian", "fedora", "arch", ".appimage", ".sh", "tar.gz", "tgz"}
	for _, term := range linuxTerms {
		if strings.Contains(text, term) {
			return PlatformLinux
		}
	}

	// Windows indicators
	windowsTerms := []string{"windows", "win", ".exe", ".msi", ".zip", "setup", "installer"}
	for _, term := range windowsTerms {
		if strings.Contains(text, term) {
			return PlatformWindows
		}
	}

	// MacOS indicators
	macTerms := []string{"macos", "mac", "osx", ".dmg", ".pkg", "darwin"}
	for _, term := range macTerms {
		if strings.Contains(text, term) {
			return PlatformMacOS
		}
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

// PlatformMatches returns true if the download platform matches or is compatible with the current platform.
func PlatformMatches(downloadPlatform, current Platform) bool {
	if downloadPlatform == PlatformAll || current == PlatformAll {
		return true
	}
	if downloadPlatform == PlatformUnknown {
		return true // Unknown is compatible with all
	}
	return downloadPlatform == current
}

// PlatformPriority returns a priority score for platform matching.
// Higher is better. Used for sorting download links by platform preference.
func PlatformPriority(downloadPlatform, current Platform) int {
	if downloadPlatform == current {
		return 100 // Native platform is best
	}
	if downloadPlatform == PlatformAll {
		return 50 // Cross-platform is second best
	}
	if downloadPlatform == PlatformUnknown {
		return 25 // Unknown might work
	}
	return 0 // Different platform, lowest priority
}
