# FAQ

(See README for quick answers. This document covers edge cases.)

## Steam Integration

### Why doesn't artwork appear after `steam fix-artwork`?

The artwork pipeline has a two-tier fallback chain:

1. **F95Zone cover** — The primary source. `moxie steam fix-artwork` first tries to download the cover image URL scraped from the game's F95Zone thread.
2. **SteamGridDB** — If F95Zone has no usable image (e.g., an SVG placeholder or data: URI), the tool falls back to SteamGridDB — but only if you've configured an API key.

If neither source provides an image, the command reports "No cover artwork URL found." Configure a SteamGridDB API key and retry:

```bash
moxie config set steamgriddb-key YOUR_KEY
moxie steam fix-artwork <id>
```

### What if I have multiple Steam users?

By default, `steam add`, `steam remove`, and `steam fix-artwork` operate on the currently logged-in Steam user's `shortcuts.vdf` and `grid/` directory.

To target all Steam users on the machine (logged in at least once):

```bash
moxie steam add 42 --all-users
```

To target a specific user directory:

```bash
moxie steam add 42 --user 12345678
```

The `--user` flag accepts a numeric Steam ID (the directory name under `userdata/`). Combine with `--all-users` to add to every user except the current one.

### Can I add games without closing Steam?

**No.** Steam locks its `shortcuts.vdf` while running. Writing to it while Steam is open causes corruption. moxie checks that Steam is closed before any VDF write and refuses to proceed if it detects a running Steam process. Close Steam fully (check system tray), run the command, then restart Steam.

### Steam wiped my shortcuts after using moxie

moxie creates a timestamped backup of `shortcuts.vdf` before every write. Backups are named `shortcuts.vdf.backup-<ISO8601-timestamp>` and stored alongside the original file.

**To restore:**

1. Close Steam fully.
2. Find the most recent backup:
   ```
   ls -lt ~/.steam/steam/userdata/<steamid>/config/shortcuts.vdf.backup-*
   ```
3. Replace the current file with the backup:
   ```
   cp shortcuts.vdf.backup-<timestamp> shortcuts.vdf
   ```
4. Restart Steam.

If the backup approach doesn't work, the Steam client's own `steam://flushconfig/` can rebuild its configuration (though this is a drastic step that resets all settings).

## Scraping

### Why is scraping slow?

moxie deliberately rate-limits HTTP requests to F95Zone to avoid being blocked. Each thread scrape inserts a 1-second delay between requests. For a library with 100 associated games, a full `check-updates` or `sync` takes at least 100 seconds.

**Bypassing the rate limit** is possible but not recommended:

```bash
moxie sync --unsafe  # removes delays — may get your IP blocked
```

The `--unsafe` flag skips the rate limiter. Use it only if you know what you're doing and accept the risk of being rate-limited or IP-banned by F95Zone.

### My game got associated with the wrong thread

The auto-association engine scores candidate threads by title similarity (exact match → contains match → word overlap). It makes mistakes, especially for games with generic names ("My Game," "Project X").

**To fix:**

1. Re-associate a single game: `moxie sync <id>` — re-runs the scoring and gives a new best match.
2. Manual override in the TUI: open the detail view for the game, press `u`, and paste the correct F95Zone thread URL. The game's metadata will update immediately on save.

### Some games never get a cover image

Some F95Zone threads use SVG placeholders or inline `data:` URIs instead of real image URLs. The parser detects these and treats them as "no cover" because they can't be downloaded and forwarded to Steam's grid format (which expects PNG/JPG files).

This is expected behavior. The workaround is to configure a SteamGridDB API key, which provides artwork sourced from Steam's own grid database rather than F95Zone.

## TUI

### How do I search for a game?

Press `/` to focus the filter bar at the top of the library list. Start typing — the table filters in real time as you type, matching against game titles. Press `Esc` to clear the filter and return to the full list.

The filter bar is always visible at the top of the TUI. Even without pressing `/`, you can see it displaying the current filter text. The game count in the header updates to reflect how many games match: "42 games (12 filtered)".

### My filter shows no results

If you type a search term and the table goes empty, check:

1. **Engine filter** — Press `Ctrl+E` to cycle through engine types. If you're filtering for "Ren'Py" but all your games are "Unity," nothing will show. The active engine filter is shown in the header.
2. **Status filter** — Press `Ctrl+S` to cycle through game statuses (Playing, Completed, Dropped, etc.). An active status filter combined with a title search can produce no results.
3. **Reset everything** — Clear the search with `Esc`, then cycle engine/status filters back to "All" by pressing `Ctrl+E` / `Ctrl+S` until the header shows no active filter.

## Troubleshooting

### `moxie sync` fails with a block error

F95Zone uses Cloudflare's anti-bot protection. moxie authenticates via browser cookies — if your session cookie expires, the scraping layer gets a Cloudflare challenge page instead of real content.

**Resolution steps:**

1. Log in to F95Zone in Firefox (or your configured browser).
2. Run `moxie sync` again — the tool auto-detects fresh cookies from Firefox's cookie store.
3. If that doesn't work, manually export cookies to a file from a browser extension (cookies.txt format) and pass with `--cookie-file`.

**Prevention:** F95Zone sessions last weeks to months from a single login. You should only need to re-authenticate every few months.
