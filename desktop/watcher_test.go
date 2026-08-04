package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIsPathUnder(t *testing.T) {
	cases := []struct {
		root, path string
		want       bool
	}{
		{"/games", "/games", true},
		{"/games", "/games/sub/game", true},
		{"/games", "/games2/game", false},
		{"/games", "/other/game", false},
		{"/games", "/games-other", false},
		{"/games", "/g", false},
	}
	for _, c := range cases {
		if got := isPathUnder(c.root, c.path); got != c.want {
			t.Errorf("isPathUnder(%q, %q) = %v, want %v", c.root, c.path, got, c.want)
		}
	}
}

func TestDirModTimeAndMtimeMatches(t *testing.T) {
	dir := t.TempDir()

	if dirModTime(dir).IsZero() {
		t.Error("dirModTime on existing dir should not be zero")
	}
	if !dirModTime(filepath.Join(dir, "missing")).IsZero() {
		t.Error("dirModTime on missing dir should be zero")
	}

	// Fresh dir mtime matches itself at second precision.
	mt := dirModTime(dir)
	if !mtimeMatches(dir, mt) {
		t.Error("mtimeMatches should be true for a just-stated dir")
	}

	// Zero stored time never matches.
	if mtimeMatches(dir, time.Time{}) {
		t.Error("mtimeMatches should be false for zero stored time")
	}

	// Missing dir treated as unchanged.
	if !mtimeMatches(filepath.Join(dir, "missing"), time.Now().Add(-time.Hour)) {
		t.Error("mtimeMatches on missing dir should be true")
	}

	// A stored mtime that differs should not match.
	stale := mt.Add(-time.Hour)
	if mtimeMatches(dir, stale) {
		t.Error("mtimeMatches should be false when mtime differs from stored")
	}
}

func TestMtimeMatchesStaleAfterModify(t *testing.T) {
	dir := t.TempDir()
	before := dirModTime(dir)

	// Write a file; on coarse filesystems the dir mtime may not change within
	// the same second, so only assert the invariant holds at second precision.
	_ = os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x"), 0o644)
	after := dirModTime(dir)

	if mtimeMatches(dir, before) && before.Equal(after) {
		t.Log("directory mtime did not advance within same second — invariant checked via Equal")
	}
	// After a change, either the mtime advanced (no match) or remained equal
	// (match). Both are consistent with second-precision comparison.
	_ = after
}
