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
| `<id>.thumb` | Grid thumbnail, JPEG q85 (format version `t2`) |
| `<id>.large` | Detail-view variant, long edge ≤ 1400px, built on first request |
| `<id>.url` | URL the original came from |
| `<id>.meta.json` | `{w, h, tone, source, url, locked}` |
| `<id>.prev`, `<id>.prev.meta.json` | The cover before the last upgrade/pick (one-step undo) |

`source` is `f95`, `steam`, `steamgriddb`, `vndb` or `manual`. A cover is
**pinned** when it's `locked` or its source isn't `f95`. F95 cover syncs
(`cacheCoverCtx`) never overwrite a pinned cover.

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
- **CLI:** `moxie covers upgrade [--dry-run] [--vndb] [--no-steam] [--limit N] [--game ID]`.
  `--dry-run` lists what would be replaced without downloading.

## Not covered

- DLsite and itch.io only publish landscape art.
- Johren, Fakku and other storefront exclusives have no keyless API.
- Those games keep their F95 banner, letterboxed.
