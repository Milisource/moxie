package downloader

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeResolutionStore struct {
	url    string
	host   string
	saves  int
	getHit bool
}

func (f *fakeResolutionStore) GetResolvedURL(maskedURL string, maxAge time.Duration) (string, string, bool) {
	if f.getHit && f.url != "" {
		return f.url, f.host, true
	}
	return "", "", false
}

func (f *fakeResolutionStore) SaveResolvedURL(maskedURL, resolvedURL, resolvedHost string) error {
	f.saves++
	f.url = resolvedURL
	f.host = resolvedHost
	return nil
}

func TestResolveMasked_PersistsAndReadsCache(t *testing.T) {
	store := &fakeResolutionStore{}
	SetResolutionStore(store)
	defer SetResolutionStore(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"ok","msg":"https://pixeldrain.com/u/persist1"}`)
	}))
	defer server.Close()

	r := NewHostResolver()
	r.SetF95Cookie("xf_user=abc")
	toTestServer(r, server)

	got, err := r.resolveMasked("https://f95zone.to/masked/pixeldrain.com/persist/1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://pixeldrain.com/u/persist1" {
		t.Errorf("got %q", got)
	}
	if store.saves != 1 {
		t.Errorf("expected 1 save, got %d", store.saves)
	}

	// A new resolver with the store primed should not issue an HTTP request.
	store.getHit = true
	called := false
	r2 := NewHostResolver()
	r2.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		return nil, fmt.Errorf("should not be called")
	})
	got2, err := r2.resolveMasked("https://f95zone.to/masked/pixeldrain.com/persist/2")
	if err != nil {
		t.Fatalf("cached resolve failed: %v", err)
	}
	if got2 != "https://pixeldrain.com/u/persist1" {
		t.Errorf("got cached %q", got2)
	}
	if called {
		t.Error("expected no HTTP request for a cached resolution")
	}
}
