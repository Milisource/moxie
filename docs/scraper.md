# Scraper

## What

The scraper sends authenticated HTTP requests to F95Zone, parses XenForo thread pages for game metadata, searches the forum for matching threads, and auto-associates local games with F95Zone entries. It lives in `internal/scraper/`: HTTP client (`scraper.go`), HTML parser (split across `parser.go`, `parser_version.go`, `parser_metadata.go`, `parser_links.go`), cookie handling (`cookies.go`), search (`search.go`), association algorithm (`associate.go`), title handling (`title.go` — `StripThreadPrefix`), metadata application (`apply.go` — `ApplyThreadData`), non-game thread detection (`nongame.go`), and types (`types.go`).

## How

### Cookie-Based Authentication

The scraper authenticates via the `Cookie` header. No password, no API key, no OAuth. The `cookieTransport` wraps Go's `http.DefaultTransport` and injects the Cookie header **only on F95Zone and F95Checker hosts**:

```
cookieTransport.RoundTrip(req)
  → attaches cookie only when host is f95zone.to / *.f95zone.to / api.f95checker.dev
  → appends cookie to existing Cookie header
  → delegates to http.DefaultTransport
```

Injection is host-scoped at the transport level: without this, the session cookie would be re-attached on every redirect hop and on the Google SERP fallback search — Go's cross-domain sensitive-header stripping only covers headers set on the initial request, not transport-level injection. (`NewClientWithHTTP`, the httptest-testing constructor, is explicitly unrestricted.)

Cookie sources, in priority order:
1. `--cookie` flag (explicit string from command line)
2. `--cookie-file` path (file read — Firefox `cookies.sqlite` via the read-only SQLite fallback)
3. Browser auto-detection via `browserutils/kooky` (Firefox, Chrome, Chromium, Brave, Edge — reads cookie stores at the binary level; Firefox is the reliable default, see `docs/browser.md` for the Windows Chrome ≥127 caveat)

### Rate Limiting Design

The scraper is designed to be a polite guest. The `Client.do()` method enforces:

```
minDelay       = 1.5s     (thread reads)
searchMinDelay = 3s       (searches are more expensive)
maxJitter      = 0-1.5s   (randomized per request)
cooldown       = pause 10s every 35 requests
backoff        = delay × 2 on 429, cap at 2min
```

On each successful request, the delay decays by 5% back to `minDelay` — but only when it is already above the floor, so `--unsafe` (delay 0) and the unpaced F95Checker `CacheClient` stay unpaced instead of snapping up to 1.5s after the first success. On HTTP 429, it doubles (exponential backoff). Non-2xx statuses surface as a typed `HTTPStatusError` (or `BlockedError` for 403/429/503) so callers can branch on the code instead of matching error strings.

### Retries, Circuit Breaker, and Preflight

`Client.do()` wraps each request in a retry loop (up to 3 attempts) with growing pauses (2s, 4s + jitter) so retries look human:

- **Transient failures** (network errors, 5xx, 429, 503) are retried
- **403** gets a single retry — Cloudflare challenges are sometimes per-request — then fails
- **Cloudflare challenge pages** are never retried (the same challenge would recur)
- **Context cancellation** aborts immediately, never retried

After **3 consecutive blocking responses** the circuit breaker trips: the client refuses further requests (fast-fail with a clear "refresh your F95Zone cookies" error) so a dead session doesn't waste a whole sync run failing game by game. A success resets the counter. Callers can check `Client.Blocked()` before scheduling more work.

`Client.Preflight()` verifies the session before a long direct-scrape run by fetching the forum index — fail fast instead of wasting minutes on doomed requests.

### Browser-Identical Headers

Go's `net/http` TLS fingerprint (JA4) matches no browser, which makes replayed `cf_clearance` cookies get probabilistically rejected. `applyBrowserHeaders()` makes the HTTP layer look like a Firefox navigation (UA, Accept, Accept-Language, Upgrade-Insecure-Requests, and per-method `Sec-Fetch-*` headers) — not a full TLS fingerprint fix, but it lifts the pass rate for the direct-scrape fallback path.

### Block Detection

The client checks every response for blocking signals:

- **HTTP 429** — rate limited, backs off and returns `BlockedError`
- **HTTP 403** — access denied, likely IP block or expired cookies; the body is scanned for Cloudflare markers to distinguish a challenge from a plain block
- **HTTP 503** — service unavailable, likely Cloudflare challenge
- **Body content** — scanned for `cf-browser-verification`, `cf-challenge-running`, `_cf_chl_opt` markers

When a block is detected, the scraper stops gracefully and surfaces the issue to the user rather than hammering the server.

### Cookie-Free Endpoints (primary sync path)

F95Zone exposes several JSON endpoints that require **no login and no cookies** — the same endpoints its own "Latest Updates" browser and the F95Checker indexer use. They are the primary data source for sync, making it work even when the browser session is expired or blocked:

| Endpoint | Purpose | Response |
|---|---|---|
| `GET /sam/checker.php?threads={csv}` | **Bulk version lookup** — up to 100 IDs per request (101+ → `Invalid threads data or >100`) | `{"status":"ok","msg":{"106408":"v6.3"}}`; `"Unknown"` for untracked threads; untracked threads absent from `msg` |
| `GET /sam/latest_alpha/latest_data.php?cmd=list&cat=games&search={q}&sort=likes&rows=15` | **Title search** (Redis-backed full-text; needs stopword stripping + 30-char cap, see `SanitizeSearchQuery`) | `msg.data[]` with `thread_id`, `title`, `version`, `prefixes`, `tags`, `cover` |
| `GET /sam/latest_alpha/latest_data.php?cmd=rss&cat=games` | Recent-updates RSS | Standard RSS with `[Title] [Version]` |
| `GET https://api.f95checker.dev/fast?ids={csv}` | F95Checker cache API: last-change timestamps (10 IDs/request) | `{"106408":1786204423}` |
| `GET https://api.f95checker.dev/full/{id}?ts=0` | F95Checker cache API: full thread data (version, status, developer, image, description, changelog) | Numeric fields arrive as **strings** (Redis); 403/404 → thread missing |

`PublicAPI` (`latestapi.go`) wraps these with the same pacing/retry/breaker machinery: a **paced** client for F95Zone's own endpoints (politeness) and an **unpaced** client for api.f95checker.dev (that service exists to serve programmatic clients — F95Checker itself runs 10 concurrent connections).

**Version normalization:** checker.php and cache versions carry qualifiers (`"v21.0.0 wip.7944"`, `"v1.03 + DLC"`, `"DX v5.0.5s"`). `StripVersionQualifier` reduces them to the numeric core before comparison so they don't surface as phantom updates against the cleaner versions the HTML parser stores.

**Prefix IDs:** search responses include F95Zone's internal prefix IDs. `EngineNameFromPrefixes` maps the ~15 engine IDs (RPGM=13, RenPy=14, Unity=19, HTML=5, …) to moxie engine names for engine-mismatch validation; `HasNonGamePrefix` rejects mods/tools/requests/comics threads. Unknown IDs are ignored (the set grows over time).

**Fallback chain (sync):**

```
1. checker.php bulk versions            — 1-2 requests for the whole library
2. api.f95checker.dev /full             — for threads checker.php doesn't track
3. direct thread scrape (cookie path)   — last resort, per game
```

**Desktop sync throughput:** the desktop app runs auto-association (the per-game search phase) on **3 parallel workers**, each with its own `PublicAPI` client so F95Zone pacing applies per worker rather than serializing the whole pool through one shared client. Two guards keep repeat runs cheap: unassociated games whose last search found nothing are skipped for 24h (`version_checked_at` cooldown), and the persistent association cache (`~/.config/moxie/associations.json`, shared with the CLI) short-circuits games whose thread was identified by an earlier run — no search round trip at all. A second sync start while one is running is rejected by a backend guard (the sync tab's state lives in the app shell, so it survives tab switches).

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

Version extraction is three-tiered, in descending order of reliability:

1. **Structured metadata block** (`extractVersionFromMetaOnly`) — the "Overview" section's `key: value` pairs, which thread authors maintain. The parser normalizes keys (`"Release Date"` → `release_date`, `"Ver"` → `version`) and only recognizes fields in a known allowlist.
2. **Bracketed tag in the thread title** (`extractVersionFromBrackets`) — the form F95Zone's posting rules mandate.
3. **Regex scan of the post body** (`extractVersionFromBody`) — last resort. The scan prefers the region *before* the first `Changelog`/`Download`/`Installation`/`Developer Notes` heading, because a changelog lists every past release and `extractVersion`'s longest-match rule would otherwise happily return an older entry (or an engine string like `Unity 2019.4.31`). If that leading region yields nothing, the full body is scanned so no recall is lost on threads that open with a download section.

The bracketed-title fallback (`extractVersionFromBrackets`) follows F95Zone's [official title format rules](https://f95zone.to/threads/game-uploading-rules-2024-02-29.524/) — `Game Name [Version] [Developer]` — and tries patterns in priority order:

1. **`[vX.Y]` / `[ver X.Y]` / `[version X.Y]`** — explicit prefix, also matches embedded forms like `[Ch. 2 v3.0]` and `[v1.0 Alpha]`
2. **`[YYYY-MM-DD]`** — date-based version for games without a version number
3. **`[X.Y]`** — bare version without v/ver prefix (safe: `]` must be immediate, so `[Ch. 1.5]` is not a false positive)
4. **`[Final]`** — sentinel for complete games with no version number

### Auto-Association (`scrape --auto`)

`FindMatches()` batch-processes all unassociated games:

1. Filter games that have no `f95_url` set
2. For each game, sanitize the title via `SanitizeTitle()` (strips version numbers, `[tags]`, `-PC`/`-Win`/`-Linux`, HG numeric prefixes, bracketed tags, platform suffixes)
3. Search F95Zone — **primary:** the latest-updates search (`latest_data.php?cmd=list&search={q}`, with `SanitizeSearchQuery` stopword stripping), carrying the session cookie when one is available (lifts the hard anonymous per-hour quota); **fallback:** the XenForo POST search (`/search/search` with `_xfToken`) and then Google site search
4. Parse results into candidates with title, thread ID, version, prefixes
5. Score each candidate with `ComputeMatchScore()`:
   - Exact match after sanitization: **1.0**
   - One contains the other **at word boundaries**: **0.85** — plain substring containment is NOT a match (`'Aurelia'` vs `'Aurelian Nostrum'` scores 0.0; the tokens don't nest, they're different games)
   - Word overlap: `shared_words / max(words_a, words_b)`
6. Auto-accept the best match if score >= 0.3. Non-game rejection is **title-based only** (`IsNonGameThread`) — the search API's `prefixes` array uses numbering unrelated to the F95Checker Type enum and must never drive classification or engine decisions (a real RenPy game returned prefix `[7]`, the enum's "Collection"). The winning title only replaces the local game title when the match is strong (score >= 0.7) **and** one title's tokens contain the other's — weak substring matches ('Aurelia' vs 'Aurelian Nostrum') never clobber a scanned title (F95-6duz, F95-sj8o)
7. **Primary:** fetch full thread data from the F95Checker cache API (version, status, developer, cover, and engine via the `type` enum — the correct numbering, unlike latest_data.php's `prefixes`); **fallback:** scrape the winning thread page directly and save metadata. Engine consistency is checked against the cache `type` field first, prefix IDs second (F95-ytru)

The title sanitizer pipeline handles real-world download directory names:

| Input | Sanitized |
|---|---|
| `SiNiSistar2_v1_0_6-WIN` | `SiNiSistar2` |
| `HG755_Inn, Tavern and Halberd` | `Inn, Tavern and Halberd` |
| `BootyHunter-0.8-pc` | `BootyHunter` |
| `Latex_Dungeon_V1.5.7-WIN` | `Latex Dungeon` |

### Host Detection

Download links are classified against 40+ F95Zone-approved file hosts. Links from unrecognized hosts are excluded — only known hosts (MEGA, Keep2Share, Uploaded, MediaFire, GoFile, Pixeldrain, Google Drive, Dropbox, Workupload, etc.) are reported. `identifyHost()` matches in two phases: **hostname needles are suffix-matched against the parsed URL host** (short needles like `k2s`, `dp.ua`, `terminal`, `bunkr` must not match an unrelated URL that merely contains the substring), with a fallback to link-text substring matching; F95Zone masked URLs (`f95zone.to/masked/<host>/…`) extract the real host from the path. All labels are single lowercase words (e.g. `googledrive`), and the downloader normalizes legacy space-variants.

**Link names:** threads often list the same host once per part/platform row (`<b>Part 2</b> … <b>Win</b>: GOFILE - MEGA - …`), so the bare anchor text ("GOFILE") can't tell links apart. `downloadLinkName()` recovers the row context — the nearest preceding bold/h2-h5 section heading ("Part 2", "Update 26", "DOWNLOAD") and the row's platform label (Win/Mac/Linux/Android/Web) — and uses it as the link name (e.g. `"DOWNLOAD Part 2 · Win"`) whenever the anchor text is just the host. Real filenames in anchor text are kept as-is. The name also drives `DetectPlatform`, so labeled rows get correct platform classification.

## Why

**Cookie auth over browser automation** — Cloudflare evasion via Playwright/Puppeteer is legally risky (CF ToS prohibits automated access) and technically fragile (undetected-chromedriver needs constant updates). Cookie import is simpler, works with months-valid sessions, and requires no browser process. Firefox auto-detection via kooky makes it seamless for the common case.

**Raw net/http over Colly** — Colly's rate limiting and parallelism features are convenient but add an extra dependency. The custom `cookieTransport` + manual backoff logic is ~80 lines and gives direct control over every aspect of request flow. Colly can still be swapped in later if needed.

**Structured metadata block parsing over full-text regex** — The structured block (key: value pairs in the Overview section) is more reliable than regex-scraping the entire post body. Release thread authors maintain this section; parsing it with known key normalization gives higher quality version and developer extraction. Full-text regex is the fallback for threads without a structured block.

**Conservative rate limiting** — a single `scrape --auto` run of 80 games takes ~8 minutes sequentially (all workers share one rate-limited client, so `--parallel` helps CPU/DB-bound work and fallback scrapes but does not multiply the request rate). The desktop sync instead gives each of its 3 workers its own client, which genuinely parallelizes search throughput. F95Zone runs behind Cloudflare and aggressive scraping gets you blocked — rate limiting per-worker is still enforced even in parallel mode.
