# Cover art

How Moxie gets, stores and serves cover images, and how it finds portrait box
art to replace F95Zone's landscape banners.

## Why

The library grid is 3:4. Most F95Zone covers are 16:9 banners, and many are
only ~560px wide (DLsite `_img_main`, `ss-1.jpg` screenshots). Cropping a
banner to 3:4 leaves a ~300px-wide slice that the card stretches 2–3×. The fix
has two halves:

1. **Sharper local thumbnails** (`desktop/covermeta.go`): see "Thumbnails" below.
2. **Real portrait art** (`internal/coverart`): Steam library capsules,
   SteamGridDB grids and VNDB covers are 600×900-class portrait images.

## On disk

Covers live in `~/.config/moxie/covers/`:

| File | What |
|------|------|
| `<id>` | The original image (any format; most F95 covers are AVIF) |
| `<id>.thumb` | Grid thumbnail, JPEG q85 (format version `t3`) |
| `<id>.large` | Detail-view variant, long edge ≤ 1400px, built on first request |
| `<id>.banner` | Retained landscape banner for the wide view, at original resolution |
| `<id>.wide` | Wide-view rendition, JPEG q85, ≤ 960px wide (built on first request) |
| `<id>.url` | URL the original came from |
| `<id>.meta.json` | `{w, h, tone, source, url, locked}` plus the banner (`banW`, `banH`, `banSource`, `banURL`) and rendition (`wideW`, `wideH`) fields |
| `<id>.prev`, `<id>.prev.meta.json` | The cover before the last upgrade/pick (one-step undo) |

`source` is `f95`, `steam`, `steamgriddb`, `vndb` or `manual`. A cover is
**pinned** when it's `locked` or its source isn't `f95`. F95 cover syncs
(`cacheCoverCtx`) never overwrite a pinned cover — but they still retain the
thread's landscape banner as `<id>.banner` (see below).

## Two versions per game

The library has two card shapes: the **grid/list** card is 3:4 portrait, the
**wide** card (F95Zone `/sam/latest_alpha` style) is landscape. Rather than
crop one image to both shapes, Moxie keeps up to two images per game:

- The **primary cover** (`<id>`, `.thumb`, `.large`) — portrait when real box
  art is available, otherwise the F95 banner. Used by the grid, list and
  detail views.
- The **landscape banner** (`<id>.banner`) — the F95 thread banner, or Steam's
  header/hero art, or a SteamGridDB hero. Used by the wide view.

The cover server renders the banner (or, when there is none, a 16:9 centre
crop of the primary) to `<id>.wide` on first request and serves it at
`/cover/<id>/wide`. A landscape primary is its own banner, so no separate
`.banner` is kept for it.

Banners are captured automatically:

- **F95 sync** retains the thread banner even when the primary cover is pinned,
  so an upgraded game still shows its real banner in the wide view. The banner
  URL is remembered, so it is only re-downloaded when it changes.
- **Replacing a cover** (desktop upgrade, manual pick, CLI `covers upgrade`)
  keeps the outgoing landscape image as the banner.
- **Upgrade to Portrait Art** also fetches a landscape banner from Steam /
  SteamGridDB for games that have none.
- The one-time **backfill** migrates the one-step-undo `.prev` of games
  upgraded before banners were kept, so existing installs get their wide art
  without a re-fetch.

## Thumbnails

`thumbGeometry(w, h)`:

- Wide sources (aspect > 1.6) are not cropped. They are scaled to ≤ 720px wide,
  and the card letterboxes them (`object-fit: contain`) over the average tone.
- Everything else gets a centre 3:4 crop, scaled down to fit 600×800.
- Nothing is ever upscaled.

The frontend's `coverFit()` (`lib/cover.js`) picks `contain` when the aspect is
above 1.6 or the short edge is under 240px. The cover server builds a missing
`.thumb` on demand, so a replaced cover never falls back to the full image in
the grid.

`wideGeometry(w, h)` renders the wide rendition (`.wide`). Landscape sources
(aspect ≥ 1.3) keep their whole frame, scaled to ≤ 960px wide; portrait or
square sources are centre-cropped to 16:9. Nothing is upscaled. The wide card
always `object-fit: cover`s the rendition, so it fills the 16:9 tile.

## Portrait sources (`internal/coverart`)

| Source | Key | Lookup | Image |
|--------|-----|--------|-------|
| Steam | none | `steam_app_id`, else `store.steampowered.com/api/storesearch` (exact title, type `app`) | `IStoreBrowseService/GetItems` → `library_capsule_2x` (600×900) or `library_capsule` (300×450) |
| SteamGridDB | API key | `/grids/steam/{appid}`, else `/search/autocomplete` (exact title) → `/grids/game/{id}` | `dimensions=600x900,660x930,342x482&types=static&nsfw=any&humor=false`, first 6 |
| VNDB | none (opt-in) | `POST api.vndb.org/kana/vn` `["search","=",title]` | `image.url` / `image.dims`, exact title against title/alttitle/titles |

- The Steam capsule filenames are hashed for newer apps, so the plain
  `library_600x900_2x.jpg` CDN path 404s. That's why the code asks `GetItems`
  for `asset_url_format` instead of guessing.
- A Steam AppID found by title is saved to `games.steam_app_id` (only when it
  was empty).
- **Matching is exact.** `NormalizeTitle` lower-cases the title, drops `[..]`
  and `(..)` groups and edition suffixes (Remastered, Definitive Edition, …),
  and keeps only letters and digits. A fuzzy match would put the wrong game's
  art on the shelf, and a wrong cover is worse than a blurry one.
- **Pacing:** Steam 1 req/s (burst 2), SteamGridDB 1 req/1.1s, VNDB 1 req/2s
  (VNDB allows 200 requests per 5 minutes).
- **Order:** `SortCandidates` puts portrait first, then Steam > SteamGridDB >
  VNDB, then the larger short edge.

### Landscape sources

`FindBanner` looks up the wide-view banner separately, so the extra requests
only run when a banner is actually wanted:

| Source | Image |
|--------|-------|
| Steam | `GetItems` → `library_hero` (1920×620), else `header` (460×215) |
| SteamGridDB | `/heroes/steam/{appid}` (or a game ID from autocomplete) |
| VNDB | none (covers only) |

`BestLandscape` ranks candidates by how close their aspect is to 16:9 (so an
ordinary banner beats an ultra-wide 3:1 hero that a 16:9 tile would crop hard),
then by pixel area, then by source preference.

### When a cover is replaced

`NeedsUpgrade(w, h, locked)`: the cover is not locked and is missing,
landscape, or has a short edge under 600px.

`Better(candidates, curW, curH)` picks the first **portrait** candidate that
meets one of these:

- The game has no cover.
- The current cover is portrait and the candidate's short edge is larger.
- The current cover is landscape and the candidate's short edge is at least
  `min(current short edge, 600)`.

## Settings

| Config key | Default | Meaning |
|------------|---------|---------|
| `cover-steam` | `true` | Use Steam |
| `cover-vndb` | `false` | Use VNDB |
| `steamgriddb-key` | — | SteamGridDB API key (also `STEAMGRIDDB_KEY` env, and the legacy key file the Steam commands use) |

The desktop app only ever sends the frontend the key's last 4 characters.

## Entry points

- **Desktop → Covers → Upgrade to Portrait Art** (`UpgradeCovers`). Shares
  the cover-fetch single-flight guard and emits `covers:upgrade-progress`,
  `covers:upgrade-complete {checked, replaced, failed, errors}` and
  `covers:error`.
- **Desktop → game detail → Choose cover…** (`FindCoverCandidates`,
  `SetGameCover`). Lists every source's candidates. A picked cover is locked.
  **Lock** (`SetCoverLocked`) pins the current cover, and **Undo**
  (`RevertCover`) restores `.prev` and locks it.
- **Desktop → Edit Game → Cover art** — the Edit Game dialog also edits the
  cover: paste an image URL, **Choose file…** (`PickCoverImage` +
  `PreviewCoverFile` for the preview, `SetGameCoverFromFile` to install) or
  **Search online…** (`FindCoverCandidates`). Changes are staged and applied
  on **Save**, then locked as `manual`, exactly like a picked candidate.
- **CLI:** `moxie covers upgrade [--dry-run] [--vndb] [--no-steam] [--limit N] [--game ID]`.
  `--dry-run` lists what would be replaced without downloading.

## Not covered

- DLsite and itch.io only publish landscape art.
- Johren, Fakku and other storefront exclusives have no keyless API.
- Those games keep their F95 banner: letterboxed in the grid, filling the wide
  view.
