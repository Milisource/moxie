package f95session

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mili/moxie/internal/browser"
	"github.com/mili/moxie/internal/config"
)

// fakeBrowserEnv turns the test binary into a stand-in Firefox: invoked with
// "--profile <dir>", it writes a cookies.sqlite holding cf_clearance (after a
// short delay, like a browser passing the check) and then idles until killed.
const fakeBrowserEnv = "MOXIE_F95SESSION_FAKE_BROWSER"

func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeBrowserEnv); mode != "" {
		runFakeBrowser(mode)
		return
	}
	os.Exit(m.Run())
}

func runFakeBrowser(mode string) {
	var profile string
	for i, a := range os.Args {
		if a == "--profile" && i+1 < len(os.Args) {
			profile = os.Args[i+1]
		}
		if a == "--version" {
			os.Stdout.WriteString("Mozilla Firefox 159.0\n")
			return
		}
	}
	if mode == "clearance" && profile != "" {
		time.Sleep(300 * time.Millisecond)
		_ = writeCookieDB(filepath.Join(profile, "cookies.sqlite"), "cf_clearance", "minted-value", time.Now().Add(time.Hour).UnixMilli())
	}
	time.Sleep(time.Minute)
}

func writeCookieDB(path, name, value string, expiry int64) error {
	db, err := sql.Open("sqlite3", "file:"+path)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS moz_cookies (id INTEGER PRIMARY KEY, name TEXT, value TEXT,
		host TEXT, path TEXT, expiry INTEGER, creationTime INTEGER)`); err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO moz_cookies (name, value, host, path, expiry, creationTime) VALUES (?, ?, '.f95zone.to', '/', ?, ?)`,
		name, value, expiry, time.Now().UnixMicro())
	return err
}

func TestMintWith_ReadsClearanceFromProfile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-group handling differs")
	}
	t.Setenv(fakeBrowserEnv, "clearance")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	cl, err := mintWith(context.Background(), candidate{name: "firefox", bin: self, version: "159.0"})
	if err != nil {
		t.Fatalf("mintWith: %v", err)
	}
	if cl.Value != "minted-value" {
		t.Errorf("clearance = %q", cl.Value)
	}
	if !strings.Contains(cl.UserAgent, "rv:159.0) Gecko/20100101 Firefox/159.0") {
		t.Errorf("UA = %q, want the launched browser's Firefox 159 UA", cl.UserAgent)
	}
	if cl.Expires.Before(time.Now().Add(50 * time.Minute)) {
		t.Errorf("expires = %v, want ~1h out (millisecond expiry)", cl.Expires)
	}
	if time.Since(start) > 20*time.Second {
		t.Errorf("mint took %v", time.Since(start))
	}
}

func TestMintWith_NoClearanceTimesOut(t *testing.T) {
	if testing.Short() || runtime.GOOS == "windows" {
		t.Skip("waits for the mint timeout")
	}
	t.Setenv(fakeBrowserEnv, "idle")
	self, _ := os.Executable()
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	if _, err := mintWith(ctx, candidate{name: "firefox", bin: self, version: "159.0"}); err == nil {
		t.Fatal("mintWith succeeded without a clearance")
	}
}

func TestBinaryVersion(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bin := filepath.Join(dir, "camoufox")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	// Camoufox ships platform.ini with a three-part milestone.
	if err := os.WriteFile(filepath.Join(dir, "platform.ini"), []byte("[Build]\nBuildID=20261006031529\nMilestone=156.0.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if v := binaryVersion(bin); v != "156.0" {
		t.Errorf("binaryVersion = %q, want 156.0", v)
	}
}

func TestFindCamoufox_ActiveVersion(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG cache layout")
	}
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	dir := filepath.Join(cache, "camoufox", "browsers", "official", "156.0.1-beta.36")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "camoufox")
	if err := os.WriteFile(exe, []byte("x"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := `{"active_version": "browsers/official/156.0.1-beta.36"}`
	if err := os.WriteFile(filepath.Join(cache, "camoufox", "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := findCamoufox(); got != exe {
		t.Errorf("findCamoufox = %q, want %q", got, exe)
	}
}

func TestExpiryTime(t *testing.T) {
	t.Parallel()
	sec := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	if got := expiryTime(sec.Unix()); !got.Equal(sec) {
		t.Errorf("seconds: %v", got)
	}
	if got := expiryTime(sec.UnixMilli()); !got.Equal(sec) {
		t.Errorf("milliseconds: %v", got)
	}
	if !expiryTime(0).IsZero() {
		t.Error("session cookie should have no expiry")
	}
}

func TestPersistRoundTrip(t *testing.T) {
	config.SetConfigDirForTest(t.TempDir())
	t.Cleanup(func() { config.SetConfigDirForTest(""); browser.SetF95Clearance(browser.F95Clearance{}) })

	cl := browser.F95Clearance{Value: "v", UserAgent: "ua", Created: time.Now(), Expires: time.Now().Add(time.Hour)}
	persist(cl)
	fi, err := os.Stat(statePath())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("state file mode = %v, want 0600", fi.Mode().Perm())
	}
	browser.SetF95Clearance(browser.F95Clearance{})
	loadPersisted()
	if got, ok := browser.CurrentF95Clearance(); !ok || got.Value != "v" || got.UserAgent != "ua" {
		t.Errorf("loaded %+v ok=%v", got, ok)
	}

	// An expired clearance is dropped, file included.
	persist(browser.F95Clearance{Value: "old", UserAgent: "ua", Expires: time.Now().Add(-time.Minute)})
	browser.SetF95Clearance(browser.F95Clearance{})
	loadPersisted()
	if _, ok := browser.CurrentF95Clearance(); ok {
		t.Error("expired clearance was loaded")
	}
	if _, err := os.Stat(statePath()); !os.IsNotExist(err) {
		t.Error("expired state file was not removed")
	}
}

func TestMintingEnabled(t *testing.T) {
	config.SetConfigDirForTest(t.TempDir())
	t.Cleanup(func() { config.SetConfigDirForTest("") })
	t.Setenv(envVar, "off")
	if mintingEnabled() {
		t.Error("MOXIE_F95_CLEARANCE=off did not disable minting")
	}
	t.Setenv(envVar, "")
	if !mintingEnabled() {
		t.Error("minting should default to on")
	}
}
