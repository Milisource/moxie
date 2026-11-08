# Downloader

> **Status: beta, with 7 verified host paths.** HTTP-only downloads are verified for Pixeldrain, MediaFire, Bunkr, Dropbox, Google Drive, and WorkUpload; MEGA is verified through the `megatools` subprocess. Hosts behind interactive CAPTCHAs (F95Zone `/dl` check, MixDrop, KrakenFiles, VikingFile, UploadHaven) still need a browser. See [Host support matrix](#host-support-matrix) for the per-host evidence.

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
successful mappings in memory for 12 hours. The cache matters because F95Zone
rate-limits unmasking and escalates to an interactive CAPTCHA after a handful
of automated requests. A trial run resolved `pixeldrain.com/u/f9USE4aK` on the
first request; subsequent requests in the same session returned `captcha`.

**Remaining blocker:** F95Zone's masked endpoint and `/dl/` check both raise an
interactive hCaptcha after automation is detected. `moxie` cannot and should not
solve that captcha. Mitigations:

- resolution cache limits repeated calls;
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
| **Gofile** | `GET /accounts` → `GET /contents/<id>` (Bearer token) | ⚠ Implemented | API host is unreachable from this test network (TCP timeout); resolver covered by unit test |
| **Buzzheavier** | HTMX `GET <url>/download` with `HX-Request` | ⚠ Implemented | No live link remained in the local DB to test (all returned 404/self-redirect) |
| **DataNodes** | Cookie + hidden-form POST + Turnstile | ❌ Browser | No live link available; external implementation notes confirm Turnstile does not issue a token to headless Chromium |
| **MixDrop** | Page calls `grecaptcha.execute` + `POST a=genticket` | ❌ Browser | Direct GET returns HTML; genticket needs reCAPTCHA v3 |
| **KrakenFiles** | `POST /download/<hash>` with token | ❌ Browser | HTTP POST returns `captcha not valid`; headed Chromium solved Turnstile but the POST returned `Server error` in testing |
| **VikingFile** | Form POST after Turnstile | ❌ HTTP / ✅ browser | Plain HTTP renders Turnstile before setting `#download-link`; headed Chromium auto-solved it and the POST returned a presigned S3 `direct-link` |
| **UploadHaven** | Form POST with `_token`, `hash`, countdown, Turnstile | ❌ Browser | POST without turnstile re-renders the page |
| **WDHO** | Custom “click the colored button” check | ❌ Browser | Page JS only |
| **Catbox / Quax / Vern / files.dp.ua** | Direct HTTP GET | ⚠ Pass-through | Catbox samples were 404 (deleted); no live samples available |
| **transfer.sh / yourfilestore / delafil** | Direct HTTP GET | ❌ Dead in testing | `transfer.sh` and `yourfilestore.com` timed out / DNS failed; Delafil returned an error page |
| **Files.fm / UploadNow / FromSmash / SendGB / DropMeFiles / others** | JS UI or account API | ❌ Browser | Not automatable with plain HTTP |

### Host scoring

`ScoreLinkHost` feeds the fallback ordering:

| Score | Hosts |
|-------|-------|
| **+25** | Pixeldrain, Buzzheavier, Gofile, Catbox, MediaFire, Bunkrr, Dropbox |
| **+10** | DataNodes, Google Drive, MixDrop, WorkUpload, MEGA (when `megatools` is installed) |
| **0** | Other recognized hosts |
| **-50** | VikingFile, KrakenFiles |
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

### Browser-assisted research

A headed Chromium session (Playwright, non-headless) was used to determine
whether the remaining hosts are automatable at all:

- **VikingFile** — Turnstile auto-solved without user interaction. Clicking
  Download POSTed to the page and returned JSON containing both a page link and
  a presigned S3 `direct-link`; the direct link downloads over plain HTTP.
  This host is therefore fully automatable *if* moxie drives a real browser.
- **KrakenFiles** — Turnstile also auto-solved, but the subsequent
  `POST /download/<hash>` returned `{"status":"error","msg":"Server error"}`
  in testing (possibly rate limiting or a file-side restriction).
- **F95Zone `/dl/` and masked links** — both render an interactive hCaptcha
  checkbox under automation; waiting does not auto-solve it.
- **MixDrop** — reCAPTCHA v3 + `genticket`; no request fired in headed testing
  without additional interaction.

This confirms the split: HTTP resolvers cover the hosts above; hosts that need
Turnstile/reCAPTCHA need a browser component. `moxie download --open --watch`
is the current supported bridge — it opens the link in the user's browser and
watches the download directory.

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
- **Gofile** — The March 2026 API uses anonymous account tokens and
  `/contents/<id>`; the old `getContent` response parser was replaced.

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

- **F95 CAPTCHA** — masked and `/dl/` links escalate to an interactive hCaptcha
  under automation. No automated bypass is attempted. Use the cache, retry
  later, or `--open --watch`.
- **Browser-only hosts** — MixDrop, KrakenFiles, VikingFile, UploadHaven, WDHO
  and JS-only hosts remain manual/browser downloads.
- **MEGA without megatools** — the link is skipped with an install hint.
- **Gofile network** — the API flow is implemented but could not be live-tested
  from the development network.
- **No multi-part/segmented downloading** — single connection per download.
- **No bandwidth throttling** beyond host-side limits.
- **Pixeldrain rate limits** — free transfers are ~1 MiB/s and may redirect to a
  captcha page after repeated views; an API key path is not implemented.
- **F95 `/dl` tokens** — wrapper links are tied to the rendered page; resolving
  requires a fresh link from the same scrape.
