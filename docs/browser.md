# Browser Cookie Extraction

## What

Extracts F95Zone authentication cookies from installed browsers — Firefox plus Chromium-family stores (Chrome, Chromium, Brave, Edge, Helium, Playwright profiles) — so the scraper and masked-link resolver can send authenticated requests. Lives in `internal/browser/browser.go`.

## How

`GetF95Cookies()` merges cookies from three sources, deduplicating by cookie name and preferring later, non-empty values:

1. kooky's registered finders (Firefox) with `kooky.DomainHasSuffix("f95zone.to")`.
2. Non-standard Firefox profiles such as `~/.config/mozilla/firefox` through the direct `cookies.sqlite` reader.
3. Chromium-family profiles that kooky does not discover, scanned with globs and read through `chrome.CookieStore` (which handles Chromium's encrypted cookie values):
   - `~/.config/*/*/Cookies`
   - `~/.config/*/*/*/Cookies`
   - `~/.config/*/*/Network/Cookies`
   - `~/.cache/ms-playwright*/*/Default/Cookies`

The Chromium globbing matters in practice: Helium stores its profile at `~/.config/net.imput.helium/Default/Cookies`, which is outside kooky's fixed Chrome/Chromium/Brave roots. Before this, an F95Zone login in Helium was invisible to moxie and every masked link failed with "requires a session cookie".

For Firefox, kooky reads `cookies.sqlite` at the binary level using its own B-tree parser — it does **not** use a SQL driver. This is critical because Firefox's `cookies.sqlite` is always in WAL mode and locked by the browser process; a SQL driver would fail with "database is locked." kooky's binary-level reading bypasses the SQLite driver entirely.

The function filters for `f95zone.to` domain cookies, sorts them by name for deterministic header construction, and joins them into a Cookie header string: `"cf_clearance=abc; xf_captcha=…; xf_csrf=…; xf_session=…; xf_user=def"`.

## Why

**kooky over ncruces/go-sqlite3 for cookie reading** — The original approach tried using ncruces/go-sqlite3 to open Firefox's `cookies.sqlite` directly. This failed because Firefox keeps its cookie database in WAL mode with an active WAL file and a shared-memory file (`cookies.sqlite-wal` and `cookies.sqlite-shm`). A SQL driver requires exclusive access to flush the WAL; without it, queries return stale or empty results. kooky's binary-level B-tree parser reads committed data directly from the main database file, ignoring the WAL entirely.

**kooky over cookie file parsing** — F95Zone cookies (`cf_clearance`) contain Cloudflare challenge tokens that expire after the browser session or a few days. Re-extracting from the browser on each run ensures the token is fresh. Manual cookie export would need to be repeated every time the token expires.

**Firefox as the primary target** — Firefox has the most portable cookie store format (SQLite on all platforms). Chrome-family browsers use a variety of formats (SQLite with AES-256-GCM encryption on modern versions, old SQLite on some platforms). kooky handles both, but Firefox is the most reliable across all three target OSes.
