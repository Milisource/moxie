# F95Zone API usage audit — moxie

> Scope: every outbound F95Zone interaction in the Go tree, traced from
> `internal/scraper`, `internal/commands`, `desktop/` and `internal/downloader`,
> compared against the verified surface in
> [`../latest-data-api.md`](../latest-data-api.md) (2026-10-08) and the live
> probe report [`f95-api-surface.md`](f95-api-surface.md) (2026-11-07), plus the
> behaviour docs (`scraper.md`, `updates.md`, `f95zone-browser-design.md`,
> `browser.md`).
>
> Method: read the code, follow the call graph, count requests. Claims marked
> **verified** were read in the source at the cited line. Claims marked
> **inferred** follow from the code but were not exercised against the live site.
>
> No code was changed for this audit.

---

## 0. The one-paragraph version

moxie has **three independent metadata sources** — the XenForo thread HTML, the
F95Checker cache API, and F95Zone's `latest_data.php` index — and **no single
resolver** that owns "get me thread X's metadata". Every call site re-decides
which source to use and what to keep, so the same thread is fetched twice in
normal flows (`sync <id>`: `sync_game.go:115` + `:186`; desktop
preview→add: `app.go:2401` + `:2461`), the same `latest_data.php` query is run
twice in one desktop search (`app.go:2242` + `:2270`), and fields that a second
source already returned (version, title, stats) are thrown away. The pacing
machinery, block detection and breaker are genuinely good; the cost and
fragility problems are above that layer, in source selection and parsing
contracts.

---

## 1. Call-site inventory

Every distinct outbound F95Zone path. All requests to `f95zone.to` share **one
process-wide pacer** (`pacer.go:54-69`, key `f95PacerKey` at `pacer.go:21`);
`api.f95checker.dev` deliberately runs unpaced (`latestapi.go:123-131`,
`newClient` with `unsafe=true`, `scraper.go:176-182`).

| # | `file:line` | Function | Endpoint | Purpose | Triggered by | Auth | Caching | Concurrency / pacing |
|---|---|---|---|---|---|---|---|---|
| 1 | `internal/scraper/scraper.go:731` | `ScrapeThreadWithContext` | `GET https://f95zone.to/threads/{id}/` (slug-agnostic URL from `scraper.go:705`) | Full thread metadata: title, version, dev, tags, overview, cover, download links, saves, JSON-LD | CLI `scrape`, `scrape-batch`, `sync <id>`, `sync`, `check-updates` (fallback), `auto-update`, `download`, `download --open`, `saves fetch`; desktop `GetThreadPreview`, `AddGameFromF95Zone`, `SyncSingleGame`, sync phase 1/2; TUI download | Cookie optional; guest page ⇒ `LinksHidden` + `SessionError` when authenticated (`scraper.go:756-762`) | None | Shared pacer, `minDelay` 1.5 s + 0–1.5 s jitter (`scraper.go:26-28`); 10 s pause every 35 slots (`pacer.go:111-120`); 3 attempts, breaker at 3 blocks |
| 2 | `internal/scraper/search.go:160` | `fetchXFToken` | `GET https://f95zone.to/` | Scrape `_xfToken` from page HTML | Every XenForo search | Cookie | Per-`Client`, `xfTokenTTL` 10 min (`search.go:31`, `:140-157`) | Same pacer, `searchMinDelay` 3 s |
| 3 | `internal/scraper/search.go:191` | `xfSearchPOST` | `POST /search/search` (`keywords`, `c[title_only]=1`) | ≤5 relevance-ranked thread results | CLI `sync <id>` association, CLI `sync` phase-1 fallback, `scrape --auto` fallback; desktop `SearchF95Zone`, sync phase-1 fallback | Cookie + `_xfToken`; 400 ⇒ refetch token once (`search.go:95-101`) | None | Same pacer, 3 s |
| 4 | `internal/scraper/search.go:223` | `googleSiteSearch` | `GET google.com/search?q=site:f95zone.to "…"` | Last-resort search | When token/POST unavailable or fails | None (host-scoped cookie transport, `scraper.go:807-811`) | None | **Runs on the F95 pacer** (`search.go:232-236`) despite being a different host |
| 5 | `internal/scraper/latestapi.go:671` | `SearchTitle` | `GET /sam/latest_alpha/latest_data.php?cmd=list&search={q}&sort=likes&rows=15` | Cookie-free title search | CLI `sync`/`sync --force` phase 1, `scrape --auto`, desktop sync phase 1 (`app.go:3951`), desktop search enrichment (`app.go:2270`) | Cookie optional (lifts anon quota) | None; `_` cache-buster set per call (`latestapi.go:696`) | Same pacer, 3 s |
| 6 | `internal/scraper/latestapi.go:788` | `ListLatest` | `GET latest_data.php?cmd=list&cat=games&page=N&sort=S&rows=30` | Discover feed | Desktop `BrowseF95Zone` (`app.go:2337`) | Cookie optional | None (cache-buster) | Same pacer, 3 s |
| 7 | `internal/scraper/latestapi.go:548` | `BulkVersions` | `GET /sam/checker.php?threads={csv}` (≤100, `latestapi.go:43`) | Bulk current versions | CLI `sync` phase 2, `check-updates`, `auto-update`; desktop sync phase 2 (`app.go:4344`) | Cookie optional | None | Same pacer, 1.5 s; chunks sequential |
| 8 | `internal/scraper/latestapi.go:826` | `CacheFastCheck` | `GET https://api.f95checker.dev/fast?ids={csv}` (≤10, `:45`) | Last-change timestamps | CLI `refreshStatusViaCache` (`sync_check.go:336`); desktop `syncPhase2MetadataRefresh` (`app.go:4714`) | Cookie optional | None | **Unpaced** (`CacheClient`), chunks sequential |
| 9 | `internal/scraper/latestapi.go:858` | `CacheFullThread` | `GET /full/{id}?ts=0` | version, status, developer, description, cover, downloads (masked) | CLI association (`sync.go:595`), CLI update pass 2 (`sync_check.go:207`), desktop association (`app.go:4096`, `:4170`), desktop update (`app.go:4448`), desktop single sync (`app.go:4886`), desktop covers (`app.go:2847`) | Cookie optional | None; one 404 retry after 500 ms (`latestapi.go:867-880`) | **Unpaced**; always sequential |
| 10 | `internal/downloader/hosts_f95dl.go:147` | `resolveF95DownloadLink` → `solveF95DownloadLink` (`:168`) | `GET /dl/<postID>/?u&k&r&t` then `POST` the check form | Resolve a `/dl/` link to the host URL | `download`, `auto-update`, desktop update/install | Cookie + UA bound to `cf_clearance` (`:246-273`) | None; link freshness checked by `F95LinkStale` 90 min (`:71`) | Own `http.Client` (`:226`), not on the F95 pacer |
| 11 | `desktop/app.go:2965` | `fetchAndCacheCover` | `GET <cover URL>` (attachments/preview f95zone.to) | Cover bytes | Covers backfill (`app.go:2792`), sync/add via `saveScrapedMeta` (`app.go:4655`) | Cookie only for `*.f95zone.to` (`app.go:2981-2985`) | File cache with `.url` marker (`app.go:2928-2948`) | Own client, no pacer; Phase B limited to 4 (`app.go:2654`, `:2784`) |
| 12 | `internal/browserresolve/` | `engine.go` / `click.go:486` | Real browser navigation to `/dl/` or `/masked/` | Pass hCaptcha/reCAPTCHA/Turnstile | `download --open`/`--watch`, update pipeline fallback | Browser profile session | None | One browser, serial |
| 13 | `internal/scraper/scraper.go:562` | `DownloadToWriter` | `GET <attachment>` | Save archive / attachment bytes | `saves fetch` attachments (`saves_fetch.go:247`) | Cookie | None | Same pacer, 1.5 s; no total timeout (ctx only) |

Additional F95-adjacent paths that do **not** leave the machine or do not hit
F95Zone: `browser.GetF95Cookies` (60 s snapshot cache, `browser.go` per
`docs/browser.md`), `f95session.Refresh` (headless clearance mint, 2 min
dedupe, `internal/f95session/f95session.go:44`, `:95-104`).

---

## 2. Request-cost analysis

### 2.1 Per-operation request count

Pacing arithmetic (verified constants): thread reads `1.5 s + jitter 0–1.5 s`
⇒ **2.25 s mean**; searches `3 s + jitter` ⇒ **3.75 s mean**; every 35th slot
adds 10 s (`pacer.go:111-120`). Steady-state ceilings: **~24 thread reads/min**,
**~15 searches/min** on the shared pacer. No client count changes this — see
§2.4.

| Operation | HTTP requests | Notes |
|---|---|---|
| `moxie scrape <id>` | **1** | `scrape.go:69`; then N cover downloads only if the desktop backfills later |
| `moxie sync <id>`, already associated, cookie | **1 thread GET** | `sync_game.go:186` (phase 2), unless within the 24 h cooldown (`:170`) |
| `moxie sync <id>`, unassociated, cookie | **4** | `GET /` + `POST /search/search` (`sync_game.go:59`) + thread GET for association (`:115`) + **the same thread GET again** in phase 2 (`:186`), because `ApplyThreadData` never stamps `VersionCheckedAt` (`apply.go:56-89`) and the cooldown check at `sync_game.go:170` therefore passes |
| `moxie sync <id>`, no cookie | **1–2** | `syncGameCookieFree` (`sync.go:235-251`) → phase 2 only; no association possible |
| `moxie sync` (bulk), N unassociated | **N searches** (+ up to 6 more per miss, `search_variants.go:97`) + **1 `CacheFullThread` per hit** | Phase 1 (`sync.go:311-828`). 100 games @ 80 % hit ≈ 100 searches (≈6.7 min) + 80 unpaced cache calls; 20 misses × up to 7 queries ≈ +140 searches (≈+9 min) |
| `moxie sync`/`check-updates`, M associated | **⌈M/100⌉** checker + **1 `CacheFullThread` per thread checker doesn't track** + **⌈K/10⌉** `/fast` + **1 `CacheFullThread` per changed thread** | Phase 2 (`sync_check.go:136-266`, `:329-388`) |
| `moxie download` / `auto-update` per game | **1 thread GET** (fresh links, `download.go:226`) + **1 `/dl/` GET + POST** + host transfer | `updates.md` "re-scrape every time" is deliberate |
| `moxie saves fetch` | **1 thread GET** + **1 attachment GET** | `saves_fetch.go:76`, `:247` |
| **Desktop Discover page** | **1** | `app.go:2345`. Plus ~30 cover image GETs issued by the webview directly against `preview.f95zone.to` (`app.go:2371`), outside the pacer and any concurrency cap |
| **Desktop search** | **3** | `GET /` token (`app.go:2242` → `search.go:160`) + `POST /search/search` + `latest_data.php` catalog for covers/classification (`app.go:2270`). ≈11.3 s wall clock minimum, all three on the 3 s search slot |
| **Desktop preview click** | **1 thread GET** | `app.go:2401` |
| **Desktop Add to Library** | **1 more thread GET** of the *same* URL | `app.go:2461` — preview data is not reused |
| **Desktop `SyncSingleGame`** | **1 thread GET**, or **1 unpaced cache call** if scrape fails/no cookie | `app.go:4828-4838` |
| **Desktop `FetchCovers`** | **1 metadata request per game missing a cover file** (cache full, or thread GET if no thread ID/no image) + **1 cover GET per game** | `app.go:2737-2762` (serial resolve), `:2786-2816` (4 workers) |

### 2.2 The same data fetched twice

All **verified** from the code, not inferred:

1. **`moxie sync <id>` scrapes the thread twice.** Association at
   `sync_game.go:115`, then phase 2 at `:186`. `ApplyThreadData` (`apply.go:56`)
   never sets `VersionCheckedAt`, and the phase-2 guard at `sync_game.go:170`
   only skips when it is non-zero.
2. **Desktop Add-to-Library re-scrapes what the preview already scraped.**
   `GetThreadPreview` (`app.go:2401`) and `AddGameFromF95Zone` (`app.go:2461`)
   are separate bindings; the frontend calls them one after the other
   (`F95Browser.svelte:402`, `:431`) and the preview payload carries everything
   the add path re-derives.
3. **Desktop search runs the same title query twice.**
   `client.SearchF95Zone(query)` (`app.go:2242`) and
   `SearchCatalog(ctx, query)` → `SearchTitle` (`app.go:2270`,
   `latestapi.go:199-213`) both search the same string. The second response's
   `Title`, `URL`, `Version`, `Creator` are **discarded** — `enrichSearchResults`
   keeps only cover/prefix/tags/stats (`app.go:2279-2299`).
4. **Untracked threads get `CacheFullThread` twice per run.** CLI pass 2
   fetches it (`sync_check.go:207`) and appends the game to `entries`
   (`:241`); `refreshStatusViaCache` then fetches it again for every entry whose
   `/fast` timestamp is newer than the *pre-check* timestamp (`:350-353`) —
   which is exactly the case for a thread that just changed. Desktop has the
   same shape (`app.go:4448` then `app.go:4731`).
5. **`/fast` is asked about threads whose full data was just fetched.** Neither
   `refreshStatusViaCache` (`sync_check.go:332-336`) nor
   `syncPhase2MetadataRefresh` (`app.go:4708-4714`) excludes threads whose
   `CacheFullThread.LastUpdated` is already known — and `LastUpdated` is parsed
   (`latestapi.go:925`, `:940`) but never read anywhere in the tree.
6. **`FetchCovers` ignores the cover URL already in the database.** The
   function comment claims "games with a stored cover URL download it directly"
   (`app.go:2673-2676`), but `resolveCoverURL` only takes a `db.Game`
   (`app.go:2844`) and `db.Game` has no cover field (`internal/db/models.go:54-85`);
   `scraped_meta.cover_url` is never read. Every cover-less game therefore costs
   one metadata fetch even when the URL is known. (`GetScrapedMeta` exists,
   `internal/db/scraped_meta.go:97`.)
7. **`/fast` id lists are not deduplicated** by thread ID
   (`sync_check.go:332-335`, `app.go:4708-4713`), so platform copies of the same
   game consume separate slots in a 10-per-request batch.

### 2.3 HTML where JSON would answer

This is the core thesis, and it holds in three concrete places:

1. **Desktop search** (`app.go:2235-2272`). `latest_data.php?cmd=list&search=`
   already returns `thread_id`, `title`, `version`, `cover`, `prefixes`,
   `tags`, `views`, `likes`, `rating`, `date` and carries 15 rows
   (`latestapi.go:168-181`, `docs/latest-data-api.md:57-79`). The desktop
   instead pays for `GET /` + `POST /search/search` (cookie + CSRF + XenForo
   HTML selectors) to get ≤5 rows, then pays for a second `latest_data.php`
   call only to enrich those rows with data the same endpoint already had.
   Net: three requests and two HTML fragile layers where one JSON request
   returns a superset.
2. **Desktop preview** (`app.go:2384-2435`). Preview needs
   title/version/developer/overview/cover/tags/status/links. The cache API's
   `/full/{id}` returns name, version, developer, status, description,
   image_url, downloads (`latestapi.go:254-273`) over an unpaced,
   Cloudflare-free host. Only `/dl/` links and `a.tagItem` tags are HTML-only,
   and the download links are re-scraped anyway before a download
   (`freshRankedDownloadLinks`, `app.go:5018`). A JSON-first preview would cut
   a 1.5 s+ paced, block-prone request per click to an unpaced one (and is the
   same call `syncSingleViaCache` already makes).
3. **`moxie sync <id>`** (`sync_game.go:186`). Version/status/tags are exactly
   what `BulkVersions` (1 request per 100 threads) and `CacheFullThread` answer;
   the second HTML fetch exists only to refresh links on a path that the code
   then does not use for links.

### 2.4 Sequential vs parallel, and what the pacer really does

- **Search is not parallelised in either frontend**, contrary to the comment at
  `desktop/app.go:4037-4040` ("paced per worker — a shared client would
  serialize the whole pool") and `docs/scraper.md:211` ("gives each of its 3
  workers its own client, which genuinely parallelizes search throughput").
  `NewPublicAPIWithCookie` → `NewClient` → `sharedPacer("f95zone.to")`
  (`scraper.go:176-182`, `pacer.go:54-69`), so the 3 desktop workers (or
  `--parallel` CLI workers) issue `latest_data.php` searches on **one** 3 s
  stream. The workers do overlap unpaced cache-API calls and parsing; search
  throughput is 1 / 3.75 s regardless of worker count.
- Sequential where it could be pipelined: `CacheFullThread` per untracked thread
  (`sync_check.go:203-244`, `app.go:4440-4473`), `/fast` chunks
  (`latestapi.go:828`), covers Phase A resolve (`app.go:2737-2762`). The cache
  host is unpaced and F95Checker itself runs ~10 concurrent connections
  (`latestapi.go:100-105`), so these are the safe ones to parallelise.
- Already parallel: desktop cover downloads (4 workers, `app.go:2784`), CLI
  direct-scrape fallback (3 workers, `sync_check.go:409`), CLI/desktop phase 1
  (3 workers, pacer-bound).
- Fetch token cost is per `Client`, and every desktop binding constructs a new
  `Client` (`app.go:2241`, `:2400`, `:2460`, `:2345`), so the 10-minute token
  cache (`search.go:31`) **never hits** in the desktop app. One search = one
  `GET /`.

### 2.5 Cooldowns that actually bound cost

`UpdateCheckCooldown` 24 h (`sync.go:36`) is one column, `version_checked_at`,
used for **both** "association search found nothing" and "version checked".
That is what makes repeat syncs cheap (`sync.go:328-334`,
`sync_check.go:40-45`, `app.go:3975`, `:4314`). Two leaks: (a) association
success also stamps it (`sync.go:619`, `:715`), which delays a brand-new game's
first *version* check by up to 24 h; (b) desktop `markSearchDone` stamps it on
"no match" (`app.go:4266-4273`), so a user's later manual single-game sync within
24 h skips its update check (`app.go:4314`).

---

## 3. Field coverage gap

### 3.1 `latest_data.php` row (16 fields, per `docs/latest-data-api.md:57-79`)

Wire struct: `latestItem` (`internal/scraper/latestapi.go:168-181`); mapped into
`LatestSearchResult` (`:143-162`) by `latestResults` (`:731-754`); consumed by
`buildBrowsePage` (`desktop/app.go:2357-2381`) and `enrichSearchResults`
(`app.go:2279-2299`).

| Field | Parsed? | Persisted? | Surfaced? | Verdict |
|---|---|---|---|---|
| `thread_id` | ✅ `:169` | ✅ `games.f95_thread_id` | ✅ | fine |
| `title` | ✅ `:170` | ✅ via apply | ✅ | fine |
| `creator` | ✅ `:171` | ❌ | ✅ Discover card (`app.go:2370`), search rows drop it (`F95SearchResult` has no creator) | **UI-only, never stored** |
| `version` | ✅ (`rawJSONString`) `:172` | ✅ `latest_version` | ✅ | fine |
| `views` | ✅ `:173` | ❌ | ✅ browse/search (`app.go:2143`, `:2180`) | **transient stat** |
| `likes` | ✅ `:174` | ❌ | ✅ browse/search | **transient stat** |
| `prefixes` | ✅ `:175` | ❌ | engine only (`EngineFromLatestPrefixes`, `:403-410`); status 18/20/22, category 13/19/23 discarded | **half-decoded** |
| `tags` | ✅ `:176` | ❌ | only `2265` AI-CG (`HasAICGTag`, `:420-427`); the other 152 tag IDs dropped, no names | **1 of 153 used** |
| `rating` | ✅ `:177` | ❌ | ✅ browse/search sort (`F95Browser.svelte:245-252`) | **transient stat** |
| `cover` | ✅ `:178` | via cache API/scrape only | ✅ (preview CDN in browse, full-res in search) | fine, but two host policies (`latestapi.go:244`, `app.go:2353-2356`) |
| `screens` | ❌ | ❌ | ❌ | **missing gallery** (0 extra requests if parsed) |
| `date` (relative) | ✅ `:179` | ❌ | ✅ card (`app.go:2375`) | UI-only |
| `watched` | ❌ | ❌ | ❌ | **missing** (session state) |
| `ignored` | ❌ | ❌ | ❌ | **missing** (session state; `ignored=hide` filter available server-side) |
| `new` | ❌ | ❌ | ❌ | **missing unread badge** |
| `ts` | ✅ `:180` | ❌ | ❌ — `LatestSearchResult.Ts` (`:161`) has **no reader anywhere** | **dead field** |

### 3.2 Thread page (`ThreadData`, `internal/scraper/types.go:12-40`)

| Field | Parse site | Persistence | Gap |
|---|---|---|---|
| Title | `parser.go:73-83` | `games.title` | `stripBracketed` (`parser.go:41`, `:211-216`) removes **every** parenthesised/bracketed substring, so "Game (Remastered)" loses the qualifier before it is ever stored |
| Version | `parser.go:110-116`, `parser_version.go:69-151` | `games.latest_version` | fine (three-tier) |
| Developer | `parser_metadata.go:151-156` | `scraped_meta.developer` | fine |
| Overview | `parser_metadata.go:54-78` | `scraped_meta.overview` | if the "Overview" heading is missing it returns the **entire post text** (`:57-59`), including the changelog and download rows, and that is persisted |
| Tags | `parser_metadata.go:159-168` | `games.tags` (JSON) | `a.tagItem` is the only source; writers guard `len(tags) > 0` (`apply.go:77`, `sync_check.go:481`, `app.go:4519`) so a selector break leaves stale tags forever **with no signal** |
| Prefixes | `parser.go:190-209` | ❌ | engine is derived (`apply.go:34-39`); status is derived (`parser_metadata.go:304-323`); category/collection/siterip labels are discarded |
| Cover | `parser_metadata.go:186-247`, JSON-LD fallback `parser.go:127-131` | `scraped_meta.cover_url` (signature stripped, `parser_metadata.go:268-282`) | fine |
| Download links | `parser_links.go:141-177` | `download_links` | **`Size` is parsed (`parser_links.go:173`) and then dropped by almost every writer:** `scrape.go:123-129`, `app.go:2555-2562`, `app.go:4934-4941` omit it; only `download.go:235-242` and `tui/commands.go:568-573` store it. The DB column exists (`db.go:250`), and `DownloadLink.Size` is read by the downloader's expected-total logic |
| Store links | `parser_links.go:355-379` | `games.store_links` | fine |
| Status | `parser_metadata.go:304-323` | `games.status` via `ResolveStatus` (`apply.go:17-25`) | fine |
| Save candidates | `parser_saves.go:58-132` | not stored (fetched on demand) | fine, but **CLI-only surface** (§5) |
| `PublishedAt` / `UpdatedAt` (JSON-LD) | `parser.go:135-140` | ❌ no column | **parsed and discarded**; no "last thread update" anywhere in the UI or sort |
| `LinksHidden` | `scraper.go:756-757` | ❌ | only used as a flag in the error path; the desktop preview cannot say "links hidden" (`ThreadPreview` has no field, `app.go:2149-2160`) |

### 3.3 Cache API (`CacheThread`, `latestapi.go:254-273`)

`Name`, `Version`, `Developer`, `Status`, `ImageURL`, `Description` are used.
`Changelog` (`:261`) and `Score` (`:263`) are parsed and dropped; `LastUpdated`
(`:262`) is parsed and dropped *and* is the key that would let §2.2 items 5–6
disappear. `Downloads` are used only as a fallback for `/masked/` links
(`download_links.go:108-141`).

---

## 4. Fragility inventory

Ranked by "silent partial data" risk, since that is worse than a hard error.
Legend for failure mode: **E** = error surfaced, **∅** = silently empty,
**P** = silently partial/stale.

| # | Dependency | Site | What breaks when F95Zone changes | Failure mode |
|---|---|---|---|---|
| 1 | Download-row **favicon style** `…/ip3/<host>.ico` | `parser_links.go:19`, `:188-198` | If the `/ip3/` favicon service moves, every `/dl/` link has `host == "other"` and is dropped at `parser_links.go:162-164`. Anchor text is the only remaining signal (`identifyHost`, `:389-544`), which fails for rows labelled "Mirror". **All download links vanish, scrape reports success.** | ∅ / P, no log |
| 2 | `a.tagItem` | `parser_metadata.go:161` | Tags empty ⇒ writers' `len(tags) > 0` guards keep the old value forever; engine/status fall back to title words. | P (stale) |
| 3 | `h1.p-title-value` / `.p-title-prefix` | `parser.go:73-80`, `:190-209` | Title text still parses (all labels inline) but `Prefixes` empty ⇒ `ThreadEngine` falls back to title (`apply.go:34-39`); `ThreadTitle` re-enables legacy inline prefix stripping (`apply.go:47-52`), which can eat a legitimate leading word. | P |
| 4 | `article.message-content .bbWrapper` → `.bbWrapper` | `parser.go:57-60` | No content selection ⇒ overview empty, version falls back to the title bracket, **zero download links, zero saves**. `ScrapeThread` returns `nil` error. | ∅, no log |
| 5 | `span.u-hidden[aria-hidden]` duplicate-anchor skip | `parser_links.go:182-184` | If F95 drops the hidden duplicate, each row yields two links (one dead); if it changes the hiding mechanism, the dead link is stored and later fails with "That link is no longer in the post". | P |
| 6 | `isHiddenAnchor` complement: section-header heuristics | `parser_links.go:29`, `:230-313` | Link names/`DetectPlatform` degrade; a mod row can be labelled as a patch (the code comment at `:24-28` documents this exact past bug). | P |
| 7 | Google SERP selectors | `search.go:321` (`a[href*='f95zone.to']`), `:333` (`/url?q=`), `:367` (`div.g, div[jscontroller]`) | Google markup changes ⇒ 0 results, no error. This path is now reachable from cookie-free sync (§5.1), so it can silently burn requests. | ∅ |
| 8 | XenForo search rows `.contentRow` / `h3.contentRow-title a` | `search.go:254`, `:259`, `:276` | Results empty ⇒ CLI reports "no results" and desktop shows an empty list. `_xfToken` regex (`:47`) failing reroutes every search to Google with only a `Debug` log (`:117-120`). | ∅ |
| 9 | `/sam/checker.php` envelope + 100-ID limit | `latestapi.go:43`, `:626-658` | `msg` object→string handled (`bulkErrorMsg`, `:1055-1065`); but a limit change (e.g. 50) would classify as `endpoint_gone`/`transient` for a 100-ID chunk, and the CLI silently degrades to `CacheFullThread` per game (`sync_check.go:199-244`), i.e. 100× the requests. | P, logged |
| 10 | latest-data **prefix ID → engine** map | `latestapi.go:381-397`, `:403-410` | New engine prefix (or renumbering) ⇒ `EngineFromLatestPrefixes` returns "" for those rows. The desktop **drops unknown-engine rows entirely** when an engine filter is active (`F95Browser.svelte:229-238`) and offers no badge. | ∅ under filter |
| 11 | `nonGamePrefixes` / `enginePrefixNames` (F95Checker Type enum) | `latestapi.go:60-94` | Only used for the cache API's `type` (`:930`), which is correct; but the two maps coexist with `latestEnginePrefixes` and are easy to cross-wire — the file itself warns three times (`:337-369`, `:375-380`). | latent |
| 12 | `searchStopwords` + 30-char cap | `latestapi.go:285-335` | A title whose meaningful words are all stopwords sanitises to "" and `SearchTitle` errors (`:672-675`) → CLI treats it as "search failed" per game. The 30-char cut can drop the distinguishing token of long titles. | ∅ / E |
| 13 | `rows` whitelist / `sort` whitelist | `latestapi.go:763-773` | Unknown `sort` silently becomes `date` server-side and `browseSorts` drops `title`; `rows` non-multiples of 15 silently become 30. No user-visible difference except wrong ordering. | P |
| 14 | `_` cache-buster | `latestapi.go:696` | Ensures no intermediate cache can ever answer; also means a Discover "refresh" always costs a full request. | cost, not fragility |
| 15 | `data-logged-in` on `<html>` | `session_state.go:30` | If XenForo renames/renumbers it (`true`→`1`), `SessionUnknown` is returned and an expired session is **never detected** — every subsequent scrape returns guest HTML with no links and no `SessionError`. | ∅ / P |
| 16 | Attachment path regex `/attachments/<slug>.<id>/` | `parser_saves.go:18` | Saves stop being discovered; other candidates unaffected. Also drops any future attachment URL form. | ∅ |
| 17 | `saveSignalRe` (`saves?`, `100%`, `gallery`) | `parser_saves.go:24` | Wording change ⇒ fewer save candidates. Note `gallery` as a save signal will also match a gallery link. | ∅ / false positive |
| 18 | JSON-LD `script[type="application/ld+json"]` | `parser.go:154` | Optional by design; loss only removes cover fallback and timestamps. | ∅ (documented) |
| 19 | Overview heading / section-heading regexes | `parser_metadata.go:18-23` | "Overview" variants ("Overview:", "**Overview**" as HTML is fine; a nested heading is text) — a wording change makes `extractMetadata` return empty and `extractOverviewText` return the whole body. Version then falls to brackets/body scan (more false hits from changelogs; mitigated at `parser_version.go:97-104`). | P |
| 20 | Title `[version]` regex ladder | `parser_version.go:12-38` | New title conventions produce "" or a wrong number; a wrong number becomes `latest_version` and shows as a phantom update (`version.Compare` → `Changed`). | P |
| 21 | `nonGameThreadPrefixes` (literal title words) | `nongame.go:12-43` | Only affects auto-association filtering; a game titled "Help Wanted" is rejected as non-game. | false negative |
| 22 | `SearchQueryVariants` noise regex | `search_variants.go:28` | Benign: worst case is an extra request. | cost |

Two structural notes on fragility:

- **Nothing validates a parse.** `parseThreadHTML` only errors if goquery cannot
  parse the document (`parser.go:51-54`). Missing selectors produce a
  `ThreadData` with empty fields and `err == nil`, and every caller then writes
  that emptiness as truth where it can (title is guarded by `!= ""`,
  version by `!= ""`, tags by `len > 0`, but **overview and cover are
  unguarded** in `UpsertScrapedMeta` call sites — e.g. `sync_game.go:240-253`,
  `app.go:4642-4658`).
- **The host whitelist is a silent data filter**, not just classification
  (`parser_links.go:161-164`, `:449-544`). 40+ hosts are hard-coded; a new
  approved host is invisible until a release ships.

---

## 5. CLI vs desktop divergence

Both frontends share `internal/scraper`, `internal/db` and (mostly) the same
policy functions, but the *composition* differs.

| Capability | CLI | Desktop | Divergence / consequence |
|---|---|---|---|
| Discover feed | ❌ none | `BrowseF95Zone` (`app.go:2337`) | Feed exists only in the GUI; `moxie sync` cannot browse/inspect the index |
| Search | `sync`/`scrape --auto` → `latest_data.php` + variants (`sync.go:461`); `sync <id>` → XenForo POST only (`sync_game.go:59`) | XenForo POST **+** `latest_data.php` enrichment (`app.go:2242`, `:2270`) | **Three different search strategies.** CLI single-game has no cookie-free search and no variant ladder; desktop pays for both searches and discards the second's title/version |
| Search without a session | ✅ phase 1 works cookie-free (`latest_data.php`) | ❌ `SearchF95Zone` hard-errors "cookies not available" (`app.go:2236-2239`) | Desktop cannot search logged-out even though the endpoint is public |
| Single-game sync | 2 thread scrapes when unassociated (`sync_game.go:115`, `:186`); cookie-free mode cannot associate (`sync.go:235-251`) | 1 scrape then cache-API fallback (`app.go:4828-4838`) | CLI worse; also CLI overwrites the local title (`sync_game.go:140` → `apply.go:64`) while desktop restores it (`app.go:4852-4854`) |
| `--force` semantics | Only bypasses cooldowns; **still uses bulk checker + cache API** (`sync_check.go:68-78`, `:136-158`) — download links are **not** refreshed | Skips bulk entirely and scrapes every thread, explicitly to refresh the Downloads tab (`app.go:4328-4334`) | `docs/scraper.md:114` claims a forced sync refreshes links; true for desktop, **false for CLI** |
| Block handling, phase 2 | Per-game errors, continue (`sync_check.go:423-443`); phase 1 aborts (`sync.go:811-818`) | Any block aborts the whole run (`app.go:4411-4413`, `:4295-4299`) | Same error, opposite blast radius |
| Cover backfill | ❌ no `covers fetch`; only `covers upgrade` (portrait art from Steam/SGDB/VNDB, `covers.go:28-152`) | `FetchCovers` (`app.go:2681`) | CLI users cannot populate the cache; desktop covers also bypass the pacer (`app.go:2979`) |
| Cover source policy | n/a | Browse = 400 px `preview.f95zone.to`; search = full-res `attachments` (`app.go:2353-2356` vs `latestapi.go:244`) | Same endpoint response is interpreted two ways; caching/dedup would have to pick one |
| Save fetch from F95 | ✅ `saves fetch` (`saves_fetch.go:41`) | ❌ no binding | CLI-only capability |
| Batch scrape | ✅ `scrape-batch` (`scrape.go:153`) | ❌ | CLI-only |
| Update-check output | `--json` (`sync_check.go:562`, `:597-602`); auto-update loop with lock/backoff (`auto_update.go`, `updates.md`) | Event-driven pipeline; no unattended loop | Different automation story |
| Duplicate-query handling | Deduplicated jobs are **dropped** (`sync.go:405-417`) | Shared `queryMemo`, every copy still associated (`app.go:3933-3955`) | CLI never associates the second platform copy |
| Match scoring | `VersionMatchBonus` added **unconditionally** (`sync.go:534-536`) | Bonus only when `base >= 0.3` (`app.go:4609-4611`, with the reason documented) | CLI can auto-accept a wrong sequel at 0.25 + 0.3; **behavioural bug, not just drift** |
| Download-link write | `sync <id>` writes none; `scrape`/`download` delete-all then re-insert (`scrape.go:116`, `download.go:230`) | Diff-and-apply preserving IDs and `IsDead` (`app.go:4908-4954`) | CLI churns link IDs and resurrects dead links |
| Link size | stored by `download`/TUI only (`download.go:240`, `tui/commands.go:571`) | never stored (`app.go:2555`, `:4934`) | Data loss on the GUI path |
| Client-side filtering | n/a (no browse UI) | Engine include/exclude + Hide AI-CG applied over loaded pages (`F95Browser.svelte:229-238`); the server supports `prefixes[]`/`noprefixes[]`/`tags[]`/`notags[]` (`latest-data-api.md:46-55`) | Result count "27,503 games" (`F95Browser.svelte:724-725`) is the **unfiltered** count next to "N of M loaded · hidden by filters" (`:742-744`) — the headline number contradicts the grid |
| Client-side sorting | n/a | Search results re-sorted locally by likes/views/rating over ≤5 rows (`F95Browser.svelte:245-252`) | Sorting five items is cosmetic; the server sorts the whole index |
| Unread/ignored state | ❌ | ❌ | `new`/`watched`/`ignored` unparsed in both (§3.1) |

---

## 6. Reliability findings

All thresholds below are hard-coded package constants, none configurable
(`internal/scraper/scraper.go:20-44`, `pacer.go:17`).

| Mechanism | Value | Site | Assessment |
|---|---|---|---|
| Thread-read spacing | 1.5 s + 0–1.5 s jitter | `scraper.go:26-28` | Reasonable; jitter applied only when a slot is in the future (`pacer.go:105-110`) |
| Search spacing | 3 s + jitter | `scraper.go:29` | Makes a desktop search cost ≥11 s (§2.1) |
| Cooldown | 10 s every 35 slots, process-wide | `scraper.go:30-31`, `pacer.go:111-120` | Fine; counted on reservations, not successes |
| Backoff on 429 | `Retry-After` if present, else ×2, capped 2 min | `pacer.go:193-219`, `scraper.go:460-482` | Good: RFC 9110 delta-seconds **or** HTTP-date, absurd values ignored |
| Retry budget | 3 attempts, 2 s/4 s + 1 s jitter | `scraper.go:40`, `:319-331` | Fine; 4xx not retried (`:376-379`), 400 not retried (token refresh handles it, `search.go:95-101`) |
| Breaker | 3 consecutive blocks ⇒ 10 min refuse | `scraper.go:41`, `pacer.go:17`, `:200-207` | **Over-counts:** a single request that is blocked on all 3 attempts increments the counter 3× (`scraper.go:304-314` runs before the attempt check), so **one** 429/403 trips the 10-minute breaker for every client in the process |
| Backoff decay | 5 % per success toward 1.5 s | `pacer.go:178-188` | From the 2 min cap, recovery needs ≈85 successful requests (~3 min at best case). A single aggressive `Retry-After: 600` (capped to 120 s) can leave the client at 2 min/request for minutes |
| Clearance refresh | Once per `Client`, min 2 min between mints | `session.go:70-99`, `f95session.go:44` | Correct; per-client flag means desktop's per-call clients could each trigger a mint, but `minRefreshInterval` dedupes |
| UA / clearance binding | UA provider pinned to the cookie store's browser version | `session.go:42-52`, `scraper.go:443-458` | Good — the documented root cause of a past total breakage (`docs/scraper.md:64`) |
| Block detection | 429/403/503 + body markers + `cf-mitigated` | `scraper.go:488-543` | Good; body scan capped at 8 MiB (`:27`, `:489-496`) |
| 404 discrimination | `checker.php` 404 body "Thread not found" ≠ endpoint gone | `latestapi.go:522-540`, `:637-639` | Good, subtle, correct |
| Cache-API 404 | One retry after 500 ms, then `ErrThreadNotFound`; non-Cloudflare 403 also "gone" | `latestapi.go:858-894`, `:1001-1014` | Reasonable; a privated thread silently disappears from checks (`sync_check.go:209-215`) rather than alerting |
| Session expiry | Only detected for an **authenticated** client seeing `data-logged-in=false` | `scraper.go:756-762`, `session_state.go:27-39` | Guest scrapes get no links and no error (by design); but a markup change disables expiry detection entirely |
| Preflight | `Client.Preflight` fetches `/` before a long run | `scraper.go:660-675` | **Dead code** — no caller in the tree (`grep Preflight()`) |

Things that should be configurable but are not: pacing (`minDelay`,
`searchMinDelay`, jitter), cooldown interval/length, breaker threshold and
`breakerCooldown`, retry budget, `UpdateCheckCooldown`, `f95LinkMaxAge`
(`hosts_f95dl.go:71`), cover concurrency (`coverFetchWorkers`,
`desktop/app.go:2654`), Phase-2 worker count (`sync_check.go:409` literal 3).
Today `internal/commands/config.go` exposes only `auto_update`,
`auto_update_max`, `saves_dir`, `steamgriddb-key`, `scan-paths`, and
`f95_clearance` (read in `f95session.go:69-81`).

Worth noting against that table: the live probe of 2026-11-07 sent six anonymous
`latest_data.php` requests in ~20 s with no 429, no `Retry-After` and no quota
message (`f95-api-surface.md:419-433`). That does not prove the hourly cap is
gone, but it does mean our 3 s search spacing is a self-imposed constraint, not
an observed one — which is exactly why item 14 (configurable pacing) is worth
doing rather than hard-coding a different constant.

Two concrete reliability consequences worth tickets:

1. **One 429 can abort a whole desktop sync.** `BulkVersions` retries the chunk
   3× (`scraper.go:287-333`); each blocked attempt calls
   `pacer.noteBlocked` (`:313`), the third trips the breaker
   (`pacer.go:200-207`), `classifyBulkErr` returns `rate_limited`
   (`latestapi.go:512-519`), the desktop treats any non-`endpoint_gone` chunk
   error as fatal (`app.go:4358-4366`) and emits `sync:error`
   (`app.go:4295-4299`). The user sees "blocked" after a single rate-limit
   response, even when `Retry-After` was honoured.
2. **A `Retry-After` of minutes is applied to every subsequent request**, not
   just the failed one (`pacer.go:209-218` sets the shared `delay`), and decays
   only 5 % per success. A single server hint can turn the next ~80 requests
   into a 2-minute-apart crawl.
3. **Cover requests bypass the UA discipline.** `fetchAndCacheCover` sets
   `User-Agent: Mozilla/5.0 (compatible; moxie/0.4)` (`app.go:2979`) while
   attaching the F95 cookie (`:2981-2985`). `cf_clearance` is bound to the
   minting browser's UA (`docs/scraper.md:64`, `f95session.go:3-7`), so a
   Cloudflare-protected attachment host can 403 every full-res cover while the
   browse preview CDN (`preview.f95zone.to`, no cookie path) keeps working.
   **Inferred** — the cover CDN may not enforce it, but it contradicts the
   validated UA rule everywhere else.

---

## 7. Ranked improvement list

Benefit/effort ordering; effort S ≈ <½ day, M ≈ 1–3 days, L ≈ a week+.
Every item is a self-contained ticket.

### 1. Reuse the preview scrape for Add-to-Library — S
**Change:** pass the `ThreadPreview` payload (or a server-side 60 s cache keyed
by thread URL) into `AddGameFromF95Zone` so the add does not re-fetch the thread.
**Files:** `desktop/app.go:2384-2435` (`GetThreadPreview`), `:2441-2575`
(`AddGameFromF95Zone`), `desktop/frontend/src/lib/F95Browser.svelte:391-452`.
**Benefit:** −1 paced thread GET per add (≈2.25 s + block exposure); requests
saved.
**Risk:** stale data if the user waits between preview and add — bound it with
the cache TTL, and re-scrape on Download.

### 2. `moxie sync <id>`: stop scraping the same thread twice — S
**Change:** when phase 1 has just scraped the thread, compare its `Version`
against the stored version in place instead of falling through to the phase-2
scrape at `:186`. Do not paper over it by setting `VersionCheckedAt` after
association — that would also delay the game's first real update check (§2.5).
**Files:** `internal/commands/sync_game.go:115-186`.
**Benefit:** −1 thread GET per single-game sync; halved block surface.
**Risk:** none material; keep `--force` re-scraping (users expect it).

### 3. `FetchCovers`: use `scraped_meta.cover_url` before going to the network — S
**Change:** in `fetchCoversRun`, call `db.GetScrapedMeta(game.ID)` and seed
`jobs[i].cover` when a URL exists; only call `resolveCoverURL` when it does not
(or when the download fails and the URL is older than some age).
**Files:** `desktop/app.go:2696-2745`, `:2844-2880`;
`internal/db/scraped_meta.go:97`.
**Benefit:** −1 metadata request per cover-less-but-known game (can be hundreds
on a fresh cache dir); also makes the function's own comment true
(`app.go:2673-2676`).
**Risk:** a stale URL 404s — the existing `.url` marker + refetch-on-change
already handles it.

### 4. Persist download-link size everywhere links are written — S
**Change:** copy `dl.Size` into `db.DownloadLink{Size: …}` in the three writers
that drop it; the column, the model field and the downloader's expected-total
logic already exist.
**Files:** `internal/commands/scrape.go:123-129`, `desktop/app.go:2555-2562`,
`desktop/app.go:4934-4941` (and the name-refresh branch at `:4921-4931`).
**Benefit:** data gained (per-mirror size in CLI/TUI/desktop, better ranking,
progress totals); no extra requests.
**Risk:** low; the parser's `parseSize` already returns 0 when unsure
(`parser_links.go:50-64`).

### 5. Desktop search: one `latest_data.php` call instead of three requests — M
**Change:** call `SearchTitle` (or a new `SearchTitleWithFilters`) first; map
rows straight into `F95SearchResult` (title/version/cover/prefix/tags/stats) and
only fall back to the XenForo POST + Google path when it returns zero rows.
**Files:** `desktop/app.go:2213-2299`; optionally `internal/scraper/latestapi.go`
to expose version/rating in the DTO.
**Benefit:** −2 requests per search (`GET /` token + POST), removes the CSRF
and `.contentRow` fragility from the hot path, 15 rows instead of 5, and the
search becomes usable without a session (matching CLI bulk behaviour).
**Risk:** different ranking (likes-sorted latest-index vs XenForo relevance) —
keep POST as a fallback and gate the switch behind a config key if needed.

### 6. Stop counting retries as separate blocks; don't trip on a lone 429 — S
**Change:** increment `consecutiveBlocks` once per *request* (after the attempt
loop), and skip the breaker trip entirely when the error carried `Retry-After`.
**Files:** `internal/scraper/scraper.go:304-314`, `internal/scraper/pacer.go:193-219`.
**Benefit:** reliability/UX — one rate-limit response can no longer abort an
entire desktop sync for 10 minutes.
**Risk:** none; the breaker still trips on three distinct blocked requests.

### 7. Dedupe and prune the metadata-refresh pass — S
**Change:** (a) dedupe thread IDs in both `/fast` call sites; (b) skip
`CacheFullThread` for threads already fetched in this run (track a
`map[int64]CacheThread`, or use `CacheThread.LastUpdated` against
`prevChecked`); (c) at minimum drop the pass-2 threads from `entries`.
**Files:** `internal/commands/sync_check.go:203-244`, `:329-388`;
`desktop/app.go:4707-4755`.
**Benefit:** removes one duplicate cache-API request per newly-changed untracked
thread per run; fewer `/fast` chunks.
**Risk:** low — a missed refresh only delays status freshness by one run.

### 8. Give the two frontends one association scorer — S
**Change:** move the CLI's `VersionMatchBonus` behind the same `base >= 0.3`
guard the desktop uses, ideally by extracting one shared
`PickBestLatestResult` helper in `internal/scraper`.
**Files:** `internal/commands/sync.go:522-583`,
`desktop/app.go:4595-4639`.
**Benefit:** removes a correctness bug (a 0.25 sequel match becomes 0.55 and is
auto-accepted on the CLI) and future drift.
**Risk:** slightly lower recall on genuinely versioned sequels; the desktop
comment at `app.go:4603-4608` documents why this is the right trade.

### 9. Skip the impossible cookie-based fallback on cookie-free runs — S
**Change:** when the resolved cookie is empty, do not call
`SearchF95ZoneWithContext` / `client.SearchF95Zone` after a failed
`latest_data.php` search; go straight to "no match" (or the Google path only
behind an explicit flag).
**Files:** `internal/commands/sync.go:483-505`,
`internal/commands/sync_game.go:59`, `desktop/app.go:4140-4162`.
**Benefit:** −2 requests per failed association in anonymous mode (this is the
documented primary path in `docs/scraper.md:111`), and no HTML scraping of
Google from a Go client.
**Risk:** loses the Google fallback for users with no session — who currently
cannot use XenForo search anyway.

### 10. Server-side Browse filters and a truthful result count — M/L
**Change:** add a query struct (`prefixes[]`, `noprefixes[]`, `tags[]`,
`notags[]`, `tagtype`, `date`, `creator`, `sort=title`) to `PublicAPI`, wire it
through `BrowseF95Zone` and the toolbar, and display `msg.count` as the
authoritative count. Remove or demote the "N of M loaded" note.
**Files:** `internal/scraper/latestapi.go:763-817`;
`desktop/app.go:2337-2381`; `desktop/frontend/src/lib/F95Browser.svelte:229-267`,
`:653-744`.
**Benefit:** correctness + UX: filters apply to the whole 27 K index, every page
is full, the headline count matches the grid; fewer page loads for filtered
browsing.
**Risk:** low — the filter/sort language was re-verified live and anonymously on
2026-11-07 (`research/f95-api-surface.md:147-195`: `prefixes[]`, `tags[]`,
`sort=title`, `creator` all return 200 with scoped counts). Fall back to
client-side filtering only when a response comes back unfiltered
(`msg.count` unchanged by a non-empty filter set).

### 11. Parse `screens[]`, `new`, `watched`, `ignored`; use `ts` — M
**Change:** extend `latestItem`/`LatestSearchResult`/`F95BrowseResult` and the
preview pane (gallery from `screens`, unread badge from `new`, "hide ignored"
toggle wired to the server's `ignored=hide`). Replace the dead `Ts` field with a
`LastUpdated time.Time` used for sorting/badges.
**Files:** `internal/scraper/latestapi.go:143-181`, `:731-754`;
`desktop/app.go:2128-2196`; `F95Browser.svelte` cards.
**Benefit:** data gained at zero request cost (a gallery without a thread
fetch), plus a cheaper alternative to `GetThreadPreview` clicks.
**Risk:** low; unknown fields must stay optional (rows already vary).

### 12. Share the `_xfToken` cache across desktop `Client`s — S
**Change:** make the token cache package-level, keyed by a hash of the cookie
string, instead of per-`Client`; invalidate on 400 as today.
**Files:** `internal/scraper/search.go:31`, `:140-186`.
**Benefit:** −1 `GET /` per desktop search while the POST path remains (still
valuable until item 5 lands; after item 5 it removes the last extra request on
the fallback).
**Risk:** token rotation across sessions — already handled by the 400
invalidate-and-retry path.

### 13. Fetch preview from the cache API, scrape only on download — M
**Change:** `GetThreadPreview` calls `CacheFullThread` first (unpaced,
cookie-free) and builds the preview from JSON; falls back to `ScrapeThread` when
the thread is missing from the cache or the user opens the Downloads section.
**Files:** `desktop/app.go:2384-2435`; reuse `syncSingleViaCache`'s mapping
(`app.go:4882-4903`).
**Benefit:** preview becomes 1 unpaced JSON request instead of a 2.25 s-paced,
Cloudflare-exposed HTML GET; tags/`/dl/` links degrade gracefully.
**Risk:** preview loses tags and per-link detail until the user asks for them;
keep a "Refresh from thread" action.

### 14. Make pacing/breaker/thresholds configurable, with safe floors — M
**Change:** read `f95_min_delay`, `f95_search_delay`, `f95_cooldown`,
`f95_breaker_cooldown`, `check_cooldown` from config with clamped minimums;
leave defaults unchanged.
**Files:** `internal/scraper/scraper.go:20-44`, `pacer.go:17`,
`internal/commands/config.go`.
**Benefit:** throughput can be tuned when F95 relaxes, and a user hitting a
shared IP can slow down without a rebuild; makes the nice test-only knobs
(`pacer.go:43-48`) usable in production.
**Risk:** a user setting 0 reproduces the `--unsafe` ban risk — clamp to
≥500 ms (thread) / ≥1 s (search).

### 15. Log partial parses instead of returning silently — S
**Change:** after `parseThreadHTML`, if no content selection / no tags / no
download links on a page that has a first post, attach a `ParseWarnings []string`
to `ThreadData` and log/emit it once per scrape. Also split `parseSize` loss
(see item 4) from real absence.
**Files:** `internal/scraper/parser.go:50-143`, `types.go:12-40`;
callers that persist (`sync_game.go`, `app.go:4658`).
**Benefit:** fragility becomes visible — today every selector break in §4
produces a "successful" scrape with empty fields, and the UI/DB present the
emptiness as fact.
**Risk:** log noise; cap at debug level for expected-absent fields.

### 16. Reuse the `CacheThread.LastUpdated` to avoid `/fast` — S
**Change:** keep the `last_updated` of threads fetched this run and skip the
`/fast` query for them; also gives a fallback freshness signal when `/fast`
is down.
**Files:** `internal/scraper/latestapi.go:262`, `:925-940`;
`sync_check.go:329-353`; `desktop/app.go:4707-4731`.
**Benefit:** −1 `/fast` request per ≤10 threads (unpaced, so latency saving is
small, but it removes a whole failure mode: a `/fast` error currently disables
metadata refresh for the run, `sync_check.go:337-340`).
**Risk:** none; `last_updated` is the same clock the `/fast` endpoint uses.

### 17. Persist `PublishedAt`/`UpdatedAt` and surface "last thread update" — M
**Change:** add two nullable columns to `scraped_meta` (schema migration),
persist the JSON-LD values (`parser.go:135-140`), and show/sort them.
**Files:** `internal/db/db.go:216-222`, `internal/db/models.go:128-134`,
`internal/scraper/parser.go`, `desktop/app.go`.
**Benefit:** data gained: a real recency signal for the library, independent of
`version_checked_at`; enables "stale in library" views.
**Risk:** JSON-LD is absent on some threads (documented) — nullable, no
inference.

### 18. Fix the cover-download User-Agent to the session UA — S
**Change:** use `scraper.UserAgent()` (and the same `applyBrowserHeaders`
discipline) for `attachments.f95zone.to` cover fetches, and route those through
the F95 pacer or at least reuse its 429 handling.
**Files:** `desktop/app.go:2976-2986`.
**Benefit:** removes an inferred systematic 403 risk on full-res covers; keeps
`cf_clearance` UA binding consistent with every other F95 request.
**Risk:** none; covers on non-F95 hosts keep the simple UA.

### 19. Delete the dead surface and correct the docs — S
**Change:** remove `Client.Preflight` (`scraper.go:660`), `ParseCookies`
(`cookies.go:15`), `EngineNameFromPrefixes`/`HasNonGamePrefix`
(`latestapi.go:346`, `:362`), `SearchCovers` (`latestapi.go:226`) and
`extractVersionFromMeta` (`parser_version.go:83`); fix the three false claims:
`docs/scraper.md:211` (parallel search), `docs/scraper.md:114` (CLI forced sync
refreshes links), `desktop/app.go:2673-2676` (stored cover URL is used).
**Files:** as listed.
**Benefit:** removes the false confidence that makes item 5 and item 1 look
"already handled"; less code to keep compiling against F95's parser.
**Risk:** none (verify with `go build ./... && go vet ./... && go test ./...`).

### 20. Add an RSS "what changed" poll and expose `cmd=tags` — M
**Change:** implement the two unused anonymous commands:
(a) `PublicAPI.LatestRSS(cat)` → `GET latest_data.php?cmd=rss&cat=games`, parse
`[NEW]`/`[UPDATE]`, `dc:creator`, `pubDate`, thread link; use it as a cheap
"anything new?" gate before a full update check, and as the source for a
"what's new" view. (b) `PublicAPI.LatestTags(filters)` → `cmd=tags`, returning
`[]TagFacet{ID,Count}` for the tag picker in item 10.
**Files:** `internal/scraper/latestapi.go` (new methods + a small XML struct);
consumers: `internal/commands/sync_check.go` (gate), `desktop/app.go:3829`
(feed), `F95Browser.svelte`.
**Benefit:** data gained: one request yields 30 changed threads with IDs and
creators, vs a bulk-version pass plus per-thread fetches to discover the same
set; counted tag facets enable the tag filter without guessing IDs. Both were
verified anonymous on 2026-11-07 (`research/f95-api-surface.md:178-210`).
**Risk:** RSS carries no version string reliably (it is in the title bracket),
so it is a *trigger*, never the version source; keep `checker.php` as the
authority. `gofeed`-free parsing: a 10-line `encoding/xml` struct is enough.

---

## 8. What is already fine (and should be left alone)

- Pacer slot reservation under the lock, cancellation returning its slot
  (`pacer.go:93-150`) — a real fix, well implemented.
- `Retry-After` parsing incl. HTTP-dates and the 2 min cap
  (`scraper.go:460-482`).
- Typed errors (`HTTPStatusError`, `BlockedError`, `SessionError`) and the
  Cloudflare-vs-plain-403 discrimination (`scraper.go:506-533`,
  `latestapi.go:1001-1014`).
- `BulkVersions` partial-result policy and `endpoint_gone`-only fallback
  (`latestapi.go:548-661`, `sync_check.go:246-265`) — matches the documented
  contract and is shared by CLI and desktop.
- Host-scoped cookie injection (`scraper.go:820-832`) — prevents the session
  cookie leaking to Google/redirect hops.
- `/dl/` native solver + `F95LinkStale` (90 min) + browser handoff
  (`hosts_f95dl.go:71-80`, `:147-260`) — correct and unusually well documented.
- Cover cache invalidation via the `.url` sidecar (`app.go:2928-2948`) — cheap
  and correct.
- Version normalisation (`StripVersionQualifier` → `version.Canonical`,
  `latestapi.go:442-444`) and the `Same/Older/Changed` queue policy
  (`updates.md`, `sync_check.go:281-285`).

---

## Appendix — verification notes

- Request counts are derived from the call graph, not from packet capture.
  Times use the constants at `scraper.go:20-44` with the pacer's jitter model
  (`pacer.go:104-120`); actual wall clock also includes TTFB.
- "Verified" items were read at the cited lines. The only **inferred** claims
  are: (a) the cover-UA/`cf_clearance` 403 risk (§6.3) and (b) the exact
  relevance difference between XenForo title search and `latest_data.php`
  search (§7.5). The anonymous filter/sort language is no longer inferred — it
  was re-probed live on 2026-11-07 (`f95-api-surface.md:147-195`), superseding
  the "not confirmed" caveat in `latest-data-api.md:180-183`.
- Doc corrections produced here (in addition to §7.19): `scraper.md:211`
  (search is pacer-serialised across workers), `scraper.md:114` (CLI forced
  sync does not refresh download links), `app.go:4037-4040` (per-worker clients
  do not pace per worker), `app.go:2673-2676` (cover URL is not read from the
  DB), `scraper.md:97`/`latest-data-api.md` remain accurate on the field set —
  the gap is on our side.
