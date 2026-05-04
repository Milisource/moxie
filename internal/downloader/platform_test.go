package downloader

import (
	"runtime"
	"testing"
)

func TestDetectPlatform_Linux(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		url  string
	}{
		{"My Game Linux", ""},
		{"", "https://example.com/game-linux.zip"},
		{"Ubuntu build", ""},
		{"Debian package", ""},
		{"Fedora version", ""},
		{"Arch release", ""},
		{"Manjaro build", ""},
		{"openSUSE package", ""},
		{"game.AppImage", ""},
		{"install.sh", ""},
		{"archive.tar.gz", ""},
		{"game.tgz", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectPlatform(tt.name, tt.url)
			if got != PlatformLinux {
				t.Errorf("DetectPlatform(%q, %q) = %q, want %q", tt.name, tt.url, got, PlatformLinux)
			}
		})
	}
}

func TestDetectPlatform_Windows(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		url  string
	}{
		{"Windows version", ""},
		{"game.exe", ""},
		{"setup.msi", ""},
		{"Setup program", ""},
		{"win release", ""},     // standalone "win" word
		{"Win installer", ""},  // "installer" triggers windows
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectPlatform(tt.name, tt.url)
			if got != PlatformWindows {
				t.Errorf("DetectPlatform(%q, %q) = %q, want %q", tt.name, tt.url, got, PlatformWindows)
			}
		})
	}
}

func TestDetectPlatform_MacOS(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		url  string
	}{
		{"macOS version", ""},
		{"game.dmg", ""},
		{"Some .pkg file", ""},
		{"OSX build", ""},
		{"mac build", ""},
		{"darwin build", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectPlatform(tt.name, tt.url)
			if got != PlatformMacOS {
				t.Errorf("DetectPlatform(%q, %q) = %q, want %q", tt.name, tt.url, got, PlatformMacOS)
			}
		})
	}
}

func TestDetectPlatform_CrossPlatform(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		url  string
	}{
		{"HTML version", ""},
		{"Web version", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectPlatform(tt.name, tt.url)
			if got != PlatformAll {
				t.Errorf("DetectPlatform(%q, %q) = %q, want %q", tt.name, tt.url, got, PlatformAll)
			}
		})
	}
}

func TestDetectPlatform_Unknown(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		url  string
	}{
		{"", ""},
		{"Generic download link", "https://example.com/file"},
		{"Some weird name", ""},
		{"game.zip", ""}, // .zip alone is not platform-specific
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectPlatform(tt.name, tt.url)
			if got != PlatformUnknown {
				t.Errorf("DetectPlatform(%q, %q) = %q, want %q", tt.name, tt.url, got, PlatformUnknown)
			}
		})
	}
}

func TestDetectPlatform_PriorityOrder(t *testing.T) {
	t.Parallel()
	// Linux terms checked first
	got := DetectPlatform("game.linux.zip", "")
	if got != PlatformLinux {
		t.Errorf("expected Linux (checked first), got %q", got)
	}

	// Windows terms checked before MacOS
	got2 := DetectPlatform("mac.exe", "")
	if got2 != PlatformWindows {
		t.Errorf("expected Windows (.exe triggers before mac), got %q", got2)
	}

	// AppImage is Linux (specific binary format)
	got3 := DetectPlatform("game.AppImage", "")
	if got3 != PlatformLinux {
		t.Errorf("expected Linux (.AppImage), got %q", got3)
	}
}

func TestCurrentPlatform(t *testing.T) {
	t.Parallel()
	got := CurrentPlatform()
	switch runtime.GOOS {
	case "linux":
		if got != PlatformLinux {
			t.Errorf("CurrentPlatform() = %q, want %q", got, PlatformLinux)
		}
	case "windows":
		if got != PlatformWindows {
			t.Errorf("CurrentPlatform() = %q, want %q", got, PlatformWindows)
		}
	case "darwin":
		if got != PlatformMacOS {
			t.Errorf("CurrentPlatform() = %q, want %q", got, PlatformMacOS)
		}
	default:
		if got != PlatformUnknown {
			t.Errorf("CurrentPlatform() = %q, want %q", got, PlatformUnknown)
		}
	}
}

func TestPlatformMatches(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		download, current Platform
		want              bool
	}{
		{"same linux", PlatformLinux, PlatformLinux, true},
		{"same windows", PlatformWindows, PlatformWindows, true},
		{"same macos", PlatformMacOS, PlatformMacOS, true},
		{"all with linux", PlatformAll, PlatformLinux, true},
		{"all with windows", PlatformAll, PlatformWindows, true},
		{"all with macos", PlatformAll, PlatformMacOS, true},
		{"current is all", PlatformLinux, PlatformAll, true},
		{"unknown download", PlatformUnknown, PlatformLinux, true},
		{"windows on linux via wine", PlatformWindows, PlatformLinux, true},
		{"linux on windows", PlatformLinux, PlatformWindows, false},
		{"linux vs macos", PlatformLinux, PlatformMacOS, false},
		{"windows vs macos", PlatformWindows, PlatformMacOS, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PlatformMatches(tt.download, tt.current)
			if got != tt.want {
				t.Errorf("PlatformMatches(%q, %q) = %v, want %v", tt.download, tt.current, got, tt.want)
			}
		})
	}
}

func TestPlatformPriority(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		download, current Platform
		want              int
	}{
		{"native linux", PlatformLinux, PlatformLinux, 100},
		{"native windows", PlatformWindows, PlatformWindows, 100},
		{"native macos", PlatformMacOS, PlatformMacOS, 100},
		{"cross-platform (all)", PlatformAll, PlatformLinux, 50},
		{"cross-platform with windows", PlatformAll, PlatformWindows, 50},
		{"unknown download", PlatformUnknown, PlatformLinux, 25},
		{"different platform", PlatformLinux, PlatformWindows, 0},
		{"windows-vs-macos", PlatformWindows, PlatformMacOS, 0},
		{"macos-vs-linux", PlatformMacOS, PlatformLinux, 0},
		{"windows-via-wine-on-linux", PlatformWindows, PlatformLinux, 70},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PlatformPriority(tt.download, tt.current)
			if got != tt.want {
				t.Errorf("PlatformPriority(%q, %q) = %d, want %d", tt.download, tt.current, got, tt.want)
			}
		})
	}
}

func TestPlatformPriority_Ordering_OnLinux(t *testing.T) {
	t.Parallel()
	// On Linux: native(100) > windows via wine(70) > all(50) > unknown(25) > mac(0)
	native := PlatformPriority(PlatformLinux, PlatformLinux)
	wine := PlatformPriority(PlatformWindows, PlatformLinux)
	all := PlatformPriority(PlatformAll, PlatformLinux)
	unknown := PlatformPriority(PlatformUnknown, PlatformLinux)
	mac := PlatformPriority(PlatformMacOS, PlatformLinux)

	if !(native > wine && wine > all && all > unknown && unknown > mac) {
		t.Errorf("expected native(%d) > wine(%d) > all(%d) > unknown(%d) > mac(%d)",
			native, wine, all, unknown, mac)
	}
}

func TestPlatformPriority_Ordering_OnWindows(t *testing.T) {
	t.Parallel()
	// On Windows: native(100) > all(50) > unknown(25) > linux/mac(0)
	native := PlatformPriority(PlatformWindows, PlatformWindows)
	all := PlatformPriority(PlatformAll, PlatformWindows)
	unknown := PlatformPriority(PlatformUnknown, PlatformWindows)
	linux := PlatformPriority(PlatformLinux, PlatformWindows)
	mac := PlatformPriority(PlatformMacOS, PlatformWindows)

	if !(native > all && all > unknown && unknown >= linux && linux == mac) {
		t.Errorf("expected native(%d) > all(%d) > unknown(%d) >= linux(%d) == mac(%d)",
			native, all, unknown, linux, mac)
	}
}
