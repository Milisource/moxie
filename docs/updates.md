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

### 4. Merge

`updater.Merge` never edits the live game in place:

1. New files are assembled in a staging directory.
2. Engine-specific user files (saves, `Game.ini`, `options.rpy`, mods, …) are
   copied from the live install over the staged defaults.
3. The live directory is renamed aside, staging is renamed into place, and any
   failure rolls the previous directory back.

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
moxie sync --auto                  # check, then update immediately
moxie check-updates --auto
```

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

A game that still fails for a verification reason is reported as needing a
browser check. Re-running after the browser has cleared the site's check (or
with fresh cookies) retries it.

### Concurrency and retries

- `<config>/auto-update.lock` prevents two runs installing into the same game
  directory. A lock older than two hours is stale; SIGINT/SIGTERM release it.
- A failed game waits 30 minutes before another automatic attempt. A new target
  version clears the backoff immediately. `--force` bypasses both the backoff
  and the 24 h check cooldown.
- Retry state lives in `<config>/auto-update.json` and is pruned after 7 days.

## Version Comparison

`internal/version` classifies versions before ordering them:

| Left | Right | Result | Queued? |
|---|---|---|---|
| `1.10` | `1.9` | Newer (numeric) | yes |
| `1.0.0` | `1` | Same | no |
| `0.9.5a` | `0.9.5b` | Newer (build letter) | yes |
| `Ch. 2` | `Ch. 1` | Newer | yes |
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
- Backups are kept: `<gameDir>.old` and timestamped `.old` siblings can be
  deleted once an update proves stable.
- The atomic swap requires the staging directory on the same filesystem as the
  game (it is a sibling, so this holds unless the game directory is a mount
  point).
