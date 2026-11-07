# How other tools consume F95Zone — prior art

> Compiled **2026-11-07** from source reading (not READMEs) of the public
> tools listed below. Companion to
> [`f95-api-surface.md`](f95-api-surface.md) (live probe) and
> [`f95-api-our-usage.md`](f95-api-our-usage.md) (moxie audit).
>
> Every claim is tagged **read** (source inspected) or **unconfirmed**.

---

## 1. F95Checker — the reference implementation

**`github.com/WillyJL/F95Checker`** · Python · ~248★ · last commit 2026-11-01.

### 1.1 It no longer scrapes per-user

Since v11 the client talks to **`https://api.f95checker.dev`**, a cache API run
by the author (env override `F95INDEXER_URL`). The README states the reason
directly: F95Zone *"does have an API but it does not provide a few datapoints
needed … the only way is requesting full threads … puts unnecessary stress on
the forum servers."* **read**

Implication for moxie: `api.f95checker.dev` is one person's infrastructure, not
a public service. Treat it as best-effort only; do not build a dependency on it.

### 1.2 The indexer (server side) — what it actually calls

Single `xf_user` cookie from an environment variable (`indexer/f95zone.py: lifespan()`).
Cadence (`indexer/cache.py: CACHE_TTL/SHORT_TTL`, `indexer/watcher.py`):

| Job | Interval |
|---|---|
| feed poll | 5 min |
| bulk version poll | 12 h |
| untracked-thread refresh | 2 days |
| other changes | 7 days |
| missing threads retry | 14 days |

Endpoints, confirmed in source:

- **Feed** — `GET /sam/latest_alpha/latest_data.php?cmd=list&cat={cat}&page={n}&sort={sort}&rows={rows}&_={ts}`
  (`indexer/f95zone.py: LATEST_UPDATES_URL`). `cat` ∈ games/comics/animations/assets
  (`mods` commented *"Doesn't seem to work"*), `sort=date`, `rows=90`, 4 pages, every 5 min
  (`indexer/watcher.py: poll_updates`).
- **Search** — same endpoint, `&{search|creator}={query}&sort=likes&rows=90`
  (`indexer/scraper.py`, `modules/api.py: latest_updates_search`). Query is
  sanitized: ASCII-only, strips ``?&/':;-.+!~(),*``, strips Redis stopwords,
  **truncated to 30 chars** (`latest_updates_search_sanitize_query`).
  moxie's `SanitizeSearchQuery` matches this behaviour.
- **Bulk version check** — `GET /sam/checker.php?threads={csv}`,
  **up to 1000 IDs per request, unauthenticated**
  (`indexer/watcher.py: poll_versions`, `WATCH_VERSIONS_CHUNK_SIZE = 1000`).
  Returns `{"msg": {"<id>": "<version>"}}`. Must tolerate `"Unknown"` and
  `{"msg": "Missing threads data" | "Thread not found"}`.
- **Thread metadata** — `GET /threads/{id}/` (full HTML).
- **Reviews** — `GET /threads/{id}/reviews/`, skipped when `votes == 0`
  (some threads 302 away when reviews are disabled).
- **Donor DDL list** — `POST /sam/ddd.php?raw=1` with `thread_id`. Errors:
  `"Not logged in"`, `"Not a donor"`.
- **Watched / bookmarks** (import only, HTML scraped via `f95wg-row`,
  `structItem-title[uix-href]`, `contentRow-title`):
  `/watched/threads?tab=new&src=watched&hide_seen=0&page=N`,
  `?tab=games&unread=0`, `?tab=other&unread=0`, `?tab=new&src=bookmarked`.
- **Notifications** — `GET /direct-messages/popup?_xfResponseType=json`
  (reads `visitor.total_unread`), `/account/alerts/`, `/direct-messages/`.
- **Login probe** — `GET /sam/latest_alpha/`, looking for
  `Sorry, you have to be <a href="/login">logged in</a>`.

### 1.3 Auth and Cloudflare

Embedded **webview** cookie capture (`webview.start("cookies", …)`): wait for
`xf_user`, persist the whole cookie jar. Cloudflare challenge is solved by the
user in the same webview, then cookies are re-captured and the request retried
(`request()` / `cloudflare_captcha()`). The forum User-Agent is overridden with
the webview's real UA. **read**

This is the pattern moxie already approximates with `browser.GetF95Cookies()`
(kooky extraction). The difference: F95Checker re-captures on challenge
automatically rather than surfacing a hard error.

### 1.4 Update detection — two-tier, not a version string alone

1. `GET /fast?ids=…` (≤10) → `{id: last_changed_ts}`;
2. `GET /full/{id}?ts={last_changed}` → cached record (or `INDEX_ERROR`).

Server-side, a thread is considered changed when **either** the `version`
differs **or** an MD5 *meta hash* differs. The hash covers
`(title, creator, prefixes, tags, round(rating,1), cover, screens, ts)`
(`indexer/watcher.py`). This catches metadata edits that never bump the version —
something moxie currently misses.

### 1.5 Rate limiting

`aiolimiter`: 1 req/s to f95zone, 2 req/s to attachments; +5 s escalating
sleep, up to 10 retries on 429/DDoS-Guard. `asyncio.Semaphore(1)` for fast
checks; `max_connections` (≤10) for full checks. **read**

### 1.6 Parser (`common/parser.py: thread()`)

BeautifulSoup + lxml.

- name from `.p-title-value` minus prefix/label
- `version` / `mod version` / `game version` via regex over the OP plain text,
  falling back to the `[…]` in `<title>`
- developer from an alias list plus a large chop table
  (`developer_remove_patterns` strips patreon/steam/discord/etc.)
- type from prefix spans; status from Completed/Onhold/Abandoned
- `last_updated` from "thread updated"/"updated"/"release date" text, else
  `time[data-timestamp]` on `.message-lastEdit`
- score/votes from **JSON-LD `aggregateRating`**, fallback `reviews (N)` count
- tags from `a[href^="/tags/"]` inside `.js-tagList`
- cover from first `div[data-src]` in `.bbWrapper`
- screenshots from `a.js-lbImage[href]` on `attachments.f95zone.to`
- downloads by walking siblings of a "Downloads" text node, grouping mirror links

Two details worth copying:

- `preview.f95zone.to` → `attachments.f95zone.to`, and strip the signed `?h=`
  query param (`parser.attachment`).
- Non-F95 mirror links are returned as **XPath expressions** rather than raw
  URLs *"to prevent abuse"*. A deliberate anti-scraping measure; also a hint that
  the project treats link redistribution as the sensitive part.

---

## 2. Other tooling

| Repo | Lang | ★ | Last push | What it does | Notable technique |
|---|---|---|---|---|---|
| `MillenniumEarl/F95API` | TypeScript | 43 | 2026-10-13 | The only real "API wrapper" | `latest_data.php` **and** `POST /search/search` with full XF search params; JSON-LD metadata; DDoS-Guard bypass |
| `kirin-3/wLib` | Python | 18 | 2026-11-07 | Catalog server + checker | `checker.php` in chunks of 100; sweeps all ~311 feed pages with a `hwm_ts` high-water mark |
| `Official-Husko/f95-notify` | Go | 4 | 2026-10-13 | Unread alert notifier | `GET /account/unread-alert?_xfResponseType=json` every 5 min |
| `zvxfdev/playnite-f95zone` | C# | 23 | 2024-04 | Playnite library plugin | GET search (`/search/{unix}/?q=…`), OG-image cover, version compare |
| `farvend/F95-Manager` | Rust | 12 | 2026-11-05 | Local manager | Full filter matrix (`tags[]`/`notags[]`/`prefixes[]`/`noprefixes[]`, `date`) |
| `erri120/Playnite.Extensions` | C# | 88 | archived 2022 | Historical only | — |
| `Cat-Ling/f95zone-skipper` | JS userscript | — | — | Skips masked-URL redirects | **unconfirmed** (not read) |

### 2.1 F95API — details worth taking

- `LatestSearchQuery`: `cmd=list`, `ignored=hide`, `cat`, `tags[]`/`notags[]`/
  `prefixes[]`, `search`, `creator`, `date`, `sort`, `page`, `rows` ∈
  15/30/45/60/75/90.
- `ThreadSearchQuery` (`POST /search/search/`): `_xfToken`, `c[title_only]=1`,
  `keywords`, `search_type=post`, `c[newer_than]`/`c[older_than]` (`YYYY-MM-DD`),
  `c[tags]` comma-joined, `c[min_reply_count]`, `c[prefixes][i]`,
  `c[child_nodes]=1`, `c[nodes][0]` = games 2 / mods 41 / comics 40 /
  animations 94 / assets 95, `order`, `page`.
- Session handling: credential POST → 2FA at `/login/two-step` carrying
  `_xfRequestUri`/`_xfToken`; then `updateSession()` refetches `/login` to
  re-read `input[name="_xfToken"]`, because **an `_xfToken` not synced with the
  `xf_csrf` cookie makes every POST return 400** (documented in `COOKIES.md`).
  moxie's `xfTokenTTL = 10 * time.Minute` in `internal/scraper/search.go`
  is a coarser version of the same idea.
- `ddos-guard-bypass.ts` implements the `check.ddos-guard.net/check.js` →
  `/.well-known/ddos-guard/id/{id}` `__ddg*` cookie dance.
- **Anti-pattern:** the whole cookie jar + token are saved as plain JSON in the
  temp folder.

### 2.2 wLib — details worth taking

- Uses `checker.php` in **chunks of 100** (vs F95Checker's 1000); the server
  ceiling is undocumented.
- Sweeps all ~311 `rows=90` pages to build a public catalog, tracking a
  `hwm_ts` high-water mark. Treats an HTTP **200 carrying
  `"limited amount of requests"`** as rate-limited — a status-code-only check
  would misparse it (same trap as the 200 login wall, §1 of the surface report).
- Explicit anti-fan-out policy: if `checker.php` fails for a batch it does
  **not** fall back to 100 page fetches — *"would only hammer F95 harder"*.
- Detects guest-hidden content via `messageHide--attach` / `messageHide--link`
  inside `article.message-threadStarterPost`, then re-fetches through a browser
  extension when signed in.
- Normalises checker versions by feeding them back through the title parser
  (`_ [{raw}] [_]`) — useful because `checker.php` returns bare strings whose
  shape is inconsistent.
- Screenshots from `lbContainer-zoomer[data-src]`, `img.bbImage[data-src|src]`,
  and `a[href*="attachments.f95zone.to"]` wrapping a `bbImage`; capped at 20;
  cover excluded.

### 2.3 Playnite plugin — one useful trick

Search via **GET** `https://f95zone.to/search/{unix}/?q={term}&t=post&c[child_nodes]=1&c[nodes][0]=2&o=relevance&g=1`
— avoids the `_xfToken` POST dance entirely. **unconfirmed** whether it still
works; the repo has not been updated since 2024-04.

### 2.4 f95-notify — one useful trick

`GET /account/unread-alert?_xfResponseType=json` with `Cookie` + `_xfToken`
headers, reading `visitor.total_unread`. Cheapest possible "anything new?"
signal for a logged-in session.

---

## 3. `latest_data.php` in the wild

F95Checker, F95API, wLib and F95-Manager all use **`cmd=list` only**.

**No public tool uses `cmd=tags` or `cmd=rss`.** moxie's planned tag-facet and
RSS work is not duplicating anyone; it is genuinely unexploited surface.
**read** (absence across the four codebases above)

---

## 4. XenForo REST API

General XF2 capability, not F95Zone-specific:

- `GET /api/` responds with the framework error envelope when no key is sent.
- Auth is the `XF-Api-Key` request header; keys are issued by a super-admin
  from the ACP. Optional per-key scopes (`api:read`, `api:write`).
- If enabled, `/api/threads/`, `/api/threads/{id}/`, `/api/posts/`,
  `/api/search/`, `/api/watched/threads` return typed JSON.

**F95Zone status:** the endpoint is **enabled but key-gated** —
`GET https://f95zone.to/api/` returns
`400 {"errors":[{"code":"no_api_key_in_request", …}]}`
(see `f95-api-surface.md` §2.1). There is no public key issuance path, and
**no tool in the ecosystem uses `XF-Api-Key`**. Treat as unusable; the value is
that a user-issued key would immediately unlock typed JSON.

---

## 5. Capability comparison

moxie cells reflect the current implementation audit
(`f95-api-our-usage.md`).

| Capability | F95Checker | Other tools | moxie today |
|---|---|---|---|
| Find new updates | `cmd=list`, 4 cats, `rows=90`, 5 min; version **or** meta-hash compare | F95API `cmd=list`; wLib full sweep + `hwm_ts`; F95-Manager `cmd=list` | `cmd=list` games only; version compare only |
| Fetch metadata | thread HTML + JSON-LD + OP parse | F95API JSON-LD first; Playnite OG-image | thread HTML + JSON-LD (`ld["image"]`, probably dead) + regex |
| Fetch version (bulk) | **`checker.php`, 1000 IDs/req** | wLib `checker.php`, 100 IDs/req | **already adopted**: `BulkVersions` → cache API → capped (25) direct-scrape fallback, on both CLI and desktop |
| Fetch cover | `div[data-src]` in OP; preview→attachments rewrite | Playnite OG image | `cmd=list` `cover` + preview→attachments rewrite |
| Fetch screenshots | `a.js-lbImage[href]` | wLib 3 selectors, cap 20 | `screens[]` from `cmd=list` is available but **not parsed** |
| Fetch download links | OP sibling walk; non-F95 as XPath | `.bbWrapper > a` | OP anchor scrape (`parser_links.go`), size parsed but dropped |
| Search | `cmd=list` `search`/`creator`, sanitized ≤30 chars, `sort=likes` | F95API `POST /search/search` full param set | CLI: XenForo POST + Google fallback; desktop: GET `/` token + POST + a third `cmd=list` |
| Watched threads | HTML scrape, import only | not supported | not supported |
| Auth | webview cookie capture + auto-retry on captcha | F95API credential+2FA POST; Playnite manual cookie | kooky extraction + clearance refresher |
| Cloudflare | user solves in webview, auto-retry | F95API DDoS-Guard cookie dance | `BlockedError` + 10-min process-wide breaker |
| Tag facets (`cmd=tags`) | no | **no tool** | planned, unused |
| RSS (`cmd=rss`) | no | **no tool** | unused |

---

## 6. Lessons for moxie

**Adopt**

1. **`GET /sam/checker.php?threads=<csv>`** — 1000 IDs per unauthenticated
   request (F95Checker; wLib independently uses 100). **Already adopted** in
   moxie (`BulkVersions`, `sync_check.go`, `desktop/app.go:4344`); the remaining
   questions are the true ID ceiling and whether archived/delisted threads are
   reported.
2. **Two-tier change detection: cheap poll → conditional full fetch.** A stored
   `last_changed` timestamp plus a conditional re-fetch is strictly cheaper than
   re-scraping; the meta-hash comparison catches metadata edits that never bump
   the version.
3. **JSON-LD as the primary metadata contract**, with HTML selectors only for
   tags/prefixes/OP fields and a title-bracket fallback for version. Both
   F95Checker and F95API converge on this.
4. **Sanitize search queries exactly as the site does**: ASCII, strip the
   punctuation set, strip stopwords, truncate to 30 chars. Already implemented —
   keep it as a hard invariant.
5. **Treat HTTP 200 as untrusted** — the login wall is 200/87 bytes and the
   rate-limit message is 200 with a JSON error body. Validate *content*, not
   status.
6. **Strip signed `?h=` params and rewrite `preview.` → `attachments.`** on every
   cover/screenshot URL, in one shared helper.

**Reject / avoid**

7. **Do not depend on `api.f95checker.dev`.** One operator's infra, no SLA.
8. **Do not fall back from a failed bulk check to per-thread fan-out.** wLib's
   reasoning is correct: it multiplies load on the exact endpoint that is
   already refusing.
9. **Do not store cookies/tokens in plaintext JSON** (F95API, f95-notify both
   do). moxie already delegates to the browser's cookie store — keep it that way.
10. **Do not key the uploader's link policy on raw URLs** the way F95Checker
    emits XPath to "prevent abuse" — but do note that link handling is the part
    the ecosystem treats as sensitive, and keep it user-initiated.

**Open questions**

- `checker.php` rate limits and true ID ceiling (undocumented).
- Whether `checker.php` returns versions for threads the site considers
  archived/delisted.
- Whether the Playnite GET-search still works in 2026 (would remove the CSRF
  dependency from search entirely).
