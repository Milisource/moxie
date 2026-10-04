package launcher

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// TestBuildCommandWinePrefixKeepsEnvironment guards against setting cmd.Env to
// a one-element slice. exec.Cmd treats a nil Env as "inherit os.Environ()", so
// appending to it would start wine with WINEPREFIX and nothing else — no HOME,
// PATH, or DISPLAY — and the game would fail to launch.
func TestBuildCommandWinePrefixKeepsEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("wine path not used on Windows")
	}
	if _, err := exec.LookPath("wine"); err != nil {
		t.Skip("wine not installed")
	}

	// Pick a variable that is certain to be in the parent environment.
	const sentinel = "MOXIE_LAUNCH_TEST"
	t.Setenv(sentinel, "1")

	cmd, err := buildCommand("/games/test/game.exe", "/games/test", "/prefixes/test")
	if err != nil {
		t.Fatalf("buildCommand failed: %v", err)
	}

	var gotPrefix, gotSentinel bool
	for _, kv := range cmd.Env {
		switch {
		case kv == "WINEPREFIX=/prefixes/test":
			gotPrefix = true
		case strings.HasPrefix(kv, sentinel+"="):
			gotSentinel = true
		}
	}
	if !gotPrefix {
		t.Error("WINEPREFIX not set in cmd.Env")
	}
	if !gotSentinel {
		t.Errorf("parent environment was dropped: cmd.Env has %d entries, want the full environment", len(cmd.Env))
	}
	if cmd.Dir != "/games/test" {
		t.Errorf("cmd.Dir = %q, want %q", cmd.Dir, "/games/test")
	}
}

// TestBuildCommandNoWinePrefixInheritsEnvironment verifies the no-prefix case
// leaves Env nil so the child inherits the parent environment implicitly.
func TestBuildCommandNoWinePrefixInheritsEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("wine path not used on Windows")
	}
	if _, err := exec.LookPath("wine"); err != nil {
		t.Skip("wine not installed")
	}

	cmd, err := buildCommand("/games/test/game.exe", "/games/test", "")
	if err != nil {
		t.Fatalf("buildCommand failed: %v", err)
	}
	if cmd.Env != nil {
		t.Errorf("cmd.Env = %v, want nil (inherit)", cmd.Env)
	}
}

// TestBuildCommandNativeBinary covers the non-wine branches.
func TestBuildCommandNativeBinary(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "game.x86_64")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	cmd, err := buildCommand(exe, dir, "")
	if err != nil {
		t.Fatalf("buildCommand failed: %v", err)
	}
	if cmd.Dir != dir {
		t.Errorf("cmd.Dir = %q, want %q", cmd.Dir, dir)
	}
	if cmd.Path != exe {
		t.Errorf("cmd.Path = %q, want %q", cmd.Path, exe)
	}
}

// TestBuildCommandHTMLLaunchesBrowser guards the reported regression: an HTML
// game must be handed to a browser opener, never exec'd directly (which fails
// with "permission denied").
func TestBuildCommandHTMLLaunchesBrowser(t *testing.T) {
	dir := t.TempDir()
	html := filepath.Join(dir, "Applicant_0.42.html")
	if err := os.WriteFile(html, []byte("<html><canvas></canvas></html>"), 0644); err != nil {
		t.Fatal(err)
	}

	cmd, err := buildCommand(html, dir, "")
	if err != nil {
		// A Linux system without xdg-open/gio must still refuse to exec the
		// HTML file and report a clear error rather than EACCES.
		if runtime.GOOS == "linux" && strings.Contains(err.Error(), "browser opener") {
			return
		}
		t.Fatalf("buildCommand failed: %v", err)
	}

	if cmd.Dir != dir {
		t.Errorf("cmd.Dir = %q, want %q", cmd.Dir, dir)
	}
	if cmd.Path == html {
		t.Errorf("HTML file must not be exec'd directly (cmd.Path = %q)", cmd.Path)
	}
	if !slices.Contains(cmd.Args, html) {
		t.Errorf("cmd.Args = %v, want the HTML path passed to the opener", cmd.Args)
	}
}
