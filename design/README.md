# Moxie desktop — polish prototype (Nov 2026)

This is the design pass for the findings in
[`docs/desktop-ui-polish-review.md`](../docs/desktop-ui-polish-review.md).
It is a FigCraft file, not production code: nine 1440×900 screens showing the
intended hierarchy, density and flows before touching Svelte.

## Screens

| Frame | What it demonstrates | Report item |
|---|---|---|
| `01 Library` | Page header with count/sync summary; one toolbar row; 11px meta; neutral engine tags with colour dots; persistent Play on hover/focus; updating tile with a per-card progress bar | P0-1, P0-2, P0-3, P2-2 |
| `02 Library — View menu` | Grid/Wide/List + density + card size collapsed into one View popover | P0-3 |
| `03 Library — Filters` | Status/engine/sort collapsed into one Filters popover with an active count | P0-3 |
| `04 Detail` | Play bar directly under the title; update action as its secondary; last-played context; advanced metadata behind a disclosure | P1-1 |
| `05 Cover art` | Coverage stat, missing-cover grid, sources panel, recent activity, primary fetch action | P0-4 |
| `06 Sync` | Cookie/last-sync/updates stats, "what will run" plan, recent runs, primary Start sync | P0-4 |
| `07 Add game` | Constrained form, detected-game preview, one primary Add action, bulk-scan path, recent folders | P0-4 |
| `08 Settings` | Section nav + two-column content, no full-width dead space | P1-4 |
| `09 Trash` | Restore promoted to the row primary; Empty trash demoted into a help card; designed empty state | P1-2 |

## What changed, in one paragraph

The library keeps its archive/catalog identity but loses the control wall: a
single header carries the count and sync age, the toolbar is one row, and the
secondary controls live in two popovers. Caption metadata moves to 11px with a
4.5:1+ grey, and engine colour moves off the text and onto a dot, so the tags
survive every engine palette. Play is the only solid button on Detail and sits
in a full-width bar under the title. The four management screens now show
coverage/plan/results instead of explanatory paragraphs, and Settings uses the
horizontal space it already has. Trash no longer leads with a destructive
button.

## Regenerate

```bash
python3 design/build_moxie.py
```

That writes `design/moxie-polish.figcraft` + `.jsonl` and the nine PNGs in
`design/renders/`. The builder uses the local `figkit` toolkit and the Moxie
token set (not the kit's default SaaS palette); every frame is named and
checked after the run.

Lint:

```bash
python3 ~/.config/opencode/skills/craft-suite/lib/designlint.py design/moxie-polish.figcraft
```

**Result: 0 FIX / 1 WARN.** The warning is `color/accents — 5 distinct
saturated hues across the page`. That is intentional and belongs to the
placeholder cover art (violet, indigo, teal, rust, olive), not to the UI
chrome: the interface uses one violet accent plus semantic good/warn/bad. The
linter cannot separate content art from UI colour.

## Caveats

- Covers are generated gradient/wordmark placeholders. They are there to test
  grid rhythm and caption contrast, not to represent real key art.
- Data is the mock library fixture (81 games, 17 updates, 13 missing covers)
  so the screens read as a real session.
- The `Saves` view is not stubbed in the mock harness, so it is not in this
  prototype; it should follow the same shared header + two-column pattern.
- This is a design artefact. Fonts: IBM Plex Sans / Mono (the app's bundled
  families). The production build should use the same tokens.

## Files

- `moxie-polish.figcraft` — editable source, nine frames
- `moxie-polish.pdf` — 9-page review PDF
- `renders/01…09-*.png` — full-size screens
- `renders/00-contact-sheet.png` — 3×3 overview
- `build_moxie.py` — generator with the token table and layout math
