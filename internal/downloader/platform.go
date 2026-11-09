package downloader

import (
	"regexp"
	"runtime"
	"strings"
)

type Platform string

const (
	PlatformLinux   Platform = "linux"
	PlatformWindows Platform = "windows"
	PlatformMacOS   Platform = "macos"
	PlatformAll     Platform = "all"
	PlatformAndroid Platform = "android"
	PlatformUnknown Platform = "unknown"
)

// DetectPlatform attempts to determine the target platform from a download link's name/URL.
//
// Link names are "section · platform" (scraper.linkSectionPlatform), e.g.
// "DOWNLOAD Win · Mac": the trailing segment is the most specific label, so
// it is classified first and the whole text only as a fallback.
func DetectPlatform(name, url string) Platform {
	if i := strings.LastIndex(name, "·"); i >= 0 {
		if p := detectPlatformText(strings.ToLower(name[i+len("·"):])); p != PlatformUnknown {
			return p
		}
		if p := detectPlatformText(strings.ToLower(name[:i])); p != PlatformUnknown {
			return p
		}
		return detectPlatformText(strings.ToLower(url))
	}
	return detectPlatformText(strings.ToLower(name + " " + url))
}

// winWordRE matches "win" as a word, including "win32"/"win64" and
// combined labels like "win/linux".
var winWordRE = regexp.MustCompile(`(^|[^a-z])win(32|64)?([^a-z]|$)`)

// macWordRE matches "mac" as a word ("DOWNLOAD · Mac", "-mac").
var macWordRE = regexp.MustCompile(`(^|[^a-z])mac([^a-z]|$)`)

// distroRE matches Linux distro names as whole words ("arch" must not
// match "search").
var distroRE = regexp.MustCompile(`\b(ubuntu|debian|fedora|arch|manjaro|opensuse)\b`)

func detectPlatformText(text string) Platform {
	// Android builds never run as a desktop install.
	if strings.Contains(text, "android") || strings.Contains(text, ".apk") {
		return PlatformAndroid
	}

	hasLinux := strings.Contains(text, "linux") || distroRE.MatchString(text)
	hasWin := strings.Contains(text, "windows") || strings.Contains(text, ".exe") || winWordRE.MatchString(text)
	// "Win/Linux" builds bundle both launchers — Ren'Py ships
	// one archive with .exe and .sh.
	if hasLinux && hasWin {
		return PlatformAll
	}
	if hasLinux {
		return PlatformLinux
	}

	// Binary/shell formats that are platform-specific
	if strings.Contains(text, ".appimage") {
		return PlatformLinux
	}
	if strings.Contains(text, ".sh") && !strings.Contains(text, ".sh.") && !strings.Contains(text, ".sh?") {
		return PlatformLinux
	}
	if strings.Contains(text, "tar.gz") || strings.Contains(text, ".tgz") || strings.Contains(text, "tar.bz2") || strings.Contains(text, "tar.xz") {
		return PlatformLinux
	}

	// Windows indicators
	if hasWin || strings.Contains(text, ".msi") || strings.Contains(text, "setup") || strings.Contains(text, "installer") {
		return PlatformWindows
	}

	// MacOS indicators
	macTerms := []string{"macos", ".dmg", ".pkg", "osx"}
	for _, term := range macTerms {
		if strings.Contains(text, term) {
			return PlatformMacOS
		}
	}
	if macWordRE.MatchString(text) || strings.Contains(text, "_mac") {
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
	if downloadPlatform == PlatformAndroid {
		return false
	}
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

// InstalledPlatform infers which build of a game is installed from its
// launcher path: ".exe"/".bat" → Windows, ".sh"/".x86_64"/".appimage" or an
// extension-less binary → Linux, ".app" → macOS. Empty or unrecognised
// paths return PlatformUnknown.
func InstalledPlatform(exePath string) Platform {
	if exePath == "" {
		return PlatformUnknown
	}
	lower := strings.ToLower(exePath)
	if strings.Contains(lower, ".app/") || strings.HasSuffix(lower, ".app") {
		return PlatformMacOS
	}
	base := lower
	if i := strings.LastIndexAny(base, `/\\`); i >= 0 {
		base = base[i+1:]
	}
	switch {
	case strings.HasSuffix(base, ".exe"), strings.HasSuffix(base, ".bat"):
		return PlatformWindows
	case strings.HasSuffix(base, ".sh"), strings.HasSuffix(base, ".x86_64"),
		strings.HasSuffix(base, ".x86"), strings.HasSuffix(base, ".appimage"):
		return PlatformLinux
	case !strings.Contains(base, "."):
		return PlatformLinux
	}
	return PlatformUnknown
}

// InstallPlatformPriority scores a link for updating an existing install
// whose build platform is known. Unlike PlatformPriority it never swaps
// builds: a Linux archive merged over a Windows install (or vice versa)
// leaves a broken game, so any other explicit platform scores -1 (reject).
func InstallPlatformPriority(downloadPlatform, installed Platform) int {
	switch downloadPlatform {
	case installed:
		return 100
	case PlatformAll:
		return 90
	case PlatformUnknown:
		return 25
	}
	return -1
}
