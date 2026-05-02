# Browser Cookie Extraction

## What

Extracts F95Zone authentication cookies from installed browsers (primarily Firefox, fallback to Chrome/Chromium/Brave/Edge) so the scraper can send authenticated requests. Lives in `internal/browser/browser.go` — a single file of ~56 lines.

## How

The `GetF95Cookies()` function uses `github.com/browserutils/kooky` to read browser cookie stores:

```go
cookies, err := kooky.ReadCookies(
    context.Background(),
    kooky.Valid,
    kooky.Domain("f95zone.to"),
)
```

kooky discovers installed browsers by scanning standard install paths on each platform:
- **Linux**: `~/.mozilla/firefox/`, `~/.config/google-chrome/`, `~/.config/BraveSoftware/`, etc.
- **macOS**: `~/Library/Application Support/Firefox/`, `~/Library/Application Support/Google/Chrome/`, etc.
- **Windows**: `%APPDATA%/Mozilla/Firefox/`, `%LOCALAPPDATA%/Google/Chrome/`, etc.

For Firefox, kooky reads `cookies.sqlite` at the binary level using its own B-tree parser — it does **not** use a SQL driver. This is critical because Firefox's `cookies.sqlite` is always in WAL mode and locked by the browser process; a SQL driver would fail with "database is locked." kooky's binary-level reading bypasses the SQLite driver entirely.

The function filters for `f95zone.to` domain cookies, sorts them by name for deterministic header construction, and joins them into a Cookie header string: `"xf_session=abc; xf_user=def; cf_clearance=ghi"`.

## Why

**kooky over ncruces/go-sqlite3 for cookie reading** — The original approach tried using ncruces/go-sqlite3 to open Firefox's `cookies.sqlite` directly. This failed because Firefox keeps its cookie database in WAL mode with an active WAL file and a shared-memory file (`cookies.sqlite-wal` and `cookies.sqlite-shm`). A SQL driver requires exclusive access to flush the WAL; without it, queries return stale or empty results. kooky's binary-level B-tree parser reads committed data directly from the main database file, ignoring the WAL entirely.

**kooky over cookie file parsing** — F95Zone cookies (`cf_clearance`) contain Cloudflare challenge tokens that expire after the browser session or a few days. Re-extracting from the browser on each run ensures the token is fresh. Manual cookie export would need to be repeated every time the token expires.

**Firefox as the primary target** — Firefox has the most portable cookie store format (SQLite on all platforms). Chrome-family browsers use a variety of formats (SQLite with AES-256-GCM encryption on modern versions, old SQLite on some platforms). kooky handles both, but Firefox is the most reliable across all three target OSes.
