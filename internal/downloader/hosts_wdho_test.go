package downloader

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResolveWDHO_SignedToken(t *testing.T) {
	oldDelay := wdhoTokenDelay
	wdhoTokenDelay = 10 * time.Millisecond
	defer func() { wdhoTokenDelay = oldDelay }()

	direct := "https://wdho.ru/kbhw?pt=abc123"
	encoded := base64.StdEncoding.EncodeToString([]byte(direct))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") == "" {
			t.Error("missing Referer header")
		}
		http.SetCookie(w, &http.Cookie{Name: "filehosting", Value: "test-cookie", Path: "/"})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<div class="download-timer"></div><script>var e='%s';</script>`, encoded)
	}))
	defer server.Close()

	r := NewHostResolver()
	res, err := r.resolveWDHO(server.URL)
	if err != nil {
		t.Fatalf("resolveWDHO: %v", err)
	}
	if res.URL != direct {
		t.Errorf("URL = %q, want %q", res.URL, direct)
	}
	if !strings.Contains(res.Headers["Cookie"], "filehosting=test-cookie") {
		t.Errorf("missing session cookie: %q", res.Headers["Cookie"])
	}
	if res.Headers["Referer"] != server.URL {
		t.Errorf("Referer = %q, want %q", res.Headers["Referer"], server.URL)
	}
}

func TestResolveWDHO_Challenge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<form action="/kbhw" method="post"><input name="antibot" value="x"><button class="btn-color">GREEN</button></form>`)
	}))
	defer server.Close()

	r := NewHostResolver()
	_, err := r.resolveWDHO(server.URL)
	if err == nil || !strings.Contains(err.Error(), "colour") {
		t.Fatalf("expected colour challenge error, got %v", err)
	}
}
