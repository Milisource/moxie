package db

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// Round-trip: PutResolvedURL then GetResolvedURL returns the cached value;
// a URL that was never cached misses.
func TestResolvedURLCacheRoundTrip(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	const masked = "https://f95zone.to/masked/pixeldrain.com/abc/x"
	if _, ok := d.GetResolvedURL(masked); ok {
		t.Fatal("GetResolvedURL on an empty cache must miss")
	}

	if err := d.PutResolvedURL(masked, "https://pixeldrain.com/u/BYzajuVk", "pixeldrain"); err != nil {
		t.Fatalf("PutResolvedURL: %v", err)
	}

	got, ok := d.GetResolvedURL(masked)
	if !ok {
		t.Fatal("GetResolvedURL after PutResolvedURL must hit")
	}
	if got != "https://pixeldrain.com/u/BYzajuVk" {
		t.Errorf("GetResolvedURL = %q, want the pixeldrain URL", got)
	}

	// An empty masked URL never hits.
	if _, ok := d.GetResolvedURL(""); ok {
		t.Error("GetResolvedURL(\"\") must miss")
	}
}

// Upsert: re-putting the same masked URL replaces the destination, keeps a
// single row, restarts the TTL, and bumps hits.
func TestResolvedURLUpsert(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	const masked = "https://f95zone.to/masked/mega.nz/abc/x"
	if err := d.PutResolvedURL(masked, "https://mega.nz/file/AAA", "mega"); err != nil {
		t.Fatalf("first PutResolvedURL: %v", err)
	}
	if err := d.PutResolvedURL(masked, "https://mega.nz/file/BBB", "mega"); err != nil {
		t.Fatalf("second PutResolvedURL: %v", err)
	}

	var rows int
	if err := d.conn.QueryRow("SELECT COUNT(*) FROM resolved_urls").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("resolved_urls rows = %d, want 1 (upsert must not duplicate)", rows)
	}

	got, ok := d.GetResolvedURL(masked)
	if !ok || got != "https://mega.nz/file/BBB" {
		t.Errorf("GetResolvedURL after upsert = %q, %v; want the second destination", got, ok)
	}

	var hits int64
	if err := d.conn.QueryRow("SELECT hits FROM resolved_urls WHERE masked_url = ?", masked).Scan(&hits); err != nil {
		t.Fatal(err)
	}
	if hits != 2 {
		t.Errorf("hits = %d, want 2", hits)
	}
}

// Stale entries read as misses: a row older than ResolvedURLTTL is not
// returned even before pruning runs.
func TestGetResolvedURL_StaleEntryMisses(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	const masked = "https://f95zone.to/masked/pixeldrain.com/old/x"
	if err := d.PutResolvedURL(masked, "https://pixeldrain.com/u/OLD", "pixeldrain"); err != nil {
		t.Fatal(err)
	}
	backdate := time.Now().Add(-2 * ResolvedURLTTL).Unix()
	if _, err := d.conn.Exec("UPDATE resolved_urls SET created_at = ? WHERE masked_url = ?", backdate, masked); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.GetResolvedURL(masked); ok {
		t.Error("stale entry must read as a miss")
	}
}

// Prune: DeleteResolvedURLsOlderThan removes only expired rows, keeps fresh
// ones, and returns the deleted count.
func TestDeleteResolvedURLsOlderThan(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	old := "https://f95zone.to/masked/example.com/old/x"
	fresh := "https://f95zone.to/masked/example.com/fresh/x"
	if err := d.PutResolvedURL(old, "https://old.example.com/f", "example.com"); err != nil {
		t.Fatal(err)
	}
	if err := d.PutResolvedURL(fresh, "https://fresh.example.com/f", "example.com"); err != nil {
		t.Fatal(err)
	}
	backdate := time.Now().Add(-2 * ResolvedURLTTL).Unix()
	if _, err := d.conn.Exec("UPDATE resolved_urls SET created_at = ? WHERE masked_url = ?", backdate, old); err != nil {
		t.Fatal(err)
	}

	n, err := d.PruneResolvedURLs()
	if err != nil {
		t.Fatalf("PruneResolvedURLs: %v", err)
	}
	if n != 1 {
		t.Errorf("PruneResolvedURLs deleted %d rows, want 1", n)
	}
	if _, ok := d.GetResolvedURL(old); ok {
		t.Error("pruned entry must miss")
	}
	if _, ok := d.GetResolvedURL(fresh); !ok {
		t.Error("fresh entry must survive pruning")
	}
}

// v7 → v10: opening a pre-v10 database must create the resolved_urls table
// and the cache must work. makeV7DB exercises the full v8 → v9 → v10 chain.
func TestMigrateV10AddsResolvedURLs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v7.sqlite")
	makeV7DB(t, path)

	d, err := Open(path)
	if err != nil {
		t.Fatalf("Open on v7 db: %v", err)
	}
	defer d.Close()

	var userVersion int
	if err := d.conn.QueryRow("PRAGMA user_version").Scan(&userVersion); err != nil {
		t.Fatal(err)
	}
	if userVersion != currentSchemaVersion {
		t.Fatalf("user_version = %d, want %d", userVersion, currentSchemaVersion)
	}

	const masked = "https://f95zone.to/masked/pixeldrain.com/mig/x"
	if err := d.PutResolvedURL(masked, "https://pixeldrain.com/u/mig", "pixeldrain"); err != nil {
		t.Fatalf("PutResolvedURL on migrated db: %v", err)
	}
	if got, ok := d.GetResolvedURL(masked); !ok || got != "https://pixeldrain.com/u/mig" {
		t.Errorf("GetResolvedURL on migrated db = %q, %v; want hit", got, ok)
	}
}

// A bare user_version=9 database (no resolved_urls table yet) must upgrade
// cleanly with the v10 step alone.
func TestMigrateV10FromBareV9(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v9.sqlite")
	conn, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open bare v9 db: %v", err)
	}
	if _, err := conn.Exec("PRAGMA user_version = 9"); err != nil {
		t.Fatalf("set user_version 9: %v", err)
	}
	conn.Close()

	d, err := Open(path)
	if err != nil {
		t.Fatalf("Open on bare v9 db: %v", err)
	}
	defer d.Close()

	var userVersion int
	if err := d.conn.QueryRow("PRAGMA user_version").Scan(&userVersion); err != nil {
		t.Fatal(err)
	}
	if userVersion != currentSchemaVersion {
		t.Fatalf("user_version = %d, want %d", userVersion, currentSchemaVersion)
	}
	if _, ok := d.GetResolvedURL("https://f95zone.to/masked/x"); ok {
		t.Fatal("empty migrated cache must miss")
	}
}
