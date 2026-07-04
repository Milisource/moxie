# F95Zone Browser — Design Reference

> Inspected `https://f95zone.to/sam/latest_alpha/` (v4.0.3-Alpha) on 2026-07-04
> via Playwright headless Chromium with authenticated session cookies.

---

## Overview

The F95Zone `/sam/latest_alpha` page is a **custom-built game browser** (not standard XenForo) that replaced the old "latest updates" page due to performance issues. It uses a completely new backend with far better performance and new features. This document captures its UI structure, game data model, and interaction patterns as a reference for improving Moxie's `F95Browser.svelte`.

---

## Page Structure

```
┌─────────────────────────────────────────────────────────────────┐
│ NAV: Downloads | Forums | Search                                │
├─────────────────────────────────────────────────────────────────┤
│ Notice: Old latest updates page disabled, new alpha only        │
├──────┬──────────────────────────────────────────────────────────┤
│      │  [Filter Button]  [Options Button]                       │
│      │  Prev  1  2  3  …  878  Next                            │
│ FILT │                                                          │
│  ERS │  ┌─────┐  ┌─────┐  ┌─────┐  ┌─────┐  ┌─────┐           │
│ SIDEB│  │Cover│  │Cover│  │Cover│  │Cover│  │Cover│           │
│  AR  │  │     │  │     │  │     │  │     │  │     │           │
│      │  │Title│  │Title│  │Title│  │Title│  │Title│           │
│      │  │Ver  │  │Ver  │  │Ver  │  │Ver  │  │Ver  │           │
│ Categ│  │Eng  │  │Eng  │  │Eng  │  │Eng  │  │Eng  │           │
│ Sor t│  └─────┘  └─────┘  └─────┘  └─────┘  └─────┘           │
│ Date │                                                          │
│ Tags │  ┌─────┐  ┌─────┐  ┌─────┐  ┌─────┐                    │
│ Prefi│  │Cover│  │Cover│  │Cover│  │Cover│                    │
│ x    │  │ ... │  │ ... │  │ ... │  │ ... │                    │
│      │  └─────┘  └─────┘  └─────┘  └─────┘                    │
│      │                                                          │
│ 26320│  Prev  1  2  3  …  878  Next                            │
│ Resul│                                                          │
│ ts   │                                                          │
└──────┴──────────────────────────────────────────────────────────┘
```

### Key Sections

| Section | Description |
|---------|-------------|
| **Navigation bar** | Standard XenForo nav: Downloads, Forums, Search |
| **Notice** | Banner about old page being disabled; directs to new alpha |
| **Filter sidebar** | Collapsible panel with all search/filter controls (see below) |
| **Options panel** | Popover with view preferences (tile size, ignored threads, etc.) |
| **Game grid** | Cover-art tiles in responsive grid layout |
| **Pagination** | 878 pages, 30 items/page default (configurable 15-90) |
| **Results count** | "26320 Results" at bottom of filter sidebar |

---

## Game Entry Data Model

Each tile in the grid represents a single game thread with the following fields:

| Field | Example | Notes |
|-------|---------|-------|
| **Category** | `VN`, `RPGM`, `HTML` | Visual Novel, RPG Maker, HTML, Unity, Others |
| **Engine** | `Ren'Py`, `Unity`, `RPGM` | Engine prefix — matches Moxie's engine detection |
| **Version** | `v0.1.4`, `Ch.4 Free`, `Ep.6 Free` | Latest version string |
| **Title** | `PutMEOn`, `My Tradwife Is a Femboy?` | Game thread title |
| **Thread ID** | `22` | Internal F95Zone thread identifier |
| **Days Ago** | `87` | Days since last update |
| **Views** | `192K`, `3.2M` | Human-readable view count |
| **Rating** | `3.3`, `4.5`, `—` | Star rating (1-5 scale, `—` = unrated) |

**Raw data example** (fields appear in order, newline-separated per tile):
```
VN                    ← Category
Ren'Py                ← Engine
v0.1.4                ← Version
PutMEOn               ← Title
22                    ← Thread ID
87                    ← Days since last update
192K                  ← Views
3.3                   ← Rating
```

---

## Filter Sidebar

The filter sidebar provides comprehensive search/filter controls:

| Control | Type | Description |
|---------|------|-------------|
| **Category** | Multi-select | Filter by game type: VN, RPGM, HTML, Unity, etc. |
| **GAMES** | Count display | Shows "26320 Results" |
| **SORTING** | Dropdown | Sort by date, title, rating, views, etc. |
| **DATE** | Date picker | Filter by update date range |
| **DATE LIMIT** | Dropdown | Anytime, today, this week, this month, this year |
| **SEARCH** | Text input | Free-text search across titles and creators |
| **CREATOR/TITLE** | Text input | Search specific creator or title |
| **TAGS (MAX 10)** | Tag selector | Include up to 10 tags (OR/AND toggle) |
| **EXCLUDE TAGS** | Tag selector | Exclude up to 10 tags |
| **PREFIX: ENGINE** | Multi-select | Filter by engine prefix |
| **PREFIX: OTHER** | Multi-select | Filter by other prefixes |
| **PREFIX: STATUS** | Multi-select | Filter by status prefix (Completed, Demo, etc.) |

---

## Options Panel

The options panel (gear icon) controls display preferences:

| Option | Choices | Default |
|--------|---------|---------|
| **Tile layout** | Normal grid, Large grid, List | Normal grid |
| **Ignored threads** | Hide, Transparent, Show | Transparent |
| **Open links in new tab** | Yes, No | Yes |
| **Filters Sidebar** | Normal, Sticky | Normal |
| **Version style** | Small (Prefix), Large (Title) | Small (Prefix) |
| **Search Highlight** | On, Off | On |
| **Notifications** | NSFW, SFW | NSFW |
| **Hover delay** | Range slider (0-1000ms) | 50ms |
| **Items Per Page** | Range slider (15-90) | 30 |

---

## Design Patterns to Emulate

### 1. Cover-Art-Centric Grid

F95Zone's tile layout prioritizes cover art as the primary visual element. Each tile is roughly 200×280px with:
- Cover image filling the top ~70%
- Title, version, and engine badge overlaid/below
- Consistent aspect ratio (3:4, matching game cover proportions)

**Our F95Browser currently:** Uses cards ~220px wide with thumbnails at ~60px. Covers should be larger.

### 2. Progressive Loading

The page loads 30 items at a time with pagination. A hover delay slider suggests that tooltips/previews load on hover after a configurable delay (50ms default) — probably a performance optimization for the grid.

### 3. Two-Level Classification

Each game entry shows both **Category** (VN, RPGM, HTML) and **Engine** (Ren'Py, Unity). This dual classification helps users distinguish between the narrative/structural type of a game and its underlying technology.

**Our F95Browser currently:** Shows only the engine prefix from `engine.ExtractEngineFromTitle()`. We could extract category from the thread prefix too.

### 4. Tag-Based Filtering

The tag system supports:
- Up to 10 include tags (OR/AND toggle)
- Up to 10 exclude tags
- Auto-complete tag search in the sidebar

### 5. Layout Flexibility

Users can switch between three layouts:
- **Normal grid**: ~5 columns, compact tiles
- **Large grid**: ~3 columns, bigger covers
- **List view**: Traditional table rows with smaller thumbnails

### 6. Stat Badges

Each tile shows key stats at a glance:
- Days since update (recency signal)
- View count (popularity signal)
- Star rating (quality signal)

---

## Recommendations for F95Browser.svelte

### Short-term (P2)

1. **Larger cover thumbnails** — Increase the result card thumbnail from ~60px to at least 120×160px to make covers the primary visual element.
2. **Add category badge** — Extract and display the game category (VN, RPGM, HTML) alongside the engine prefix. F95Zone's thread format includes both.
3. **Results count** — Show "X results found" in the search results header.
4. **Per-game metadata** — Display version more prominently in the card, not just in the preview panel.

### Medium-term (P1)

1. **Filter sidebar** — Replace the simple engine/status chips with a proper sidebar containing category, engine, status, and tag filters.
2. **Layout toggle** — Add Normal grid / Large grid / List view toggle.
3. **Pagination** — Add page navigation at the bottom of results (or infinite scroll).
4. **Hover preview** — Show a quick preview tooltip when hovering over a game card, with a configurable delay.

### Long-term

1. **Rating display** — If F95Zone exposes ratings in their API or search HTML, display them.
2. **Tag cloud** — Show popular tags with count for filtering.
3. **Items-per-page control** — Let users choose how many results to display.
4. **Notice integration** — Sync integration awareness: show a notice when F95Zone cookies are missing, linking to the sync dialog.

---

## Data Flow: Our F95Browser vs. F95Zone

| Aspect | F95Zone `/sam/latest_alpha` | Moxie `F95Browser.svelte` |
|--------|---------------------------|--------------------------|
| **Data source** | Custom backend API (XenForo DB) | `SearchF95Zone()` → `scraper.SearchF95Zone()` → XenForo/Google |
| **Results** | Full browse of ALL games (26K+) | Search query only (≤5 results) |
| **Covers** | Server-rendered in grid | Lazy-loaded via `GetThreadPreview()` or cached |
| **Filters** | Server-side (full query language) | Client-side only (minimal) |
| **Pagination** | 878 pages, 30/page | Single page, max 5 results |
| **Add to library** | Not available (site only) | One-click "Add to Library" button |

---

## Notes

- The `/sam/latest_alpha` page is itself an **alpha** — it may change significantly as F95Zone iterates on it.
- The old page was disabled "due to performance issues," suggesting the new backend is a significant rewrite.
- The page uses a custom options panel (not standard XenForo options), indicating first-class UX investment.
- A "Hover delay" option suggests hover-based previews/tooltips on game tiles.
- Browser detection blocks outdated User-Agents (Playwright's default Chromium was blocked, showing "Please update your browser to continue!").
