# Moxie Desktop UI — What Makes Game-Library UIs Usable, and What Moxie Is Not Doing

> **Status:** Research document · feeds the desktop-GUI redesign
> **Scope:** Moxie's Wails/Svelte desktop frontend only (the Bubble Tea TUI is out of scope)
> **Reference set:** Steam (mass-market baseline) · Playnite (closest functional analogue — local library manager) · Heroic Games Launcher (Linux-native analogue), with light passes on GOG Galaxy 2.0, Epic, itch.io, and Steam Deck / Big Picture
> **Method:** Web research with citations + an evidence-based audit of the real frontend (screenshots in `docs/screenshots/`, source-verified claims). Screenshots were captured against a mock backend runtime (see §6.1); every structural claim was cross-checked against `desktop/frontend/src/`.

---

## 1. Executive Summary

Moxie's desktop app does the *work* of a game library, but presents it like a **database-management tool**, not a game library. The single dominant finding:

> **The library is a dense 6-column table** (Cover / Title / Engine / Version / Size / Status) with uppercase 11 px column headers, 49 px rows, **72 × 40 px cover thumbnails**, and a title column that eats 53 % of the main area. There is **no grid-view option, no density control, and no Play affordance on any row**. The best-known competing products — Steam, Playnite, Heroic, Lutris, GOG Galaxy, itch — all lead with **cover-art grids**, put **Play on the primary surface**, default to **recently played**, and treat the library as a *browsable collection* rather than a *sortable spreadsheet* (sources in §4).

The rest of the surface repeats the pattern: secondary views are **management views** (Scan, Sync, Updates, Covers, Duplicates, Trash) given equal weight in the sidebar; several are a single button on an otherwise blank page; there is **no "Recently Played" surface anywhere** despite play history being recorded in the DB; filters are a search box + engine dropdown + 5 toggle-chips rather than the quick views ("Installed", "Ready to play", "Recently played") and dynamic collections that users have internalized from Steam.

All of this is fixable in layers, and this document ends with concrete, prioritized recommendations (§7). The most important insight up front: **moxie is not missing a "prettier skin" — it is missing the information architecture of a game library** (cover-first browsing, recency-first defaults, play-state model, progressive disclosure of management). That is a redesign, not a restyle.

---

## 2. Why Game-Library UIs Converge — The Underlying Principles

Every mature launcher independently converged on the same shapes. The principles explain why.

### 2.1 The job is "pick something to play," not "browse a database"

A game library's core loop is *decide what to play → launch it*. Everything else (metadata, updates, statuses, downloads) is supporting cast. A table optimized for scanning *many columns of values* serves a different job: it is built for *data inspection* (compare versions, sizes, paths across rows). Note that Steam's own stated goal for the 2020 redesign was exactly this job: Valve wanted the library to help users "identify what's going on and quickly jump back into stuff you've played recently" (PC Gamer, [steam-library-redesign-date-preview](https://www.pcgamer.com/steam-library-redesign-date-preview/)). Moxie's default landing page serves the *supporting* job and de-emphasizes the primary one.

### 2.2 Recognition over recall — why covers win

NN/g heuristic #6: "minimize the user's memory load by making elements, actions, and options visible" — external cues beat memory ([recognition-and-recall](https://www.nngroup.com/articles/recognition-and-recall/)). Cover art is *recognition* UI: the eye recognizes the artwork; a spreadsheet title column demands *recall* of a name. This is why "plaintext-is-important" complaints and cover-grid praise coexist in the Steam redesign threads ([Steam discussion 2962768084999964961](https://steamcommunity.com/groups/SteamClientBeta/discussions/3/2962768084999964961/)) and why users explicitly compared Steam's new grid to "movie streaming" plates ([Steam discussion 1644304412651136017](https://steamcommunity.com/discussions/forum/10/1644304412651136017/)). Netflix-era users *expect* artwork-first catalogs.

Practical consequence for moxie: cover art is the most valuable asset in the UI, and moxie renders it at 72 × 40 px — a size where it conveys almost nothing. Steam has shipped section after section of guidance that the **library capsule** is the primary presentation of a game ([Steamworks — Library Assets](https://partner.steamgames.com/doc/store/assets/libraryassets)).

### 2.3 Choice overload / Hick's law — why "recently played first" exists

Hick's law: decision time grows with the number and complexity of options ([lawsofux.com/hicks-law](https://lawsofux.com/hicks-law)). Choice overload is the "paradox of choice" problem ([lawsofux.com/choice-overload](https://lawsofux.com/choice-overload)). In games specifically, excess choice causes **choice deferral or quitting** — the strongest in novices ([Player Research — Spoiled for Choice](https://www.playerresearch.com/learn/spoiled-for-choice-the-psychology-of-choice-overload-in-games-and-how-to-avoid-it)).

The industry's answer is **recency-first defaults and installed-only quick views**:
- Steam's 2020 library **home drops you into recently-played** (official [libraryupdate](https://store.steampowered.com/libraryupdate); PC Gamer on [recently-played-first home](https://www.pcgamer.com/steam-library-redesign-date-preview/)).
- The **Steam Deck / new Big Picture home "drops you straight into a list of your recently-played games"** ([PC Gamer](https://www.pcgamer.com/at-long-last-the-steam-deck-ui-has-replaced-steams-big-picture-mode/)).
- **Heroic** surfaces a **Recently Played strip** at the top of its library ([Heroic issue #2914](https://github.com/Heroic-Games-Launcher/HeroicGamesLauncher/issues/2914)) and defaults the sort to **installed-first** ([Heroic PR #3523](https://github.com/Heroic-Games-Launcher/HeroicGamesLauncher/pull/3523)).
- **Steam Deck users** report manually imposing limitations to cope ("just install 1 game") ([r/SteamDeck](https://www.reddit.com/r/SteamDeck/comments/12bis0e/how_do_you_handle_choice_overload_aka_first_world)).

Moxie's default sort is **Title A–Z** (`viewState.svelte.js`, `sortColumn: 'title'`). Play history *is* recorded (`DesktopPlayEntry`, `playHistory` in the detail view) but the desktop app never surfaces recency at library level — no shelf, no view, no sort option.

### 2.4 Information scent and "a link is a promise"

Users choose the item whose label and context best predict its destination and abandon quickly when the scent dies ([NN/g — information scent](https://www.nngroup.com/articles/information-scent/); [information foraging](https://www.nngroup.com/articles/information-foraging/)). Clicking a game must land on *that game* and make *Play* obvious ([NN/g — a link is a promise](https://www.nngroup.com/articles/link-promise/)). Moxie's table row gives the click a weak scent (a title in a spreadsheet); the pager must confirm with a strong, imagery-first header. Relatedly: search is not enough — navigation shows the shape of the space ([NN/g — search not enough](https://www.nngroup.com/articles/search-not-enough/)).

### 2.5 Progressive disclosure — Play first, Wine later

"Show only the most important options first; defer advanced or rarely used features… the advanced door must be visible and honestly labeled" ([NN/g — progressive disclosure](https://www.nngroup.com/articles/progressive-disclosure/)). Every launcher follows the same 2-level split:
- **Level 1:** Play / Install / Update — one obvious primary action.
- **Level 2:** launch settings, Wine/Proton, prefix, env vars — in a per-game settings panel.

Heroic is the clearest Linux example: Play on the card, Wine/Proton in a per-game "Game Settings" tab and a separate Wine Manager page ([Heroic releases](https://github.com/Heroic-Games-Launcher/HeroicGamesLauncher/releases); [Heroic wiki](https://github.com/Heroic-Games-Launcher/HeroicGamesLauncher/wiki/Home)). Serviceability note: **skill requirements** — the developers' A/B iterations show the Play button should be a *text* primary button ("PLAY"), not an ambiguous icon ([Heroic PR #2750](https://github.com/Heroic-Games-Launcher/HeroicGamesLauncher/pull/2750)).

Moxie gets *some* of this right: `GameDetail.svelte` has a `▶ Play` primary button (line 938) and an install flow for virtual games, and the sync menu is separate. But the **library row level has no Play affordance** — the cheapest place to put the core action.

### 2.6 Empty states must teach and point to action

NN/g: empty states must (1) communicate status, (2) teach in context, (3) provide a direct pathway to populate ([NN/g — empty states](https://www.nngroup.com/articles/empty-state-interface-design/)). Moxie's **empty library** does this well: "📂 No games yet — Scan a directory to find and add games to your library" (verified in `15-library-empty.png`). But the **near-empty states** (search with one result) keep the full table chrome floating over dead space (`02-library-search.png`).

---

## 3. The Reference Set — What Each App Does and Why

### 3.1 Steam — the mass-market baseline

Valve's 2020 library ([official page](https://store.steampowered.com/libraryupdate)) shipped with goals users can recite from memory:
- **Library-home shelves** — recently played, recent purchases, updates, friend activity; users add **custom shelves** ("Add a custom shelf or ten").
- **Dynamic collections** — collections that auto-populate from store tags and filters (e.g. "RPG games installed locally"). An Aug 2023 update added **sort by date added to library** ([gamepressure](https://www.gamepressure.com/newsroom/new-steam-update-gives-you-more-control-of-your-game-library/z15c5d)).
- **Drag-and-drop organization** — including dragging a game onto "Collections" to create a new collection.
- **Per-game "play bar"** on the detail page — Play that switches to a playing/Stop state, plus a favorites toggle ([Steam news](https://store.steampowered.com/oldnews/195171)).
- **Update visibility** — "Changed how major updates are displayed in the library activity section to make them easier to find" ([Steam client news](https://steamcommunity.com/news/client?id=440)).
- **What's New shelf** — updates for games you own, top of the home shelf.

**Steam's documented failures are equally instructive** (user threads, first-party as UX research):
- **Clutter / ad-feel**: the What's-New shelf reads as ads for games you already own and was for a long time un-removable ([spuf.org](https://spuf.org/2019/11/26/the-new-steam-library/); [thread 1627412105598183155](https://steamcommunity.com/groups/SteamClientBeta/discussions/3/1627412105598183155/)). The community's ask: hideable shelves, a density knob.
- **Loss of the list view**: the 2020 redesign removed the dense list/table view and never restored it; users still say "the main thing I miss is list view" ([thread 2962768084999964961](https://steamcommunity.com/groups/SteamClientBeta/discussions/3/2962768084999964961/); [ResetEra](https://www.resetera.com/threads/after-2-years-of-wait-the-new-steam-library-ui-has-officially-released-and-i-think-it-kinda-sucks.150449/)). Valve's only concession was **Small Mode** ([PC Gamer](https://www.pcgamer.com/you-can-now-change-your-steam-library-view-back-to-small-mode)).
- **Art-without-titles**: in the grid, game names appear mainly in the sidebar; slow-loading art = "nothing but blank rectangles" ([Steam discussion](https://steamcommunity.com/groups/SteamClientBeta/discussions/3/2962768084999964961/)).
- **Density/small libraries**: no sub-categories, awkward "uninstalled" filtering (users hid games to group them) ([thread 1629663905409704456](https://steamcommunity.com/discussions/forum/10/1629663905409704456/)).
- **Performance during I/O**: CPU spikes while scrolling the grid ([ResetEra](https://www.resetera.com/threads/after-2-years-of-wait-the-new-steam-library-ui-has-officially-released-and-i-think-it-kinda-sucks.150449/)).

**Takeaway for moxie:** grid with *plaintext titles*, hideable shelves, density control, and a fast dense-list mode would be a *differentiation opportunity* — the list view is a niche Steam still doesn't serve.

### 3.2 Playnite — the closest functional analogue

Playnite is the local library manager community: a **unified library across stores + emulators**, local SQLite database, ~80 MB idle, "feels faster than Steam's client on weaker PCs" ([tech-insider comparison](https://tech-insider.org/gog-galaxy-vs-playnite-2026)).
- **Dual interface**: *Desktop mode* (windowed, config/detail, menus, bulk editing) + *Fullscreen mode* (controller-friendly, 10-foot TV grid) ([windowsforum](https://windowsforum.com/windows-news.4/playnite-10-51-guide-consolidate-steam-and-store-games-into-one-library.426776/)).
- **Default presentation is a cover grid**, themes emulate PS5 / Switch / Netflix aesthetics ([tech-insider](https://tech-insider.org/gog-galaxy-vs-playnite-2026)).
- **Why users choose it over Steam's own library**: one place for all stores, automatic metadata, deep filters, bulk editing — but "initial setup, especially emulators, can be complex" ([appmus Playnite review](https://appmus.com/software/playnite)).

**Takeaway:** a local library manager is at its best when it presents a *browsable unified collection*, with configuration pushed to a separate surface. Moxie has the exact same position (all stores → F95Zone) but currently shows the *config* surfaces (Scan/Sync/Updates/Covers/Trash) at the same weight as the *browsing* surface.

### 3.3 Heroic — the Linux-native analogue

Heroic (Epic + GOG + Amazon on Linux) is the closest analogue to moxie's audience (Linux-first, game-download heavy):
- **Grid-first, with a list-view toggle** (Grid ~190 MB vs List ~223 MB RAM benchmarks per the [Heroic wiki](https://github.com/Heroic-Games-Launcher/HeroicGamesLauncher/wiki/Home)) — note *both* views exist; the list view has a known polish gap ([issue #1113](https://github.com/Heroic-Games-Launcher/HeroicGamesLauncher/issues/1113)).
- **Recently Played strip** at the top of the library; default sort **A–Z installed-first** ([#2914](https://github.com/Heroic-Games-Launcher/HeroicGamesLauncher/issues/2914); [PR #3523](https://github.com/Heroic-Games-Launcher/HeroicGamesLauncher/pull/3523)).
- **Per-tile download progress bar**; a sidebar **Downloads** page with the queue and pause controls ([heroic FAQ](https://heroicgameslauncher.com/faq); [flathub listing](https://flathub.org/en/apps/com.heroicgameslauncher.hgl)).
- **Play button on every card** (product team explicitly A/B'd icon vs text PLAY → text won) ([PR #2750](https://github.com/Heroic-Games-Launcher/HeroicGamesLauncher/pull/2750)).
- **`/` keyboard shortcut to focus search** and pill-style filter dropdowns ([PR #5489](https://github.com/Heroic-Games-Launcher/HeroicGamesLauncher/pull/5489)).
- Long-standing FRs moxie could trivially beat: **sort by date-last-played** across the whole library ([#4384](https://github.com/Heroic-Games-Launcher/HeroicGamesLauncher/issues/4384)) and **sort by date-added-to-library** ([#3979](https://github.com/Heroic-Games-Launcher/HeroicGamesLauncher/issues/3979)) — the latter is impossible for Heroic because the store APIs don't expose it, but **moxie owns its data model** (`created_at`) and can do both today.
- **Performance lesson:** when downloading, Heroic's UI can become unresponsive ([#5046](https://github.com/Heroic-Games-Launcher/HeroicGamesLauncher/issues/5046)) — a grid must stay interactive while I/O happens off-thread. Moxie's async model is already good here.

### 3.4 Light passes — GOG Galaxy 2.0, itch.io, Epic

- **GOG Galaxy 2.0**: imagery-first detail pages ("a giant banner that takes up half the screen"), a "Recent" default view with widgets ([GOG changelog](https://support.gog.com/hc/en-us/articles/360003936637-GOG-GALAXY-Changelog)). Its cautionary tale: **"Beta" label six years after launch** ([changelog](https://support.gog.com/hc/en-us/articles/360003936637-GOG-GALAXY-Changelog)) and broken integrations ([r/gog](https://www.reddit.com/r/gog/comments/136eoe7/has_gog_abandoned_galaxy/)) — polish wins users, abandonment loses them.
- **itch.io desktop app**: owned-games grid with an **Install button per game that switches to Play/Update state after install**, and **install-locations scanning** to re-register games ([itch docs](https://itch.io/docs/itch/using/downloading.html)). Moxie already has the local-scan half; it lacks the state-switching primary button.
- **Epic**: its own VP admitted "the launcher sucks… it's really slow… calls to back-ends on every click" ([Eurogamer → GameFromScratch](https://gamefromscratch.com/epic-are-finally-fixing-the-awful-epic-game-launcher/)). The takeaway is the classic one: launcher UI must be **local-fast**; every interaction that needs a network round-trip first is a defeat.

### 3.5 Inverted do/don't rules (from community complaints)

| Do | Don't |
|---|---|
| Grid art **with plaintext titles** | Art grids where titles vanish |
| Default: recently played · installed-first | Default: an overwhelming full-sortable grid |
| Play on the primary surface (row + detail) | Play buried behind a detail click |
| Hideable shelves / views / density control | Un-removable cluttered shelves |
| Installed/ready quick filters | "Filter by uninstalled is impossible" |
| Local-fast responsiveness | Per-click backend round-trips |
| Sort by last-played and date-added | Recency hidden from the library |
| Empty states that teach + point to action | Headers floating over dead space |
| Say what the app is (Beta/Alpha honestly) | Shipping "Beta" for 6 years silently |

---

## 4. The Pattern Matrix

| Concern | Steam | Playnite | Heroic | **Moxie desktop (current)** | Usability cost of the gap |
|---|---|---|---|---|---|
| **Library presentation** | cover grid (+ Small Mode) | cover grid, themeable | grid + list toggle | **dense 6-column table, 72×40 thumbs** | library reads as a database/manager, not a collection |
| **Default view / sort** | Recently played home shelf | last-played friendly | A–Z installed-first + Recently Played strip | **Title A–Z** | 20–200+ games as flat alphabetical data; no "what do I play next" |
| **Play on primary surface** | Play bar on detail; games list rows | Play action per game | Play button on every card | **rows have no Play affordance** (detail only) | core loop requires a click-through to launch |
| **Quick views / filters** | Installed, Ready to play, hidden, favorites | full filter builder + dynamic filters | installed/installing/not-installed | search + engine dropdown + 5 status chips | no "ready to play" instant surface |
| **Collections** | custom + dynamic collections, drag-and-drop | full collection support | favorites + filters | static named collections, list-only, no cover preview | collections feel like tags, not groups |
| **Recency (sort/shelf)** | Recently played home + date-added sort (2023) | play-time views | Recently Played strip; sort-by-last-played FR | **play history recorded but never surfaced** | the strongest "pick a game" signal is invisible |
| **Updates** | What's New shelf + per-game news + patch badges | update indicators | per-tile progress + Updates page | Updates view with table + "Update All (3)" | functional but management-shaped; no per-row state in the library |
| **Detail view** | imagery-first page, play bar | tabbed, imagery-first | detail page, Play primary | two-col cover+meta, stacked cards, Play present (`GameDetail.svelte:938`) | mostly right; metadata feels form-like (muted labels) |
| **Density control** | Small Mode; grid/list | layout settings | grid/list toggle | **none** | no escape hatch for power users |
| **Empty states** | targeted shelves | — | — | **library empty-state is good** (`15-library-empty.png`) | near-empty states keep table chrome over dead space |
| **Progressive disclosure** | Play level-1, options level-2 | per-game config | Play level-1, Wine level-2 | detail-level Play + settings, but row-level action missing | management options outrank the play action in the sidebar |
| **Keyboard** | full mouseless | gamepad + keyboard | `/` focuses search | table rows are keyboard-accessible; no `/` search focus | small but a power-user tell |

---

## 5. Evidence-Based Audit of the Moxie Desktop Frontend

### 5.1 Method

The frontend (`desktop/frontend/src`, ~9,700 lines Svelte) talks to Go via generated Wails bindings (`window.go.main.App.*`, 59 methods). To run it standalone in a browser I added a **dev-only mock runtime** (never shipped): `desktop/frontend/mock/mock-data.js` + `mock-runtime.js`, a separate entry (`src/mock-main.js`, `mock.html`) and a `vite.mock.config.js` dev server that serves generated placeholder covers. Screenshots were taken at 1440 × 900, dark scheme, via headless Chromium. **Structural claims below are cross-checked against the source**, so they hold for the real app; the only mock-specific artifacts were placeholder cover art (the real cover server is exercised by the real backend) and fixture data.

Screenshots: `docs/screenshots/01-library.png` … `15-library-empty.png`.

### 5.2 The library — the central finding

Measured DOM state (1440 × 900):

| Dimension | Value |
|---|---|
| Sidebar | 240 px, brand + 12 nav items in 3 sections |
| Filter bar | 44 px: search (320×27) · engine `<select>` (82 px) · 5 status chips (11 px text) |
| Table header | 27 px, **uppercase 11 px semibold** ("COVER TITLE ENGINE VERSION SIZE STATUS") |
| Rows | **49 px tall**; 17 of 20 rows visible |
| Cover thumbs | **72 × 40 px** |
| Columns | Cover 80 · **Title 636 (53 % of the main area!)** · Engine 110 · Version 130 · Size 80 · Status 100 |
| Play button on a row | **0** |
| Grid/list/density controls | **0** |
| Default sort | Title A–Z |

Visual verdict (from the screenshot analysis): "the persistent use of a dense, 6-column HTML data table with uppercase column headers and microscopic cover thumbnails across the Library, Updates, and Trash views transforms the primary user surface from a game library into a database administration tool or file manager."

What the library **does** do well: status chips + engine dropdown are honest filters; the sortable headers work; search is debounced and survives tab switches; rows are keyboard-accessible with a context menu (rename/status/remove); covers retry after sync. The structure is sound — the *presentation model* is wrong.

### 5.3 The detail view

- **Structure:** header with "← Back to Library"; two-column hero (cover at 320×180 full art + metadata block with muted labels: Developer, Engine, Version, Status, Path, Exe, Tags); stacked cards below (Overview, Notes, Download Links, Play History); an action section with **`▶ Play` primary + Sync from F95Zone + Rename + Remove Game** (`GameDetail.svelte`). Virtual (not-yet-installed) games swap Play for a destination-picker + **↓ Install**.
- **What's right:** Play is present and primary-styled; install/update states are handled; update button behaves per-state.
- **What's not native:** the metadata block reads like a settings form (all-muted labels, no visual hierarchy beyond the title); there is no banner/hero treatment; play history is **buried behind a collapsed "Show" toggle** — the exact recency signal that should be driving the library.

### 5.4 Secondary views — management-shaped, not library-shaped

| View | Rendered as | Issue |
|---|---|---|
| **Updates** (`05`) | 4-column table + "Update All (3)" | uppercase headers again; table chrome for 3 rows |
| **Downloads** (`06`) | stats bar + search + cards | fine, but signals live in a separate tab, not on the library |
| **Collections** (`08`) | name + count rows | no cover collage → collections feel like tags |
| **Covers** (`07`) | one button on a blank page | no progress/grid/preview; feels like a CLI wrapper |
| **Sync** (`14`) | one button on a blank page | no last-synced, no plan, no history |
| **Browse (F95Zone)** (`04`) | search + empty-state placeholder | fine for a search surface |
| **Duplicates** (`09`) | cards with Keep/Remove per copy | functional |
| **Trash** (`10`) | table + Purge All | uppercase headers; Restore button visually weak |
| **Scan** (`13`) | path list + add/scan | fine — this is genuinely a management view |
| **Settings** (`11`) | sections with inputs | fine; the nested "Application Update" card-in-card is slightly off |

The pattern: almost every sidebar destination is a *management action with a form or a table*. In Steam, Playnite, and Heroic the analogous management surfaces (downloads queue, options, library settings) are reachable but **not the focus**; the default view is the collection. Moxie's sidebar gives "Scan", "Sync", "Updates", "Covers", "Duplicates", "Trash" the same visual weight as "Library" and "Browse".

### 5.5 Empty & near-empty states

- **Empty library** (`15`): strong — illustration icon, "No games yet", explicit next step ("Scan a directory…"). (*Note the "0 games loaded / 0 games" status text, a developer-voice artifact.*)
- **Search with one result** (`02`): weak — the full table header + filter bar stay over dead space; no "N results" count, no cleared-state hint.

### 5.6 What is provably *not there* (source-verified)

1. **No grid view or grid/list toggle** — the only list is the table (`GameList.svelte`; `view_switches = 0`).
2. **No recently-played surface at library level** — `grep recent|Recently` finds only a comment and the collapsed detail card.
3. **No density control** — no small/grid/list modes anywhere.
4. **No dynamic/smart collections** — collections are static named lists (`CollectionsView.svelte`).
5. **No sorting by last-played or date-added** (`viewState.svelte.js`, `sortColumn` set).
6. **No per-row Play affordance** (`play_buttons_in_rows = 0`).
7. **No quick views** (Installed / Ready / Recent).
8. **No `Ctrl/⌘/` search focus**, no mouseless power path beyond tabbing rows.
9. **No cover collage in collections**, no collection previews.

---

## 6. Moxie's Status Model vs. a Play-State Model

Moxie's `status` field is a **curation model** — *active / completed / abandoned / on_hold / unknown* — the user's own tracking judgment. That is genuinely valuable and nothing in the reference set replaces it. But the launcher pattern needs a second, orthogonal axis: **play-state** — *not installed (virtual) / installed / downloading / updating / playing now*. Steam, Heroic, and itch all render this axis on every card and across the whole library.

Recommended mapping (not a rewrite): keep `status` as user curation; add derived **playability flags** from existing data (`path` virtual vs real, `exePath` presence, active download state, update availability) and render *those* on the grid (badges, borders, state-switching primary button) while `status` stays in the detail view and filters. This is cheap — the data already exists — and it is precisely the "ready to play" affordance that makes launchers feel native.

---

## 7. Prioritized Recommendations

P0 = changes the core feel; P1 = strong usability wins; P2 = polish/power-user.

### P0 — Library as a collection, play as the core action

1. **Cover grid view as the default library presentation** (list stays available as a toggle ≈ Heroic/Playnite). Grid cells: **cover + plaintext title underneath** (avoid Steam's "art without names"), status/play-state badge, update indicator, hover Play button. Density: responsive card columns, ~12 px radius, engine/status colors as small accents.
2. **Recency first:** default sort/arrangement = **recently played** (data already exists in `play_history`), with **sort by date-added** as a first-class option (moxie owns `created_at` — where even Heroic can't). Landing view should answer "what do I play next."
3. **Play affordance on the library surface:** primary **▶ Play** per row/card (in the grid's hover layer *and* the list view), with state switching to a progress/stop treatment while running — matching the play-bar pattern.
4. **Play-state quick views:** "All · Installed · Ready to play · Recently played" primary tabs, with the existing engine/status filters as secondary. (Status chips stay — they're the curation axis.)

### P1 — Progressive disclosure and collection UX

5. **Sidebar hierarchy:** promote Library + Browse; fold Scan/Sync/Covers/Duplicates/Updates into clearly-labeled second-tier management sections (or an overflow menu). Every view gets a real primary action and better empty/progress states (Covers: grid of missing covers + progress; Sync: last-synced + expected scope).
6. **Collections with cover collage previews** and dynamic-collection rules (engine/status/flag auto-groups — Steam's dynamic collections pattern).
7. **Density + layout controls:** grid/list toggle, small-mode density, persistent preferences (Heroic persists via Redux; Steam only gave users "Small Mode" — a real gap moxie can fill).
8. **Search:** `Ctrl/⌘+F` (and `/`, Heroic-style) focuses search; show result counts; collapse table chrome on sparse results; fuzzy title matching (steal from the TUI's filter behavior).

### P2 — Polish, correctness, voice

9. **Visual voice:** replace developer-manager artifacts — muted field labels on detail, "N games loaded" status-bar text, bare uppercase headers — with launcher voice (play-action prominence, human empty states, meaningful counts).
10. **Keyboard & context:** context-menu parity for grid rows, arrow-key grid navigation, Esc closes overlays consistently, Enter launches.
11. **Status-bar real estate:** show pipeline state and game count less like a console; free space for "last sync" / upgrade-suggests.
12. **Fix the near-empty states** (search results): adaptive layout, "no results" guidance, clear-button.

### Sequencing note
P0 items 1–4 are one coherent redesign of the primary surface (`GameList.svelte` + `App.svelte` + `app.css`) and should land as a single workstream; P1 items 5–8 layer on top; P2 is continuous. The mock harness shipped with this doc (`desktop/frontend/mock/`) is intended for iterating on these without a Go backend.

---

## 8. Sources

**Primary (product pages / official statements):**
- Steam Library update (official): https://store.steampowered.com/libraryupdate
- Steamworks — Library Assets ("the library capsule is the primary way of presenting your game"): https://partner.steamgames.com/doc/store/assets/libraryassets
- Steam client news (play bar, favorites, update visibility): https://store.steampowered.com/oldnews/195171 · https://steamcommunity.com/news/client?id=440
- PC Gamer — Valve's stated redesign goal ("jump back into recently played"): https://www.pcgamer.com/steam-library-redesign-date-preview/
- PC Gamer — Steam Deck/BP recency-first home: https://www.pcgamer.com/at-long-last-the-steam-deck-ui-has-replaced-steams-big-picture-mode/
- PC Gamer — Small Mode: https://www.pcgamer.com/you-can-now-change-your-steam-library-view-back-to-small-mode
- Heroic wiki (grid/list RAM, structure, positions): https://github.com/Heroic-Games-Launcher/HeroicGamesLauncher/wiki/Home · issues #2914 · #4384 · #3979 · PRs #3523 · #2750 · #5489 · #5046
- Heroic FAQ + flathub (downloads page, queue): https://heroicgameslauncher.com/faq · https://flathub.org/en/apps/com.heroicgameslauncher.hgl
- GOG Galaxy changelog ("Beta" for 6 years): https://support.gog.com/hc/en-us/articles/360003936637-GOG-GALAXY-Changelog
- itch.io docs (install → play state switch; install-location scanning; collections): https://itch.io/docs/itch/using/downloading.html · install-locations.html · collections.html
- Epic "the launcher sucks… slow" (VP admission): https://eurogamer.net/the-launcher-sucks-lets-call-it-what-it-is-epic-game-store-boss-says… (via GameFromScratch: https://gamefromscratch.com/epic-are-finally-fixing-the-awful-epic-game-launcher/)

**UX theory (authoritative):**
- NN/g recognition vs recall: https://www.nngroup.com/articles/recognition-and-recall/
- NN/g information scent / foraging: https://www.nngroup.com/articles/information-scent/ · https://www.nngroup.com/articles/information-foraging/
- NN/g "a link is a promise": https://www.nngroup.com/articles/link-promise/
- NN/g progressive disclosure: https://www.nngroup.com/articles/progressive-disclosure/
- NN/g search is not enough: https://www.nngroup.com/articles/search-not-enough/
- NN/g empty states: https://www.nngroup.com/articles/empty-state-interface-design/
- Laws of UX — Hick's law / choice overload: https://lawsofux.com/hicks-law · https://lawsofux.com/choice-overload
- Player Research — choice overload in games: https://www.playerresearch.com/learn/spoiled-for-choice-the-psychology-of-choice-overload-in-games-and-how-to-avoid-it

**Community as UX research (user-stated pain points):**
- Steam lib 2020 threads (list-view loss, plaintext, density, What's New ads): https://steamcommunity.com/groups/SteamClientBeta/discussions/3/2962768084999964961/ · …/1627412105598183155/ · https://steamcommunity.com/discussions/forum/10/1644304412651136017/ · https://spuf.org/2019/11/26/the-new-steam-library/ · https://www.resetera.com/threads/after-2-years-of-wait-the-new-steam-library-ui-has-officially-released-and-i-think-it-kinda-sucks.150449/
- Playnite comparisons/reviews: https://tech-insider.org/gog-galaxy-vs-playnite-2026 · https://appmus.com/software/playnite · https://windowsforum.com/windows-news.4/playnite-10-51-guide-consolidate-steam-and-store-games-into-one-library.426776/
- Steam Deck choice-overload coping: https://www.reddit.com/r/SteamDeck/comments/12bis0e/how_do_you_handle_choice_overload_aka_first_world
- GOG abandonment discussions: https://www.reddit.com/r/gog/comments/136eoe7/has_gog_abandoned_galaxy/

---

## 9. How This Document Was Produced (tooling)

- **Phase A — reference research:** web research against the primary sources above (Steam official, Heroic GitHub issues/locales, NN/g, Laws of UX, Player Research, community threads).
- **Phase B — evidence audit:** the desktop frontend was run standalone in Chromium via a dev-only mock Wails runtime (`desktop/frontend/mock/` + `src/mock-main.js` + `mock.html` + `vite.mock.config.js`), captured at 1440 × 900 dark; DOM measurements (row/column sizes, cover thumbs, button counts) taken programmatically; every claim cross-checked in source.
- **Phase C — synthesis:** this document. Screenshots live in `docs/screenshots/`.

All screenshots are committed under `docs/screenshots/` so the audit is reproducible from the repo.