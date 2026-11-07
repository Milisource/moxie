# Game Updates

## What

moxie detects new versions of library games on F95Zone, downloads a working
mirror, and merges it into the existing install without losing saves or mods.
`moxie auto-update` runs that loop unattended as soon as a check finds
something, and stops only at a step that genuinely needs a person.

| File | Responsibility |
|---|---|
| `internal/commands/sync_check.go` | Version check (public bulk API + direct scrape fallback) |
| `internal/commands/download.go` | One-game pipeline: refresh links, download (multi-part aware), extract, merge |
| `internal/commands/version_record.go` | Records the installed version so the game leaves the queue |
| `internal/commands/auto_update.go` | Batch loop, lock, retry backoff, config trigger |
| `internal/commands/download_watch.go` | `--open`/`--watch` browser flow and archive watcher |
| `internal/updater/updater.go` | Staged merge with rollback and user-file preservation |
| `internal/version/` | Ordering for non-semver version strings (`Compare`, `NeedsUpdate`) |
| `internal/browserresolve/` | Browser-backed `/dl/` and `/masked/` resolution (hCaptcha, Turnstile) |
| `internal/downloader/hosts.go` | Gateway hooks, host resolvers, resolved-URL cache |

## Pipeline

```
check ──► queue ──► refresh links ──► pick mirror ──► resolve /dl/ (browser if gated)
                                     │                        │
                                     ▼                        ▼
                              multi-part / single ──────► download .part
                                                                 │
                                                                 ▼
                                                          extract to staging
                                                                 │
                                                                 ▼
                                                          staged merge (rollback)
                                                                 │
                                                                 ▼
                                                          record installed version
```

### 1. Detect

`RunUpdateCheck` prefers F95Zone's cookie-free bulk version endpoints
(`checker.php` plus the F95Checker cache API) and falls back to authenticated
thread scraping. Results are compared with `version.Compare`; only `Newer` and
`Changed` (different but unorderable) are reported as updates.

Bulk ids are deduplicated by thread ID before every `/fast` or `checker.php`
batch (platform copies of one game share a thread), and a thread whose full
cache record was already fetched in the version pass is skipped by the
status/metadata refresh — one `CacheFullThread` per changed thread per run, not
two. Both the CLI and the desktop take this path.

### 2. Queue

`db.GamesNeedingUpdate` filters candidates through `version.NeedsUpdate` in Go.
The previous SQL inequality (`latest_version != version`) also queued
**downgrades** and cosmetic differences, so a thread that reverted to an older
build made `download --all` re-install it forever. `Same` and `Older` no longer
qualify; `Changed` does, flagged for a human decision.

### 3. Refresh links and download

F95Zone's `/dl/<id>/?u=…&k=…&r=…&t=…` links are signed per session, so stored
`/dl/` links are not reused: the thread is re-scraped before downloading. The
Go path resolves the gateway directly; when F95Zone gates it behind hCaptcha
(or a masked link behind reCAPTCHA, or a host behind Turnstile), the downloader
hands the page to the installed browser (`browserresolve`) and continues with
the revealed URL. Multi-part archives are downloaded part by part with
per-part host fallback.

The plain-HTTP `/dl/` solver keeps the cookies the check page issues: XenForo
answers the Continue POST with *"Cookies are required to use this site"* when
the `xf_csrf` cookie from the GET is missing, so a static Cookie header is not
enough. That 400 is classified as a session failure, which stops the mirror
loop instead of retrying every host and reporting a captcha.

### 4. Merge

`updater.Merge` never edits the live game in place:

1. New files are assembled in a staging directory.
2. The live directory is renamed aside, and the staged root is moved into
   place with a single rename when it is on the same filesystem (a cross-device
   rename falls back to a file-by-file copy). Update payloads are dominated by
   file data, so the rename turns a multi-GB copy into metadata work.
3. Engine-specific user files (saves, `Game.ini`, `options.rpy`, mods, …) are
   copied from the backup over the new tree.
4. Any failure rolls the previous directory back into place; an adopted tree
   is discarded and the update re-downloaded.

A staging root is only adopted when it is disjoint from the live install and
contains nothing the copy path would sanitise (escaping symlinks, special
files). A flat archive that extracts into the shared downloads directory keeps
using the copy path.

Merge failures are now surfaced as errors instead of warnings, and the
downloaded archive is kept for a retry.

### 5. Record the version

`recordInstalledVersion` writes the thread version — or an archive filename
that parses to something even newer — only after a successful merge. This is
the step that removes the game from the queue. It previously existed only in
`install` and the watcher, so `moxie download` merged files and left the game
queued forever, which would have made auto-update re-download the same release
on every run. A reverted thread can never downgrade the recorded version.

## Auto-Update

```bash
moxie auto-update                  # check + install everything possible
moxie auto-update --check-only     # only report
moxie auto-update --no-check       # act on updates already detected
moxie auto-update --dry-run        # show what would happen
moxie auto-update --max 3          # cap this run
moxie auto-update --concurrency 4  # games in flight at once (1-4)
moxie sync --auto                  # check, then update immediately
moxie check-updates --auto
```

Games run in parallel up to the `update-concurrency` config key (default 2,
clamped to 1-4), matching the desktop's batch. `--concurrency` overrides it for
one run. Parallel runs suppress the `\r` progress bar so per-game pipeline
lines do not interleave with frames; `--concurrency 1` keeps the single-game
output.

Unattended mode is opt-in:

```bash
moxie config set auto_update true
moxie config set auto_update_max 5      # optional per-run cap
```

With `auto_update` enabled, `moxie sync`, `moxie check-updates`, and the
post-scan `--sync` hook proceed straight into the update loop when they find
something. `--auto` forces the same for one run without changing config.

### What auto-update installs

Only `version.Newer` candidates. A `Changed` comparison means the two version
strings cannot be ordered (for example a date-form thread version replacing a
numeric local one); that is reported as "needs review" and left alone rather
than replacing a working install on a guess.

### Verification gates

The browser fallback is installed by default when a supported browser exists
(`MOXIE_BROWSER=auto|chrome|firefox|off`). hCaptcha, reCAPTCHA and Turnstile
pages are handed to the real browser; the Go client downloads the revealed URL
with resume and progress. There is no captcha-solving code in moxie.

When the automated session hits Cloudflare's *"Performing security
verification"* page (most often in front of F95Zone's `/dl/` check page), the
solver retries once in a visible browser window and waits up to two minutes for
the user to pass the check; unattended runs on display-less machines cannot do
that and stop with a "needs a browser check" error.

A game that still fails for a verification reason is reported as needing a
browser check. Re-running after the browser has cleared the site's check (or
with fresh cookies) retries it.

### Concurrency and retries

- `<config>/auto-update.lock` prevents two runs installing into the same game
  directory. A lock whose recorded PID is no longer running is stale
  immediately; one without a readable PID falls back to a two-hour age window.
  SIGINT/SIGTERM release it.
- A failed game waits 30 minutes before another automatic attempt. A new target
  version clears the backoff immediately. `--force` bypasses both the backoff
  and the 24 h check cooldown.
- Retry state lives in `<config>/auto-update.json` and is pruned after 7 days.

## Version Comparison

`internal/version` classifies versions before ordering them:

| Left | Right | Result | Queued? |
|---|---|---|---|
| `1.10` | `1.9` | Newer (numeric) | yes |
| `1.0.0` | `1` | Same (missing segment counts as zero) | no |
| `1.13` | `1.13.0` | Same | no |
| `0.8.1` | `0.8` | Newer (added segment) | yes |
| `0.8` | `0.8.1` | Older | no |
| `0.9.5a` | `0.9.5b` | Newer (build letter) | yes |
| `Ch. 2` | `Ch. 1` | Newer | yes |
| `Ch.4 Free` | `0.12.0` | Changed (chapter vs numeric) | yes, manual only |
| `2026-10-01` | `2026-09-20` | Newer (ISO date) | yes |
| `1.1` | `1.2` | Older | no |
| `2026-10-01` | `1.0` | Changed (unorderable) | yes, manual only |

## Why

**Staged swap over in-place copy** — a full disk halfway through an in-place
copy leaves an unplayable game. The updater already builds a staging tree and
rolls back on failure; the fix here is to make the *caller* honour that result
instead of reporting success.

**Re-scrape every time** — `/dl/` links expire and are signed per account.
Cached gateway links are the most common cause of "download failed"; one thread
request is cheap compared with a wasted multi-GB download.

**Record on merge, not on download** — a downloaded archive is not an installed
game. Recording the version only after the merge is what makes the queue
converge.

**Do not auto-install ambiguous versions** — an unorderable change is a data
quality signal, not a release. Guessing wrong means silently downgrading a
working install.

## Limitations

- A browser is required for challenge-graded downloads. Without one, moxie
  reports the gate; it never attempts to solve it.
- MEGA links require `megatools` (the downloader reports this explicitly).
- Backups are kept: `<gameDir>.old` siblings can be deleted once an update
  proves stable with `moxie cleanup --backups` (add `--older-than` to keep
  recent ones); `moxie cleanup --archives` prunes leftover downloads.
- The fast path requires the staging directory on the same filesystem as the
  game (it is a sibling, so this holds unless the game directory is a mount
  point); a cross-device staging directory falls back to copying.
