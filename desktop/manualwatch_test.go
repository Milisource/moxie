package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalizeMatchText(t *testing.T) {
	cases := map[string]string{
		"Ashfall_RPG_v0.5.5.zip":    "ashfall rpg v0 5 5 zip",
		"Ashfall RPG":               "ashfall rpg",
		"HH Trap: Reality's Glitch": "hh trap reality s glitch",
	}
	for in, want := range cases {
		if got := normalizeMatchText(in); got != want {
			t.Errorf("normalizeMatchText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTitleMatchScore(t *testing.T) {
	if got := titleMatchScore("Ashfall_RPG_v0.5.5.zip", "Ashfall RPG"); got == 0 {
		t.Error("expected the archive to match its game title")
	}
	if got := titleMatchScore("Lunatic.zip", "Luna"); got != 0 {
		t.Errorf("short title matched inside a longer word: score %d", got)
	}
	if got := titleMatchScore("other.zip", "Ashfall RPG"); got != 0 {
		t.Error("unrelated archive matched the title")
	}
}

func TestManualWatcherMatch(t *testing.T) {
	w := &manualWatcher{
		waiters: map[int64]manualWaiter{
			1: {title: "Ashfall RPG", since: time.Now()},
			2: {title: "Barely Working", since: time.Now()},
		},
		seen: map[string]bool{},
	}

	id, ok := w.match("Ashfall_RPG_v0.5.5.zip")
	if !ok || id != 1 {
		t.Fatalf("match = %d, %v; want game 1", id, ok)
	}
	id, ok = w.match("Barely_Working_22.0.2.zip")
	if !ok || id != 2 {
		t.Fatalf("match = %d, %v; want game 2", id, ok)
	}
	if _, ok := w.match("Some_Other_Game.zip"); ok {
		t.Error("ambiguous archive must not be assigned to a game")
	}

	// One waiter is unambiguous even when the filename says nothing.
	w.waiters = map[int64]manualWaiter{3: {title: "Zzz", since: time.Now()}}
	if id, ok := w.match("download.zip"); !ok || id != 3 {
		t.Fatalf("single waiter match = %d, %v; want game 3", id, ok)
	}
}

func writeZip(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	w, err := zw.Create("game/data.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestListArchiveFiles(t *testing.T) {
	dir := t.TempDir()
	writeZip(t, filepath.Join(dir, "Game.zip"))
	for name, body := range map[string]string{
		"notes.txt":           "x",
		"Game.part":           "x",
		"Game.zip.crdownload": "x",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "folder.zip"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := listArchiveFiles(dir)
	if len(got) != 1 || got[0] != "Game.zip" {
		t.Fatalf("listArchiveFiles = %v, want [Game.zip]", got)
	}
}
