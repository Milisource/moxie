# F95Zone `latest_data.php` — Reverse-Engineered Reference

> Verified **2026-10-08** against the live endpoint. Anonymous requests were used
> for `cmd=list` / `cmd=tags` / `cmd=rss`; the authenticated page and app JS were
> read with a real session extracted via moxie's own `browser.GetF95Cookies()`
> (kooky). Counts and IDs below are real live values.

F95Zone's "Latest Updates" browser, `/sam/latest_alpha`, is a custom app backed by
a small JSON/RSS API. moxie uses only a slice of it (`SearchTitle`, `ListLatest`).
This document records the **complete** surface found by probing it and by reading
the page's inline globals and `assets/js/latest.min.js`, and it plans the features
that surface unlocks.

Related: [`scraper.md`](scraper.md) (how the endpoints are used in sync),
[`f95zone-browser-design.md`](f95zone-browser-design.md) (UI patterns).

---

## Endpoint

```
GET https://f95zone.to/sam/latest_alpha/latest_data.php
```

Commands: `cmd=list`, `cmd=tags`, `cmd=rss`, `cmd=options` (POST). Requests are
`X-Requested-With: XMLHttpRequest` from the page, but that header is not required.

The response envelope is always `{"status":"ok","msg":…}` on success and
`{"status":"error","msg":"…"}` on failure — `msg` is an **object** on success and a
**string** on error (parsed leniently by `latestData`/`bulkErrorMsg`).

---

## `cmd=list` — the feed / search

### Parameters

| Param | Meaning | Notes |
|---|---|---|
| `cat` | Category | `games` (27562), `assets` (23652), `comics` (3765), `animations` (1795), `mods` (0). Unknown values → `"Missing category"`. |
| `page` | 1-based page | |
| `sort` | `date` \| `likes` \| `views` \| `title` \| `rating` | `title` = alphabetical "Name"; `rating` is labelled "Weighted Rating". Anything unknown falls back to `date`. |
| `rows` | Page size | Honored **only** at multiples of 15: `15/30/45/60/75/90`. Any other value (incl. `5`, `20`, `100`) silently returns **30**. Omitted when 30. |
| `search` | Full-text title search | Redis-backed; needs stopword stripping + ≤30 chars (`SanitizeSearchQuery`). |
| `creator` | Creator/title search | Separate from `search`; `creator=dik` → 12. |
| `prefixes[]` | Include by prefix ID (repeatable) | `prefixes[]=7` (RenPy) → 10359. |
| `noprefixes[]` | Exclude by prefix ID (repeatable) | `noprefixes[]=7` → 17203. |
| `tags[]` | Include by tag ID (repeatable) | `tags[]=2265` (ai cg) → 2793. |
| `notags[]` | Exclude by tag ID (repeatable) | `notags[]=2265` → 24769. |
| `tagtype` | `and` (default) \| `or` | OR between included tags. Two tags → 15284. |
| `date` | Updated within N **days** | UI slider: 1=Today, 3, 7, 14, 30, 90, 180, 365; `0`/`9999`=Anytime. `date=30`→1750, `7`→531, `1`→52. It is **not** a unix timestamp. |
| `ignored` | `hide` | Hide threads the session has ignored. |
| `_` | Cache-buster | moxie sends `time.Now().Unix()`. |

Combined filters multiply correctly: `prefixes[]=7&date=30` → 793.

### Row schema (`msg.data[]`)

The row carries **16 fields** — moxie currently models 11:

| Field | Type | Notes |
|---|---|---|
| `thread_id` | int | |
| `title` | string | |
| `creator` | string | |
| `version` | string \| number | Can be a bare number (`1.482`) **or** the literal `"Unknown"` (treat as empty). Parsed by `rawJSONString`. |
| `views` | int | |
| `likes` | int | |
| `prefixes` | int[] | F95Zone taxonomy — engine **and** category/status IDs (see below). |
| `tags` | int[] | F95Zone taxonomy tag IDs. |
| `rating` | number | `0` when unrated. Int when whole (`2`) else float (`4.14`). |
| `cover` | string | `preview.f95zone.to/...`; rewritten to `attachments.f95zone.to` for full-res. |
| `screens` | string[] | **Not parsed.** Gallery images (0–19+ shown), same `preview.f95zone.to` host → same full-res rewrite applies. |
| `date` | string | Relative human string, e.g. `"4 hrs"`. |
| `watched` | bool | **Not parsed.** Session-scoped. |
| `ignored` | bool | **Not parsed.** Session-scoped. |
| `new` | bool | **Not parsed.** "Updated since your last visit" — a free unread indicator. |
| `ts` | int | Unix update timestamp. |

### Envelope

```json
{ "status":"ok",
  "msg": { "data":[...],
           "pagination": { "page":1, "total":1838 },
           "count": 27562 } }
```

- `msg.pagination.total` is the total number of **pages** (so it changes with `rows`: 27562/15→1838, /30→919).
- `msg.count` is the total number of results.

---

## `cmd=tags` — counted tag facets

```
GET …/latest_data.php?cmd=tags&cat=games&<same filters as cmd=list>
```

Returns `{"status":"ok","msg":{"data":[{"tag_id":2265,"count":2793}, …],"count":N}}`.
`count` is the number of tag rows. It accepts **all** the `cmd=list` filter params, so
the counts are scoped to the filtered subset — e.g. `prefixes[]=7&cmd=tags` returns 113
tags with RenPy-specific counts (`2dcg`=3571, `3d game`=7, …). This is exactly the
site's tag-filter facet data. It is **cookie-free** but carries **no names** — resolve
names from the page taxonomy below.

---

## `cmd=rss` — recent-updates RSS

Per category (`cat=games|assets|animations|comics|mods`). Each `<item>`:

- `<title>` = `[NEW] Title [Version]` or `[UPDATE] Title [Version]`.
- `<description>` = CDATA holding the cover `<img src="https://preview.f95zone.to/…">`.
- `<dc:creator>`, `<author>`, `<link>`/`<guid>` (thread URL), `<pubDate>`.

---

## `cmd=options` (POST)

Persists per-user display options (tile size, ignored-thread mode, rows, hover delay,
…). Site-internal; not useful to moxie.

---

## Taxonomy: prefix IDs and tag names

The `prefixes`/`tags` numbers are F95Zone's **own** taxonomy, not F95Checker's
`Type`/`Tag` enums (which are valid only for the cache API's `type` field). The
authenticated `/sam/latest_alpha/` page publishes the whole thing as a JS global:

```js
latestUpdates = { "prefixes": { "<cat>": [ {id,name,prefixes:[{id,name,class}]} ] },
                  "tags":     { "<id>": "<name>", … },
                  "options":  … };
```

### Prefixes (per category, with group names)

**games**
- Engine: `1` QSP, `2` RPGM, `3` Unity, `4` HTML, `5` RAGS, `6` Java, `7` Ren'Py, `8` Flash, `12` ADRIFT, `14` Others, `17` Tads, `30` Wolf RPG, `31` Unreal Engine, `47` WebGL, `116` Godot
- Status: `18` Completed, `20` Onhold, `22` Abandoned
- Other: `13` VN, `19` Collection, `23` SiteRip

**comics** — Status (same 3) · Other: `43` Manga, `23` SiteRip, `19` Collection, `16` Comics, `49` CG, `44` Pinup

**animations** — Animation: `39` Video, `38` GIF, `37` Flash, `59` App · Other: `19` Collection

**assets** — Assets: `115` RPGM, `33` Daz, `35` VAM, `36` Illusion, `40` AutoDesk, `41` Poser, `42` Blender, `110` Unreal, `45` Tutorial, `71` Other, `114` Unity · Other: `19` Collection

moxie already decodes the engine subset in `latestEnginePrefixes`
(`EngineFromLatestPrefixes`); the **Status** and **Category** groups are the decoded
signals we don't yet surface.

### Tags

153 ID→name entries. Structural/notable examples:

```
1434=3d game   2214=2d game   1507=2dcg     107=3dcg      2265=ai cg
783=animated   2214=2d game   1111=kinetic novel  348=dating sim
522=text based 1525=point & click  2257=sandbox  776=side-scroller
1508=platformer 628=strategy  452=turn based combat  1079=shooter
448=simulator  449=management  1471=puzzle    895=virtual reality
1707=real porn 736=japanese game  2229=mobile game
```

The `asset-*` family (`2219`–`2278`: `asset-clothing`, `asset-character`,
`asset-hair`, `asset-daz-gen8`, …) only applies to the `assets` category. The full
153-entry map is embedded on the page; refresh it with the procedure below rather
than hard-coding.

---

## Auth, rate limits, caching

- `cmd=list` works **without** a session, but anonymous requests are hard-capped
  ("Anonymous users have a limited amount of requests per hour") — moxie passes the
  browser cookie whenever available (`NewPublicAPIWithCookie`), which lifts the cap.
- Whether the **filter params** work anonymously was not confirmed: the probe tripped
  the anonymous per-hour quota before the correct `prefixes[]` syntax could be tested
  without a cookie. Assume they may require a session.
- The **taxonomy with names** requires a session — both `/sam/latest_alpha/` (HTML)
  and the taxonomy inside it are behind the login wall. `cmd=tags` gives IDs + counts
  without names cookie-free.

### Refresh procedure (for the taxonomy)

```go
cookie, _ := browser.GetF95Cookies()          // kooky; internal/browser
// GET https://f95zone.to/sam/latest_alpha/ with the cookie + Firefox UA
// Extract the `latestUpdates = {…};` object, json.Unmarshal it.
```

`internal/scraper/latestapi.go` is the natural home for a small cached
`LatestTaxonomy{Prefixes map[string][]…; Tags map[int]string}` fetch that returns
the raw map when a session exists and an engine-only fallback otherwise.

---

## Planned integrations

These are the features the surface unlocks. Filed as bd issues; the design outlines
below are the intended shape.

### F95-oz99 — server-side filters + sorts

*Why:* the Browse Discover feed fetches unfiltered pages and filters **client-side**
(`F95Browser.svelte` → `passesFilters`, "N of M loaded · hidden by filters"). Counts are
misleading and matches require scrolling. The server filters exactly and reports the
real count.

*Shape:*
- Add a `latestQuery`/`ListLatestQuery(ctx, q)` to `PublicAPI` carrying
  `cat, page, sort, rows, search, creator, prefixes[], noprefixes[], tags[], notags[],
  tagtype, dateDays, hideIgnored`; `ListLatest` becomes a thin wrapper.
- Expose `sort=title` (add to `browseSorts`).
- Desktop: extend `BrowseF95Zone` and `buildBrowsePage`, add a
  `F95BrowseQuery` binding shape; the toolbar changes reload from page 1 and display
  `msg.count` as the authoritative result count instead of hiding loaded rows.
- UI: engine include/exclude (prefix IDs), a **Tag** include/exclude control (see
  F95-rpvh for facets), a **Date** limit, and a **Creator** field.

### F95-8jit — screenshot gallery + new/watched/ignored

*Why:* `screens[]` is a ready-made gallery that removes the need to scrape a thread
for the preview pane; `new`/`ignored`/`watched` are free session state.

*Shape:*
- Parse `screens`, `watched`, `ignored`, `new` into `latestItem`,
  `LatestSearchResult`, `F95BrowseResult`.
- Preview pane: render the gallery (rewrite `preview.f95zone.to` → `attachments.f95zone.to`,
  same helper as covers).
- Feed: `new` → unread badge; `ignored` + `ignored=hide` → a "Hide ignored" toggle;
  `watched` → optional filter/badge.

### F95-rpvh — tag facets + status/category badges

*Why:* `cmd=tags` gives counted facets; the page taxonomy gives names and the
Status/Category prefix groups.

*Shape:*
- `PublicAPI.LatestTags(ctx, q)` wrapping `cmd=tags`, returning `[]TagFacet{ID,Count}`.
- Cache the `latestUpdates.tags` name map (session-gated); a counted tag
  include/exclude control fed by `LatestTags` scoped to the active filters.
- Decode `prefixes[]` for Status (`18/20/22`) and Category (`13` VN) alongside the
  engine, and show badges on feed cards (reuse the Wide-card badge styling).

### Other unlocked features

- **Browse other categories** — `cat=comics|animations|assets` (and `cmd=rss` for
  each) turns Browse from games-only into the whole site.
- **Creator/title search** — `creator=` is a distinct, narrower search than
  `search=`, matching the site's own "CREATOR/TITLE" field.
- **Date-limit presets** — Today / 3 / 7 / 14 / 30 / 90 / 180 / 365 days.
- **Rows tuning** — expose 15/30/45/60/75/90.
- **`[NEW]`/`[UPDATE]` from RSS** — an alternate, lightweight update stream.

---

## Documentation corrections this research produced

- `scraper.md` field lists omitted `creator`, `screens`, `watched`, `ignored`, `new`
  and the `title` sort; the `rows` behaviour ("below ~20 still returns 20") was wrong
  (multiples of 15, else 30); the RSS row was understated; the "Data sources
  considered and rejected" paragraph claimed moxie already did everything the
  reference project does.
- `f95zone-browser-design.md` claimed the feed's `prefixes`/`tags` are "opaque IDs"
  and that Filters are "client-side only" — both corrected.
- `latestapi.go` comments on `browseRows`, `browseSorts` and `latestItem` updated to
  match.
