# F95Zone Scraper — Web Scraping Patterns

Lives in `internal/scraper/` (files: scraper.go, parser*.go, associate.go, assoc_cache.go, search.go, apply.go, title.go, cookies.go, nongame.go, types.go + tests). Scrapes XenForo-based F95Zone threads for game metadata, version info, and download links.

## Client Architecture

`Client` wraps `http.Client` with a `sync.Mutex` for thread-safe rate limiting:

- **Cookie injection** via `cookieTransport` (injects raw Cookie header on every request)
- **Rate limiting** — min 3s delay between requests, +2s random jitter (search uses 5s base)
- **Periodic cooldown** — every 25 requests, pause 15s to look human
- **Exponential backoff** — on 429, multiply delay by 2 (capped at 2 min)
- **Gradual recovery** — on success, reduce delay by 5% (floor at minDelay)
- **Bot detection** — checks for Cloudflare challenge pages, 403/503 responses
- **Thread safety:** All rate-limit state is protected by `c.mu` — safe for concurrent `do()` calls

## Creating a Client

```go
// Normal client (with rate limiting)
client := scraper.NewClient(cookieStr)

// Unsafe client (no rate limiting — risk of IP ban)
client := scraper.NewUnsafeClient(cookieStr)

// Test client (inject httptest.Server)
client := scraper.NewClientWithHTTP(cookieStr, httpClient)
```

Cookie source priority:
1. `--cookie` flag (string from CLI or TUI)
2. `--cookie-file` flag (path to file)
3. Firefox auto-detect via `browser.GetF95Cookies()` (reads Firefox SQLite)
4. Direct SQLite file: `browser.GetF95CookiesFromSQLite(path)`
5. TUI auto-load at startup via `main.go:187` (`browser.GetF95Cookies()` → `scraper.NewClient()`)

## HTML Parsing

Uses `goquery` (jQuery-like selectors) for XenForo HTML parsing. Parsers are split:

| File | Parses |
|------|--------|
| `parser.go` | Main thread page structure, title, thread ID |
| `parser_metadata.go` | Developer, tags, overview |
| `parser_version.go` | Version string from title (variants: `vX.Y.Z`, `X.Y.Z`, `X.Y`, date-based) |
| `parser_links.go` | Download link extraction from posts and download tabs |

Key patterns:
- Thread ID extracted from URL path `/threads/<id>/`
- Version parsed from thread title via `parser_version.go` — handles `[v1.0]`, `v2.5.1`, `1.0.0`, date-format
- Download links found in post attachments and dedicated download tabs
- Tags parsed from tag list markup

## Auto-Association

`associate.go` matches local game titles to F95Zone threads via search:

1. Sanitize local title via `title.go`: strip parenthetical notes, version suffixes, cleanup whitespace
2. Search F95Zone API with sanitized title
3. Score results by title similarity (fuzzy match)
4. Return best match above confidence threshold

`assoc_cache.go` persists successful associations to a JSON cache file at `~/.config/moxie/assoc_cache.json` so future auto-runs skip searching.

```go
scraper.LoadAssociationCache()       // load from disk
scraper.SetCachedThreadID(title, id) // add entry
scraper.SaveAssociationCache()       // persist to disk
```

## Blocked Request Handling

`BlockedError` type signals anti-bot protection:

```go
type BlockedError struct {
    Reason     string
    StatusCode int
}
```

Detection in `checkBlocked()` (`scraper.go:229`):
- HTTP 429 → rate limited
- HTTP 403 → IP block or missing cookies
- HTTP 503 → Cloudflare challenge
- Body contains `cf-browser-verification`, `cf-challenge-running`, `_cf_chl_opt` → Cloudflare challenge page

Callers check with `util.IsBlocked(err)` and display user-friendly messages.

## Non-Game Detection

`nongame.go` identifies F95Zone threads that aren't games (e.g., mods, tools, discussions). These are filtered from scraping results to avoid polluting the game library.

## Adding a New Parser

1. Create `parser_<feature>.go` in `internal/scraper/`
2. Use `goquery.NewDocumentFromReader` or parse from HTML string
3. Add extraction function, wire it into `parseThreadHTML()`
4. Add field to `ThreadData` struct in `types.go`
5. Add DB column migration in `internal/db/db.go`
6. Add field to `ApplyThreadData()` in `apply.go`

## URL Convention

Slug-agnostic thread URLs: `https://f95zone.to/threads/<id>/` — XenForo resolves by thread ID regardless of slug content, so these are stable across version updates.

```go
scraper.ThreadURL(threadID)              // build URL from ID
scraper.ResolveScrapeURL(url, threadID)  // prefer ID-based URL
```
