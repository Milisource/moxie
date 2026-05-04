# Scraper

## What

The scraper sends authenticated HTTP requests to F95Zone, parses XenForo thread pages for game metadata, searches the forum for matching threads, and auto-associates local games with F95Zone entries. It lives across 8 files in `internal/scraper/`: HTTP client (`scraper.go`), HTML parser (`parser.go`), cookie handling (`cookies.go`), search (`search.go`), association algorithm (`associate.go`), and types (`types.go`).

## How

### Cookie-Based Authentication

The scraper authenticates via the `Cookie` header. No password, no API key, no OAuth. The `cookieTransport` wraps Go's `http.DefaultTransport` and injects the Cookie header on every request:

```
cookieTransport.RoundTrip(req)
  → appends cookie to existing Cookie header
  → delegates to http.DefaultTransport
```

Cookie sources, in priority order:
1. `--cookie` flag (explicit string from command line)
2. `--cookie-file` path (file read)
3. Firefox auto-detection via `browserutils/kooky` (reads binary cookie store)

### Rate Limiting Design

The scraper is designed to be a polite guest. The `Client.do()` method enforces:

```
minDelay       = 3s       (thread reads)
searchMinDelay = 5s       (searches are more expensive)
maxJitter      = 0-2s     (randomized per request)
cooldown       = pause 15s every 25 requests
backoff        = delay × 2 on 429, cap at 2min
```

On each successful request, the delay decays by 5% back to `minDelay`. On HTTP 429, it doubles (exponential backoff). The `--unsafe` flag sets delay to 0 — useful for testing but risks IP bans.

### Block Detection

The client checks every response for blocking signals:

- **HTTP 429** — rate limited, backs off and returns `BlockedError`
- **HTTP 403** — access denied, likely IP block or expired cookies
- **HTTP 503** — service unavailable, likely Cloudflare challenge
- **Body content** — scanned for `cf-browser-verification`, `cf-challenge-running`, `_cf_chl_opt` markers

When a block is detected, the scraper stops gracefully and surfaces the issue to the user rather than hammering the server.

### Thread Parsing

`parseThreadHTML()` uses `goquery` (jQuery-style DOM traversal) to extract from XenForo 2.x HTML:

| Field | Selector / Strategy |
|---|---|
| **Title** | `h1.p-title-value` text |
| **Version** | Structured metadata block between "Overview" and next section heading, then regex `Version:`, `Ver.`, `v` patterns |
| **Developer** | Same metadata block, `Developer:` or `Publisher:` fields |
| **Tags** | `a.tagItem` elements |
| **Cover URL** | First `img.bbImage` in the first post, falling back to first large `<img>` |
| **Download Links** | All `<a href>` anchors hosting on 40+ approved file hosts |
| **Thread ID** | Regex from URL: `/threads/slug.12345/` → `12345` |

The version extraction is three-tiered: first the structured metadata block (the "Overview" section with key: value pairs), then bracketed tags in the thread title, then free-form regex scanning of the full post body. The metadata block parser normalizes keys (`"Release Date"` → `release_date`, `"Ver"` → `version`) and only recognizes fields in a known allowlist.

The bracketed-title fallback (`extractVersionFromBrackets`) follows F95Zone's [official title format rules](https://f95zone.to/threads/game-uploading-rules-2024-02-29.524/) — `Game Name [Version] [Developer]` — and tries patterns in priority order:

1. **`[vX.Y]` / `[ver X.Y]` / `[version X.Y]`** — explicit prefix, also matches embedded forms like `[Ch. 2 v3.0]` and `[v1.0 Alpha]`
2. **`[YYYY-MM-DD]`** — date-based version for games without a version number
3. **`[X.Y]`** — bare version without v/ver prefix (safe: `]` must be immediate, so `[Ch. 1.5]` is not a false positive)
4. **`[Final]`** — sentinel for complete games with no version number

### Auto-Association (`scrape --auto`)

`FindMatches()` batch-processes all unassociated games:

1. Filter games that have no `f95_url` set
2. For each game, sanitize the title via `SanitizeTitle()` (strips version numbers, `[tags]`, `-PC`/`-Win`/`-Linux`, HG numeric prefixes, bracketed tags, platform suffixes)
3. Search F95Zone via `https://f95zone.to/search/search?keywords=<query>&c[title_only]=1`
4. Parse results with `parseSearchResults()` — extracts title, URL, snippet from `.contentRow` elements
5. Score each candidate with `ComputeMatchScore()`:
   - Exact match after sanitization: **1.0**
   - One contains the other: **0.85**
   - Word overlap: `shared_words / max(words_a, words_b)`
6. Auto-accept the best match if score >= 0.3
7. Scrape the winning thread and save metadata

The title sanitizer pipeline handles real-world download directory names:

| Input | Sanitized |
|---|---|
| `SiNiSistar2_v1_0_6-WIN` | `SiNiSistar2` |
| `HG755_Inn, Tavern and Halberd` | `Inn, Tavern and Halberd` |
| `BootyHunter-0.8-pc` | `BootyHunter` |
| `Latex_Dungeon_V1.5.7-WIN` | `Latex Dungeon` |

### Host Detection

Download links are classified against 40+ F95Zone-approved file hosts. Links from unrecognized hosts are excluded — only known hosts (MEGA, Keep2Share, Uploaded, MediaFire, GoFile, Pixeldrain, Google Drive, Dropbox, Workupload, etc.) are reported. The host string is matched case-insensitively against a flat switch block in `identifyHost()`.

## Why

**Cookie auth over browser automation** — Cloudflare evasion via Playwright/Puppeteer is legally risky (CF ToS prohibits automated access) and technically fragile (undetected-chromedriver needs constant updates). Cookie import is simpler, works with months-valid sessions, and requires no browser process. Firefox auto-detection via kooky makes it seamless for the common case.

**Raw net/http over Colly** — Colly's rate limiting and parallelism features are convenient but add an extra dependency. The custom `cookieTransport` + manual backoff logic is ~80 lines and gives direct control over every aspect of request flow. Colly can still be swapped in later if needed.

**Structured metadata block parsing over full-text regex** — The structured block (key: value pairs in the Overview section) is more reliable than regex-scraping the entire post body. Release thread authors maintain this section; parsing it with known key normalization gives higher quality version and developer extraction. Full-text regex is the fallback for threads without a structured block.

**Conservative rate limiting** — A full `scrape --auto` run of 80 games takes ~8 minutes. This is intentional. F95Zone runs behind Cloudflare and aggressive scraping gets you blocked. Slow is safe.
