package browserresolve

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeBin creates an executable script at dir/<name> so binary detection
// finds it on PATH.
func fakeBin(t *testing.T, dir, name string) {
	t.Helper()
	script := filepath.Join(dir, name)
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// pathWith returns PATH consisting only of dir (plus the original PATH when
// includeOrig, which keeps real binaries visible).
func pathWith(t *testing.T, dir string, includeOrig bool) {
	t.Helper()
	path := dir
	if includeOrig {
		path += string(os.PathListSeparator) + os.Getenv("PATH")
	}
	t.Setenv("PATH", path)
}

// TestSelectEngine_FirefoxOnly picks the firefox engine when only a firefox
// binary exists.
func TestSelectEngine_FirefoxOnly(t *testing.T) {
	old := cookieBrowsersForHost
	defer func() { cookieBrowsersForHost = old }()
	cookieBrowsersForHost = func(string) []string { return nil }
	binDir := t.TempDir()
	fakeBin(t, binDir, "firefox")
	pathWith(t, binDir, false)

	eng, err := selectEngine(&Options{}, "https://vikingfile.com/f/abc")
	if err != nil {
		t.Fatalf("selectEngine: %v", err)
	}
	if _, ok := eng.(*firefoxEngine); !ok {
		t.Fatalf("engine = %T, want *firefoxEngine", eng)
	}
}

// TestSelectEngine_ChromePreferredOverFirefox verifies the design order:
// Chrome-family beats Firefox when both are installed.
func TestSelectEngine_ChromePreferredOverFirefox(t *testing.T) {
	old := cookieBrowsersForHost
	defer func() { cookieBrowsersForHost = old }()
	cookieBrowsersForHost = func(string) []string { return nil }
	binDir := t.TempDir()
	fakeBin(t, binDir, "google-chrome")
	fakeBin(t, binDir, "firefox")
	pathWith(t, binDir, false)

	eng, err := selectEngine(&Options{}, "https://vikingfile.com/f/abc")
	if err != nil {
		t.Fatalf("selectEngine: %v", err)
	}
	if _, ok := eng.(*rodEngine); !ok {
		t.Fatalf("engine = %T, want *rodEngine (chrome preferred)", eng)
	}
}

// TestSelectEngine_CookieHolderWins verifies the browser holding cookies
// for the host is chosen even when another browser is installed.
func TestSelectEngine_CookieHolderWins(t *testing.T) {
	binDir := t.TempDir()
	fakeBin(t, binDir, "google-chrome")
	fakeBin(t, binDir, "firefox")
	pathWith(t, binDir, false)

	old := cookieBrowsersForHost
	defer func() { cookieBrowsersForHost = old }()
	cookieBrowsersForHost = func(string) []string { return []string{"firefox"} }

	eng, err := selectEngine(&Options{}, "https://vikingfile.com/f/abc")
	if err != nil {
		t.Fatalf("selectEngine: %v", err)
	}
	if _, ok := eng.(*firefoxEngine); !ok {
		t.Fatalf("engine = %T, want *firefoxEngine (cookie holder)", eng)
	}
}

// TestSelectEngine_ChromeCookieHolderWins similarly for chrome cookies.
func TestSelectEngine_ChromeCookieHolderWins(t *testing.T) {
	binDir := t.TempDir()
	fakeBin(t, binDir, "firefox")
	pathWith(t, binDir, false)

	old := cookieBrowsersForHost
	defer func() { cookieBrowsersForHost = old }()
	cookieBrowsersForHost = func(string) []string { return []string{"brave"} }

	eng, err := selectEngine(&Options{}, "https://vikingfile.com/f/abc")
	if err != nil {
		t.Fatalf("selectEngine: %v", err)
	}
	if _, ok := eng.(*rodEngine); !ok {
		t.Fatalf("engine = %T, want *rodEngine (brave cookie holder)", eng)
	}
}

// TestSelectEngine_NoBrowser verifies ErrNoBrowser when nothing is
// installed and no cookies point at a browser.
func TestSelectEngine_NoBrowser(t *testing.T) {
	pathWith(t, t.TempDir(), false)
	old := cookieBrowsersForHost
	defer func() { cookieBrowsersForHost = old }()
	cookieBrowsersForHost = func(string) []string { return nil }

	_, err := selectEngine(&Options{}, "https://vikingfile.com/f/abc")
	if !errors.Is(err, ErrNoBrowser) {
		t.Fatalf("expected ErrNoBrowser, got %v", err)
	}
}

// TestSelectEngine_ForcedBrowser verifies explicit WithBrowser overrides
// auto-selection.
func TestSelectEngine_ForcedBrowser(t *testing.T) {
	binDir := t.TempDir()
	fakeBin(t, binDir, "google-chrome")
	fakeBin(t, binDir, "firefox")
	pathWith(t, binDir, false)

	eng, err := selectEngine(&Options{Browser: BrowserFirefox}, "https://vikingfile.com/f/abc")
	if err != nil {
		t.Fatalf("selectEngine: %v", err)
	}
	if _, ok := eng.(*firefoxEngine); !ok {
		t.Fatalf("engine = %T, want *firefoxEngine (forced)", eng)
	}
}

// TestEffectiveBrowserMode covers MOXIE_BROWSER parsing.
func TestEffectiveBrowserMode(t *testing.T) {
	tests := []struct {
		env  string
		want string
	}{
		{"", BrowserAuto},
		{"1", BrowserAuto},
		{"true", BrowserAuto},
		{"auto", BrowserAuto},
		{"chrome", BrowserChrome},
		{"firefox", BrowserFirefox},
		{"bogus", ""},
	}
	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			t.Setenv(browserEnvVar, tt.env)
			if got := effectiveBrowserMode(""); got != tt.want {
				t.Errorf("effectiveBrowserMode = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestInstallDownloaderFallback_InvalidMode verifies invalid MOXIE_BROWSER
// values are rejected without touching the downloader's global hook.
func TestInstallDownloaderFallback_InvalidMode(t *testing.T) {
	t.Setenv(browserEnvVar, "netscape")
	installed, why := InstallDownloaderFallback()
	if installed {
		t.Fatal("invalid mode must not install the fallback")
	}
	if !strings.Contains(why, "invalid") {
		t.Errorf("reason = %q, want invalid-value mention", why)
	}
}
