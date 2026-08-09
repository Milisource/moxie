package downloader

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mili/moxie/internal/db"
)

func TestParsePartName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		prefix     string
		index      int
		ok         bool
	}{
		{"Game.part1.rar", "Game", 1, true},
		{"Game.part01.zip", "Game", 1, true},
		{"Game.part2.7z", "Game", 2, true},
		{"Game part 1.rar", "Game", 1, true},
		{"Game [part 2].zip", "Game", 2, true},
		{"Game_v1.0.part3.rar", "Game_v1.0", 3, true},
		{"Game.7z.001", "Game.7z", 1, true},
		{"Game.7z.002", "Game.7z", 2, true},
		{"Game.rar.001", "Game.rar", 1, true},
		{"Game_1.rar", "Game", 1, true},
		{"Game-2.zip", "Game", 2, true},
		{"Game_v1.rar", "", 0, false},
		{"Game_v1.0.rar", "", 0, false},
		{"Game.rar", "", 0, false},
		{"Game.zip", "", 0, false},
		{"Game.7z", "", 0, false},
		{"single-file.zip", "", 0, false},
		{"", "", 0, false},
	}
	for _, tt := range tests {
		prefix, idx, ok := parsePartName(tt.name)
		if ok != tt.ok || prefix != tt.prefix || idx != tt.index {
			t.Errorf("parsePartName(%q) = (%q, %d, %v), want (%q, %d, %v)",
				tt.name, prefix, idx, ok, tt.prefix, tt.index, tt.ok)
		}
	}
}

func TestGroupMultiPartLinks(t *testing.T) {
	t.Parallel()
	links := []db.DownloadLink{
		{Name: "Game.part1.rar", Host: "buzzheavier", URL: "https://bzzhr.to/a"},
		{Name: "Game.part2.rar", Host: "buzzheavier", URL: "https://bzzhr.to/b"},
		{Name: "Game.part2.rar", Host: "mega", URL: "https://mega.nz/file/x"},
		{Name: "Other.part1.zip", Host: "pixeldrain", URL: "https://pixeldrain.com/u/1"},
		{Name: "Other.part2.zip", Host: "pixeldrain", URL: "https://pixeldrain.com/u/2"},
		{Name: "Single.rar", Host: "catbox", URL: "https://catbox.moe/a.rar"},
		{Name: "Game.part1.rar", Host: "gofile", URL: "https://gofile.io/d/zz"},
	}
	groups := GroupMultiPartLinks(links)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	// The pixeldrain group has the best host score → ordered first.
	if groups[0].Prefix != "Other" || groups[1].Prefix != "Game" {
		t.Fatalf("group order = %q, %q; want Other, Game", groups[0].Prefix, groups[1].Prefix)
	}
	// Game part2 candidates are score-ordered: buzzheavier (+10) before mega (-200).
	game := groups[1]
	if len(game.Parts[2]) != 2 || game.Parts[2][0].Host != "buzzheavier" {
		t.Fatalf("Game part2 candidates = %v, want buzzheavier first", game.Parts[2])
	}
	// Part1 of Game has two candidates (buzzheavier + gofile).
	if len(game.Parts[1]) != 2 {
		t.Fatalf("Game part1 candidates = %d, want 2", len(game.Parts[1]))
	}
	if game.MaxIndex() != 2 {
		t.Fatalf("MaxIndex = %d, want 2", game.MaxIndex())
	}
}

func TestGroupMultiPartLinks_SingleMemberIgnored(t *testing.T) {
	t.Parallel()
	links := []db.DownloadLink{
		{Name: "Game.part1.rar", Host: "pixeldrain", URL: "https://pixeldrain.com/u/1"},
		{Name: "Solo.rar", Host: "pixeldrain", URL: "https://pixeldrain.com/u/2"},
	}
	groups := GroupMultiPartLinks(links)
	if len(groups) != 0 {
		t.Fatalf("expected no groups (part1 alone is a single link), got %d", len(groups))
	}
}

func TestConcatSplitParts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	parts := make([]string, 3)
	for i := 0; i < 3; i++ {
		p := filepath.Join(dir, fmt.Sprintf("Game.7z.%03d", i+1))
		os.WriteFile(p, []byte(strings.Repeat("x", 100*(i+1))), 0600)
		parts[i] = p
	}
	target := filepath.Join(dir, "Game.7z")
	if err := ConcatSplitParts(parts, target); err != nil {
		t.Fatalf("concat: %v", err)
	}
	fi, err := os.Stat(target)
	if err != nil {
		t.Fatalf("target missing: %v", err)
	}
	if fi.Size() != 600 {
		t.Fatalf("target size = %d, want 600", fi.Size())
	}
	b, _ := os.ReadFile(target)
	if len(b) != 600 || string(b[:100]) != strings.Repeat("x", 100) {
		t.Fatalf("target content corrupted")
	}

	// Missing part → error, no partial target left.
	bad := []string{parts[0], filepath.Join(dir, "nope.001")}
	if err := ConcatSplitParts(bad, filepath.Join(dir, "bad.7z")); err == nil {
		t.Fatal("expected error for missing part")
	}
	if _, err := os.Stat(filepath.Join(dir, "bad.7z")); !os.IsNotExist(err) {
		t.Fatal("partial target must be removed on error")
	}
}

func TestConcatSplitParts_TooFew(t *testing.T) {
	t.Parallel()
	if err := ConcatSplitParts([]string{"a"}, "b"); err == nil {
		t.Fatal("expected error for a single part")
	}
}

// multipartTestServer serves a distinct payload per file name so part
// downloads are distinguishable, dialed through the testTransportOverride.
func multipartTestServer(t *testing.T, fileSizes map[string]int) *httptest.Server {
	t.Helper()
	requests := map[string]int{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		requests[name]++
		w.Header().Set("Content-Type", "application/octet-stream")
		if size, ok := fileSizes[name]; ok {
			w.Write([]byte(strings.Repeat(name[0:1], size)))
		} else {
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() {
		srv.Close()
		t.Logf("multipart requests: %v", requests)
	})
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	tr := srv.Client().Transport.(*http.Transport).Clone()
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	}
	old := testTransportOverride
	testTransportOverride = tr
	t.Cleanup(func() { testTransportOverride = old })
	return srv
}

func multipartTestURL(srv *httptest.Server, name string) string {
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	return fmt.Sprintf("https://example.com:%d/%s", port, name)
}

func TestDownloadMultiPart_SplitStyle(t *testing.T) {
	srv := multipartTestServer(t, map[string]int{
		"Game.7z.001": 100,
		"Game.7z.002": 200,
	})

	group := PartGroup{
		Prefix: "Game.7z",
		Parts: map[int][]db.DownloadLink{
			1: {{Name: "Game.7z.001", Host: "catbox", URL: multipartTestURL(srv, "Game.7z.001"), Size: 100}},
			2: {{Name: "Game.7z.002", Host: "catbox", URL: multipartTestURL(srv, "Game.7z.002"), Size: 200}},
		},
	}

	dir := t.TempDir()
	final, err := DownloadMultiPart(group, dir, nil, "")
	if err != nil {
		t.Fatalf("DownloadMultiPart: %v", err)
	}
	if filepath.Base(final) != "Game.7z" {
		t.Fatalf("final = %q, want Game.7z", final)
	}
	fi, err := os.Stat(final)
	if err != nil || fi.Size() != 300 {
		t.Fatalf("final size = %v, want 300 (err %v)", fi.Size(), err)
	}
	// Parts were removed after concat.
	if _, err := os.Stat(filepath.Join(dir, "Game.7z.001")); !os.IsNotExist(err) {
		t.Error("part 001 must be removed after concat")
	}
	if _, err := os.Stat(filepath.Join(dir, "Game.7z.002")); !os.IsNotExist(err) {
		t.Error("part 002 must be removed after concat")
	}
}

func TestDownloadMultiPart_VolumeStyle(t *testing.T) {
	srv := multipartTestServer(t, map[string]int{
		"Game.part1.rar": 50,
		"Game.part2.rar": 80,
	})
	group := PartGroup{
		Prefix: "Game",
		Parts: map[int][]db.DownloadLink{
			1: {{Name: "Game.part1.rar", Host: "catbox", URL: multipartTestURL(srv, "Game.part1.rar"), Size: 50}},
			2: {{Name: "Game.part2.rar", Host: "catbox", URL: multipartTestURL(srv, "Game.part2.rar"), Size: 80}},
		},
	}
	dir := t.TempDir()
	final, err := DownloadMultiPart(group, dir, nil, "")
	if err != nil {
		t.Fatalf("DownloadMultiPart: %v", err)
	}
	if filepath.Base(final) != "Game.part1.rar" {
		t.Fatalf("final = %q, want Game.part1.rar", final)
	}
	// Both parts remain on disk for the extractor.
	for _, name := range []string{"Game.part1.rar", "Game.part2.rar"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("part %s missing: %v", name, err)
		}
	}
}

func TestDownloadMultiPart_SkipPresentPart(t *testing.T) {
	// Game.part1.rar is deliberately NOT served (404) — the test passes only
	// if the pre-placed file makes the downloader skip it entirely.
	srv := multipartTestServer(t, map[string]int{
		"Game.part2.rar": 80,
	})
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Game.part1.rar"), []byte(strings.Repeat("p", 50)), 0600)
	group := PartGroup{
		Prefix: "Game",
		Parts: map[int][]db.DownloadLink{
			1: {{Name: "Game.part1.rar", Host: "catbox", URL: multipartTestURL(srv, "Game.part1.rar"), Size: 50}},
			2: {{Name: "Game.part2.rar", Host: "catbox", URL: multipartTestURL(srv, "Game.part2.rar"), Size: 80}},
		},
	}
	final, err := DownloadMultiPart(group, dir, nil, "")
	if err != nil {
		t.Fatalf("DownloadMultiPart: %v", err)
	}
	if filepath.Base(final) != "Game.part1.rar" {
		t.Fatalf("final = %q, want Game.part1.rar", final)
	}
}

func TestDownloadMultiPart_MissingPart(t *testing.T) {
	// Parts 1 and 3 exist; index 2 is missing → the loop must stop at the
	// gap with a clear error (parts 1/3 are served so the loop reaches it).
	srv := multipartTestServer(t, map[string]int{
		"Game.part1.rar": 50,
		"Game.part3.rar": 80,
	})
	group := PartGroup{
		Prefix: "Game",
		Parts: map[int][]db.DownloadLink{
			1: {{Name: "Game.part1.rar", Host: "catbox", URL: multipartTestURL(srv, "Game.part1.rar"), Size: 50}},
			// part 2 absent — an index gap in the thread's link set
			3: {{Name: "Game.part3.rar", Host: "catbox", URL: multipartTestURL(srv, "Game.part3.rar"), Size: 80}},
		},
	}
	_, err := DownloadMultiPart(group, t.TempDir(), nil, "")
	if err == nil || !strings.Contains(err.Error(), "missing part 2") {
		t.Fatalf("expected missing-part error, got %v", err)
	}
}

func TestDownloadMultiPart_PartFallback(t *testing.T) {
	// First candidate 404s (dead-part1.rar is not served); the second
	// (Game.part1.rar) must be tried and succeed. catbox = pass-through host
	// so no resolver network calls slow the test.
	srv := multipartTestServer(t, map[string]int{
		"Game.part1.rar": 50,
		"Game.part2.rar": 80,
	})
	group := PartGroup{
		Prefix: "Game",
		Parts: map[int][]db.DownloadLink{
			1: {
				{Name: "Game.part1.rar", Host: "catbox", URL: multipartTestURL(srv, "dead-part1.rar"), Size: 50},
				{Name: "Game.part1.rar", Host: "catbox", URL: multipartTestURL(srv, "Game.part1.rar"), Size: 50},
			},
			2: {{Name: "Game.part2.rar", Host: "catbox", URL: multipartTestURL(srv, "Game.part2.rar"), Size: 80}},
		},
	}
	dir := t.TempDir()
	final, err := DownloadMultiPart(group, dir, nil, "")
	if err != nil {
		t.Fatalf("DownloadMultiPart: %v", err)
	}
	if filepath.Base(final) != "Game.part1.rar" {
		t.Fatalf("final = %q, want Game.part1.rar", final)
	}
	if fi, _ := os.Stat(filepath.Join(dir, "Game.part1.rar")); fi == nil || fi.Size() != 50 {
		t.Fatal("fallback candidate file missing or wrong size")
	}
}
