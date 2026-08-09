package scraper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/mili/moxie/internal/db"
)

// ---------------------------------------------------------------------------
// Cookie-free F95Zone endpoints
// ---------------------------------------------------------------------------
//
// F95Zone exposes several JSON endpoints that require no login and are not
// behind Cloudflare's challenge — the same endpoints its own "Latest
// Updates" browser and the F95Checker indexer use:
//
//	GET /sam/checker.php?threads={csv}                     — bulk version lookup
//	GET /sam/latest_alpha/latest_data.php?cmd=list&...     — title search
//	GET https://api.f95checker.dev/fast?ids={csv}          — last-change timestamps
//	GET https://api.f95checker.dev/full/{id}?ts={ts}       — full thread data
//
// These make sync cookie-independent: version checks for an entire library
// are a single request, and search needs no session at all. Direct thread
// scraping (with cookies) remains the fallback layer.

const (
	checkerPath    = "/sam/checker.php"
	latestDataPath = "/sam/latest_alpha/latest_data.php"
	cacheAPIHost   = "https://api.f95checker.dev"

	// checker.php accepts up to 100 thread IDs per request — 101 returns
	// "Invalid threads data or >100". Verified against the live endpoint.
	maxBulkVersionIDs = 100
	// The cache API fast-check accepts up to 10 IDs per request.
	maxFastCheckIDs = 10
	// Latest-search row count. F95Checker uses 15; the API accepts up to 90.
	searchRows = 15
)

// ErrThreadNotFound is returned by CacheFullThread when the cache API reports
// the thread as missing (privated, moved, or deleted).
var ErrThreadNotFound = errors.New("thread not found")

// enginePrefixNames maps F95Zone "Latest Updates" prefix IDs to moxie's
// canonical engine names. The IDs come from F95Zone's prefix taxonomy (the
// same numbering the F95Checker project uses for its Type enum). Unknown IDs
// are ignored — the set grows over time and tolerance beats staleness.
var enginePrefixNames = map[int]string{
	2:  "ADRIFT",
	4:  "Flash",
	5:  "HTML",
	6:  "Java",
	9:  "Others",
	10: "QSP",
	11: "RAGS",
	13: "RPGM",
	14: "RenPy",
	16: "Tads",
	19: "Unity",
	20: "UnrealEngine",
	21: "WebGL",
	22: "WolfRPG",
	31: "Godot",
}

// nonGamePrefixes marks prefix IDs that identify non-game threads: mods,
// tools, requests, comics, and other media that must never be auto-associated
// with a game. Unknown IDs are NOT treated as non-game.
var nonGamePrefixes = map[int]bool{
	1: true, 3: true, 7: true, 8: true, 12: true, 15: true,
	17: true, 18: true, 23: true, 24: true, 25: true, 26: true,
	27: true, 28: true, 29: true, 30: true, 32: true,
}

// cacheStatusNames maps the cache API's status enum to moxie's canonical
// database values. Unknown statuses map to "" (caller keeps current value).
var cacheStatusNames = map[int]string{
	1: "active",
	2: "completed",
	3: "on_hold",
	4: "abandoned",
}

// PublicAPI is a cookie-free client for F95Zone's public JSON endpoints and
// the F95Checker cache API. It reuses the Client machinery (pacing, retries,
// circuit breaker, browser headers) with an empty cookie, so sync works even
// when the user's browser session is expired or blocked.
//
// Two clients are used: Client paces requests to F95Zone's own endpoints
// (politeness — the site rate-limits aggressively), while CacheClient talks
// to api.f95checker.dev unpaced: that service exists to serve programmatic
// clients (F95Checker itself runs 10 concurrent connections), and it does the
// F95Zone scraping on its own servers.
type PublicAPI struct {
	Client      *Client
	CacheClient *Client

	// Hosts and paths are fields so tests can point at httptest servers.
	Host        string // https://f95zone.to
	CheckerPath string // /sam/checker.php
	SearchPath  string // /sam/latest_alpha/latest_data.php
	CacheHost   string // https://api.f95checker.dev
}

// NewPublicAPIWithCookie creates a client for F95Zone's public endpoints that
// carries the given F95Zone cookie. F95Zone rate-limits anonymous users hard
// ("Anonymous users have a limited amount of requests per hour"), so passing
// a real session cookie keeps sync under the limit; the F95Checker cache API
// also lifts its anonymous per-hour cap when a cookie is present. An empty
// cookie falls back to anonymous mode.
func NewPublicAPIWithCookie(cookie string) *PublicAPI {
	return &PublicAPI{
		Client:      NewClient(cookie),
		CacheClient: NewUnsafeClient(cookie),
		Host:        "https://f95zone.to",
		CheckerPath: checkerPath,
		SearchPath:  latestDataPath,
		CacheHost:   cacheAPIHost,
	}
}

// NewPublicAPI creates a cookie-free client for F95Zone's public endpoints.
// Prefer NewPublicAPIWithCookie when a session cookie is available — the
// anonymous rate limit is low enough to stall full-library syncs.
func NewPublicAPI() *PublicAPI {
	return NewPublicAPIWithCookie("")
}

// LatestSearchResult is one hit from the latest_data.php title search.
type LatestSearchResult struct {
	Title    string
	URL      string
	ThreadID int64
	Version  string
	Prefixes []int
	CoverURL string
	Creator  string
}

// CacheThread is the parsed response of the F95Checker cache API /full
// endpoint. Fields are mapped to moxie conventions (Status is the canonical
// database string, not the cache API's integer enum).
type CacheThread struct {
	Name        string
	Version     string
	Developer   string
	Status      string // "", "active", "completed", "on_hold", "abandoned"
	ImageURL    string
	Description string
	Changelog   string
	LastUpdated int64
	Score       float64
	// Engine is the canonical moxie engine name derived from the cache API's
	// Type enum ("" when the type is unknown/unmapped). Unlike the prefix
	// IDs from latest_data.php, the Type numbering is correct.
	Engine string
}

// searchStopwords are stripped from latest-updates search queries — the
// Redis-backed search matches poorly on stopwords, so "Corruption of
// Champions" must become "Corruption Champions". Mirrors F95Checker.
var searchStopwords = map[string]bool{
	"a": true, "is": true, "the": true, "an": true, "and": true,
	"are": true, "as": true, "at": true, "be": true, "but": true,
	"by": true, "for": true, "if": true, "in": true, "into": true,
	"it": true, "no": true, "not": true, "of": true, "on": true,
	"or": true, "such": true, "that": true, "their": true, "then": true,
	"there": true, "these": true, "they": true, "this": true, "to": true,
	"was": true, "will": true, "with": true,
}

// SanitizeSearchQuery prepares a title for latest-updates search: ASCII-only,
// stopwords stripped, capped at 30 characters. Mirrors F95Checker's query
// sanitizer so search quality matches what its users get.
func SanitizeSearchQuery(query string) string {
	query = strings.ReplaceAll(query, "’s ", " ")
	query = strings.ReplaceAll(query, "'s ", " ")
	var b strings.Builder
	for _, r := range query {
		if r < 128 {
			b.WriteRune(r)
		}
	}
	query = b.String()
	query = strings.ReplaceAll(query, ". ", " ")
	query = strings.ReplaceAll(query, " .", " ")
	for _, ch := range "?&/':;-.+!~(),*" {
		query = strings.ReplaceAll(query, string(ch), " ")
	}
	query = strings.Join(strings.Fields(query), " ")

	words := strings.Split(query, " ")
	var kept []string
	for _, w := range words {
		if !searchStopwords[strings.ToLower(w)] {
			kept = append(kept, w)
		}
	}

	query = ""
	for _, w := range kept {
		tok := " " + w
		if len(query)+len(tok) > 30 {
			truncated := tok[:30-len(query)]
			if len(truncated) > 3 && !searchStopwords[strings.ToLower(strings.TrimSpace(truncated))] {
				query += truncated
			}
			break
		}
		query += tok
	}
	return strings.TrimSpace(query)
}

// EngineNameFromPrefixes returns the canonical moxie engine name implied by
// the thread's F95Zone prefix IDs, or "" when no known engine prefix is
// present (unknown IDs are ignored).
//
// WARNING: the latest_data.php `prefixes` numbering is NOT the F95Checker
// Type enum — a single real RenPy game can appear as 4, 7, 13, 14, or 116 —
// so do NOT use this function with latest_data.php search prefixes. The
// numbering is only valid for the cache API's `type` field, which
// CacheFullThread already maps to CacheThread.Engine. Prefer ct.Engine.
func EngineNameFromPrefixes(prefixes []int) string {
	for _, p := range prefixes {
		if name, ok := enginePrefixNames[p]; ok {
			return name
		}
	}
	return ""
}

// HasNonGamePrefix reports whether the thread's prefixes mark it as a
// non-game thread (mods, tools, requests, comics, media).
//
// WARNING: the latest_data.php `prefixes` numbering is NOT the F95Checker
// Type enum, so prefix IDs there do not reliably separate games from
// non-games (a real RenPy game returned [7]). Do NOT use this function with
// latest_data.php search prefixes — prefer IsNonGameThread(title).
func HasNonGamePrefix(prefixes []int) bool {
	for _, p := range prefixes {
		if nonGamePrefixes[p] {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Bulk version lookup: /sam/checker.php?threads={csv}
// ---------------------------------------------------------------------------

// StripVersionQualifier reduces a checker.php / cache-API version string to
// its numeric core so "v21.0.0 wip.7944" compares equal to "21.0.0" (the form
// the HTML parser stores). The first whitespace-delimited token that looks
// like a version wins; without one, the whole string is kept ("Final").
// Compare with version.Compare after stripping — stored versions from older
// runs may carry qualifiers that would otherwise surface as phantom updates.
func StripVersionQualifier(v string) string {
	v = strings.TrimSpace(v)
	for _, tok := range strings.Fields(v) {
		if looksLikeVersion(tok) {
			return tok
		}
	}
	return v
}

// looksLikeVersion reports whether a token plausibly carries a version:
// it has a digit after any leading v/V (so "v5.0.5s" and "B.0.10.8" count,
// but "DX" and "Hotfix" don't).
func looksLikeVersion(tok string) bool {
	t := strings.TrimLeft(tok, "vV")
	if t == "" {
		return false
	}
	for _, r := range t {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

// BulkVersions fetches the current F95Zone versions for up to maxBulkVersionIDs
// threads in a single request (chunked for larger sets). Returns a map from
// thread ID to version. Threads not tracked by F95Zone's "Latest Updates"
// index are simply absent from the map. Versions are reduced to their
// numeric core (see stripVersionQualifier) — callers compare with
// version.Compare.
func (p *PublicAPI) BulkVersions(ctx context.Context, ids []int64) (map[int64]string, error) {
	versions := make(map[int64]string, len(ids))
	for _, chunk := range chunkInt64(ids, maxBulkVersionIDs) {
		u := p.Host + p.CheckerPath + "?threads=" + csvInt64(chunk)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, fmt.Errorf("scraper: bulk versions: %w", err)
		}
		body, err := p.Client.do(req, minDelay)
		if err != nil {
			return nil, err
		}

		var resp struct {
			Status string            `json:"status"`
			Msg    map[string]string `json:"msg"`
		}
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			return nil, fmt.Errorf("scraper: bulk versions: invalid response: %w", err)
		}
		if resp.Status != "ok" {
			return nil, fmt.Errorf("scraper: bulk versions: api error: %s", resp.Msg["msg"])
		}
		for idStr, v := range resp.Msg {
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil || id <= 0 {
				continue
			}
			// "Unknown" means the thread exists but has no tracked version.
			if v == "Unknown" {
				v = ""
			}
			versions[id] = StripVersionQualifier(v)
		}
	}
	return versions, nil
}

// ---------------------------------------------------------------------------
// Title search: /sam/latest_alpha/latest_data.php?cmd=list&search={query}
// ---------------------------------------------------------------------------

// SearchTitle searches F95Zone's latest-updates index for threads matching
// the query. Unlike the XenForo search this needs no login, no CSRF token,
// and no cookies. The query is sanitized the way F95Checker does (stopwords
// stripped, ≤30 chars).
func (p *PublicAPI) SearchTitle(ctx context.Context, query string) ([]LatestSearchResult, error) {
	q := SanitizeSearchQuery(query)
	if q == "" {
		return nil, fmt.Errorf("scraper: search query is empty after sanitization")
	}

	params := url.Values{}
	params.Set("cmd", "list")
	params.Set("cat", "games")
	params.Set("page", "1")
	params.Set("search", q)
	params.Set("sort", "likes")
	params.Set("rows", strconv.Itoa(searchRows))
	params.Set("_", strconv.FormatInt(time.Now().Unix(), 10))

	u := p.Host + p.SearchPath + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("scraper: search: %w", err)
	}
	body, err := p.Client.do(req, searchMinDelay)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Status string `json:"status"`
		Msg    struct {
			Data []struct {
				ThreadID int64  `json:"thread_id"`
				Title    string `json:"title"`
				Creator  string `json:"creator"`
				Version  string `json:"version"`
				Prefixes []int  `json:"prefixes"`
				Cover    string `json:"cover"`
			} `json:"data"`
		} `json:"msg"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("scraper: search: invalid response: %w", err)
	}
	if resp.Status != "ok" {
		return nil, fmt.Errorf("scraper: search: api error: %v", resp.Msg.Data)
	}

	results := make([]LatestSearchResult, 0, len(resp.Msg.Data))
	for _, d := range resp.Msg.Data {
		if d.ThreadID <= 0 {
			continue
		}
		results = append(results, LatestSearchResult{
			Title:    d.Title,
			URL:      ThreadURL(d.ThreadID),
			ThreadID: d.ThreadID,
			Version:  d.Version,
			Prefixes: d.Prefixes,
			CoverURL: d.Cover,
			Creator:  d.Creator,
		})
	}
	return results, nil
}

// ---------------------------------------------------------------------------
// F95Checker cache API (public fallback source)
// ---------------------------------------------------------------------------

// CacheFastCheck asks the F95Checker cache API when each thread last changed.
// Returns a map from thread ID to a unix timestamp. The caller compares these
// against its last-check time to decide which threads need a full refresh.
func (p *PublicAPI) CacheFastCheck(ctx context.Context, ids []int64) (map[int64]int64, error) {
	lastChanged := make(map[int64]int64, len(ids))
	for _, chunk := range chunkInt64(ids, maxFastCheckIDs) {
		u := p.CacheHost + "/fast?ids=" + csvInt64(chunk)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, fmt.Errorf("scraper: cache fast check: %w", err)
		}
		body, err := p.CacheClient.do(req, 0)
		if err != nil {
			return nil, err
		}
		var resp map[string]int64
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			return nil, fmt.Errorf("scraper: cache fast check: invalid response: %w", err)
		}
		for idStr, ts := range resp {
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil || id <= 0 {
				continue
			}
			lastChanged[id] = ts
		}
	}
	return lastChanged, nil
}

// CacheFullThread fetches full thread data from the F95Checker cache API.
// Returns ErrThreadNotFound when the cache API reports the thread as missing
// (404, or a non-Cloudflare 403) — the thread was privated, moved, or
// deleted. Cloudflare-challenge 403s are returned as-is so callers trip
// block detection instead of silently skipping every game.
func (p *PublicAPI) CacheFullThread(ctx context.Context, id int64) (*CacheThread, error) {
	if id <= 0 {
		return nil, fmt.Errorf("scraper: cache full thread: invalid id %d", id)
	}
	u := fmt.Sprintf("%s/full/%d?ts=0", p.CacheHost, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("scraper: cache full thread: %w", err)
	}
	// The cache API 404s transiently (its backend re-fetches expired
	// entries); retry once before concluding the thread is gone. The retry
	// sleep honors ctx cancellation.
	body, err := p.CacheClient.do(req, 0)
	var statusErr *HTTPStatusError
	if err != nil && errors.As(err, &statusErr) && statusErr.StatusCode == 404 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
		req, _ = http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		body, err = p.CacheClient.do(req, 0)
	}
	if err != nil {
		// A 404 — or a non-Cloudflare 403, which the cache API uses for
		// privated/deleted threads — means the thread is gone. A Cloudflare
		// challenge 403 means WE are blocked, not that the thread vanished;
		// return the block error so callers stop the run instead of
		// degrading every game to a per-game "not found" skip. This mirrors
		// retryable()'s Cloudflare discrimination (checkBlocked embeds
		// "Cloudflare" in the reason for challenge pages).
		if errIsThreadGone(err) {
			deleteCachedThreadIDByThread(id)
			return nil, ErrThreadNotFound
		}
		return nil, err
	}

	var resp struct {
		Name        string          `json:"name"`
		Version     string          `json:"version"`
		Developer   string          `json:"developer"`
		Status      json.RawMessage `json:"status"`
		ImageURL    string          `json:"image_url"`
		Description string          `json:"description"`
		Changelog   string          `json:"changelog"`
		LastUpdated json.RawMessage `json:"last_updated"`
		Score       json.RawMessage `json:"score"`
		Type        json.RawMessage `json:"type"`
		IndexError  string          `json:"INDEX_ERROR"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("scraper: cache full thread: invalid response: %w", err)
	}
	if resp.IndexError != "" {
		return nil, fmt.Errorf("scraper: cache full thread: index error: %s", resp.IndexError)
	}
	// An empty name means the response didn't carry thread data.
	if resp.Name == "" && resp.Version == "" {
		return nil, fmt.Errorf("scraper: cache full thread: empty response for thread %d", id)
	}

	// The cache API stores everything as strings (Redis), so numeric fields
	// arrive as JSON strings like "1" or "3.4". Parse leniently: accept both
	// numbers and strings, defaulting to zero on garbage.
	status, _ := rawJSONInt(resp.Status)
	lastUpdated, _ := rawJSONInt(resp.LastUpdated)
	score, _ := rawJSONFloat(resp.Score)
	// The Type enum uses the same numbering as the prefix taxonomy, so the
	// enginePrefixNames map applies directly (unknown types → "").
	engineType, _ := rawJSONInt(resp.Type)
	engine := enginePrefixNames[int(engineType)]

	return &CacheThread{
		Name:        resp.Name,
		Version:     StripVersionQualifier(resp.Version),
		Developer:   resp.Developer,
		Status:      cacheStatusNames[int(status)],
		ImageURL:    resp.ImageURL,
		Description: resp.Description,
		Changelog:   resp.Changelog,
		LastUpdated: lastUpdated,
		Score:       score,
		Engine:      engine,
	}, nil
}

// errIsThreadGone reports whether a cache-API error means the thread itself
// no longer exists — 404, or a non-Cloudflare 403 (the cache API answers
// privated/deleted threads with 403) — rather than that our session or IP is
// blocked. Cloudflare-challenge BlockedErrors carry "Cloudflare" in Reason
// (see checkBlocked); a tripped circuit breaker surfaces as StatusCode 0 and
// is also NOT a thread-gone signal.
func errIsThreadGone(err error) bool {
	var blockedErr *BlockedError
	if errors.As(err, &blockedErr) {
		switch blockedErr.StatusCode {
		case 404:
			return true
		case 403:
			return !strings.Contains(blockedErr.Reason, "Cloudflare")
		}
		return false
	}
	var statusErr *HTTPStatusError
	return errors.As(err, &statusErr) && statusErr.StatusCode == 404
}

// rawJSONInt parses an integer from a JSON number or a JSON string.
func rawJSONInt(raw json.RawMessage) (int64, error) {
	s := strings.Trim(string(raw), `"`)
	if s == "" || s == "null" {
		return 0, nil
	}
	return strconv.ParseInt(s, 10, 64)
}

// rawJSONFloat parses a float from a JSON number or a JSON string.
func rawJSONFloat(raw json.RawMessage) (float64, error) {
	s := strings.Trim(string(raw), `"`)
	if s == "" || s == "null" {
		return 0, nil
	}
	return strconv.ParseFloat(s, 64)
}

// strongTitleMatchScore is the minimum ComputeMatchScore confidence for the
// cache API's thread name to replace a game's local title. Below it — or
// without token containment — the local title is kept: weak substring
// matches ("Aurelia" vs "Aurelian Nostrum") must not clobber a scanned
// title with a different game's name.
const strongTitleMatchScore = 0.7

// ApplyCacheThreadData copies cookie-free cache data onto a Game: thread URL
// and ID, version, status, and engine. Unlike ApplyThreadData it has no
// content tags (the cache API returns them as opaque IDs), so tag and title
// handling is left to a later direct scrape when one happens.
//
// The thread name only replaces game.Title when the association is strong:
// matchScore >= strongTitleMatchScore AND one title's tokens contain the
// other's (see titlesContain). The engine comes EXCLUSIVELY from ct.Engine
// (the cache API's Type enum — the reliable source). The prefixes argument
// is retained for signature compatibility but IGNORED: latest_data.php
// `prefixes` numbering is not the F95Checker Type enum, so it cannot be
// trusted for engine assignment (see EngineNameFromPrefixes). An
// already-known engine is never flipped to a different one.
func ApplyCacheThreadData(game *db.Game, ct *CacheThread, threadID int64, prefixes []int, matchScore float64) {
	game.F95ThreadID = threadID
	game.F95URL = ThreadURL(threadID)
	if ct.Version != "" {
		game.LatestVersion = ct.Version
	}
	if ct.Status != "" {
		game.Status = ct.Status
	}
	if ct.Name != "" && game.Title != ct.Name && matchScore >= strongTitleMatchScore && titlesContain(game.Title, ct.Name) {
		// Cache names are the clean thread title (no version brackets);
		// applying it matches what a thread scrape would produce. Only
		// strong, token-consistent matches may rename a scanned title.
		game.Title = ct.Name
	}
	// Engine: the cache API's Type enum (ct.Engine) is the only reliable
	// signal. The prefix IDs from latest_data.php use a different numbering
	// (not the Type enum) and are unreliable — they are deliberately
	// ignored. Never flip an already known engine to a different one.
	if ct.Engine != "" {
		if game.Engine == "" || game.Engine == "Unknown" || game.Engine == "Others" {
			game.Engine = ct.Engine
		}
	}
}

// titlesContain reports whether one title's lowercase tokens are a subset of
// the other's (tokenized on non-alphanumeric boundaries). This is the
// containment guard for title renames: a substring match alone ("Aurelia"
// inside "Aurelian Nostrum") must not count as containment.
func titlesContain(a, b string) bool {
	ta := titleTokens(a)
	tb := titleTokens(b)
	if len(ta) == 0 || len(tb) == 0 {
		return false
	}
	return tokenSubset(ta, tb) || tokenSubset(tb, ta)
}

// titleTokens splits a title into lowercase tokens on non-alphanumeric
// boundaries ("Aurelia v1.0" → ["aurelia", "v1", "0"]).
func titleTokens(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
}

// tokenSubset reports whether every token in sub appears in sup.
func tokenSubset(sub, sup []string) bool {
	set := make(map[string]bool, len(sup))
	for _, t := range sup {
		set[t] = true
	}
	for _, t := range sub {
		if !set[t] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func chunkInt64(ids []int64, size int) [][]int64 {
	if len(ids) == 0 {
		return nil
	}
	var chunks [][]int64
	for start := 0; start < len(ids); start += size {
		end := start + size
		if end > len(ids) {
			end = len(ids)
		}
		chunks = append(chunks, ids[start:end])
	}
	return chunks
}

func csvInt64(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ",")
}
