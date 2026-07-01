# Download System — Host Resolver Framework

Lives in `internal/downloader/` (files: downloader.go, detect.go, platform.go, links.go, hosts*.go, hostsizes.go, types.go, validator.go, download_watch.go + tests). Multi-host HTTP download engine with platform detection, resume support, and SSRF protection.

## Architecture

```
downloader.go      — Core download functions (Download, DownloadWithHost)
detect.go          — Platform detection from download link names
platform.go        — Platform constants/priority (Windows/Wine/Proton/Mac/Linux)
links.go           — Host scoring and link selection
hosts.go           — Host resolver interface + factory
hosts_helpers.go   — Shared HTTP helpers for host resolvers
hosts_pixeldrain.go
hosts_buzzheavier.go
hosts_gofile.go
hosts_googledrive.go
hosts_datanodes.go
hosts_mega.go
hosts_mixdrop.go
hosts_vikingfile.go
hostsizes.go       — Expected file size database (keyed by host+URL pattern)
types.go           — Shared types (Progress, HostResolver interface)
validator.go       — Dead link validation (HEAD requests, size checks)
download_watch.go  — Directory watcher for auto-installing completed downloads
```

## Host Resolver Interface

Each host implements a resolver that translates a generic download URL into a direct download URL with proper headers:

```go
type HostResolver interface {
    Resolve(urlStr string) (*ResolvedURL, error)
    Name() string
}
```

`ResolvedURL` contains:
- `URL` — the direct download URL
- `Headers` — map of HTTP headers needed (e.g., `Referer`, `HX-Request`)

## Adding a New Host

1. Create `hosts_<name>.go` in `internal/downloader/`
2. Implement the `HostResolver` interface
3. Register in the factory function in `hosts.go`
4. Update the host scoring matrix in `links.go`
5. Add host to `IdentifyHostInURL()` in `hosts.go`
6. Add test file `hosts_<name>_test.go`
7. Update `docs/downloader.md` with the new host entry

## Download Flow

1. `Download(url, destDir, ...)` or `DownloadWithHost(url, host, ...)`
2. URL validated via `isValidDownloadURL()` (SSRF protection)
3. Host resolver transforms URL (handles redirects, auth, cookies)
4. HTTP GET with resume support (`Range` header if `.part` exists from previous attempt)
5. Write to `.part` temp file with progress callbacks every ~500ms via `writeCounter`
6. On completion, rename `.part` → final filename
7. Validate with `IsValidGameFile()` (min 4KB, known archive/executable extensions)

## SSRF Protection

`isValidDownloadURL()` blocks:
- Loopback IPs (127.0.0.1, ::1)
- Private IPs (10.x, 172.16-31.x, 192.168.x)
- Link-local IPs (169.254.x)
- Hardcoded blocklist: metadata endpoints (169.254.169.254, metadata.google.internal, 100.100.100.200)
- Non-HTTPS URLs

## Host Scoring

`links.go` implements a scoring system for link selection:

| Criterion | Score |
|-----------|-------|
| Explicitly marked as working | +25 |
| Known failure | -200 (auto-skip) |
| Platform match | +10 |
| File size match | +5 |

Links tried in score order with fallback chain.

## Progress Tracking

`Progress` struct emitted via callback approximately every 500ms during active download:

```go
type Progress struct {
    BytesDownloaded int64
    TotalBytes      int64
    SpeedBytesPerSec float64
    Percent         float64
}
```

Download commands in `internal/commands/download.go` and download UI in `internal/commands/download_ui.go` use this for status display.

## Platform Detection

`detect.go` / `platform.go` identifies which platform a download link targets:

- Windows/Wine/Proton → `.exe`, `.zip`, Windows-specific markers
- Linux → `.sh`, `.AppImage`, `.tar.gz`, Linux-specific markers
- macOS → `.dmg`, `.app`, macOS-specific markers

Priority: Wine > Proton > Windows > Mac > Linux (most games target Windows first).

## Expected Sizes

`hostsizes.go` maintains a known file-size database keyed by host and URL pattern. This helps validate downloads and provides progress percentage even when the server doesn't return `Content-Length`.

## Dead Link Validation

`validator.go` checks download links with HEAD requests to detect dead URLs before attempting a download. Results update the `download_links` table's `is_dead` flag.

## Download Watch

`download_watch.go` implements a directory watcher for auto-installing completed downloads. It monitors a configurable directory and triggers the install pipeline when new files appear.
