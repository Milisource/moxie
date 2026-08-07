package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The desktop updater must never resolve to the CLI asset names published by
// .github/workflows/release.yml — installing one over the running GUI binary
// is unrecoverable.
func TestBinaryNameIsDesktopSpecific(t *testing.T) {
	name := binaryName()
	if name == "" {
		t.Skipf("no desktop asset naming for this platform")
	}
	if !strings.HasPrefix(name, "moxie-desktop-") {
		t.Errorf("binaryName() = %q, want a moxie-desktop-* asset", name)
	}
	// The CLI assets are moxie-<os>-<arch>; make sure we cannot collide.
	for _, cli := range []string{
		"moxie-linux-amd64", "moxie-linux-arm64",
		"moxie-macos-amd64", "moxie-macos-arm64",
		"moxie-windows-amd64.exe", "moxie-windows-arm64.exe",
	} {
		if name == cli {
			t.Errorf("binaryName() returned CLI asset %q", cli)
		}
	}
}

type fakeNetErr struct{ timeout bool }

func (e fakeNetErr) Error() string   { return "fake net error" }
func (e fakeNetErr) Timeout() bool   { return e.timeout }
func (e fakeNetErr) Temporary() bool { return false }

func TestIsTransientDownloadError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"timeout", fakeNetErr{timeout: true}, true},
		{"non-timeout net error", fakeNetErr{timeout: false}, false},
		{"wrapped timeout", fmt.Errorf("download: %w", fakeNetErr{timeout: true}), true},
		{"http 500", errors.New("host returned HTTP 500"), true},
		{"http 503", errors.New("host returned HTTP 503"), true},
		{"http 404", errors.New("host returned HTTP 404"), false},
		{"unexpected eof", fmt.Errorf("read body: %w", io.ErrUnexpectedEOF), true},
		{"conn reset", fmt.Errorf("read: %w", syscall.ECONNRESET), true},
		{"plain error", errors.New("bad archive"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isTransientDownloadError(c.err); got != c.want {
				t.Errorf("isTransientDownloadError(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

// net.Error is satisfied by our fake — guards against the interface changing
// out from under the type switch above.
var _ net.Error = fakeNetErr{}

func TestIsNewerVersion(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"0.5.0", "0.4.0-alpha", true},
		{"0.4.0", "0.4.0-alpha", false}, // prerelease suffix is stripped
		{"v1.0.0", "0.9.9", true},
		{"0.4.1", "0.4.0", true},
		{"0.4.0", "0.4.1", false},
		{"0.4.0", "0.4.0", false},
		{"1.0", "0.9.9", true},
	}
	for _, c := range cases {
		if got := isNewerVersion(c.latest, c.current); got != c.want {
			t.Errorf("isNewerVersion(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}

func TestUpdateStagePathIsNotWorldWritableTemp(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	p, err := updateStagePath("moxie-desktop-linux-amd64")
	if err != nil {
		t.Fatalf("updateStagePath: %v", err)
	}
	// The staged binary is later executed as the app; it must not sit at a
	// predictable path inside the shared system temp directory.
	if strings.HasPrefix(p, "/tmp/") {
		t.Errorf("update staged under shared temp: %q", p)
	}
	if !strings.HasSuffix(p, "moxie-desktop-linux-amd64") {
		t.Errorf("unexpected staged path %q", p)
	}
}

// bgShutdownGrace must stay short enough that quitting feels immediate.
func TestBgShutdownGraceIsBounded(t *testing.T) {
	if bgShutdownGrace <= 0 || bgShutdownGrace > 15*time.Second {
		t.Errorf("bgShutdownGrace = %v, want a small positive bound", bgShutdownGrace)
	}
}
