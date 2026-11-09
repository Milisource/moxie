# Desktop perf follow-up: grid/table virtualization

**Status:** done. Grid + table are windowed via `@tanstack/svelte-virtual` in
`desktop/frontend/src/lib/GameList.svelte` (user opted into the dependency
over hand-rolling, given the trade-off called out below). Verified with
`npm run build`, `go build/vet/test`, and a Playwright pass against the mock
dev server padded to 421 games: DOM node count stayed in the 22–55 range
(vs. all 421 unwindowed) while scrolling, hover/Play and scroll-position
restore across tab switches still work. The rest of this doc is kept as
implementation history/rationale.

## Context

The desktop app (Wails + Svelte 5 frontend, Go backend) was reported as slow, application-wide.
An investigation (two parallel Explore sweeps over frontend + Go backend) found the Go side
already well-engineered — background goroutines, debounced fs watcher, worker pools, singleflight
cover coalescing — so all real regressions were on the frontend, concentrated in the recent
library redesign (`89b6359` "library as a collection — cover grid, recency-first, quick views,
per-card Play").

### Already fixed (do not re-litigate these)
1. Grid cards were requesting full-resolution cover originals (`coverSrc(game.id, 'full')`)
   instead of the pre-built 320px thumbnail — fixed in `desktop/frontend/src/lib/GameList.svelte`.
2. Downloads tab issued the identical full-library DB join twice on every mount
   (`GetGamesWithDownloadLinks` + `GetAllDownloadLinks` both called `AllDownloadLinks(true)`) —
   fixed in `desktop/frontend/src/lib/DownloadsView.svelte` (now derives the games list
   client-side from the single `GetAllDownloadLinks()` call).
3. The "Ready to play" quick view's `displayed` derived was re-filtering/re-sorting the entire
   library up to 5×/sec during any active download/update (because `gameStates` is replaced
   wholesale on every progress tick) — fixed with a `busyIds` membership-diff guard in
   `GameList.svelte` that only retriggers the derived on actual phase transitions.
4. Go-side log writes were unbuffered (one syscall per `log.Info`/`slog.Info` line, thousands of
   lines during a cover backfill/sync) — `internal/log/log.go` now buffers file writes through an
   8KB `bufio.Writer`, with a `flushingHandler` slog wrapper that force-flushes on any Warn/Error
   record (covers both `log.Warn/Error` and raw `slog.Warn/Error`), plus `log.Flush()` wired into
   `App.shutdown()` in `desktop/app.go` so nothing is lost on exit.
5. `coverSetFromDir()` (`desktop/app.go`) re-read the whole cover directory from disk on every
   `GetGames`/`SearchGames`/`GetUpdatableGames` call — now cached for 2s (`coverSetCache`,
   mirroring the cookie-cache pattern in `internal/browser/browser.go`), invalidated immediately
   after a new cover is written so freshly fetched covers still show up right away.
6. F95Zone browser search result thumbnails loaded eagerly with no `loading="lazy"` and an
   unkeyed `{#each}` — fixed in `desktop/frontend/src/lib/F95Browser.svelte`.

All of the above landed on `wip-desktop-alpha`, verified with `go build ./...`, `go vet ./...`,
`go test ./internal/log/... ./desktop/...`, and `npm run build` in `desktop/frontend`.

## Remaining work: virtualize the library grid/table

**This is the one deferred item** — flagged as real but too large to bundle with the quick fixes
above. It needs a design decision and layout work, not a one-line change.

### The problem
`desktop/frontend/src/lib/GameList.svelte` renders the grid (`:544-618` at last check) and table
(`:647-729` at last check — line numbers have likely shifted after the fixes above, re-grep) views
by mapping every entry of the `displayed` derived into real DOM nodes inside a plain
`overflow-y: auto` scroll container (`.grid-scroll` / `.table-body`, styles around line
970-1210). There is no windowing/virtualization. Each grid card also carries a play button,
badges, and hover pseudo-elements/animations. For a library of a few hundred+ games this builds a
large DOM and, in grid layout, many cards are visible above-the-fold simultaneously (not
scroll-gated the way a single-column list would be), so `loading="lazy"` on `<img>` helps less
than it would in a table. This is the most consequential remaining architectural gap as library
size grows — it's what will make the app feel slow again once a user's library gets large, even
with fixes #1-#6 above in place.

### Constraint to respect: no new runtime dependencies
Check `desktop/frontend/package.json` before reaching for a library — as of this hand-off it has
**zero runtime dependencies**, only `devDependencies` (`svelte`, `vite`,
`@sveltejs/vite-plugin-svelte`). That's a deliberate pattern in this codebase (small Wails
bundle). Pulling in `svelte-virtual-list`/`@tanstack/svelte-virtual`/etc. would be the fastest
path but breaks that convention — raise it as an explicit trade-off with whoever picks this up
(AskUserQuestion: hand-roll a windowed renderer vs. accept a small virtualization dependency)
rather than silently deciding either way.

A hand-rolled approach is straightforward for both views since row/card height is fixed:
- Grid: `grid-template-columns: repeat(auto-fill, minmax(176px, 1fr))` — fixed card width, so row
  height is computable from container width ÷ card width. Track `scrollTop` (already captured via
  `onscroll` at `library.gridScrollTop`, `GameList.svelte`), compute visible row range, render only
  those rows plus overscan, and pad above/below with a spacer element sized to the
  not-rendered rows' total height so the scrollbar stays accurate.
- Table: rows are uniform height (`grid-template-columns: 80px 1fr 110px 130px 80px 100px 64px`)
  — simpler windowing, same spacer-padding technique.

### Suggested approach for the next session
1. Re-read `GameList.svelte` fresh (line numbers have shifted) and re-locate the grid/table markup
   and their scroll containers.
2. Decide hand-rolled vs. dependency (see constraint above) — ask the user if unsure.
3. Implement windowing for the grid first (bigger DOM cost per row due to cover image + hover
   state), then the table if time/complexity allows in the same pass.
4. Preserve existing behavior that reads from `library.gridScrollTop` / equivalent table scroll
   state (used to restore scroll position across tab switches — check `viewState.svelte.js` and
   `App.svelte` for how that's wired before changing the scroll container's structure).
5. Test with a large mock/real library (check `desktop/frontend` for existing mock fixtures —
   commit `09954c8` added "mock fixtures gain recency/date-added fields + realistic PlayGame")
   to actually see the DOM-node-count difference before/after.

### Verification
- `cd desktop/frontend && npm run build` — no syntax errors.
- Use the `run` skill (or `wails dev`) with a large mock library, switch to grid view, and confirm
  cards still render/scroll correctly, hover/Play button interactions still work, and scroll
  position survives tab switches.
- Rough DOM-node-count check (e.g. via browser devtools or a scripted `document.querySelectorAll`
  count) before/after with a few hundred games in the library, to confirm windowing actually
  reduces node count instead of just adding complexity without a measurable payoff.
