package downloader

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

// memResolvedCache is a minimal in-memory stand-in for the DB-backed cache
// pair the app wires in via SetDefaultResolvedCache: get/put keyed by the
// masked URL.
type memResolvedCache struct {
	mu    sync.Mutex
	store map[string]string
}

func newMemResolvedCache() *memResolvedCache {
	return &memResolvedCache{store: make(map[string]string)}
}

func (c *memResolvedCache) get(maskedURL string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.store[maskedURL]
	return v, ok
}

func (c *memResolvedCache) put(maskedURL, resolved, _ string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store[maskedURL] = resolved
}

func (c *memResolvedCache) seed(maskedURL, resolved string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store[maskedURL] = resolved
}

// maskedUnwrapServer returns a test server whose /masked/ endpoint unwraps
// to realURL (JSON status "ok") and counts every hit.
func maskedUnwrapServer(t *testing.T, realURL string, unwrapCalls *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		unwrapCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok","msg":"%s"}`, realURL)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A masked URL already in the cache resolves without touching the unwrap
// endpoint at all — zero POSTs across repeated Resolve calls. The cached
// destination still runs host-specific resolution (pixeldrain rewrite).
func TestResolve_MaskedURLCacheHitSkipsUnwrap(t *testing.T) {
	t.Parallel()
	const realURL = "https://pixeldrain.com/u/BYzajuVk"
	var unwrapCalls atomic.Int32
	srv := maskedUnwrapServer(t, realURL, &unwrapCalls)
	masked := srv.URL + "/masked/pixeldrain.com/123/x"

	cache := newMemResolvedCache()
	cache.seed(masked, realURL)

	r := NewHostResolver()
	r.SetResolvedCache(cache.get, cache.put)

	for i := 0; i < 2; i++ {
		result, err := r.Resolve(masked, "pixeldrain")
		if err != nil {
			t.Fatalf("Resolve #%d: %v", i+1, err)
		}
		if result.URL != "https://pixeldrain.com/api/file/BYzajuVk" {
			t.Errorf("Resolve #%d = %q, want pixeldrain API URL", i+1, result.URL)
		}
	}
	if got := unwrapCalls.Load(); got != 0 {
		t.Errorf("unwrap endpoint hit %d times, want 0 (cache served every resolve)", got)
	}
}

// Cache-miss → store: the first resolve unwraps exactly once and stores the
// result; the second resolve is served from the cache (still one unwrap
// total).
func TestResolve_MaskedURLCacheMissStores(t *testing.T) {
	t.Parallel()
	const realURL = "https://pixeldrain.com/u/BYzajuVk"
	var unwrapCalls atomic.Int32
	srv := maskedUnwrapServer(t, realURL, &unwrapCalls)
	masked := srv.URL + "/masked/pixeldrain.com/123/x"

	cache := newMemResolvedCache()
	r := NewHostResolver()
	r.SetResolvedCache(cache.get, cache.put)

	for i := 0; i < 2; i++ {
		result, err := r.Resolve(masked, "pixeldrain")
		if err != nil {
			t.Fatalf("Resolve #%d: %v", i+1, err)
		}
		if result.URL != "https://pixeldrain.com/api/file/BYzajuVk" {
			t.Errorf("Resolve #%d = %q, want pixeldrain API URL", i+1, result.URL)
		}
	}
	if got := unwrapCalls.Load(); got != 1 {
		t.Errorf("unwrap endpoint hit %d times, want 1 (second resolve must hit cache)", got)
	}
	if cached, ok := cache.get(masked); !ok || cached != realURL {
		t.Errorf("cache after resolve = %q, %v; want stored %q", cached, ok, realURL)
	}
}

// With no cache wired (nil pair), every resolve hits the unwrap endpoint —
// the zero-value resolver keeps the pre-cache behavior.
func TestResolve_MaskedURLNoCacheHitsEndpointEachTime(t *testing.T) {
	t.Parallel()
	const realURL = "https://pixeldrain.com/u/BYzajuVk"
	var unwrapCalls atomic.Int32
	srv := maskedUnwrapServer(t, realURL, &unwrapCalls)
	masked := srv.URL + "/masked/pixeldrain.com/123/x"

	r := NewHostResolver()
	for i := 0; i < 2; i++ {
		if _, err := r.Resolve(masked, "pixeldrain"); err != nil {
			t.Fatalf("Resolve #%d: %v", i+1, err)
		}
	}
	if got := unwrapCalls.Load(); got != 2 {
		t.Errorf("unwrap endpoint hit %d times, want 2 without a cache", got)
	}
}
