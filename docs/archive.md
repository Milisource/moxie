# Archive

## What

The archive package (`internal/archive/`) provides extraction of common game archive formats with progress reporting and path traversal protection. Game files downloaded from F95Zone are typically distributed as .zip, .7z, .rar, or .tar.gz archives.

The package is a thin wrapper over `internal/extractor/` (which owns ZIP/tar.gz handling, zip-bomb defense, path-traversal protection, and 7z/RAR system-tool fallback): two files — `archive.go` and `archive_test.go`.

## How

### Format Detection

`DetectFormat(path)` checks both file extension and magic bytes:

| Format | Extension | Magic Bytes |
|--------|-----------|-------------|
| ZIP | `.zip` | `50 4B 03 04` |
| 7z | `.7z` | `37 7A BC AF` |
| RAR | `.rar` | `52 61 72 21` |
| GZIP | `.tar.gz`, `.tgz`, `.gz` | `1F 8B` |

`IsArchiveFile(path)` is a convenience wrapper — returns true if format is known.

### Extraction

```go
result, err := archive.Extract(archivePath, destDir, Options{
    OnProgress: func(totalFiles, extractedFiles int, currentFile string, bytesProcessed, bytesTotal int64) {
        // progress callback
    },
})
```

`ExtractWithContext(ctx, ...)` is the cancellable variant — extraction aborts on context cancellation between files (7z/RAR run under `exec.CommandContext`). `Extract` delegates to it with `context.Background()`.

Extraction creates a subdirectory named after the archive (without extension) inside `destDir`. So `downloads/game.zip` extracts to `downloads/game/...`.

**Zip bomb defense** — ZIP extraction enforces two caps before writing anything: a per-entry compression ratio limit (100:1 — real game assets rarely exceed 20:1) and a total uncompressed-size cap (100 GB, twice the downloader's input cap). A crafted archive is rejected with a clear error and nothing is written.

**Progress callback notes:**
- `totalFiles` counts only regular files — directory entries are excluded, so progress accurately reflects file extraction work.
- `currentFile` may be long; CLI callers typically truncate to 60 characters for display.
- `bytesTotal` is computed from uncompressed sizes in ZIP archives, raw archive size in tar.gz (no per-file compression), or 0 for 7z/RAR (external tools).
- `bytesProcessed` is the cumulative uncompressed bytes written so far.

### Format-Specific Extractors

| Format | Implementation | Status |
|--------|---------------|--------|
| **ZIP** | Native Go `archive/zip` — built-in, no deps | ✅ |
| **TAR.GZ** | Native Go `archive/tar` + `compress/gzip` — built-in | ✅ |
| **7z** | External: `7z` or `7zz` command (install `p7zip-full`) | ⚡ |
| **RAR** | External: `unrar` or falls back to `7z` | ⚡ |

### Path Traversal Protection

`cleanExtractPath(filePath, destDir)` prevents zip-slip / path traversal attacks:
1. Joins the archive entry path with the destination directory
2. Resolves both to absolute paths
3. Verifies the result is within the destination directory (prefix check)

Any entry attempting `../../evil.txt` or absolute paths outside the destination is rejected.

### System Tool Fallback

For 7z and RAR formats, the package tries system tools first, falling back if unavailable (implemented in `internal/extractor/extbin.go`):

```
7z  → 7z command (p7zip-full)
7zz → 7zz command (p7zip)
RAR → unrar → 7z → error
```

ZIP can optionally use `unzip` command line tool (faster for large archives).

## Why

**Game distribution format** — Nearly all F95Zone game releases are distributed in compressed archives. The download manager must extract them automatically to provide a seamless "download and play" experience.

**Built-in formats first** — ZIP and TAR.GZ use only the Go standard library. No external CGO dependencies, no version conflicts, no binary size bloat. These cover ~90% of game archives from F95Zone.

**System tools for 7z/RAR** — 7z and RAR support adds complexity (proprietary algorithms, CGO-nativeGo libraries). Relying on system tools (`7z`, `unrar`) is pragmatic for these less common formats. Most Linux users already have them installed.

## Known Limitations

- Encrypted archives are not supported — `Options.Password` was removed; encrypted 7z/rar/zip fail with an error rather than pretending to support passwords
- 7z and RAR require external tools (install `p7zip-full` and `unrar`)
- No compression support — extraction only, no archive creation
- No large-file splitting — single archives only
- File permissions from the archive are preserved on Unix; Windows ACLs are not
- 7z/RAR delegate to external tools with no size caps (the ZIP caps above are the only enforcement)
