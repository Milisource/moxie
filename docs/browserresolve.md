# browserresolve — browser-backed download engine (design)

`internal/browserresolve` performs challenge-graded downloads inside a real
browser: Cloudflare Turnstile/managed-challenge zones refuse Go's TLS
fingerprint and require a Turnstile proof that only a real browser can
produce. The browser runs on a **copy** of the user's live profile, so the
request carries the same IP + User-Agent + TLS fingerprint the clearance was
issued to, and the file is downloaded by the browser itself.

This document is the **browser-agnostic / OS-agnostic** design: the fallback
must work with whatever major browser the user has (Chrome, Chromium, Edge,
Brave, Firefox) on any of the three target OSes (Linux, macOS, Windows).

## Current state (2026-08-09, implemented + live-verified)

- **Chrome-family engine** (rod, CDP): implemented, tested offline.
  - Binary discovery: PATH (`google-chrome`, `google-chrome-stable`,
    `chromium`, `chromium-browser`, `microsoft-edge`, `brave-browser`,
    `chrome`) + rod's own launcher discovery (macOS `.app` bundles,
    Playwright caches).
  - Profile discovery (`discoverProfileDir` in `profile.go`): Windows
    `%LOCALAPPDATA%` roots for Google Chrome, Microsoft Edge,
    BraveSoftware/Brave-Browser; macOS `~/Library/Application Support` for
    the same three; Linux `~/.config/google-chrome`, `chromium`,
    `microsoft-edge`, `BraveSoftware/Brave-Browser`.
  - Profile copy: skips `Singleton*`, `*.lock`, `*-journal`; **keeps
    `-wal`/`-shm`** (fresh cf_clearance lives in the WAL). Skips cache dirs.
  - Download: `Browser.setDownloadBehavior(allowAndName, eventsEnabled)`,
    download-event tracker with GUID first-wins + `pollDownloadDir` fallback
    (rod#971 target=_blank downloads never emit events).
- **Firefox engine** (raw-launch, zero new deps): implemented, tested, and
  **live-verified 2026-08-09** — real Firefox 153.0.1 headless downloaded a
  1 MiB payload via the full pipeline (profile discovery from `profiles.ini`
  `Default=1` → copy → user.js download prefs → `firefox --headless
  -profile <copy> -no-remote <url>` → `.part`-aware download-dir polling →
  verify → move → teardown kills the process tree). Headless→headful
  escalation (xvfb-run on display-less Linux) tested with a fake binary.
  Overrides: `MOXIE_FIREFOX_BIN`, `MOXIE_FIREFOX_PROFILE_DIR`.
- **Selection + wiring (F95-675j): implemented + live-verified.**
  `WithBrowser("auto"|"chrome"|"firefox")` (or `MOXIE_BROWSER` env /
  `browser_fallback` config key; `off` disables); auto order =
  cookie-holding browser for the host (kooky per-browser) → Chrome-family →
  Firefox. **F95Zone masked URLs (`/masked/`) prefer the rod engine** — the
  reCAPTCHA-gated Continue interstitial needs clicks. `InstallDownloaderFallback()`
  wires the downloader's `SetDefaultBrowserFallback` hook (TUI download,
  CLI `download`, desktop app startup): the hook fires at most once per
  download when the Go path hits a Cloudflare challenge (Cf-Mitigated /
  403 challenge body at the file hop, or a challenge/captcha failure at the
  resolver stage), and the browser performs the download itself into the
  destination dir. Go path stays primary.
- **Click automation + cookie injection (F95-1ng2 core): implemented +
  live-verified 2026-08-09** with a real Chromium:
  - `clickDownloadTriggers` drives click-required flows: F95Zone masked
    "Continue" (`a.host_link`), file-host free-download buttons (text /
    `href` containing download, `id` containing download — pixeldrain icon
    buttons), and the **reCAPTCHA checkbox** inside its iframe (headless
    sessions get the "I'm not a robot" challenge instead of the invisible
    auto-pass). Click chain bounded (5 clicks), auto-download pages never
    delayed (tracker `started()` bail).
  - **Session cookies injected via CDP** (`Browser.SetCookies`) from every
    browser store (kooky) for the target host — a Chromium session carries
    cookies that live in the user's Firefox (and vice versa), so the masked
    flow works on machines whose session is not in a Chrome-family profile.
  - Live: masked Continue → host Download button → file downloaded and
    byte-verified; reCAPTCHA-checkbox fixture likewise. The REAL Meltys
    Quest run (2026-08-09): F95Zone's masked captcha wall is
    **intermittent** — the Go unwrap answered `ok` on the winning attempt
    and the whole pipeline (unwrap → pixeldrain API → 1.17 GB → extract →
    merge) completed without a browser.
- **Masked-URL captcha defenses, self-hosted (Tier 1 + 2, 2026-08-09):**
  the wall is rate-budgeted (live A/B: ok, ok, wall, wall, wall, wall).
  Tier 1 (downloader): `unwrapMasked` paces unwrap POSTs (3s min
  interval), retries a walled unwrap with backoff {2s,5s,15s,45s}, then
  invokes the `maskedSolver` hook. Tier 2 (browserresolve):
  `ResolveMaskedURL` drives the interstitial in the user's browser — click
  Continue, click the reCAPTCHA checkbox if it appears, return the
  destination URL (nothing downloads; Go fetches with progress/resume).
  Wired via `SetDefaultMaskedSolver` in `InstallDownloaderFallback` (3 min
  bound). Live-verified against the real masked pixeldrain link (~4s
  solve). **Stealth:** `go-rod/stealth` JS injected on a blank page before
  navigation (bot.sannysoft.com 31/31; the
  `--disable-blink-features=AutomationControlled` launch flag was
  rejected — it core-dumps Playwright Chromium on profiles with
  conflicting blink prefs, e.g. Brave). **Profile copy fix:** `Sync Data`
  excluded from copies — a Brave sync LevelDB deterministically core-dumps
  vanilla Chromium 2-4s after launch (had been silently killing masked
  sessions all day; looked like navigation races).
- **No browser is shipped or downloaded** — both engines launch the user's
  own installed browser (`launcher.New()`, never rod's `NewBrowser()`
  download path); default is auto-detect on the user's own installs.
- **Known limitation:** raw-launch Firefox only auto-downloads — hosts whose
  pages need a button click (vikingfile/datanodes free-download forms) time
  out unless the resolved URL starts the download on navigation. Click
  automation is a future rod/CDP enhancement (see F95-* follow-up).

## Firefox engine (raw-launch, zero new deps)

Instead of CDP (rod does not support Firefox; playwright-go would add ~50 MB
driver + ~90 MB browser download), launch the user's **own Firefox binary**
on a copied profile and watch the download directory. Firefox's download
prefs are profile-based, so no driver is needed at all.

### Discovery (per OS)

Profile root (contains `profiles.ini`):

| OS | Path |
|----|------|
| Linux | `~/.mozilla/firefox/` |
| macOS | `~/Library/Application Support/Firefox/` |
| Windows | `%APPDATA%\Mozilla\Firefox\` |

Pick the profile from `profiles.ini` with `Default=1` (fallback: first
`[ProfileN]` with `IsRelative=1`; resolve `Path=` relative to the root).
Override env: `MOXIE_FIREFOX_PROFILE_DIR` (mirrors
`MOXIE_CHROME_PROFILE_DIR`).

Binary:

| OS | Candidates |
|----|------------|
| Linux | `firefox`, `firefox-esr` on PATH; flatpak/snap paths noted but not auto-probed |
| macOS | `/Applications/Firefox.app/Contents/MacOS/firefox`, `~/Applications/Firefox.app/Contents/MacOS/firefox` |
| Windows | `%ProgramFiles%\Mozilla Firefox\firefox.exe`, `%ProgramFiles(x86)%\Mozilla Firefox\firefox.exe`, `%LOCALAPPDATA%\Mozilla Firefox\firefox.exe` |

Override env: `MOXIE_FIREFOX_BIN` (mirrors `WithBinPath`).

### Profile copy

Reuse `copyProfile`/`skipProfileFile` with Firefox additions:

- **Skip**: `.parentlock` (Linux/Windows), `parent.lock` (macOS) — the
  existing `*.lock` suffix rule catches `parent.lock` but not `.parentlock`;
  `cache2/`, `startupCache/`, `OfflineCache/` (multi-GB caches, rebuilt).
- **Keep**: `cookies.sqlite` + `cookies.sqlite-wal`/`-shm` (the clearance!),
  `permissions.sqlite`, `prefs.js` (do not copy `user.js` over — see below),
  `places.sqlite`.
- Windows: if Firefox is running, `cookies.sqlite` may be exclusively locked
  → surface `ErrProfileLocked`-style error with "quit Firefox first"; Linux/
  macOS tolerate a live copy (WAL is copied and recovered on open).

### Download prefs (`user.js` in the copy)

```js
user_pref("browser.download.folderList", 2);                      // custom dir
user_pref("browser.download.dir", "C:/Users/x/AppData/Local/Temp/..."); // forward slashes, even on Windows
user_pref("browser.download.useDownloadDir", true);
user_pref("browser.download.manager.showWhenStarting", false);
user_pref("browser.helperApps.neverAsk.saveToDisk", "application/zip,application/x-zip-compressed,application/octet-stream,application/x-7z-compressed,application/x-rar-compressed,application/gzip,application/x-tar");
user_pref("browser.download.always_ask_before_handling_new_types", false);
user_pref("browser.startup.page", 0);
user_pref("browser.sessionstore.resume_from_crash", false);
```

Windows paths in `browser.download.dir` must use forward slashes
(`C:/Users/…`) — backslashes are escape characters in prefs files.

### Launch

```
firefox --headless -profile <copy> -no-remote <url>
```

- `-profile` (single dash) with an explicit dir; `-no-remote` prevents
  hand-off to a running Firefox instance.
- **Never `-private`**: private-browsing mode omits the `session_ticket` +
  `psk_key_exchange_modes` TLS extensions (uTLS PR #399 observation), which
  changes the TLS fingerprint away from the normal-mode session that minted
  the clearance.
- The URL is the resolved download URL (share-page `/download` hop, DD URL,
  etc.). The browser follows redirects and starts the download natively.
- Turnstile escalation: if no file appears within `DownloadTimeout`
  (challenge page saved as HTML), retry **headful** (visible window — the
  user's desktop). On display-less Linux, `xvfb-run -a firefox …` (mirrors
  the existing `WithHeadful` semantics).
- Teardown must kill the whole process tree: Linux/macOS via process group
  (`SysProcAttr{Setpgid:true}` + `kill(-pid)`); Windows via
  `taskkill /T /F /PID <pid>` — Firefox spawns content processes.

### Download detection

Reuse `pollDownloadDir`/`stableDownloadFile`; add `.part` to the ignored
partial suffixes (Firefox writes `<name>.part` until complete, then renames).
The existing GUID/event tracking does not apply — no CDP.

## Browser selection (browser-agnostic)

Order of preference (all OSes):

1. **The browser that holds cookies for the target host.** `kooky` can say
   which store has `cf_clearance` — a clearance is bound to the minting
   browser's fingerprint, so that browser is the guaranteed match.
2. Chrome-family (rod path — battle-tested download events), if present.
3. Firefox (raw-launch path), if present.
4. Any browser found; challenge may need headful escalation.

Config: `MOXIE_BROWSER=auto|chrome|firefox` (auto = the order above), plus
the existing `MOXIE_CHROME_PROFILE_DIR` / new `MOXIE_FIREFOX_PROFILE_DIR`
overrides. Errors stay typed (`ErrNoChrome` → rename/generalize to
`ErrNoBrowser`, keep `ErrNoChrome` as an alias for compat).

## Cross-OS pitfalls checklist

| Pitfall | Handling |
|---------|----------|
| Windows locks on live `cookies.sqlite` | Surface "quit Firefox first" (typed error); WAL copy is best-effort |
| Windows paths in `user.js` | Forward slashes only |
| Windows process tree | `taskkill /T /F` |
| macOS binary is inside `.app` | Probe `Contents/MacOS/firefox` explicitly |
| Linux headless without display | `xvfb-run -a` fallback |
| `.parentlock` vs `parent.lock` naming | Both skipped |
| Firefox private mode changes TLS fingerprint | Never `-private` |
| `.part` suffix in download dir | Ignored by `stableDownloadFile` |
| Multiple Firefox profiles | `profiles.ini` `Default=1` selection |
| Edge/Brave profile roots | Add `%LOCALAPPDATA%\Microsoft\Edge\User Data`, `…\BraveSoftware\Brave-Browser\User Data`, Linux `~/.config/microsoft-edge`, `~/.config/BraveSoftware/Brave-Browser` to Chrome-family discovery |

## Verification plan

> **Scope update (2026-08-09, live-verified)**: buzzheavier **resolution** no
> longer needs a browser at all — the share page's `hx-get` embeds a
> server-signed `t=` token, and `GET /download?t=<t>&alt=true` yields a
> fafda.to direct URL (see [research-buzzheavier-cloudflare.md](research-buzzheavier-cloudflare.md),
> implementation F95-hs4y). The Firefox engine's job shrinks to: (a) true
> Turnstile hosts (vikingfile, datanodes, mixdrop), and (b) the fafda.to
> **file-bytes** hop when the origin outage (503) persists — the browser
> mints the token via the Turnstile flow, Go downloads the bytes
> (challenge-free zone, resume + progress).

1. ✅ Live: **Firefox engine** headless against a local server — real
   Firefox 153.0.1 downloaded 1 MiB via the full pipeline (1.65 s);
   headless→headful escalation tested with a fake binary.
2. ✅ Live: **`InstallDownloaderFallback`** production wiring — installed
   with `MOXIE_BROWSER=firefox`; the Go path stays primary and only
   challenge responses trigger the browser.
3. ⚠️ Vikingfile/datanodes E2E needs **click automation** — the raw-launch
   engine only auto-downloads URLs that start a download on navigation;
   free-download-button hosts need a rod/CDP interaction pass (future work).
4. OS matrix at least on Linux (this machine) + one Windows/macOS smoke test
   before release.
