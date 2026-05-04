# Downloader

## What

The downloader package (`internal/downloader/`) provides HTTP download capabilities for game files from F95Zone approved file hosts. It handles URL resolution, progress reporting, resume support, SSRF protection, and dead link validation.

Six files: `types.go`, `downloader.go`, `platform.go`, `hosts.go`, `hostsizes.go`, `validator.go`.

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
downloader.Download(urlStr, destDir, expectedTotal, onProgress)
// or with explicit host:
downloader.DownloadWithHost(urlStr, host, destDir, expectedTotal, onProgress)
```

1. **SSRF check**: `isValidDownloadURL()` verifies HTTPS-only, blocks private/loopback IPs and cloud metadata endpoints (169.254.169.254, metadata.google.internal, 100.100.100.200).
2. **URL resolution**: `HostResolver.Resolve()` handles host-specific protocols (see below).
3. **Resume**: If a `.part` file exists, sends `Range: bytes=<existingSize>-` header. On 206 Partial Content, appends to the file.
4. **Download**: Reads response body in 32KB chunks, writes to a `.part` temp file.
5. **Progress**: `writeCounter` reports progress via callback every 500ms (speed, bytes, percentage).
6. **Finalize**: `fsync()` + rename `.part` → final filename.

### Host-Specific Resolvers (`hosts.go`)

| Host | Strategy | Status |
|------|----------|--------|
| **Pixeldrain** | API: `pixeldrain.com/api/file/<ID>` — direct download | ✅ |
| **Buzzheavier** | HTMX: `<url>/download` with `HX-Request: true` + `Referer`, follows `hx-redirect` header | ✅ |
| **Gofile** | Content API + direct download to `{fileID}.gofile.io/{fileID}` | ✅ |
| **VikingFile** | Direct HTTP (standard) | ✅ |
| **Mega** | Instructs user to run `megatools` CLI (encrypted protocol, not HTTP-accessible) | ⚠️ |

All other hosts pass through for standard HTTP download.

### Host File Size Limits (`hostsizes.go`)

All 41 F95Zone approved hosts have documented max file sizes. E.g.: Catbox (200MB), Meg/a (15GB), Buzzheavier/VikingFile (unlimited), WDHO (3GB). Used to warn users before attempting large downloads on small hosts.

### Platform Priority (`platform.go`)

Downloads are ranked by compatibility with the user's OS:

| Current OS | Priority Chain |
|------------|----------------|
| **Linux**  | native (100) > Windows via Wine/Proton (70) > cross-platform (50) > unknown (25) > Mac (0) |
| **Windows**| native (100) > cross-platform (50) > unknown (25) > Linux/Mac (0) |
| **Mac**    | native (100) > cross-platform (50) > unknown (25) > Linux/Windows (0) |

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

**Host-specific resolvers** — only Pixeldrain, Buzzheavier, Gofile, and VikingFile are handled today because they are the most-used hosts. Each has different API requirements (HTMX headers, content APIs, direct URLs). Mega is intentionally not supported — its encrypted protocol requires `megatools`.

**SSRF protection** — reused from the Steam artwork downloader (`internal/steam/grid.go`). Blocks metadata endpoints that could leak cloud credentials.

## Known Limitations

- No Mega SDK integration (requires megatools CLI)
- No multi-part download support
- No bandwidth throttling
- Buzzheavier resolution may break if their HTMX API changes
- Gofile may require an account token for full-speed downloads (2025+)
