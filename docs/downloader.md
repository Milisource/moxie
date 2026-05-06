# Downloader

> **⚠️ Beta Feature** — The downloader works reliably for Pixeldrain and Buzzheavier. Most other hosts have anti-bot protection that HTTP clients cannot bypass. Downloaded files are validated and rejected if they aren't actual archives/executables. See [Host Feasibility](#host-feasibility) for per-host support levels.

## What

The downloader package (`internal/downloader/`) provides HTTP download capabilities for game files from F95Zone approved file hosts. It handles URL resolution, progress reporting, resume support, SSRF protection, and dead link validation. The companion `internal/updater/` package handles post-download merging into existing game directories.

Package files: `types.go` (Status/Progress/Job types), `downloader.go` (Download, DownloadWithHost), `platform.go` (Platform enum, priority matching), `hosts.go` (HostResolver dispatch, followRedirect, IdentifyHostInURL), 8 per-host resolver files (`hosts_pixeldrain.go`, `hosts_buzzheavier.go`, etc.), `hosts_helpers.go` (shared URL utilities), `hostsizes.go` (size limits), `links.go` (IsOnlineOnly, ScoreDownloadLink, FindMostRecentFile), `detect.go` (DetectPlatformFromLink), `validator.go` (SSRF protection).

## How

### Types

```go
type Status int          // Pending, Downloading, Paused, Completed, Failed, Cancelled
type Progress struct {    // BytesDownloaded, TotalBytes, SpeedBytesPerSec, Percent
    BytesDownloaded  int64
    TotalBytes      int64
    SpeedBytesPerSec float64
    Percent         float64
}
type Job struct { ID, GameID, URL, Host, DestPath, Status, Progress, Error, timestamps }
type ProgressEvent struct { JobID, GameID, Status, Progress, Error }
```

### Download Flow

```go
downloader.Download(urlStr, destDir, expectedTotal, onProgress, f95Cookie)
// or with explicit host:
downloader.DownloadWithHost(urlStr, host, destDir, expectedTotal, onProgress, f95Cookie)
```

The `f95Cookie` parameter is an optional F95Zone session cookie string used to authenticate HEAD requests when resolving F95Zone masked redirect URLs (`f95zone.to/masked/...`). Pass `""` if no cookie is available; masked URLs will still be attempted but may fail on hosts that require authentication.

1. **SSRF check**: `isValidDownloadURL()` verifies HTTPS-only, blocks private/loopback IPs and cloud metadata endpoints (169.254.169.254, metadata.google.internal, 100.100.100.200).

2. **URL resolution**: `HostResolver.Resolve()` handles host-specific protocols.
   - F95Zone masked URLs (`f95zone.to/masked/...`) are first followed via HEAD redirect to get the real download URL, using the optional `f95Cookie` for authentication, then host-specific resolution is applied on the real URL.
   - Host-specific resolvers handle each provider's protocol (API calls, cookie exchange, POST forms, confirm tokens).

3. **Resume**: If a `.part` file exists, sends `Range: bytes=<existingSize>-` header. On 206 Partial Content, appends to the file. Resume is skipped for host-specific resolvers that don't support range requests.

4. **Download**: Reads response body in 32KB chunks, writes to a `.part` temp file.

5. **Progress**: `writeCounter` reports progress via callback every 500ms (speed, bytes, percentage).

6. **Validation**: After download completes, the caller calls `IsValidGameFile(path)` to reject files smaller than 4096 bytes or that aren't archives/executables. This catches interstitial HTML pages that some hosts serve instead of real files.

7. **Finalize**: `fsync()` + rename `.part` → final filename.

### Host-Specific Resolvers (`hosts.go`)

The `HostResolver.Resolve()` dispatches to per-host resolvers based on the host label:

| Host | Strategy | Status |
|------|----------|--------|
| **Pixeldrain** | API: `pixeldrain.com/api/file/<ID>` — direct download via API endpoint | ✅ Verified |
| **Buzzheavier** | HTMX: `<url>/download` with `HX-Request: true` + `Referer`, follows `hx-redirect` header | ✅ Verified |
| **Gofile** | Content API + direct download to `{fileID}.gofile.io/{fileID}` | ✅ Verified |
| **Google Drive** | Two-step: `GET /uc?export=download&id=<ID>` → parse HTML for `confirm=` token (for >100 MB virus-scan interstitial) → re-request with `&confirm=<TOKEN>` | ✅ Verified |
| **DataNodes** | Cookie + POST: `GET /download/<CODE>` for session cookies → parse hidden form fields → `POST` with cookies → follow 302 redirect to CDN | ⚡ May work |
| **MixDrop** | Pass-through with User-Agent (no API call; some file pages may serve interstitial instead of direct download) | ⚡ May work |
| **VikingFile** | Form POST: `GET /f/<HASH>` for hidden fields → `POST op=download1` → follow 302 redirect (blocked by Cloudflare Turnstile captcha) | ❌ Beta (blocked) |
| **Mega** | Unsupported — encrypted protocol not HTTP-accessible. Deprioritized to -200 in host scoring; auto-fallbacks to next-best link. Manual workaround: `moxie install <id> <path>` | ❌ Unsupported |

All other detected hosts (40+) pass through for standard HTTP download. See [Host Feasibility](#host-feasibility) below for the full breakdown of which hosts work.

### Host Scoring (`links.go`)

Download links are scored by combining platform priority with host reliability:

| Score | Hosts | Meaning |
|-------|-------|---------|
| **+25** | Pixeldrain, Buzzheavier, Gofile, Catbox | Verified — these resolvers reliably produce downloadable URLs |
| **+10** | DataNodes, Google Drive, MixDrop | May work — resolvers exist but hosts may have anti-bot protection or interstitials |
| **0** | All other recognized hosts | Unknown — passed through for standard HTTP download; no specialized resolver |
| **-200** | Mega, VikingFile, WorkUpload, KrakenFiles, Bunkrr | Borked — known to be blocked, encrypted, or requiring CAPTCHA; tried last in fallback order |

### Host Feasibility

Comprehensive research of all 44 F95Zone approved file hosts and their downloadability via HTTP:

#### ✅ Direct (standard HTTP GET with User-Agent, no interstitial)

These hosts serve files directly at the URL — no download page, no ads, no timers.

| Host | Notes |
|------|-------|
| **Catbox** | `files.catbox.moe/<id>.<ext>` — direct GET. 200 MB limit. Rate-limited for uploads only |
| **Transfer.sh** | `transfer.sh/<hash>/<filename>` — direct GET (service may have reliability issues) |
| **Quax** | `qu.ax/<code>.<ext>` — direct GET. 256 MB limit. Blocks Tor exit nodes |
| **Vern** | `vern.cc/<path>` — direct GET from user directory. 256 MB limit |
| **YourFileStore** | Direct file serving. 500 MB limit. Optional password protection |
| **Files.dp.ua** | `files.dp.ua/<code>` — direct GET. 100 GB limit. 25-day retention |

#### ⚠️ Interstitial / Download Page

These are NOT direct — the URL returns an HTML page with a download button, timer, or ad. Simple HTTP GET downloads the HTML, not the file. These require a browser or JDownloader.

| Host | Obstacle |
|------|----------|
| **VikingFile** | Cloudflare anti-bot + possible captcha. Our scraper attempted form POST flow but CF blocks unauthenticated requests |
| **FromSmash** | Download button page + JavaScript. Also requires API key for programmatic access |
| **SendGB** | Download page UI. Behind Cloudflare |
| **DropMeFiles** | Must click DOWNLOAD button on page |
| **Bowfile** | 7-second countdown timer + ad block detection. Behind Cloudflare |
| **UploadNow** | Download page with registration UI |
| **Anontransfer** | Download page. Behind Cloudflare |
| **Files.fm** | Web-based folder UI. Account-based with API keys |

Hosts already covered in other sections: WorkUpload (captcha), MediaFire (captcha), MixDrop (API), DataNodes (API), Anonymfile (Cloudflare).

#### ⚡ API (needs API call, cookie, or header exchange for real URL)

| Host | Flow |
|------|------|
| **Pixeldrain** | Already implemented. Note: rate-limited files may trigger captcha at 3× views/downloads ratio. API key bypasses |
| **Buzzheavier** | Already implemented. HTMX flow |
| **Gofile** | Already implemented. ⚠ Breaking change March 2026: API may restrict to premium accounts |
| **MixDrop** | Official API at `api.mixdrop.ag`. For zips/archives direct; for MP4 add `?download`. Domains: m1xdrop.click, mixdrop.co, etc. |
| **DataNodes** | POST flow: visit `/download/<ID>` → acquire `file_code` cookie → POST for download URL. Has Cloudflare |
| **Google Drive** | Two-step: `drive.google.com/uc?export=download&id=<ID>` → parse HTML for `confirm=` token → request with `&confirm=<token>`. Large files (>100MB) have virus-scan interstitial |
| **WeTransfer** | Unofficial: POST `api/ui/transfers/<ID>/<hash>/download` → get S3 presigned URL. Official API deprecated. Free tier now limits to 10 transfers/month |
| **Dropbox** | Shared link with `?dl=1` → redirect follows to direct file. ~20 GB/day bandwidth limit for free accounts |

#### ⚠️ Difficult (captcha, login, Cloudflare, JS execution, or multi-step flow needed)

| Host | Obstacle |
|------|----------|
| **VikingFile** | Cloudflare anti-bot blocks unauthenticated requests. Form POST scraper cannot bypass CF challenge |
| **MediaFire** | Captcha + Cloudflare. Free users get periodic reCAPTCHA. Larger files (>4 GB) need premium |
| **WorkUpload** | reCAPTCHA required for every free download. "Checking for robots" interstitial page |
| **Bunkrr/Bunkr** | Multi-domain extraction. Album → item → CDN URL chain. DDoS-Guard anti-bot. Domains change frequently |
| **KrakenFiles** | 90-second wait timer for free users + Cloudflare Turnstile captcha. Has official API (requires account) |
| **Bowfile** | 7-second countdown timer + anti-adblock scripts. Behind Cloudflare |
| **UploadHaven** | Captcha + ads + wait timers. Premium link generator target (heavily restricted free tier) |
| **1CloudFile** | Standard premium host pattern with restrictions |
| **CyberFile** | Password-protected folders. May have captchas for free downloads |
| **EasyUpload** | Google reCAPTCHA on upload; download likely has timer |
| **HexLoad/HexUpload** | Captcha + wait timer pattern |
| **SendGB** | Download page UI behind Cloudflare |
| **Anonymfile** | Behind Cloudflare proxy — may serve CF challenge page instead of file |
| **Anontransfer** | Download page behind Cloudflare |
| **Files.fm** | Web-based folder UI, account-based with API keys |
| **FromSmash** | Requires API key (account-based) or clicking download button |
| **DropMeFiles** | Download button on page must be clicked first |
| **WDHO** | Colored-button bot check ("click the colored button") + 3 MB/s speed cap |

#### ❌ Impossible (encrypted protocol, premium-walled, or requires auth)

| Host | Reason |
|------|--------|
| **Mega** | Encrypted protocol (RSA key exchange + AES-CTR). Cannot be HTTP-downloaded. Requires MEGA SDK |
| **Keep2Share (K2S)** | Premium-walled. Free downloads blocked by reCAPTCHA + throttling. API requires paid account |
| **Uploaded (ul.to)** | Premium-walled. Free users face extreme speed limits, long waits, hourly quotas. Effectively unusable free |
| **ProtonDrive** | E2E encrypted, requires Proton account + browser SSO session. Not feasible |
| **Apkadmin** | Premium host pattern. Unclear if free downloads work |
| **UploadNow** | Download page with registration/account requirements |
| **Terminal** | Invite-only uploader system, unclear download flow |

### Masked F95Zone URLs

F95Zone wraps external download links in redirect endpoints:
```
https://f95zone.to/masked/pixeldrain.com/210467/... → HEAD redirect → https://pixeldrain.com/u/L3sayv61
```

The `HostResolver.Resolve()` handles this transparently:
1. `IdentifyHostInURL` matches the masked URL against the host table using embedded domain hints
2. `Resolve` detects `/masked/` in the URL → calls `followRedirect()` (HEAD request, 10s timeout)
3. Gets the real download URL after the redirect chain
4. Re-identifies the host from the real URL
5. Recursively calls `Resolve` with the real URL and correct host

**Cookie-based authentication**: The caller sets the F95Zone session cookie via `HostResolver.SetF95Cookie(cookie)` before resolving. When set, `followRedirect()` includes the `Cookie` header in the HEAD request, which is required for some F95Zone masked URLs that redirect through authenticated endpoints. The cookie flows through the pipeline: `Download() → DownloadWithHost() → HostResolver.SetF95Cookie() → followRedirect()`.

If `followRedirect` fails (timeout, network error), it falls through to host-specific resolution with the masked URL — which will likely fail, but the caller's fallback loop will try other links.

### Host-Specific Resolvers (`hosts.go`)

| Host | Strategy | Status |
|------|----------|--------|
| **Pixeldrain** | API: `pixeldrain.com/api/file/<ID>` — direct download | ✅ |
| **Buzzheavier** | HTMX: `<url>/download` with `HX-Request: true` + `Referer`, follows `hx-redirect` header | ✅ |
| **Gofile** | Content API + direct download to `{fileID}.gofile.io/{fileID}` | ✅ |
| **VikingFile** | Scraper: form POST + redirect follow (blocked by Cloudflare anti-bot) | ❌ Beta |
| **DataNodes** | Cookie + POST flow to extract CDN URL | ⚡ Beta |
| **MixDrop** | Pass-through with User-Agent (blocked by interstitial on file pages) | ❌ Beta |
| **Google Drive** | Two-step confirm token extraction for large files | ⚡ Beta |
| **Mega** | Unsupported — encrypted protocol not HTTP-accessible. Deprioritized to -200 | ❌ Unsupported |

All other detected hosts (40+) pass through for standard HTTP download. See [Host Feasibility](#host-feasibility) below for the full breakdown of which hosts work.

### Host File Size Limits (`hostsizes.go`)

All 41 F95Zone approved hosts have documented max file sizes. E.g.: Catbox (200MB), Meg/a (15GB), Buzzheavier/VikingFile (unlimited), WDHO (3GB). Used to warn users before attempting large downloads on small hosts.

### Platform Priority (`platform.go`)

Downloads are ranked by compatibility with the user's OS:

| Current OS | Priority Chain |
|------------|----------------|
| **Linux**  | native (100) > Windows via Wine/Proton (70) > cross-platform (50) > unknown (25) > Mac (0) |
| **Windows**| native (100) > cross-platform (50) > unknown (25) > Linux/Mac (0) |
| **Mac**    | native (100) > cross-platform (50) > unknown (25) > Linux/Windows (0) |

### Download Validation (`downloader.go`)

```go
downloader.IsValidGameFile(path)  // returns true if file is a real game file
```

After a download completes, the caller should validate the downloaded file before using it. `IsValidGameFile` checks:

1. **Minimum size**: Files under 4096 bytes are rejected — they're too small to be a real game archive or executable.
2. **Archive check**: If `archive.IsArchiveFile(path)` returns true (zip, 7z, rar, tar.gz), the file is accepted.
3. **Executable check**: Files with extensions `.exe`, `.sh`, `.x86_64`, `.bin`, `.run`, or `.AppImage` are accepted.

Files that fail validation are typically interstitial HTML pages, captcha walls, or ad pages that malicious or anti-bot-protected hosts serve instead of the real file. The downloader removes the invalid file and the caller's fallback loop tries the next link.

### Dead Link Validation (`validator.go`)

```go
downloader.CheckLink(url)  // HEAD request, returns nil or descriptive error
```

Response mapping:
- 200/206 → valid
- 404 → "404 Not Found — file removed"
- 403 → "403 Forbidden — access denied or DMCA'd"
- 410 → "410 Gone — permanently removed"
- 503 → "503 Service Unavailable — host down"
- 429 → "429 Too Many Requests — rate limited"
- 5xx → server error

## Why

**Resume support** — F95Zone game downloads can be 10+ GB. Network interruptions are common. `.part` files with `Range` headers let users resume without restarting.

**Host-specific resolvers** — Pixeldrain, Buzzheavier, Gofile, and VikingFile have specialized resolvers because each uses different APIs (HTMX headers, content APIs, direct URLs). Masked F95Zone URLs are automatically unmasked before resolution.

**SSRF protection** — reused from the Steam artwork downloader (`internal/steam/grid.go`). Blocks metadata endpoints that could leak cloud credentials.

## Known Limitations

- **Mega not supported** — Mega uses a proprietary encrypted protocol not accessible via HTTP. Links receive a -200 score penalty so they are always tried last. If no other link succeeds, the error suggests manual download or megatools. A native SDK integration is planned.
- **Masked URLs** — F95Zone wraps external links as `f95zone.to/masked/<host>/...`. These are unwrapped via HEAD redirect before host-specific resolution. If the redirect fails, the link is skipped and the fallback loop tries other links.
- **Captcha-heavy hosts** — MediaFire, WorkUpload, and premium hosts (Keep2Share, Uploaded) require CAPTCHA solving or premium accounts. These hosts are passed through for HTTP download but may fail on captcha interstitials. The fallback loop will try the next link.
- **Gofile API changes** — As of March 2026, Gofile's API may restrict free downloads to premium accounts. The resolver falls back to direct download patterns.
- No multi-part download support
- No bandwidth throttling
- Buzzheavier resolution may break if their HTMX API changes
