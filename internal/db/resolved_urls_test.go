package db

import (
	"path/filepath"
	"testing"
	"time"
)

func TestResolvedURLCache_RoundTrip(t *testing.T) {
	t.Parallel()
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	masked := "https://f95zone.to/masked/pixeldrain.com/1/2/3"
	resolved := "https://pixeldrain.com/u/abc123"
	if err := database.SaveResolvedURL(masked, resolved, "pixeldrain"); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, host, ok := database.GetResolvedURL(masked, time.Hour)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got != resolved || host != "pixeldrain" {
		t.Errorf("got %q/%q", got, host)
	}

	// A second read increments hits but still returns the same value.
	if got, _, ok := database.GetResolvedURL(masked, time.Hour); !ok || got != resolved {
		t.Errorf("second read: %q ok=%v", got, ok)
	}

	n, err := database.CountResolvedURLs()
	if err != nil || n != 1 {
		t.Errorf("count = %d, err = %v", n, err)
	}
}

func TestResolvedURLCache_Expiry(t *testing.T) {
	t.Parallel()
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	masked := "https://f95zone.to/masked/pixeldrain.com/1/2/3"
	if err := database.SaveResolvedURL(masked, "https://pixeldrain.com/u/abc", "pixeldrain"); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := database.GetResolvedURL(masked, -time.Second); ok {
		t.Error("expected cache miss for non-positive max age")
	}
}

func TestResolvedURLCache_Prune(t *testing.T) {
	t.Parallel()
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := database.SaveResolvedURL("m", "https://pixeldrain.com/u/a", "pixeldrain"); err != nil {
		t.Fatal(err)
	}
	if err := database.PruneResolvedURLs(-time.Second); err != nil {
		t.Fatal(err)
	}
	n, err := database.CountResolvedURLs()
	if err != nil || n != 0 {
		t.Errorf("count = %d, err = %v", n, err)
	}
}
