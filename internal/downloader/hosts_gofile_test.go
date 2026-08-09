package downloader

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// golden vector computed independently (python3 hashlib):
// sha256("Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0::en-US::tok123::123::9844d94d963d30")
const gofileGoldenWT = "c9cc6da714447e5379cd377bd1b701249c6d6b103621dde01775d2e987fb17f4"

func TestGofileToken_GoldenVector(t *testing.T) {
	got := gofileTokenWithSlot("tok123", 123)
	if got != gofileGoldenWT {
		t.Fatalf("wt mismatch: got %s want %s", got, gofileGoldenWT)
	}
}

func TestGofileToken_DeterministicWithinSlot(t *testing.T) {
	slot := time.Now().Unix() / gofileSlotSeconds
	first := gofileTokenWithSlot("tokA", slot)
	second := gofileTokenWithSlot("tokA", slot)
	if first != second {
		t.Fatalf("wt not deterministic within a slot: %s vs %s", first, second)
	}
	other := gofileTokenWithSlot("tokB", slot)
	if first == other {
		t.Fatalf("wt must change with the account token")
	}
	otherSlot := gofileTokenWithSlot("tokA", slot+1)
	if first == otherSlot {
		t.Fatalf("wt must change across slots")
	}
}

// gofileTestServer spins up a fake gofile API and returns the base URL.
func gofileTestServer(t *testing.T) (*httptest.Server, *gofileFixture) {
	t.Helper()
	fx := &gofileFixture{
		token:      "guest-token-123",
		fileLink:   "https://s123.gofile.io/downloadweb/abc123/file1",
		guestCalls: 0,
		contents:   map[string]int{},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/accounts":
			fx.guestCalls++
			if req.Method != http.MethodPost {
				t.Errorf("accounts: method = %s, want POST", req.Method)
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"status": "ok",
				"data":   map[string]any{"token": fx.token, "tier": "guest"},
			})
		case "/contents/abc123":
			fx.contents[req.URL.String()]++
			if got := req.Header.Get("Authorization"); got != "Bearer "+fx.token {
				t.Errorf("contents: Authorization = %q", got)
			}
			wt := req.Header.Get("X-Website-Token")
			if wt == "" {
				t.Errorf("contents: missing X-Website-Token header")
			}
			if got := req.Header.Get("X-BL"); got != gofileLanguage {
				t.Errorf("contents: X-BL = %q, want %q", got, gofileLanguage)
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"status": "ok",
				"data": map[string]any{
					"children": map[string]any{
						"file1": map[string]any{"type": "file", "link": fx.fileLink},
						"file2": map[string]any{"type": "file", "link": ""},
					},
				},
			})
		default:
			t.Errorf("unexpected path %s", req.URL.Path)
			http.NotFound(w, req)
		}
	}))
	fx.base = srv.URL
	return srv, fx
}

type gofileFixture struct {
	base       string
	token      string
	fileLink   string
	guestCalls int
	contents   map[string]int
}

func TestResolveGofile_FreeFlow(t *testing.T) {
	srv, fx := gofileTestServer(t)
	defer srv.Close()

	oldBase := gofileAPIBase
	gofileAPIBase = fx.base
	defer func() { gofileAPIBase = oldBase }()

	r := NewHostResolver()
	res, err := r.resolveGofile("https://gofile.io/d/abc123")
	if err != nil {
		t.Fatalf("resolveGofile: %v", err)
	}
	if res.URL != fx.fileLink {
		t.Fatalf("URL = %q, want %q", res.URL, fx.fileLink)
	}
	if fx.guestCalls != 1 {
		t.Fatalf("guest account calls = %d, want 1", fx.guestCalls)
	}
	if len(fx.contents) != 1 {
		t.Fatalf("contents calls = %d, want 1", len(fx.contents))
	}
}

func TestResolveGofile_DirectSubdomainPassthrough(t *testing.T) {
	r := NewHostResolver()
	res, err := r.resolveGofile("https://s123.gofile.io/downloadweb/abc123/file1")
	if err != nil {
		t.Fatalf("passthrough: %v", err)
	}
	if res.URL != "https://s123.gofile.io/downloadweb/abc123/file1" {
		t.Fatalf("URL = %q, want unchanged", res.URL)
	}
}

func TestResolveGofile_NoContentID(t *testing.T) {
	r := NewHostResolver()
	_, err := r.resolveGofile("https://example.com/notgofile")
	if err == nil {
		t.Fatal("expected error for non-gofile URL")
	}
}

func TestResolveGofile_AccountError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"status":"error-rateLimit"}`))
	}))
	defer srv.Close()

	oldBase := gofileAPIBase
	gofileAPIBase = srv.URL
	defer func() { gofileAPIBase = oldBase }()

	r := NewHostResolver()
	_, err := r.resolveGofile("https://gofile.io/d/abc123")
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("expected rate-limit error, got %v", err)
	}
}

func TestResolveGofile_ContentsNoLink(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/accounts":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"status": "ok",
				"data":   map[string]any{"token": "t", "tier": "guest"},
			})
		case "/contents/abc123":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"status": "ok",
				"data":   map[string]any{"children": map[string]any{}},
			})
		default:
			http.NotFound(w, req)
		}
	}))
	defer srv.Close()

	oldBase := gofileAPIBase
	gofileAPIBase = srv.URL
	defer func() { gofileAPIBase = oldBase }()

	r := NewHostResolver()
	_, err := r.resolveGofile("https://gofile.io/d/abc123")
	if err == nil || !strings.Contains(err.Error(), "no downloadable link") {
		t.Fatalf("expected no-link error, got %v", err)
	}
}
