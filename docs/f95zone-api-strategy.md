# F95Zone API strategy — update, browse, scan

> Synthesis of three deep dives, 2026-11-07:
> [`research/f95-api-surface.md`](research/f95-api-surface.md) (live probe, 27 endpoints),
> [`research/f95-api-our-usage.md`](research/f95-api-our-usage.md) (call-site audit, 667 lines),
> [`research/f95-api-prior-art.md`](research/f95-api-prior-art.md) (F95Checker, F95API, wLib, Playnite).
>
> Scope: CLI (`moxie sync|update|browse|scrape|download`) and desktop (Wails bindings + `F95Browser.svelte`).

---

## 1. Three findings that change the plan

1. **`latest_data.php` filters are fully anonymous.** `prefixes[]`, `tags[]`,
   `notags[]`, `date`, `creator`, `sort=title`, `rows` all work with no cookie,
   no CSRF, no `Set-Cookie` — verified live (`prefixes[]=7&date=30` → 891,
   `tags[]=2265` → 3101). The planned server-side Browse filtering is unblocked
   and needs no session.
2. **Update checking is already solved; Browse and metadata are not.**
   moxie already runs the three-tier path F95Checker pioneered
   (`checker.php` bulk → F95Checker cache API → capped direct scrape) on **both**
   surfaces. The remaining waste and fragility is concentrated in *browse,
   search, preview and single-game sync* — which still pay for XenForo HTML and
   duplicate fetches.
3. **HTTP 200 is untrustworthy on this site.** `/sam/latest_alpha/` returns
   **200** with an 87-byte login wall; the anonymous quota returns 200 with a
   JSON error body. Any "did it work" check based on status code silently
   misparses. Validate content shape, not status.

---

## 2. Where moxie actually stands

| Flow | Requests today | Verdict |
|---|---|---|
| `moxie sync` bulk, associated games | ⌈N/100⌉ checker + 1 cache per untracked + ⌈changed/10⌉ `/fast` | **good**, small dupes (§4.3) |
| `moxie sync <id>`, already associated | 1 thread GET | fine |
| `moxie sync <id>`, unassociated | **4** — token + search + 2× the same thread GET | **2 wasted** |
| Desktop search | **3** — `GET /` token, `POST /search/search`, then a `cmd=list` whose title/version/URL are discarded | **1 request would do** |
| Desktop Discover page | 1 JSON + ~30 unpaced image GETs | acceptable, image fan-out uncapped |
| Desktop preview → Add | 2× the same thread GET | **1 wasted** |
| Desktop `FetchCovers` | 1 metadata fetch per cover-less game, even when `scraped_meta.cover_url` exists | **N wasted** |
| `moxie sync` metadata refresh | `CacheFullThread` twice for just-changed untracked threads | **1 wasted/thread** |

Pacing reality (verified constants): thread reads **2.25 s mean**, searches
**3.75 s mean**, every 35th slot +10 s ⇒ **~24 thread reads/min**,
**~15 searches/min**. So each removed thread GET is ~2.25 s of wall clock plus
one unit of block exposure. The desktop search above is ≥11.3 s wall clock.

**Single biggest structural problem:** there is no *thread-metadata resolver*.
Three sources (thread HTML, cache API, `cmd=list` rows) overlap, and ~14 call
sites each decide independently which to use and which fields to keep. That is
the root cause of every duplication in the table, and of the CLI/desktop
divergence (search strategy, `--force` semantics, title handling, block blast
radius, association scoring).

---

## 3. Target architecture (one change that retires six tickets)

```
resolveThread(ctx, id/url, opts{fields, maxAge}) -> Metadata + Provenance
   1. cache API /full/{id}      (unpaced, cookie-free)   — name, version, dev,
                                                            status, desc, image, downloads
   2. checker.php bulk          (batched, cookie-free)   — version only, N/100
   3. latest_data.php row       (anonymous)              — version, prefix/tags,
                                                            cover, screens, ts, new
   4. thread HTML               (paced, last resort)     — tags, /dl/ links, OP detail
```

- **Field-set negotiation**: ask for what the caller needs, not "everything".
  A preview needs no `/dl/` links; a download needs no description.
- **Short-TTL URL cache** (60 s) shared across bindings, so
  preview → add → download reuses one fetch.
- **Provenance per field** (`source`, `fetched_at`) so a stale cover or an
  untracked thread degrades visibly instead of silently.
- Everything else in §4 is a caller-side change once this exists.

---

## 4. Phased plan

### Phase 0 — free wins (S each, no new endpoints)

**Status: all nine implemented 2026-11-07** (F95-7wi3.1–.9). Phase 1 is next.

| # | Change | Surface | Benefit |
|---|---|---|---|
| 0.1 | Reuse the preview payload on Add-to-Library (or 60 s URL cache) | desktop | −1 paced GET/add |
| 0.2 | Stop the double scrape in `moxie sync <id>` (compare version in place after association; keep `--force`) | CLI | −1 GET/sync |
| 0.3 | `FetchCovers`: seed from `scraped_meta.cover_url` before the network | desktop | −1 metadata fetch per cover-less game |
| 0.4 | Persist `DownloadLink.Size` in all three writers (column exists; parser returns it) | both | data gained, 0 requests |
| 0.5 | Breaker counts *requests*, not retry attempts; don't trip on a lone `429` with `Retry-After` | both | one rate-limit no longer aborts a 10-min desktop sync |
| 0.6 | Dedupe `/fast` ids; skip `CacheFullThread` for threads already fetched this run | both | −1–2 requests/run |
| 0.7 | One shared association scorer (CLI currently lets a 0.25 sequel match become 0.55 and auto-accepts) | both | correctness + no drift |
| 0.8 | Skip the impossible cookie-based search fallback when the cookie is empty | CLI + desktop | −2 requests per failed association |
| 0.9 | Share the `_xfToken` cache across desktop `Client`s, keyed by cookie hash | desktop | −1 `GET /` per search |

### Phase 1 — collapse browse/search onto JSON (M)

| # | Change | Surface | Benefit |
|---|---|---|---|
| 1.1 | Desktop search: `cmd=list&search=` first; fall back to XenForo POST + Google only on zero rows | desktop | 3 → 1 request, 11.3 s → ~3.8 s, 15 rows vs 5, no CSRF, works anonymous; deletes the `.contentRow` fragility from the hot path |
| 1.2 | `ListLatestQuery` with the full filter struct (`prefixes[]`, `noprefixes[]`, `tags[]`, `notags[]`, `tagtype`, `date`, `creator`, `sort=title`, `rows`); `ListLatest` becomes a wrapper | both | filters apply to the 27 K index, every page full, truthful `msg.count` |
| 1.3 | Browse toolbar: engine include/exclude, tag include/exclude, date preset, creator field; display server count instead of "N of M loaded" | desktop | removes client-side filtering lie |
| 1.4 | CLI `moxie browse --prefix|--tag|--date|--creator|--sort` | CLI | parity with desktop, scriptable |
| 1.5 | Preview from cache API first, thread HTML only for `/dl/` links and tags; keep "Refresh from thread" | desktop | 2.25 s paced → unpaced JSON |

### Phase 2 — new signal at zero request cost (M)

| # | Change | Surface | Benefit |
|---|---|---|---|
| 2.1 | Parse `screens[]`, `new`, `watched`, `ignored`, and `ts`→`LastUpdated` | both | gallery without a thread fetch; unread badge; "hide ignored" wired to `ignored=hide` |
| 2.2 | `cmd=tags` facets (`LatestTags`) + `/tags/` name map (anonymous) | both | counted, scoped tag picker in desktop; `moxie browse --list-tags --prefix renpy` in CLI; readable chips without login |
| 2.3 | `cmd=rss` (30 items, inherits filters) as a cheap change pre-pass | both | `update --watch` / app-start "3 of your games updated" from one request |
| 2.4 | `/forums/games.2/index.rss` for brand-new threads (title + creator + OP excerpt) | CLI `scrape --new`; desktop "New threads" tab | true chronology feed, distinct from update feed |
| 2.5 | Decode Status (`18/20/22`) and Category (`13`) prefixes as badges | both | free metadata already in every row |
| 2.6 | `to_html`-style stale guards: meta comparison (title/creator/prefixes/tags/rating/cover/screens/ts) in addition to version, as F95Checker does | both | catches metadata edits that never bump a version |

### Phase 3 — observability and tunability (S/M)

| # | Change | Why |
|---|---|---|
| 3.1 | `ParseWarnings[]` on `ThreadData` when a selector returns nothing on a page that has an OP | fragility becomes visible; today a theme change returns empty data silently |
| 3.2 | Config keys for `f95_min_delay`, search delay, cooldown, breaker cooldown, with clamped floors (≥500 ms thread / ≥1 s search) | shared-IP users can slow down without a rebuild |
| 3.3 | Count requests per operation in logs (`requests`, `bytes`, `blocked`) | makes the wins in this doc measurable |

---

## 5. Explicitly rejected

- **Depending on `api.f95checker.dev` as a primary source.** Keep it as the
  best-effort tier-2 it already is; it is one operator's infrastructure.
- **XenForo `/api/`.** Enabled but key-gated (`400 no_api_key_in_request`), no
  issuance path, no tool in the ecosystem uses it. Support a user-supplied
  `XF-Api-Key` someday, plan nothing around it.
- **Falling back from a failed bulk check to per-thread fan-out.** wLib's
  reasoning is right and moxie already caps this at 25 — keep the cap.
- **Plaintext cookie jars** (F95API, f95-notify). The kooky/browser path stays.
- **Crawling `/whats-new/`, `/search/`, `/attachments/` in the background** —
  all are `robots.txt`-disallowed. See §6.

---

## 6. Compliance flag (needs a human decision)

`robots.txt` disallows `/search/`, `/attachments/`, `/posts/`, `/whats-new/`,
`/login/`, `/account/`, `/goto/`, `/forums/*?`, `/misc/*`, `/sam-item/`.

moxie currently uses **`/search/search`** (desktop search + CLI association) and
**`/attachments/`** (saves downloads, covers). Phase 1.1 removes the search
dependency from the desktop hot path and demotes it to a fallback for CLI
association. /attachments/ usage is user-initiated and single-file; still a
decision to record, not to silently keep.

---

## 7. Metrics that prove it worked

| Metric | Today | Target |
|---|---|---|
| Requests per desktop search | 3 | 1 |
| Requests per unassociated `moxie sync <id>` | 4 | 2 |
| Requests per preview→add | 2 | 1 |
| Requests per `moxie sync` run with changed untracked threads | +2/thread | +1/thread |
| Metadata fields surfaced from `cmd=list` | 11 of 16 | 16 of 16 |
| Stores for browse filtering | client-side | server-side with truthful count |
| Silent parse failures | invisible | `ParseWarnings` logged |

---

## 8. Open questions (need a logged-in probe)

1. `latestUpdates` taxonomy global — exact shape, still 153 tag names?
2. `cmd=options` POST token/payload (anonymous returns 500, session-gated).
3. `checker.php` true ID ceiling and rate-limit behaviour; archived threads.
4. `/watched/threads` item shape (enables "import my watch list" as a library seed).
5. Whether the Playnite **GET** search still works in 2026 — would remove the
   CSRF dependency from CLI association entirely.
6. Anonymous `latest_data.php` quota threshold (do not probe deliberately;
   capture if it happens).

A logged-in probe needs `xf_user` + `xf_session` + `xf_csrf` **and**
`cf_clearance` minted under the same UA — store them together or every request
403s.
