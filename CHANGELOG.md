# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.3.2-alpha] - 2026-05-03

### Added

- **One-liner install scripts** — `curl|bash` for macOS/Linux, `irm|iex` for Windows PowerShell
- `--version` flag with version stamping via `-ldflags` from `git describe`
- First-run welcome message when no database exists (shows scan + TUI hints)
- Platform-aware Firefox User-Agent: Linux, macOS, and Windows UA strings selected by `runtime.GOOS`
- macOS native Mach-O executable detection in `play` command (permission bits, no extension required)
- TUI CrossOver wine support on macOS (matches CLI `LaunchCommand`)
- `windows/arm64` and `macos/arm64` build targets in Makefile and `build.sh`
- `make install` and `make clean` targets
- `release.sh` script (local-only) for tagging, building, and GitHub Release creation

### Fixed

- **REQ/SEEKING threads misidentified as game releases** — F95Zone uses non-breaking spaces (U+00A0) between XenForo prefix labels and thread titles; `IsNonGameThread` now normalizes all Unicode whitespace before matching
- **`FindMatches` in `associate.go`** now skips non-game thread candidates (was missing the filter)
- **Nil pointer dereference in `play` command** when Wine is not found on non-Windows systems
- **TUI URL update** now triggers a live metadata scrape instead of showing stale data from the old URL

### Changed

- **Makefile:** added `CGO_ENABLED=0` for fully static binaries, `-s -w` linker stripping (binary ~16 MB), `VERSION` injection from `git describe`, `.PHONY` declarations
- `IsNonGameThread` moved from `internal/commands/` to `internal/scraper/` package
- `scripts/build.sh` aligned with Makefile naming (`darwin` → `macos`), added `arm64` for all platforms
- **README** restructured with "Install" section at top, one-liner commands per platform
- Default binary size now ~16 MB stripped (was ~10 MB unstripped)

[Unreleased]: https://github.com/Milisource/moxie/compare/v0.3.2-alpha...HEAD
[0.3.2-alpha]: https://github.com/Milisource/moxie/releases/tag/v0.3.2-alpha
