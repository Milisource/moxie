//go:build !windows

package browserresolve

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeFakeFirefoxScript creates an executable fake-firefox script that
// mimics a real download session. Modes:
//   - "download": write fake-game.zip into the download dir from user.js,
//     then stay alive (like a real browser)
//   - "exit": exit immediately without a file (crash/refusal)
//   - "headful-only": exit without a file when launched -headless, download
//     otherwise (exercises the headless→headful escalation)
//
// Every invocation appends its args to $FAKE_FIREFOX_LOG when set.
func writeFakeFirefoxScript(t *testing.T, dir, mode string) string {
	t.Helper()
	script := filepath.Join(dir, "fake-firefox")
	body := fmt.Sprintf(`#!/bin/sh
log="$FAKE_FIREFOX_LOG"
[ -n "$log" ] && echo "$*" >> "$log"
profile=""
headless=0
prev=""
for arg in "$@"; do
  if [ "$prev" = "-profile" ]; then profile="$arg"; fi
  if [ "$arg" = "-headless" ]; then headless=1; fi
  prev="$arg"
done
if [ "%s" = "exit" ]; then exit 0; fi
if [ "%s" = "headful-only" ] && [ "$headless" = "1" ]; then exit 0; fi
dir=$(sed -n 's/.*"browser.download.dir", "\([^"]*\)".*/\1/p' "$profile/user.js")
echo "fake-game.zip" > "$dir/fake-game.zip"
while true; do sleep 30; done
`, mode, mode)
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

// buildFirefoxFixture creates a Firefox-style profile root: profiles.ini
// with a Default=1 profile, and that profile dir with cookies.sqlite.
func buildFirefoxFixture(t *testing.T) (root, profileDir string) {
	t.Helper()
	root = t.TempDir()
	profileDir = filepath.Join(root, "abcdefgh.default")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "cookies.sqlite"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	ini := `[Profile1]
Name=secondary
IsRelative=1
Path=secondary.default

[Profile0]
Name=default
IsRelative=1
Path=abcdefgh.default
Default=1
`
	if err := os.WriteFile(filepath.Join(root, "profiles.ini"), []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, profileDir
}

func TestParseFirefoxProfilesINI(t *testing.T) {
	tests := []struct {
		name    string
		ini     string
		want    string
		wantErr bool
	}{
		{
			name: "default profile wins",
			ini: `[Profile0]
IsRelative=1
Path=first.default

[Profile1]
IsRelative=1
Path=second.default
Default=1
`,
			want: "second.default",
		},
		{
			name: "first entry fallback when no default",
			ini: `[Profile0]
IsRelative=1
Path=first.default

[Profile1]
Path=second.default
`,
			want: "first.default",
		},
		{
			name: "install section default",
			ini: `[InstallXXXX]
Default=abc.default-release

[Profile0]
IsRelative=1
Path=other.default
`,
			want: "abc.default-release",
		},
		{
			name: "crlf and comments tolerated",
			ini:  ";[comment]\r\n[Profile0]\r\nPath=win.default\r\nDefault=1\r\n",
			want: "win.default",
		},
		{
			name:    "empty file",
			ini:     "",
			wantErr: true,
		},
		{
			name:    "no profile entries",
			ini:     "[General]\nStartWithLastProfile=1\n",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "profiles.ini")
			if err := os.WriteFile(path, []byte(tt.ini), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := parseFirefoxProfilesINI(path)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseFirefoxProfilesINI: %v", err)
			}
			if got != tt.want {
				t.Errorf("profile = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveFirefoxProfile(t *testing.T) {
	root, profileDir := buildFirefoxFixture(t)

	t.Run("direct profile dir", func(t *testing.T) {
		got, err := resolveFirefoxProfile(profileDir)
		if err != nil {
			t.Fatalf("resolveFirefoxProfile: %v", err)
		}
		if got != profileDir {
			t.Errorf("got %q, want %q", got, profileDir)
		}
	})
	t.Run("root with profiles.ini resolves default", func(t *testing.T) {
		got, err := resolveFirefoxProfile(root)
		if err != nil {
			t.Fatalf("resolveFirefoxProfile: %v", err)
		}
		if got != profileDir {
			t.Errorf("got %q, want %q", got, profileDir)
		}
	})
	t.Run("nonexistent", func(t *testing.T) {
		if _, err := resolveFirefoxProfile(filepath.Join(t.TempDir(), "missing")); err == nil {
			t.Fatal("expected error for missing dir")
		}
	})
	t.Run("root without profiles.ini", func(t *testing.T) {
		empty := t.TempDir()
		if _, err := resolveFirefoxProfile(empty); err == nil {
			t.Fatal("expected error for root without profiles.ini")
		}
	})
	t.Run("ini pointing at missing profile", func(t *testing.T) {
		badRoot := t.TempDir()
		os.WriteFile(filepath.Join(badRoot, "profiles.ini"), []byte("[Profile0]\nPath=ghost.default\n"), 0o600)
		if _, err := resolveFirefoxProfile(badRoot); err == nil {
			t.Fatal("expected error for ini pointing at a missing profile")
		}
	})
}

func TestDiscoverFirefoxProfileDir(t *testing.T) {
	root, profileDir := buildFirefoxFixture(t)

	t.Run("override wins", func(t *testing.T) {
		got, err := discoverFirefoxProfileDir(root)
		if err != nil {
			t.Fatalf("discoverFirefoxProfileDir: %v", err)
		}
		if got != profileDir {
			t.Errorf("got %q, want %q", got, profileDir)
		}
	})
	t.Run("env override", func(t *testing.T) {
		t.Setenv(firefoxEnvProfile, profileDir)
		got, err := discoverFirefoxProfileDir("")
		if err != nil {
			t.Fatalf("discoverFirefoxProfileDir: %v", err)
		}
		if got != profileDir {
			t.Errorf("got %q, want %q", got, profileDir)
		}
	})
	t.Run("no profile anywhere", func(t *testing.T) {
		t.Setenv(firefoxEnvProfile, "")
		t.Setenv("HOME", t.TempDir())
		_, err := discoverFirefoxProfileDir("")
		if !errors.Is(err, ErrNoProfile) {
			t.Fatalf("expected ErrNoProfile, got %v", err)
		}
	})
}

func TestWriteFirefoxUserJS(t *testing.T) {
	profile := t.TempDir()
	dlDir := filepath.Join(t.TempDir(), "dl with spaces")
	if err := os.MkdirAll(dlDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFirefoxUserJS(profile, dlDir); err != nil {
		t.Fatalf("writeFirefoxUserJS: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(profile, "user.js"))
	if err != nil {
		t.Fatalf("read user.js: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `"browser.download.folderList", 2`) {
		t.Errorf("missing folderList pref:\n%s", content)
	}
	// Forward slashes on every OS — backslashes are escapes in prefs files.
	if !strings.Contains(content, `"browser.download.dir", "`+filepath.ToSlash(dlDir)+`"`) {
		t.Errorf("download.dir must use forward slashes:\n%s", content)
	}
	if strings.Contains(content, `\`) {
		t.Errorf("user.js must not contain backslashes:\n%s", content)
	}
	if !strings.Contains(content, "neverAsk.saveToDisk") {
		t.Errorf("missing MIME auto-save list:\n%s", content)
	}
}

func TestWriteFirefoxUserJS_EscapesQuotes(t *testing.T) {
	profile := t.TempDir()
	dlDir := filepath.Join(t.TempDir(), `weird"dir`)
	if err := os.MkdirAll(dlDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFirefoxUserJS(profile, dlDir); err != nil {
		t.Fatalf("writeFirefoxUserJS: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(profile, "user.js"))
	if !strings.Contains(string(data), `weird\"dir`) {
		t.Errorf("embedded dir must be quote-escaped:\n%s", string(data))
	}
}

// TestFirefoxEngine_Run_Download exercises the full raw-launch pipeline with
// a fake Firefox binary: profile copy → user.js → launch (args verified) →
// poll the download dir (.part ignored) → file detected.
func TestFirefoxEngine_Run_Download(t *testing.T) {
	if os.Getenv("MOXIE_BROWSERRESOLVE_LIVE") == "" {
		t.Skip("set MOXIE_BROWSERRESOLVE_LIVE=1 — launches a subprocess per the fake firefox contract")
	}
	binDir := t.TempDir()
	bin := writeFakeFirefoxScript(t, binDir, "download")
	root, _ := buildFirefoxFixture(t)

	e := newFirefoxEngine(Options{
		BinPath:         bin,
		DownloadTimeout: 10 * time.Second,
	})
	logPath := filepath.Join(t.TempDir(), "calls.log")
	t.Setenv("FAKE_FIREFOX_LOG", logPath)
	profileCopy := t.TempDir() // stand-in for the resolver's profile copy
	downloadDir := t.TempDir()
	if err := copyProfile(filepath.Join(root, "abcdefgh.default"), profileCopy); err != nil {
		t.Fatalf("copyProfile: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := e.run(ctx, engineRequest{
		url:         "https://bzzhr.to/abc123",
		profileDir:  profileCopy,
		downloadDir: downloadDir,
		opts:        Options{DownloadTimeout: 10 * time.Second},
	})
	if err != nil {
		t.Fatalf("firefox engine run: %v", err)
	}
	if filepath.Base(res.path) != "fake-game.zip" {
		t.Errorf("result path = %q, want fake-game.zip", res.path)
	}
	if res.suggested != "fake-game.zip" {
		t.Errorf("suggested = %q, want fake-game.zip", res.suggested)
	}

	// The fake browser must have been launched with the profile and URL.
	logContent, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("fake firefox was never launched: %v", err)
	}
	if !strings.Contains(string(logContent), "-profile") {
		t.Errorf("fake firefox did not receive -profile (calls: %q)", logContent)
	}
	if !strings.Contains(string(logContent), "bzzhr.to/abc123") {
		t.Errorf("fake firefox did not receive the URL (calls: %q)", logContent)
	}
	if !strings.Contains(string(logContent), "-headless") {
		t.Errorf("fake firefox was not launched headless (calls: %q)", logContent)
	}
}

// TestFirefoxEngine_Run_ExitsWithoutFile verifies an early browser exit
// (crash at startup) fails fast instead of idling out the timeout.
func TestFirefoxEngine_Run_ExitsWithoutFile(t *testing.T) {
	if os.Getenv("MOXIE_BROWSERRESOLVE_LIVE") == "" {
		t.Skip("set MOXIE_BROWSERRESOLVE_LIVE=1 — launches a subprocess per the fake firefox contract")
	}
	bin := writeFakeFirefoxScript(t, t.TempDir(), "exit")
	e := newFirefoxEngine(Options{BinPath: bin})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := e.run(ctx, engineRequest{
		url:         "https://bzzhr.to/abc123",
		profileDir:  t.TempDir(),
		downloadDir: t.TempDir(),
		opts:        Options{DownloadTimeout: 20 * time.Second},
	})
	if err == nil {
		t.Fatal("expected error when firefox exits without a file")
	}
	if !strings.Contains(err.Error(), "exited without") {
		t.Errorf("error = %v, want early-exit message", err)
	}
}

// TestFirefoxEngine_Run_HeadfulEscalation verifies the headless→headful
// retry: the headless session fails, the headful session downloads.
func TestFirefoxEngine_Run_HeadfulEscalation(t *testing.T) {
	if os.Getenv("MOXIE_BROWSERRESOLVE_LIVE") == "" {
		t.Skip("set MOXIE_BROWSERRESOLVE_LIVE=1 — launches a subprocess per the fake firefox contract")
	}
	// Headful needs a display (or xvfb-run); the escalation wraps in
	// xvfb-run only when DISPLAY is unset.
	if os.Getenv("DISPLAY") == "" {
		if _, err := exec.LookPath("xvfb-run"); err != nil {
			t.Skip("no DISPLAY and no xvfb-run — cannot run headful")
		}
	}
	bin := writeFakeFirefoxScript(t, t.TempDir(), "headful-only")
	logPath := filepath.Join(t.TempDir(), "calls.log")
	t.Setenv("FAKE_FIREFOX_LOG", logPath)
	root, _ := buildFirefoxFixture(t)

	e := newFirefoxEngine(Options{BinPath: bin, DownloadTimeout: 10 * time.Second})
	profileCopy := t.TempDir()
	downloadDir := t.TempDir()
	if err := copyProfile(filepath.Join(root, "abcdefgh.default"), profileCopy); err != nil {
		t.Fatalf("copyProfile: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	res, err := e.run(ctx, engineRequest{
		url:         "https://bzzhr.to/abc123",
		profileDir:  profileCopy,
		downloadDir: downloadDir,
		opts:        Options{DownloadTimeout: 10 * time.Second},
	})
	if err != nil {
		t.Fatalf("firefox engine run: %v", err)
	}
	if filepath.Base(res.path) != "fake-game.zip" {
		t.Errorf("result path = %q, want fake-game.zip (headful attempt)", res.path)
	}
	// Two launches: headless (failed) + headful (succeeded).
	logContent, _ := os.ReadFile(logPath)
	if got := strings.Count(string(logContent), "bzzhr.to/abc123"); got != 2 {
		t.Errorf("firefox launched %d times, want 2 (headless + headful)\ncalls:\n%s", got, logContent)
	}
}

// TestFirefoxEngine_NoBinary verifies ErrNoBrowser when no firefox binary
// can be found.
func TestFirefoxEngine_NoBinary(t *testing.T) {
	e := newFirefoxEngine(Options{})
	t.Setenv(firefoxEnvBin, "")
	// Force a PATH without firefox: replace PATH with an empty dir.
	t.Setenv("PATH", t.TempDir())
	_, err := e.run(context.Background(), engineRequest{
		url:         "https://bzzhr.to/abc123",
		profileDir:  t.TempDir(),
		downloadDir: t.TempDir(),
		opts:        Options{},
	})
	if !errors.Is(err, ErrNoBrowser) {
		t.Fatalf("expected ErrNoBrowser, got %v", err)
	}
}

// TestFirefoxEngine_ProfileCopyKeepsCookies verifies the profile copy keeps
// cookies.sqlite + WAL (the clearance) and skips lock files and caches.
func TestFirefoxEngine_ProfileCopyKeepsCookies(t *testing.T) {
	src := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(src, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("cookies.sqlite", "cookies")
	write("cookies.sqlite-wal", "wal")
	write("cookies.sqlite-shm", "shm")
	write(".parentlock", "lock")
	write("parent.lock", "lock")
	write("prefs.js", "prefs")
	if err := os.MkdirAll(filepath.Join(src, "cache2"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join("cache2", "big.bin"), "cache")
	if err := os.MkdirAll(filepath.Join(src, "startupCache"), 0o755); err != nil {
		t.Fatal(err)
	}

	dst := t.TempDir()
	if err := copyProfile(src, dst); err != nil {
		t.Fatalf("copyProfile: %v", err)
	}
	for _, keep := range []string{"cookies.sqlite", "cookies.sqlite-wal", "cookies.sqlite-shm", "prefs.js"} {
		if _, err := os.Stat(filepath.Join(dst, keep)); err != nil {
			t.Errorf("profile copy missing %s: %v", keep, err)
		}
	}
	for _, skip := range []string{".parentlock", "parent.lock", "cache2", "startupCache"} {
		if _, err := os.Stat(filepath.Join(dst, skip)); !os.IsNotExist(err) {
			t.Errorf("profile copy must not contain %s", skip)
		}
	}
}
