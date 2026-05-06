package downloader

import "strings"

// DetectPlatformFromLink attempts to determine the platform from a download link's name and URL.
func DetectPlatformFromLink(name, url string) string {
	lower := strings.ToLower(name + " " + url)

	// Linux indicators
	linuxTerms := []string{"linux", "ubuntu", "debian", "fedora", "arch", ".appimage", ".sh", "tar.gz", "tgz"}
	for _, term := range linuxTerms {
		if strings.Contains(lower, term) {
			return "linux"
		}
	}

	// Windows indicators
	windowsTerms := []string{"windows", "win", ".exe", ".msi", "setup", "installer"}
	for _, term := range windowsTerms {
		if strings.Contains(lower, term) {
			return "windows"
		}
	}

	// Mac indicators
	macTerms := []string{"macos", "mac", "osx", ".dmg", ".pkg", "darwin"}
	for _, term := range macTerms {
		if strings.Contains(lower, term) {
			return "macos"
		}
	}

	return "unknown"
}
