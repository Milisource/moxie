package downloader

import (
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/mili/moxie/internal/db"
)

func TestResolveBrowserFileUnwrapIsNotCached(t *testing.T) {
	const token = "moxie-browser-file://one-shot"
	var calls atomic.Int32
	srv := maskedUnwrapServer(t, token, &calls)
	original := http.DefaultTransport
	http.DefaultTransport = rewriteToServer("f95zone.to", srv.URL)
	defer func() { http.DefaultTransport = original }()
	cache := newMemResolvedCache()
	r := NewHostResolver()
	r.unwrapLimiter = nil
	r.SetResolvedCache(cache.get, cache.put)
	const masked = "https://f95zone.to/masked/example.com/123/x"
	for i := 0; i < 2; i++ {
		result, err := r.Resolve(masked, "")
		if err != nil || result.URL != token {
			t.Fatalf("Resolve = %v, %v", result, err)
		}
	}
	if _, ok := cache.get(masked); ok {
		t.Fatal("browser token was cached")
	}
	if calls.Load() != 2 {
		t.Fatalf("unwrap calls=%d, want 2", calls.Load())
	}
}

func TestResolveLegacyBrowserFileCacheIsDeleted(t *testing.T) {
	const masked = "https://f95zone.to/masked/example.com/123/x"
	database, err := db.Open(filepath.Join(t.TempDir(), "cache.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.PutResolvedURL(masked, "moxie-browser-file://old", "unknown"); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	const real = "https://downloads.example.com/game.zip"
	srv := maskedUnwrapServer(t, real, &calls)
	original := http.DefaultTransport
	http.DefaultTransport = rewriteToServer("f95zone.to", srv.URL)
	defer func() { http.DefaultTransport = original }()
	r := NewHostResolver()
	r.unwrapLimiter = nil
	r.SetResolvedCache(database.GetResolvedURL, nil)
	result, err := r.Resolve(masked, "")
	if err != nil || result.URL != real {
		t.Fatalf("Resolve = %v, %v", result, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("unwrap calls=%d, want 1", calls.Load())
	}
	if cached, ok := database.GetResolvedURL(masked); ok {
		t.Fatalf("legacy browser token remains cached: %q", cached)
	}
}
