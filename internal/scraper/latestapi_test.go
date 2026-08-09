package scraper

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mili/moxie/internal/db"
)

func TestSanitizeSearchQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"taimanin", "taimanin"},
		{"Taimanin Asagi", "Taimanin Asagi"},
		{"corruption of champions", "corruption champions"},
		{"the trials of tainted space", "trials tainted space"},
		{"Barely Working", "Barely Working"},
		{"HG755 The Inn, Tavern and Halberd", "HG755 Inn Tavern Halberd"},
		{"Queen's Brothel", "Queen Brothel"},
		{"a is the of and", ""},
		{"", ""},
		{"  spaced   out  ", "spaced out"},
		{"this is a very long title that exceeds thirty characters easily", "very long title exceeds thirt"},
		{"Sphilia's Familiar", "Sphilia Familiar"},
		{"Ren’Py game", "RenPy game"},
	}
	for _, tt := range tests {
		got := SanitizeSearchQuery(tt.in)
		if got != tt.want {
			t.Errorf("SanitizeSearchQuery(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestEngineNameFromPrefixes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		prefixes []int
		want     string
	}{
		{[]int{13}, "RPGM"},
		{[]int{14}, "RenPy"},
		{[]int{13, 14, 18}, "RPGM"}, // first known engine wins
		{[]int{19}, "Unity"},
		{[]int{5}, "HTML"},
		{[]int{22}, "WolfRPG"},
		{[]int{31}, "Godot"},
		{[]int{116}, ""}, // unknown IDs are ignored
		{[]int{999}, ""}, // unknown
		{nil, ""},        // no prefixes
		{[]int{18}, ""},  // only unknown
	}
	for _, tt := range tests {
		got := EngineNameFromPrefixes(tt.prefixes)
		if got != tt.want {
			t.Errorf("EngineNameFromPrefixes(%v) = %q, want %q", tt.prefixes, got, tt.want)
		}
	}
}

// TestHasNonGamePrefix documents the prefix-table lookup itself. NOTE: this
// numbering is not the F95Checker Type enum and must not be applied to
// latest_data.php search prefixes — sync rejects non-games by title
// (IsNonGameThread) only. The function is retained for the desktop UI's
// candidate filtering.
func TestHasNonGamePrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		prefixes []int
		want     bool
	}{
		{[]int{13}, false},   // RPGM game
		{[]int{14}, false},   // RenPy game
		{[]int{8}, true},     // Mod
		{[]int{24}, true},    // Comics
		{[]int{15}, true},    // Request
		{[]int{3}, true},     // Cheat Mod
		{[]int{13, 8}, true}, // game + mod prefix still non-game
		{[]int{116}, false},  // unknown ID: tolerant, treated as game
		{nil, false},
	}
	for _, tt := range tests {
		got := HasNonGamePrefix(tt.prefixes)
		if got != tt.want {
			t.Errorf("HasNonGamePrefix(%v) = %v, want %v", tt.prefixes, got, tt.want)
		}
	}
}

// newTestPublicAPI starts an httptest server that routes by path and returns
// a PublicAPI pointed at it.
func newTestPublicAPI(t *testing.T, handler http.Handler) (*PublicAPI, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	api := NewPublicAPI()
	api.Host = srv.URL
	api.CacheHost = srv.URL
	return api, srv
}

func TestStripVersionQualifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"v21.0.0 wip.7944", "v21.0.0"},
		{"v1.03 + DLC", "v1.03"},
		{"v0.4.11.3 Hotfix", "v0.4.11.3"},
		{"v0.5.2 EA", "v0.5.2"},
		{"DX v5.0.5s", "v5.0.5s"},
		{"2025-06-20 Cracked", "2025-06-20"},
		{"v0.9.3", "v0.9.3"},
		{"Final", "Final"},
		{"B.0.10.8.12", "B.0.10.8.12"},
		{"", ""},
		{"  v1.0  ", "v1.0"},
	}
	for _, tt := range tests {
		got := StripVersionQualifier(tt.in)
		if got != tt.want {
			t.Errorf("StripVersionQualifier(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestBulkVersions(t *testing.T) {
	t.Parallel()

	var gotQuery string
	api, _ := newTestPublicAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		fmt.Fprint(w, `{"status":"ok","msg":{"106408":"v6.3","300333":"v0.02","69497":"Unknown"}}`)
	}))

	versions, err := api.BulkVersions(context.Background(), []int64{106408, 300333, 69497})
	if err != nil {
		t.Fatalf("BulkVersions failed: %v", err)
	}
	if gotQuery != "threads=106408,300333,69497" {
		t.Errorf("query = %q, want threads=106408,300333,69497", gotQuery)
	}
	if len(versions) != 3 {
		t.Fatalf("got %d versions, want 3", len(versions))
	}
	if versions[106408] != "v6.3" {
		t.Errorf("versions[106408] = %q, want v6.3", versions[106408])
	}
	if versions[300333] != "v0.02" {
		t.Errorf("versions[300333] = %q, want v0.02", versions[300333])
	}
	// "Unknown" is mapped to empty — untracked version.
	if versions[69497] != "" {
		t.Errorf("versions[69497] = %q, want empty for Unknown", versions[69497])
	}
}

func TestBulkVersions_Chunking(t *testing.T) {
	t.Parallel()

	var queries []string
	api, _ := newTestPublicAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		fmt.Fprint(w, `{"status":"ok","msg":{}}`)
	}))

	var ids []int64
	for i := 0; i < maxBulkVersionIDs+5; i++ {
		ids = append(ids, int64(1000+i))
	}
	_, err := api.BulkVersions(context.Background(), ids)
	if err != nil {
		t.Fatalf("BulkVersions failed: %v", err)
	}
	if len(queries) != 2 {
		t.Fatalf("expected 2 chunked requests, got %d", len(queries))
	}
	if queries[1] != "threads=1100,1101,1102,1103,1104" {
		t.Errorf("second chunk = %q, want threads=1100..1104", queries[1])
	}
}

func TestBulkVersions_ErrorStatus(t *testing.T) {
	t.Parallel()

	api, _ := newTestPublicAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"error","msg":"You have been temporarily blocked because of a large amount of requests, please try again later"}`)
	}))

	_, err := api.BulkVersions(context.Background(), []int64{1})
	if err == nil {
		t.Fatal("expected error for error status, got nil")
	}
}

func TestSearchTitle(t *testing.T) {
	t.Parallel()

	var gotQuery string
	api, _ := newTestPublicAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		fmt.Fprint(w, `{"status":"ok","msg":{"data":[
			{"thread_id":32778,"title":"Taimanin Asagi Premium Box","creator":"PinkTea","version":"vFinal","prefixes":[19,13,14,18],"cover":"https://preview.f95zone.to/x.jpg"},
			{"thread_id":36774,"title":"Taimanin Asagi .ZERO","creator":"PinkTea","version":"v1.0.3","prefixes":[13,14,18]}
		]}}`)
	}))

	results, err := api.SearchTitle(context.Background(), "taimanin")
	if err != nil {
		t.Fatalf("SearchTitle failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	r := results[0]
	if r.ThreadID != 32778 || r.Version != "vFinal" || r.Title != "Taimanin Asagi Premium Box" {
		t.Errorf("unexpected first result: %+v", r)
	}
	if r.URL != "https://f95zone.to/threads/32778/" {
		t.Errorf("URL = %q, want thread URL", r.URL)
	}
	if len(r.Prefixes) != 4 {
		t.Errorf("prefixes = %v, want 4 entries", r.Prefixes)
	}

	// Sanitized query must be used in the request.
	if got := queryParam(gotQuery, "search"); got != "taimanin" {
		t.Errorf("search param = %q, want taimanin", got)
	}
	if got := queryParam(gotQuery, "cmd"); got != "list" {
		t.Errorf("cmd param = %q, want list", got)
	}
}

func TestSearchTitle_EmptyResult(t *testing.T) {
	t.Parallel()

	api, _ := newTestPublicAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"ok","msg":{"data":[]}}`)
	}))
	results, err := api.SearchTitle(context.Background(), "zzzz nonexistent")
	if err != nil {
		t.Fatalf("SearchTitle failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results, want 0", len(results))
	}
}

func TestSearchTitle_EmptyQuery(t *testing.T) {
	t.Parallel()
	api := NewPublicAPI()
	_, err := api.SearchTitle(context.Background(), "the of a is")
	if err == nil {
		t.Fatal("expected error for fully-stopword query")
	}
}

func TestCacheFastCheck(t *testing.T) {
	t.Parallel()

	var gotQuery string
	api, _ := newTestPublicAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		fmt.Fprint(w, `{"106408":1786204423,"300333":1786206277}`)
	}))

	lastChanged, err := api.CacheFastCheck(context.Background(), []int64{106408, 300333})
	if err != nil {
		t.Fatalf("CacheFastCheck failed: %v", err)
	}
	if gotQuery != "ids=106408,300333" {
		t.Errorf("query = %q, want ids=106408,300333", gotQuery)
	}
	if lastChanged[106408] != 1786204423 || lastChanged[300333] != 1786206277 {
		t.Errorf("unexpected timestamps: %v", lastChanged)
	}
}

func TestCacheFullThread(t *testing.T) {
	t.Parallel()

	api, _ := newTestPublicAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Real-world response: numeric fields arrive as strings (Redis).
		fmt.Fprint(w, `{"version":"v6.3","name":"Double Perception","developer":"Zett","status":"1",
			"image_url":"https://attachments.f95zone.to/x.gif","description":"desc","changelog":"log",
			"last_updated":"1786190400","score":"3.4","INDEX_ERROR":""}`)
	}))

	ct, err := api.CacheFullThread(context.Background(), 106408)
	if err != nil {
		t.Fatalf("CacheFullThread failed: %v", err)
	}
	if ct.Version != "v6.3" || ct.Name != "Double Perception" || ct.Developer != "Zett" {
		t.Errorf("unexpected thread data: %+v", ct)
	}
	if ct.Status != "active" {
		t.Errorf("status = %q, want active (status 1)", ct.Status)
	}
	if ct.LastUpdated != 1786190400 {
		t.Errorf("last_updated = %d, want 1786190400", ct.LastUpdated)
	}
	if ct.Score != 3.4 {
		t.Errorf("score = %f, want 3.4", ct.Score)
	}
}

func TestCacheFullThread_EngineType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		typ  string // JSON value for "type" ("" = omit the field)
		want string
	}{
		{"renpy string", `"14"`, "RenPy"},
		{"renpy number", `14`, "RenPy"},
		{"java string", `"6"`, "Java"},
		{"godot type", `"31"`, "Godot"}, // Godot is a first-class engine
		{"unknown type", `"999"`, ""},
		{"type omitted", ``, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := `{"version":"v1","name":"X","status":"1"`
			if tt.typ != "" {
				body += `,"type":` + tt.typ
			}
			body += `}`
			api, _ := newTestPublicAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, body)
			}))
			ct, err := api.CacheFullThread(context.Background(), 1)
			if err != nil {
				t.Fatalf("CacheFullThread failed: %v", err)
			}
			if ct.Engine != tt.want {
				t.Errorf("type %s → Engine %q, want %q", tt.typ, ct.Engine, tt.want)
			}
		})
	}
}

func TestCacheFullThread_StatusMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status int
		want   string
	}{
		{1, "active"},
		{2, "completed"},
		{3, "on_hold"},
		{4, "abandoned"},
		{5, ""},
		{0, ""},
		{99, ""},
	}
	for _, tt := range tests {
		api, _ := newTestPublicAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, `{"version":"v1","name":"X","status":%d}`, tt.status)
		}))
		ct, err := api.CacheFullThread(context.Background(), 1)
		if err != nil {
			t.Fatalf("CacheFullThread failed: %v", err)
		}
		if ct.Status != tt.want {
			t.Errorf("status %d → %q, want %q", tt.status, ct.Status, tt.want)
		}
	}
}

func TestCacheFullThread_NotFound(t *testing.T) {
	t.Parallel()

	api, _ := newTestPublicAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	_, err := api.CacheFullThread(context.Background(), 999999)
	if err != ErrThreadNotFound {
		t.Errorf("err = %v, want ErrThreadNotFound", err)
	}
}

// Regression: a Cloudflare-challenge 403 means WE are blocked, not that the
// thread is gone. CacheFullThread must return the BlockedError so callers
// trip block detection (desktop isBlockedErr, CLI circuit breaker) instead
// of silently skipping every game as "not found".
func TestCacheFullThread_Cloudflare403_NotThreadNotFound(t *testing.T) {
	t.Parallel()

	api, _ := newTestPublicAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `<html><body>cf-browser-verification challenge</body></html>`)
	}))
	_, err := api.CacheFullThread(context.Background(), 1)
	if err == nil {
		t.Fatal("expected BlockedError for Cloudflare 403, got nil")
	}
	if err == ErrThreadNotFound {
		t.Fatal("Cloudflare 403 must not map to ErrThreadNotFound")
	}
	var blockedErr *BlockedError
	if !errors.As(err, &blockedErr) {
		t.Fatalf("err = %v, want *BlockedError", err)
	}
	if blockedErr.StatusCode != 403 {
		t.Errorf("BlockedError.StatusCode = %d, want 403", blockedErr.StatusCode)
	}
}

// TestErrIsThreadGone covers the CacheFullThread error mapping directly
// (constructed errors — the HTTP round-trip would burn the retry backoff).
func TestErrIsThreadGone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"404 HTTPStatusError", &HTTPStatusError{StatusCode: 404}, true},
		{"403 HTTPStatusError", &HTTPStatusError{StatusCode: 403}, false},
		{"404 BlockedError", &BlockedError{StatusCode: 404}, true},
		{"403 BlockedError no Cloudflare", &BlockedError{Reason: "access denied (HTTP 403) — possible IP block or missing cookies", StatusCode: 403}, true},
		{"403 BlockedError Cloudflare", &BlockedError{Reason: "Cloudflare challenge (HTTP 403) — refresh your browser session and re-import cookies", StatusCode: 403}, false},
		{"503 BlockedError", &BlockedError{Reason: "service unavailable (HTTP 503)", StatusCode: 503}, false},
		{"429 BlockedError", &BlockedError{Reason: "rate limited", StatusCode: 429}, false},
		{"tripped circuit breaker", &BlockedError{Reason: "repeated blocking responses", StatusCode: 0}, false},
		{"wrapped BlockedError", fmt.Errorf("outer: %w", &BlockedError{StatusCode: 404}), true},
		{"plain error", fmt.Errorf("boom"), false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errIsThreadGone(tt.err); got != tt.want {
				t.Errorf("errIsThreadGone(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestCacheFullThread_IndexError(t *testing.T) {
	t.Parallel()

	api, _ := newTestPublicAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"version":"","name":"","INDEX_ERROR":"F95ZONE_RATELIMIT"}`)
	}))
	_, err := api.CacheFullThread(context.Background(), 1)
	if err == nil {
		t.Fatal("expected error for INDEX_ERROR response")
	}
}

func TestApplyCacheThreadData_TitleRename(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		localTitle string
		threadName string
		score      float64
		wantTitle  string
	}{
		{
			name:       "weak match keeps local title",
			localTitle: "Aurelia",
			threadName: "Aurelian Nostrum",
			score:      0.4,
			wantTitle:  "Aurelia",
		},
		{
			// The Aurelia bug: ComputeMatchScore("Aurelia", "Aurelian
			// Nostrum") used to be 0.85 via substring containment, though
			// the tokens don't nest — the thread is a different game.
			// ComputeMatchScore now rejects it outright (word-boundary
			// containment); the titlesContain guard below is the
			// association layer's second line of defense.
			name:       "strong substring without token containment keeps title",
			localTitle: "Aurelia",
			threadName: "Aurelian Nostrum",
			score:      0.85,
			wantTitle:  "Aurelia",
		},
		{
			name:       "strong match with token containment renames",
			localTitle: "Aurelia",
			threadName: "Aurelia Nostrum",
			score:      0.85,
			wantTitle:  "Aurelia Nostrum",
		},
		{
			name:       "exact match renames",
			localTitle: "Aurelia",
			threadName: "Aurelia",
			score:      1.0,
			wantTitle:  "Aurelia",
		},
		{
			name:       "containment without strong score keeps title",
			localTitle: "Aurelia",
			threadName: "Aurelia Nostrum",
			score:      0.6,
			wantTitle:  "Aurelia",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			game := &db.Game{Title: tt.localTitle}
			ct := &CacheThread{Name: tt.threadName}
			ApplyCacheThreadData(game, ct, 42, nil, tt.score)
			if game.Title != tt.wantTitle {
				t.Errorf("title = %q, want %q", game.Title, tt.wantTitle)
			}
		})
	}
}

func TestApplyCacheThreadData_Engine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current string // game.Engine before the call
		ctEng   string // ct.Engine (cache API type)
		prefix  int    // single prefix ID (0 = none)
		want    string
	}{
		{"empty engine takes cache type", "", "RenPy", 0, "RenPy"},
		// latest_data.php prefix numbering is NOT the F95Checker Type enum,
		// so ApplyCacheThreadData ignores prefixes entirely — engine comes
		// only from ct.Engine.
		{"empty engine ignores prefixes when type absent", "", "", 13, ""},
		{"engine from type ignores contradicting prefixes", "", "RenPy", 19, "RenPy"},
		{"unknown engine takes cache type", "Unknown", "Unity", 0, "Unity"},
		{"others engine ignores prefixes", "Others", "", 19, "Others"},
		// Known engines are never flipped to a different one.
		{"known engine never flipped by type", "Unity", "RenPy", 0, "Unity"},
		{"known engine never flipped by prefixes", "RenPy", "", 19, "RenPy"},
		{"no signals leaves engine untouched", "Unity", "", 0, "Unity"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			game := &db.Game{Engine: tt.current}
			ct := &CacheThread{Engine: tt.ctEng}
			var prefixes []int
			if tt.prefix != 0 {
				prefixes = []int{tt.prefix}
			}
			ApplyCacheThreadData(game, ct, 42, prefixes, 1.0)
			if game.Engine != tt.want {
				t.Errorf("engine = %q, want %q", game.Engine, tt.want)
			}
		})
	}
}

func TestNewPublicAPIWithCookie(t *testing.T) {
	t.Parallel()

	api := NewPublicAPIWithCookie("xf_user=abc")
	if api == nil {
		t.Fatal("NewPublicAPIWithCookie returned nil")
	}
	if api.Client == nil || api.CacheClient == nil {
		t.Fatal("NewPublicAPIWithCookie left a client nil")
	}
	if api.Host != "https://f95zone.to" || api.CacheHost != cacheAPIHost {
		t.Errorf("unexpected hosts: %q / %q", api.Host, api.CacheHost)
	}
	if api.CheckerPath != checkerPath || api.SearchPath != latestDataPath {
		t.Errorf("unexpected paths: %q / %q", api.CheckerPath, api.SearchPath)
	}
	// NewPublicAPI is the cookie-free form of the same constructor.
	plain := NewPublicAPI()
	if plain == nil || plain.Client == nil || plain.CacheClient == nil {
		t.Fatal("NewPublicAPI returned nil or incomplete client")
	}
}

func TestCacheFastCheck_Chunking(t *testing.T) {
	t.Parallel()

	var queries []string
	api, _ := newTestPublicAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		fmt.Fprint(w, `{}`)
	}))

	var ids []int64
	for i := 0; i < maxFastCheckIDs+1; i++ {
		ids = append(ids, int64(i+1))
	}
	if _, err := api.CacheFastCheck(context.Background(), ids); err != nil {
		t.Fatalf("CacheFastCheck failed: %v", err)
	}
	if len(queries) != 2 {
		t.Fatalf("expected 2 chunked requests, got %d", len(queries))
	}
}

func TestBulkVersions_BlocksOn404(t *testing.T) {
	t.Parallel()

	api, _ := newTestPublicAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	_, err := api.BulkVersions(context.Background(), []int64{1})
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

// queryParam extracts a query parameter value for assertions.
func queryParam(rawQuery, key string) string {
	vals := parseQuery(rawQuery)
	return vals[key]
}

func parseQuery(rawQuery string) map[string]string {
	out := map[string]string{}
	// Minimal parser sufficient for tests.
	start := 0
	for i := 0; i <= len(rawQuery); i++ {
		if i == len(rawQuery) || rawQuery[i] == '&' {
			pair := rawQuery[start:i]
			start = i + 1
			for j := 0; j < len(pair); j++ {
				if pair[j] == '=' {
					out[pair[:j]] = pair[j+1:]
					break
				}
			}
		}
	}
	return out
}
