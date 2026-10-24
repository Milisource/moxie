package extensionbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// newTestBridge starts a bridge whose opener records the tagged URL.
func newTestBridge(t *testing.T) (*Bridge, *string) {
	t.Helper()
	var opened string
	b, err := New(WithOpener(func(u string) error {
		opened = u
		return nil
	}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b, &opened
}

// tokenFromOpened extracts port and token from the fragment the bridge opens.
func tokenFromOpened(t *testing.T, raw string) (int, string) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse opened URL: %v", err)
	}
	var port int
	var token string
	if _, err := fmt.Sscanf(u.Fragment, "moxie=%d:%s", &port, &token); err != nil {
		t.Fatalf("fragment %q: %v", u.Fragment, err)
	}
	return port, token
}

func postResolved(t *testing.T, port int, body any) *http.Response {
	t.Helper()
	buf, _ := json.Marshal(body)
	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/resolved", port), "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("POST /resolved: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestResolveReturnsExtensionDestination(t *testing.T) {
	t.Parallel()
	b, opened := newTestBridge(t)

	result := make(chan string, 1)
	errs := make(chan error, 1)
	go func() {
		dest, err := b.Resolve(context.Background(), "https://f95zone.to/dl/1/?u=2&k=x&r=3&t=y")
		if err != nil {
			errs <- err
			return
		}
		result <- dest
	}()

	var port int
	var token string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if *opened != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if *opened == "" {
		t.Fatal("opener was never called")
	}
	port, token = tokenFromOpened(t, *opened)
	if !strings.HasPrefix(*opened, "https://f95zone.to/dl/1/?u=2&k=x&r=3&t=y#moxie=") {
		t.Errorf("opened URL = %q, want the target with a moxie fragment", *opened)
	}

	resp := postResolved(t, port, resolveRequest{Token: token, URL: "https://pixeldrain.com/u/abc"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("callback status = %d, want 200", resp.StatusCode)
	}
	select {
	case dest := <-result:
		if dest != "https://pixeldrain.com/u/abc" {
			t.Errorf("dest = %q", dest)
		}
	case err := <-errs:
		t.Fatalf("Resolve: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("Resolve did not return")
	}
}

func TestResolveRejectsWrongToken(t *testing.T) {
	t.Parallel()
	b, opened := newTestBridge(t)

	errs := make(chan error, 1)
	go func() {
		_, err := b.Resolve(context.Background(), "https://f95zone.to/dl/1/")
		errs <- err
	}()
	waitFor(t, opened)
	port, token := tokenFromOpened(t, *opened)

	if resp := postResolved(t, port, resolveRequest{Token: "nope", URL: "https://pixeldrain.com/u/abc"}); resp.StatusCode != http.StatusForbidden {
		t.Errorf("wrong token status = %d, want 403", resp.StatusCode)
	}
	// The correct token still works afterwards.
	if resp := postResolved(t, port, resolveRequest{Token: token, URL: "https://pixeldrain.com/u/abc"}); resp.StatusCode != http.StatusOK {
		t.Errorf("right token status = %d, want 200", resp.StatusCode)
	}
	if err := <-errs; err != nil {
		t.Fatalf("Resolve: %v", err)
	}
}

func TestResolveRejectsF95Destination(t *testing.T) {
	t.Parallel()
	b, opened := newTestBridge(t)

	errs := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_, err := b.Resolve(ctx, "https://f95zone.to/dl/1/")
		errs <- err
	}()
	waitFor(t, opened)
	port, token := tokenFromOpened(t, *opened)

	if resp := postResolved(t, port, resolveRequest{Token: token, URL: "https://f95zone.to/masked/mega.nz/1/2/3"}); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("f95 destination status = %d, want 400", resp.StatusCode)
	}
	cancel()
	if err := <-errs; err == nil {
		t.Fatal("Resolve should fail once the context is cancelled")
	}
}

func TestTokenIsSingleUse(t *testing.T) {
	t.Parallel()
	b, opened := newTestBridge(t)

	go func() { _, _ = b.Resolve(context.Background(), "https://f95zone.to/dl/1/") }()
	waitFor(t, opened)
	port, token := tokenFromOpened(t, *opened)

	if resp := postResolved(t, port, resolveRequest{Token: token, URL: "https://pixeldrain.com/u/a"}); resp.StatusCode != http.StatusOK {
		t.Fatalf("first callback = %d, want 200", resp.StatusCode)
	}
	if resp := postResolved(t, port, resolveRequest{Token: token, URL: "https://pixeldrain.com/u/b"}); resp.StatusCode != http.StatusForbidden {
		t.Errorf("replayed callback = %d, want 403", resp.StatusCode)
	}
}

func waitFor(t *testing.T, s *string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if *s != "" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("opener was never called")
}
