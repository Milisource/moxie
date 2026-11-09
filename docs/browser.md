# Browser Cookie Extraction

## What

Extracts authentication cookies from installed browsers (Firefox, Chrome, Chromium, Brave, Edge) so the scraper can send authenticated requests and the downloader can pass Cloudflare clearance to file hosts. Lives in `internal/browser/browser.go` — a single file covering cookie discovery with dedup and dot-domain handling (`GetF95Cookies` for F95Zone sessions, `GetCookiesForHost` for download hosts), a read-only SQLite helper for a specific Firefox store (`GetF95CookiesFromSQLite`/`GetCookiesFromSQLite`), and header building with control-character sanitization.

## How

The `GetF95Cookies()` function uses `github.com/browserutils/kooky` to read browser cookie stores:

```go
cookies, err := kooky.ReadCookies(
    context.Background(),
    kooky.Valid,
)
```

The full cookie snapshot is cached for 60 seconds (`cachedBrowserCookies`) and shared across callers — a read walks every browser profile **plus every Firefox profile on disk**, so it is not repeated per request. Filters run locally on the cached snapshot; `kooky.Domain(...)` filtering is deliberately not used because it matches with exact equality and drops domain-scoped cookies stored with a leading dot (`.f95zone.to`).

kooky discovers installed browsers by scanning standard install paths on each platform, and all five browser drivers are registered (`kooky/browser/{firefox,chrome,chromium,brave,edge}`):
- **Linux**: `~/.mozilla/firefox/`, `~/.config/google-chrome/`, `~/.config/BraveSoftware/`, etc.
- **macOS**: `~/Library/Application Support/Firefox/`, `~/Library/Application Support/Google/Chrome/`, etc.
- **Windows**: `%APPDATA%/Mozilla/Firefox/`, `%LOCALAPPDATA%/Google/Chrome/`, etc.

### Firefox profiles are enumerated from disk, not from profiles.ini

kooky's Firefox driver only walks the profiles named in `profiles.ini`, which is not enough to find the profile the user is actually logged in with. On current Firefox, `profiles.ini` is a mutable single-pointer registry: Firefox rewrites `[Profile0]` / `[Install…] Default` to whichever profile it activates last, and a second window's profile can be missing from it entirely. That pointer has nothing to do with which profile holds the F95Zone session, and starting any Firefox — including moxie's own download engine on its temp profile copy (`internal/browserresolve`) — rewrites it.

So `readExtraFirefoxCookies` globs `<root>/*/cookies.sqlite` across the standard roots (`~/snap/firefox/common/.mozilla/firefox`, `~/.mozilla/firefox`, `~/.mozilla/firefox-esr`, `~/.config/mozilla/firefox`) and reads every profile kooky did not already cover, via `firefox.ReadCookies` (the same binary-level B-tree reader). The snapshot therefore spans all profiles regardless of `profiles.ini`.

For Firefox, kooky reads `cookies.sqlite` at the binary level using its own B-tree parser — it does **not** use a SQL driver. This is critical because Firefox's `cookies.sqlite` is always in WAL mode and locked by the browser process; a SQL driver would fail with "database is locked." kooky's binary-level reading bypasses the SQLite driver entirely.

`selectF95Cookies` then picks the F95Zone cookies to send: `xf_user`/`xf_session` are only valid as a pair, so the store holding the newest login cookie wins outright and only cookie names it lacks are filled in from the other stores (e.g. a `cf_clearance` held by a profile that is not logged in). Without a login cookie anywhere, the cookies are merged by name as before.

**Known limitation (kooky v0.2.9):** on Windows, Chrome/Chromium/Brave/Edge **≥ 127** encrypt cookie values with App-Bound Encryption (v20) — a per-user DPAPI layer bound to the browser's own service, which kooky cannot decrypt. Those browsers yield no cookies, so extraction degrades to the Firefox driver (whose `cookies.sqlite` format is unaffected). Firefox remains the recommended browser for moxie's cookie-based scraping on Windows.

The F95Zone filter keeps `f95zone.to` domain cookies, sorts them by name for deterministic header construction, and joins them into a Cookie header string: `"xf_session=abc; xf_user=def; cf_clearance=ghi"`.

### Download-host cookies (`GetCookiesForHost`)

`GetCookiesForHost(hostname)` returns a Cookie header with all cookies the browser holds for a host (e.g. `cf_clearance` + `__cf_bm` for buzzheavier.com). Matching follows the RFC 6265 domain-match rule — a cookie stored for `.buzzheavier.com` is returned for `dd.buzzheavier.com` but never for an unrelated host. The header is sorted, deduped to the newest value per name, and capped at 6 KB (some servers reject larger headers; `cf_clearance` alone is ~1-2 KB).

The downloader's `HostResolver` attaches these cookies automatically: every resolver request (GET/POST) and the final download request merge in the browser's cookies for the request's own hostname. This is what lets downloads pass Cloudflare's challenge for hosts the user has visited in a browser — no clearance, no extra config. Extraction is best-effort: a missing cookie store or a host the user never visited simply means no cookie is attached and the request proceeds as before.

## Why

**kooky over ncruces/go-sqlite3 for cookie reading** — The original approach tried using ncruces/go-sqlite3 to open Firefox's `cookies.sqlite` directly. This failed because Firefox keeps its cookie database in WAL mode with an active WAL file and a shared-memory file (`cookies.sqlite-wal` and `cookies.sqlite-shm`). A SQL driver requires exclusive access to flush the WAL; without it, queries return stale or empty results. kooky's binary-level B-tree parser reads committed data directly from the main database file, ignoring the WAL entirely.

**kooky over cookie file parsing** — F95Zone cookies (`cf_clearance`) contain Cloudflare challenge tokens that expire after the browser session or a few days. Re-extracting from the browser on each run ensures the token is fresh. Manual cookie export would need to be repeated every time the token expires. The same applies to download hosts: `cf_clearance` is IP-bound and short-lived (roughly 30 min-2 h), so the 60 s cache keeps downloads using a token that is still valid.

**Profiles enumerated from disk, not profiles.ini** — kooky's Firefox driver trusts `profiles.ini`, but on current Firefox that file is rewritten to point at whichever profile was activated last (verified on Firefox 158: `[Profile0]` flipped between two open profiles, and starting moxie's own headless download engine moved it again). Reading only the listed profile means moxie intermittently reads a profile with no F95Zone login — or none at all — while the logged-in profile sits unread. Globbing every profile directory makes the snapshot independent of that pointer.

**Firefox as the primary target** — Firefox has the most portable cookie store format (SQLite on all platforms). Chrome-family browsers use a variety of formats (SQLite with AES-256-GCM encryption on modern versions, old SQLite on some platforms). kooky handles both, but Firefox is the most reliable across all three target OSes.
