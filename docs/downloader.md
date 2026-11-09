# Downloader

> **Status: beta, with 11 verified host paths and optional browser resolvers.** HTTP-only downloads are verified for Pixeldrain, MediaFire, Bunkr, Dropbox, Google Drive, WorkUpload, Gofile and WDHO; MEGA is verified through the `megatools` subprocess. Buzzheavier, MixDrop, UploadHaven, VikingFile and DataNodes are fully automated with the optional headed-browser resolver (`--browser`). F95Zone masked links work with cookies imported from Chromium-family browsers; a pending reCAPTCHA still needs a human. See [Host support matrix](#host-support-matrix) for the per-host evidence.

## What

The downloader package (`internal/downloader/`) fetches game files from F95Zone-approved file hosts. It handles URL resolution, F95Zone link unmasking, progress reporting, resume support, SSRF protection, dead-link validation, and optional delegation to external downloaders.

Package files:

| File | Purpose |
|------|---------|
| `types.go` | `Status`, `Progress`, `Job`, `ProgressEvent` |
| `downloader.go` | `Download`, `DownloadWithHost`, resume, validation, HTML-game handling |
| `platform.go` | Platform enum and OS-priority matching |
| `hosts.go` | Resolver dispatch, F95 masked link unmasking, host identification, in-memory resolution cache |
| `hosts_cloud.go` | MediaFire, Bunkr, Dropbox resolvers |
| `hosts_workupload.go` | WorkUpload proof-of-work + direct-link resolver |
| `hosts_f95dl.go` | F95Zone `/dl/` wrapper links |
| `browser.go` | Optional chromedp headed-browser resolver (VikingFile Turnstile) |
| `resolution_store.go` | Persistent masked-link cache interface |
| `hosts_*.go` | Per-host resolvers (Pixeldrain, Buzzheavier, Gofile, DataNodes, Google Drive, MixDrop, VikingFile, MEGA) |
| `external.go` | External downloader adapters (megatools) |
| `hosts_helpers.go` | Shared parsing helpers (`decodeJSON`, Content-Disposition, link extraction) |
| `hostsizes.go` | Per-host size limits |
| `links.go` | `IsOnlineOnly`, `ScoreDownloadLink`, host scoring, newest-file lookup |
| `detect.go` | Platform detection from link text |
| `validator.go` | SSRF protection and dead-link checks |

## How

### Download flow

```go
downloader.Download(urlStr, destDir, expectedTotal, onProgress, f95Cookie)
// or with an explicit host:
downloader.DownloadWithHost(urlStr, host, destDir, expectedTotal, onProgress, f95Cookie)
```

1. **SSRF check** — HTTPS only; private, loopback, link-local and cloud-metadata hosts are blocked (`isValidDownloadURL`).
2. **Link resolution** — `HostResolver.Resolve()`:
   - F95Zone masked links (`/masked/...`) are unmasked through the site's JSON endpoint.
   - F95Zone `/dl/...` wrapper links are followed; a plain redirect is used, an hCaptcha interstitial produces a clear error.
   - Host-specific resolvers handle APIs, cookies, POST forms and signing.
   - MEGA links return `ExternalCommand: megatools` and are downloaded by `downloadWithExternal`.
3. **Resume** — an existing `.part` file produces `Range: bytes=<size>-`; a `206 Partial Content` response is appended.
4. **Transfer** — 32 KiB read loop with 500 ms progress callbacks, 50 GB hard cap, 6 hour timeout.
5. **Content validation** — HTML responses are rejected unless they are genuine HTML games (see below).
6. **Filename** — `Content-Disposition` is preferred for fresh downloads, so Pixeldrain `/u/<id>` links get their real name/extension.
7. **Finalize** — `fsync`, rename `.part` → final, `chmod 0600`.

### F95Zone masked links

Masked links used to be treated as redirects. They are not: the site serves a
JavaScript page and exposes a JSON endpoint. `masked.js` performs:

```http
POST https://f95zone.to/masked/<host>/...
Cookie: xf_user=...; cf_clearance=...
X-Requested-With: XMLHttpRequest
Content-Type: application/x-www-form-urlencoded

xhr=1&download=1
```

Successful response:

```json
{"status":"ok","msg":"https://pixeldrain.com/u/f9USE4aK"}
```

Other responses:

```json
{"status":"captcha","msg":"Please complete the CAPTCHA to continue"}
{"status":"error","msg":"You are not logged in"}
```

`resolveMasked` implements this POST, validates the returned URL, and caches
successful mappings in two layers:

1. in-memory for the current process (12 hours),
2. persistently in the SQLite `resolved_urls` table (12 hours), so repeated
   `moxie download` runs reuse the unmasking without touching F95Zone.

The cache matters because F95Zone rate-limits unmasking and escalates to an
interactive CAPTCHA after a handful of automated requests. A trial run resolved
`pixeldrain.com/u/f9USE4aK` on the first request; subsequent requests in the
same session returned `captcha`. After roughly 30 minutes the limit reset: the
same path resolved to a MEGA folder link (`mega.nz/#!...`), and a second attempt
resolved to a Pixeldrain mirror and downloaded 24.9 MB of
`A Red Flower Shining in the Moonlight-v1.02-Deepseek-Compressed.7z` before the
test timeout. The endpoint appears to rotate among the thread's mirrors.

A live `moxie download 7` run confirmed persistence: `resolved_urls` grew from
18 to 19 rows with
`https://f95zone.to/masked/pixeldrain.com/256438/...` → `https://pixeldrain.com/u/BYzajuVk`,
and the download started immediately from that cached URL.

On 2026-11-08 cookie extraction was widened to Chromium-family browsers.
`internal/browser` now scans Helium, Chrome/Chromium, Brave, Edge, Playwright
profiles and other `~/.config` Chromium stores through kooky's encrypted-cookie
reader, in addition to Firefox. With the Helium profile logged into F95Zone the
masked POST succeeded and returned real mirrors:

```text
masked/pixeldrain.com/277896/... -> {"status":"ok","msg":"https://pixeldrain.com/u/BJzfpdhX"}
masked/gofile.io/277896/...     -> {"status":"ok","msg":"https://gofile.io/d/ZK4v1p"}
masked/mega.nz/277896/...       -> {"status":"ok","msg":"https://mega.nz/#!..."}
```

A range GET of the unmasked Pixeldrain URL returned **206**, 1024 bytes,
`application/zip`, `Content-Disposition: ...Crowb4rd_Collection_2024_-_2025-10.zip`
and `PK\x03\x04`. Unmasking one link and then immediately requesting more in the
same session returned `{"status":"captcha",...}` and the check stayed sticky for
at least 15 minutes, so the cache and link fallback remain essential.

**Remaining blocker:** F95Zone's masked endpoint and `/dl/` check both raise an
interactive reCAPTCHA/hCaptcha after automation is detected. `moxie` cannot and
should not solve that captcha. With `--browser`, `resolveMaskedBrowser` opens the
masked page in a headed browser, injects the same F95 cookies, clicks the host
link, and captures the document navigation to the real host URL once the
challenge is clear. Live evidence (challenge temporarily clear): the browser
captured `https://pixeldrain.com/u/ysLVsEEB` from a masked pixeldrain link in
~2 seconds. When the challenge is pending the resolver waits up to four minutes
for a human to complete it, then reports that the captcha was not cleared.
Mitigations:

- resolution cache and 12-hour reuse limit repeated calls;
- F95Zone's rate limit is temporary — pausing and retrying works;
- the fallback loop tries direct/non-masked links first;
- `moxie download --open --watch` hands the link to a real browser and watches
  the download directory for the completed file.

### F95Zone `/dl/` links

Current F95 threads wrap links as `/dl/<postID>/?u=…&k=…&r=…&t=…`. The wrapper
sometimes 302s straight to the host. When it does, `resolveF95DL` re-runs host
resolution on the final URL. Otherwise the page is either:

- `Just a quick check` — invisible hCaptcha (browser required),
- `That link is no longer in the post` — stale link.

Both produce descriptive errors instead of downloading HTML.

Because `/dl` tokens are tied to the page that rendered them, `moxie download`
re-scrapes the thread when every stored link is a `/dl/` wrapper instead of
reusing stale tokens. This is the only way `/dl` links stay usable over time.

### HTML games

F95Zone hosts many Twine/SugarCube/HTML releases. `downloadWithHeaders` now
accepts `text/html` when the filename (from `Content-Disposition` or the URL)
ends in `.html`, `.htm`, or `.swf` **and** `Content-Length >= 64 KiB`.
`IsValidGameFile` applies the same rule. Smaller HTML responses are still
treated as captcha/interstitial pages and rejected.

### Host support matrix

Evidence collected 2026-11-08 on Linux against real links. “Verified” means a
range or full download returned real bytes with the expected magic/type.

| Host | Method | Status | Evidence |
|------|--------|--------|----------|
| **Pixeldrain** | `GET https://pixeldrain.com/api/file/<id>` | ✅ Verified | 206 + `PK\x03\x04`; full 4.9 MB HTML game downloaded and named from Content-Disposition |
| **MediaFire** | Parse `downloadNNN.mediafire.com` link from file page | ✅ Verified | 206 + ZIP; full 497 KB archive downloaded with correct name |
| **Bunkr / Bunkrr** | Page → `dl.bunkr.*/file/<id>` → `POST /api/_001_v2` → `glb-apisign.cdn.cr/sign` | ✅ Verified | 206 + `Rar!` from a 1.1 GB file; token/`ex` flow works |
| **Dropbox** | Append `dl=1` | ✅ Verified | Full 32.5 MB `.odt` downloaded, `application/binary` |
| **Google Drive** | `drive.usercontent.google.com/download?id=…&export=download&confirm=t` (+ `uuid` when present) | ✅ Verified | 206 + first bytes from a 1 GB public test file; handles both old `confirm=` and new hidden-form pages |
| **MEGA** | `megatools dl` subprocess | ✅ Verified (external tool) | 1.28 MB transferred in 25 s from a public link; resume handled by megatools |
| **WorkUpload** | Solve SHA-256 proof-of-work, then parse direct link | ✅ PoW verified | Live `/puzzle` solved (3 targets in range 10 000) and `/captcha` accepted; test file had been deleted |
| **Gofile** | `POST /accounts` → `GET /contents/<id>` with `X-Website-Token` (SHA-256 of UA::language::token::4h-bucket::salt) | ✅ Verified | Uploaded a 50-byte file to `gofile.io/d/krheSqXq`; resolver returned `store5.gofile.io/download/...`, GET 200, exact 50 bytes and correct filename |
| **Buzzheavier** | Page → HTMX `GET <id>/download?t=…` → `hx-redirect` | ✅ Verified with `--browser` | Live `bzzhr.to/wp0penkq05sy`: browser read the signed endpoint and returned `ts.bzzhr.to/d/...`; GET 206, 256 bytes of `PK\x03\x04` ZIP (417 MB file) |
| **DataNodes** | Optional headed-browser resolver: click `#method_free`, wait for invisible Turnstile, click Free Download, capture `nodeNN.datanodes.to:8443/d/...` | ✅ Verified with `--browser` | Fresh upload `datanodes.to/swyya9wz61je` resolved in ~16 s; full 145-byte ZIP downloaded and validated (`PK\x03\x04`); cached second run ~1 s |
| **MixDrop** | File page → `grecaptcha.execute('6LetXao…')` → `POST a=genticket` → JSON `url` | ✅ Verified with `--browser` | Live `mxdrop.click/e/vkkl8zlzhq88gjl`: resolver returned `a-delivery38.mxcontent.net/d/...`; GET 206, 256 bytes `ftypisom` MP4 (1.42 GB file) |
| **KrakenFiles** | Optional headed-browser resolver: wait Turnstile + FingerprintJS, POST `/download/<hash>`, read JSON `{url}` | ⚠ Browser implemented / server-blocked | Valid Turnstile, fingerprint and behavior data sent; every tested live file returned `{"status":"error","msg":"Server error"}` (3 files, 95 s wait, mouse activity) — server-side refusal from this network |
| **VikingFile** | Optional headed-browser resolver → `/d/...` → presigned S3 | ✅ Verified with `--browser` | Turnstile auto-solved in a real Chromium; `#download-link` returned a `/d/` URL that 302s to a presigned S3 object; full 989 MB `.7z` downloaded in ~40 s |
| **UploadHaven** | Browser waits for countdown + Turnstile, submits the form, reads the manual download link | ✅ Verified with `--browser` | Live `uploadhaven.com/download/96db…`: resolved in ~18 s to `download141.uploadhaven.com/.../KuroyaraCollection2025-05.zip`; GET 206, 256 bytes of `PK\x03\x04` ZIP (581 MB file) |
| **WDHO** | Referer header → file page → base64 `e='…'` signed URL → wait out countdown → GET signed URL | ✅ Verified | Live `wdho.ru/kbhw`: page returned the signed `?pt=…` URL; after the countdown the GET returned 200, 256 bytes `PK\x03\x04`, `Content-Disposition: result.zip`, 7,985,981-byte ZIP. The colour-button challenge only appears without a Referer; the browser resolver waits at it if it does. |
| **Catbox / Quax / Vern / files.dp.ua** | Direct HTTP GET | ⚠ Pass-through | Catbox samples were 404 (deleted); no live samples available |
| **transfer.sh / yourfilestore / delafil** | Direct HTTP GET | ❌ Dead in testing | `transfer.sh` and `yourfilestore.com` timed out / DNS failed; Delafil returned an error page |
| **Files.fm / UploadNow / FromSmash / SendGB / DropMeFiles / others** | JS UI or account API | ❌ Browser | Not automatable with plain HTTP |

### Host scoring

`ScoreLinkHost` feeds the fallback ordering:

| Score | Hosts |
|-------|-------|
| **+25** | Pixeldrain, Gofile, Catbox, MediaFire, Bunkrr, Dropbox, WDHO; Buzzheavier when `--browser` is enabled |
| **+10** | Google Drive, WorkUpload, MEGA (when `megatools` is installed), MixDrop/UploadHaven with `--browser`, DataNodes/VikingFile/KrakenFiles with `--browser` |
| **+5** | Buzzheavier without `--browser` (Cloudflare often rejects plain HTTP) |
| **0** | Other recognized hosts |
| **-50** | MixDrop/UploadHaven, DataNodes/VikingFile/KrakenFiles when `--browser` is disabled |
| **-200** | MEGA when `megatools` is missing |

### External downloader: MEGA

MEGA uses RSA/AES-CTR, so it cannot be fetched with a plain HTTP GET.
`resolveMega` checks `exec.LookPath("megatools")` and returns
`ExternalCommand` when available; `downloadWithExternal` runs:

```bash
megatools dl --path <destDir> --no-progress --no-ask-password <url>
```

It polls the destination directory once per second for progress and reports the
newest file when the command exits. Install with `apt install megatools`,
`brew install megatools`, or `pacman -S megatools`. Without it, MEGA links score
-200 and clearly report the missing dependency.

### WorkUpload proof-of-work

WorkUpload's “Are you a human?” page is not a captcha; it is SHA-256 grinding:

1. `GET /puzzle` → `{"data":{"puzzle":"…","range":10000,"find":["<sha256>",…]}}`
2. For `i` in `[0, range)`, compute `sha256(puzzle + i)` until it matches every
   hash in `find`.
3. `POST /captcha` with `captcha="<i1> <i2> … "`.
4. Reload the file page and read the direct download link.

`hosts_workupload.go` performs this with the standard library, using a cookie
jar for the session. A live run solved 3 targets and the server accepted the
captcha (the sample file itself had been deleted).

### Optional browser resolver (Buzzheavier, MixDrop, UploadHaven, WDHO, F95 masked, VikingFile, DataNodes, KrakenFiles)

Turnstile does not issue a token to headless Chromium or to plain HTTP clients.
Moxie therefore has an opt-in headed-browser resolver implemented with
`chromedp` (Chrome DevTools Protocol, pure Go, no Node dependency):

```bash
moxie download --browser <id>      # or MOXIE_BROWSER=1 moxie download <id>
MOXIE_BROWSER=/path/to/chrome moxie download --browser <id>
```

Flow:

1. With `--browser` enabled, browser-gated hosts (Buzzheavier, MixDrop,
   UploadHaven, WDHO, F95 masked links, VikingFile, DataNodes, KrakenFiles) go
   straight to the browser resolver because their HTTP flow stops at a
   challenge. Other hosts use the normal HTTP resolver.
2. Launch a visible Chromium-family browser with a dedicated profile under
   `~/.cache/moxie/browser-profile-<host>`.
3. Drive the host's page until Turnstile auto-solves and a direct URL appears.
4. Return that URL; the normal HTTP downloader fetches it with Range/resume.

Live evidence: the resolver obtained the link in ~8 s and downloaded the full
**989,346,221-byte** `RJ01316694-Red_Flower_Shining_in_the_Moonlight.7z` in
about 40 seconds (≈25 MB/s). The browser window closes when the resolver
returns; the download itself is plain HTTP. Resolved `/d/` links are cached in
`resolved_urls` for 12 hours, so a second run returned the same URL in **0.095 s**
without launching the browser.

**DataNodes** uses the same browser session for its two-step flow:

1. wait for `#method_free` to be enabled (the virus-scan component arms it
   after ~6 s) and submit step 1 (`op=download1`),
2. wait for step 2's invisible Turnstile token,
3. click **Free Download**, and capture the `nodeNN.datanodes.to:8443/d/...`
   request that the page issues.

The captured node URL is a normal HTTP download with Range support. Live
evidence: a freshly uploaded `https://datanodes.to/swyya9wz61je` resolved in
~16 s and the full 145-byte ZIP downloaded and validated (`PK\x03\x04`); the
cached second run completed in ~1 s without opening the browser. DataNodes uses
its own browser profile and clears cookies before each run, because its step-1
token is single-use.

**KrakenFiles** is wired to the same framework:

1. wait for Turnstile + FingerprintJS visitor ID,
2. populate the behavior payload (`mousemove`, focus gaps, telemetry),
3. POST `/download/<hash>` and parse the JSON response.

The browser side works: valid Turnstile tokens (709 chars), fingerprints and
behavior data were sent. The server returned
`{"status":"error","url":"","msg":"Server error"}` for every live file tested
(3 files, including a 95-second wait and real mouse movement), so no range
download could be verified. This looks like a server-side/IP refusal rather than
a client-flow bug; the resolver reports the server message instead of pretending
to succeed.

**Buzzheavier** — the file page exposes
`<a class="download-btn" hx-get="/<id>/download?t=<token>">`. The resolver first
tries the HTMX request over plain HTTP; when Cloudflare answers 403 (as it did
from this network) it opens the page in the browser, performs the same request
from page context and reads the `hx-redirect` header. Live: 206 + 256 bytes of
`PK\x03\x04` from a 417,042,839-byte ZIP.

**MixDrop** — the download view (`/f/<id>?download`) posts
`{csrf, token, a:"genticket"}` after executing the site's invisible reCAPTCHA v3
key (`6LetXao…`). The JSON response carries the signed
`a-deliveryNN.mxcontent.net/d/<id>/<token>` URL. Live: 206 + 256 bytes
`ftypisom` MP4 from a 1,522,001,145-byte file. MixDrop moved off `mixdrop.co`
(parked domain) to `mixdrop.ag` → `mxdrop.click`; `/e/` URLs are converted to
`/f/` automatically.

**UploadHaven** — the page combines a ~20 s countdown with an invisible
Turnstile. The resolver polls until `#submitFree` becomes `type=submit`, submits
the form directly (ad overlays make clicking unreliable), then reads the manual
link (`downloadNNN.uploadhaven.com/...?key=…&expire=…`). Live: 206 + 256 bytes
of `PK\x03\x04` from a 581,886,038-byte ZIP.

**F95Zone masked links** — on a `captcha` response, `resolveMaskedBrowser`
opens the masked page, injects the caller's F95 cookies through CDP, clicks
`.host_link`, and listens for the document navigation to a known file host. It
never interacts with the reCAPTCHA. Live (challenge temporarily clear): captured
`https://pixeldrain.com/u/ysLVsEEB` in ~2 s. If the challenge is pending, the
resolver waits up to `browserChallengeTimeout` (four minutes) and then reports
that it was not cleared.

**WDHO** — the normal HTTP path now works: a `Referer` header makes the site
serve the real file page instead of the colour-button interstitial. The page
embeds the signed download URL as base64 in `e='…'`; the token is single-use and
only valid after the 4–8 second countdown, so the resolver waits, then hands the
URL plus the page's `filehosting` cookie and `Referer` to the normal downloader.
Live: 200 + 256 bytes of `PK\x03\x04` from a 7,985,981-byte ZIP. If an IP does
get the colour-button page, `resolveWDHOBrowser` performs the `/robots.txt`
device probe, waits for a human to clear the check, and captures the resulting
download event/link; no colour solving is attempted.

Browser discovery order: `MOXIE_BROWSER`, `google-chrome-stable`,
`google-chrome`, `chromium`, `chromium-browser`, `brave-browser`,
`microsoft-edge`, then Playwright's cached Chromium. If no browser is found the
error explains how to set `MOXIE_BROWSER`.

Hosts that still need a human:

- **F95Zone `/dl/` and masked links** — both render an interactive
  reCAPTCHA/hCaptcha after automation is detected; `--browser` waits for a human
  to clear it but does not solve it.
- **WDHO** — only when the colour-button page is served (normally bypassed by
  the Referer header); `--browser` waits for a human to clear it and captures the
  download, but does not solve the colour check.

`moxie download --open --watch` remains the fallback for hosts that need a human
to solve a CAPTCHA.

### Host-specific notes

- **Pixeldrain** — `X-Ratelimit-*` headers are present; free downloads are
  throttled (~1 MiB/s in testing). API key support is not implemented.
- **MediaFire** — Files that require a captcha show a challenge page instead of
  a `downloadNNN` link; the resolver reports that rather than downloading HTML.
- **Bunkr** — Domains rotate (`bunkr.ws`, `bunkr.cr`, `dl.bunkr.cr`,
  `psu.scdn.st`). The resolver derives all hosts from the page and CDN URL.
- **Google Drive** — Large files use a hidden form to
  `drive.usercontent.google.com/download`; both that and the legacy `confirm=`
  token are parsed.
- **Dropbox** — Shared links use `dl=0` for preview; `dl=1` is required.
- **Gofile** — the 2026 API mints an anonymous account with `POST /accounts`
  and requires an `X-Website-Token` header on `/contents/<id>`. That token is a
  SHA-256 over `userAgent::language::accountToken::floor(unix/14400)::salt`
  (`12af056dacea0b`); the salt is rotated server-side, so a 401 means refresh
  `gofileWTSalt` from `/js/wt.obf.js`. The old `getContent` parser and the
  `<id>.gofile.io` fallback host are gone.

### Masked F95Zone URLs (historical note)

Older releases stored `https://f95zone.to/masked/pixeldrain.com/...` links. The
host is identified from the embedded domain, so `IdentifyHostInURL` still routes
them correctly before unmasking.

### File size limits

`hostsizes.go` records documented maximum sizes for 41 approved hosts (Catbox
200 MB, MEGA 15 GB, Buzzheavier unlimited, etc.). Limits are checked before the
request when metadata is known, and again against `Content-Length`.

### Dead-link validation

`CheckLink`/`CheckLinkWithHost` perform a HEAD request (or host resolver) and map
404/403/410/429/5xx to descriptive errors. MEGA validation succeeds when
`megatools` is installed, since there is no HTTP HEAD to perform.

## Why

**Resolution cache and throttling** — F95Zone's anti-bot threshold is low.
Caching successful unmaskings means a batch download touches F95Zone only once
per masked URL, and repeated runs stay under the CAPTCHA trigger.

**Content-Disposition filenames** — ID-only URLs (Pixeldrain `/u/<id>`,
Bunkr CDN) otherwise save files with no extension, which breaks archive
detection, extraction, and engine matching.

**HTML-game support** — Twine/SugarCube games are a large share of F95Zone
releases; rejecting all `text/html` silently discarded valid downloads.

**MEGA via megatools** — Implementing MEGA's key exchange and AES-CTR in Go is
possible but duplicates a maintained, resume-capable implementation. The
subprocess adapter keeps `moxie` pure-Go while giving users full MEGA support
when they install one package.

## Known limitations

- **F95 CAPTCHA** — masked and `/dl/` links escalate to an interactive
  reCAPTCHA/hCaptcha under automation. No automated bypass is attempted. Use the
  cache, retry later, or `--open --watch`.
- **WDHO** — its colour-button challenge is an interactive anti-bot check; moxie
  detects it and tells you to use `--open` rather than solving it.
- **Browser resolver hosts** — with `--browser`, Buzzheavier, MixDrop,
  UploadHaven, VikingFile, DataNodes and KrakenFiles are automated (KrakenFiles
  is currently server-blocked). Without it, these links score −50 and fail fast
  with an actionable message.
- **Browser resolver requirements** — `--browser` needs a Chrome/Chromium-family
  browser and opens a visible window; `MOXIE_BROWSER` can point at a specific
  binary.
- **MEGA without megatools** — the link is skipped with an install hint.
- **Gofile website-token salt** — `gofileWTSalt` is copied from
  `/js/wt.obf.js`; Gofile rotates it periodically. A 401 from `/contents` means
  the constant must be refreshed.
- **No multi-part/segmented downloading** — single connection per download.
- **No bandwidth throttling** beyond host-side limits.
- **Pixeldrain rate limits** — free transfers are ~1 MiB/s and may redirect to a
  captcha page after repeated views; an API key path is not implemented.
- **F95 `/dl` tokens** — wrapper links are tied to the rendered page; resolving
  requires a fresh link from the same scrape.
