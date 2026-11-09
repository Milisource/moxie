# Save Manager

## What

The save manager keeps **save sets** for your games: named copies of a game's save
data that you can snapshot, import, apply, and restore. It exists so you can switch
between your own progress and a community "100%" save without losing either one.

A save set is one of:

- **`snapshot`** — a copy of *your own* saves, captured from the game's live save
  location. Snapshots are named `mine-<YYYYMMDD-HHMMSS>` by default and are also
  created automatically the first time you apply a set.
- **`import`** — a set you added from an archive or directory (`moxie saves add`).
  This is how a manually downloaded "Full Save" or "100%" save enters the library.
- **`f95_attachment` / `f95_link`** — sets downloaded straight from an F95Zone
  thread's first post with `moxie saves fetch`. `f95_attachment` sets come from
  F95Zone-hosted attachments; `f95_link` sets come from external file-host links.
  Both record the origin URL (`source_url`) and the game version.

The switching model is a **named set library per game**. Every set lives under a
storage root and is recorded in the database. At most one set is *active* — the set
currently applied to the game's live save location:

```
my saves  ──apply──▶  live save location  ──snapshot──▶  mine-20261012-143000
100% save ──apply──▶  live save location  ──restore───▶  my saves (newest snapshot)
```

`apply` copies a set into the live location; `restore` is shorthand for applying
your newest snapshot. Nothing is deleted, so you can move back and forth freely.

### Engine support

Save-path resolution is implemented for the engines that keep save data in plain
files on disk:

| Engine | Supported | Live save data |
|---|---|---|
| **Ren'Py** | ✅ | `game/saves/`, `saves/`, `config.save_directory`, `~/.renpy`, Wine/Proton prefixes |
| **RPG Maker MV** | ✅ | `www/save/` (or `save/`) |
| **RPG Maker MZ** | ✅ | `save/` |
| **RPG Maker VX Ace** | ✅ | `Save*.rvdata2`, `Global.rvdata2` in the game directory |
| HTML / Flash | ❌ | Browser `localStorage` — no filesystem save location |
| Others (Unity, Unreal, WolfRPG, QSP, …) | ❌ | No resolver; pin a location manually with `moxie saves path --set` |

For an unsupported engine you can still use the save manager by pinning the live
location by hand (see [Manual override](#manual-override)).

## How

The manager lives in `internal/saves/` with the CLI in
`internal/commands/saves.go` (snapshot/import/apply/scan) and
`internal/commands/saves_fetch.go` (the F95 fetch pipeline):

- `saves.go` — types (`Location`, `Engine`) and construction
- `paths.go` — engine save-path discovery (pure filesystem reads)
- `resolve.go` — combines the manual override with discovered candidates
- `store.go` — set storage on disk: import, list, remove
- `apply.go` — snapshot, apply, restore, live-directory mutation

### Storage layout

Set data is stored under a root directory, keyed by the immutable game ID and
the set name:

```
<saves_dir>/<gameID>/<setName>/
```

Keying by the game ID — not the title — keeps sets reachable when a game is
renamed or re-scraped: the title is mutable, the ID is not. A directory created
by an earlier build under the old `<gameID>-<slug>` layout is migrated in place
the first time its sets are touched.

The set directory is a **verbatim copy** of the imported source — moxie never moves
or reorganizes it. The subpath within the set whose *contents* get applied is
recorded separately as `apply_root`, so the tree on disk always mirrors the source.

Symlinks are not allowed in save data: importing, snapshotting, or applying a set
that contains a symlink is refused, and the shell-out archive extractors verify
after extraction that no symlink entries (or entries escaping the destination)
were produced. Save archives are expected to contain regular files only.

Fetched and imported archives are also extracted under a **decompressed-size and
entry-count budget** (512 MiB / 50 000 entries). A save is a few MB, so an archive
that unpacks far beyond that is treated as hostile — a "decompression bomb" — and
refused before it can fill the disk. The native extractors enforce the budget
while extracting; the `7z`/`unzip`/`unrar` shell-outs read the archive's listing
first. Game downloads are unaffected (they pass no budget).

The storage root defaults to `~/.local/share/moxie/saves` (honoring `XDG_DATA_HOME`)
and is configurable with the `saves_dir` config key:

```bash
moxie config set saves_dir /mnt/games/saves
```

### Save-path resolution

Given a game, moxie produces a list of candidate live save locations, best first.
It is a pure filesystem read — nothing is guessed and nothing is written.

**Ren'Py** candidates, scored by confidence:

1. **Portable saves** inside the game directory — `<game>/game/saves/` or
   `<game>/saves/` (confidence 0.95).
2. **Title-matched directories** under known Ren'Py save roots:
   - `~/.renpy/<name>`
   - `~/.wine/drive_c/users/*/AppData/Roaming/RenPy/<name>`
   - Steam Proton: `compatdata/<steam_app_id>/pfx/drive_c/users/steamuser/AppData/Roaming/RenPy/<name>`
     (the Steam root is probed at `~/.local/share/Steam`, `~/.steam/steam`, and
     `~/.steam/root`)

   Names are matched against the game title and the game directory name, and — when
   the build ships readable sources — against `config.save_directory` read from
   `<game>/game/options.rpy`. A trailing `-<digits>` timestamp that Ren'Py appends
   to save directories is stripped before matching. Directories with no match at all
   are dropped rather than offered with low confidence.

**RPG Maker:**

- **VX Ace** — `KindFiles` in the game directory, matching `Save*.rvdata2` and
  `Global.rvdata2`.
- **MV** — `<game>/www/save/` (or `<game>/save/` when there is no `www/`).
- **MZ** — `<game>/save/`.

  MV/MZ targets are offered even before the game has created the directory, because
  the save folder only appears on the first save.

Up to 8 Ren'Py candidates are offered. When **more than one** candidate is equally
plausible, moxie refuses to pick one: `saves path` prints them all and waits for you
to choose. Ambiguity is surfaced, never guessed.

#### Manual override

Pin the live location for a game — this always wins over auto-detection:

```bash
moxie saves path 42 --set ~/.renpy/MyGame-1234567890
moxie saves path 42            # show resolved path + all candidates
moxie saves path 42 --auto     # clear the override, back to auto-detection
```

The override is stored in the `save_configs` table (`manual_path`).

#### Finding your save location

`saves scan` lists what it can see under the game's candidate locations so you can
pick one to pin. It is a pure filesystem read: nothing is written and nothing is
pinned by the scan itself.

```bash
moxie saves scan 42
```

It prints the resolved location (when one is known) as `Current:`, then one row per
candidate that holds save-like files, newest first:

```
Current: /home/me/.renpy/MyGame-1234567890
DIR                                             FILES  NEWEST        MODIFIED
-----------------------------------------------------------------------------------------------
/home/me/.renpy/MyGame-1234567890                14     persistent    2026-10-12 14:30
/home/me/games/MyGame/game/saves                    2     slot1.save    2026-08-01 09:00

Pin a location with: moxie saves path 42 --set /home/me/.renpy/MyGame-1234567890
```

`DIR` and `NEWEST` are truncated with `...` when long, to keep the table aligned;
the pin hint always prints the top candidate's full path.

When nothing save-like is found it prints `No save-like files found for <game>` and
prints no table. Copy the path you want from the `DIR` column (or use the suggested
hint) into `moxie saves path 42 --set <dir>`.

### Apply semantics

Applying a set is deliberately non-destructive by default:

- **Copy, don't move.** The set's `apply_root` subtree is copied *into* the live
  location. Same-name files are overwritten; live files that the set does not contain
  are left alone. **Live user data is never deleted.**
- **`--replace` mirrors the set.** With `moxie saves apply <id> <set> --replace`,
  the live location is mirrored instead: the set is still copied in, but live files the
  set does not contain are removed first, so the live directory ends up matching the
  set. The auto-snapshot guard below still runs first, so the previous live state is
  recoverable. Output reports what was removed, e.g.
  `Replaced live saves (3 files removed)`.
- **Auto-snapshot on first apply.** If the game has no active set yet and the live
  location holds saves, moxie first snapshots them (label `my saves (auto)`) so the
  previous state can be restored. Afterwards it applies the requested set.
- **Version-mismatch warning.** Each set records the `game_version` it was captured
  for. When it differs from the game's current version (after light
  normalization — case, leading `v`, and trailing `.0` segments are ignored),
  apply/restore prints
  `warning: this set was captured for <setVersion>; the game is at <gameVersion> — saves may not load.`
  It is a warning only: nothing is blocked, because the recorded version can be stale
  and either side may be unknown.
- **One active set.** A partial unique index enforces at most one active set per
  game; applying a set deactivates the previous one in the same transaction. The
  set is marked active *before* its contents are copied, so a database failure
  leaves the live location untouched and a copy failure is retryable with a plain
  re-run of `apply`.
- **The active set can't be removed.** `moxie saves remove` refuses to delete the
  game's active set; apply another set (or `restore`) first. Without this, removing
  the active set would leave foreign save data live with no record of where it came
  from, and the next apply would auto-snapshot it as "my saves".
- **Restore.** `moxie saves restore <id>` applies the game's newest snapshot —
  shorthand for putting your own saves back.

> **⚠️ Drift caveat — snapshot before switching.** Progress you make *while a set is
> marked active* is **not** captured automatically. The auto-snapshot only runs on
> the first apply, when no set is active. If you play the 100% save and then apply a
> different set, your in-between progress is overwritten. **Snapshot again first**:
>
> ```bash
> moxie saves snapshot 42     # capture progress made while the 100% save was active
> moxie saves apply 42 mine-20261012-090000
> ```

### Database

The save manager uses two tables added in **schema v7** (`internal/db/db.go`,
`internal/db/save_sets.go`):

**`save_sets`** — one row per set:

| Column | Notes |
|---|---|
| `id` | primary key |
| `game_id` | `REFERENCES games(id) ON DELETE CASCADE` |
| `name` | filesystem-safe slug, unique per game (`UNIQUE(game_id, name)`) |
| `label` | human-readable display label |
| `source` | `snapshot` \| `import` \| `f95_attachment` \| `f95_link` |
| `source_url` | origin URL for fetched sets |
| `game_version` | game version when the set was created |
| `apply_root` | relative subpath within the set to apply (`''` = the set root) |
| `size_bytes`, `file_count` | set totals |
| `active` | `1` when currently applied to the live location |
| `created_at` | creation timestamp |

Indexes: `idx_save_sets_game` on `game_id`, and a partial unique index
`idx_save_sets_one_active` on `game_id WHERE active = 1` to guarantee one active set
per game.

**`save_configs`** — one row per game that has a pinned location:

| Column | Notes |
|---|---|
| `game_id` | primary key, `REFERENCES games(id) ON DELETE CASCADE` |
| `manual_path` | user-pinned live save location (wins over auto-detection) |
| `resolved_path` | last auto-resolved path (display/cache only) |
| `updated_at` | timestamp |

The database is the source of truth — there are no sidecar metadata files on disk.

### CLI reference

```
moxie saves list [id]
moxie saves snapshot <id> [--name N] [--label L]
moxie saves add <id> <archive|dir> [--label L] [--name N] [--root R]
moxie saves apply <id> <set> [--replace]
moxie saves restore <id>
moxie saves path <id> [--set PATH|--auto]
moxie saves scan <id>
moxie saves remove <id> <set>
moxie saves fetch <id|name> [--list] [--pick N] [--password P] [--url URL]
```

Game arguments accept an ID or a fuzzy name (numeric ID first, then title search).
Flags may appear before or after positionals.

| Subcommand | Flags | What it does |
|---|---|---|
| `list [id]` | — | List sets for one game, or every game's sets grouped by game. Shows source, size, and active status. |
| `snapshot <id>` | `--name N`, `--label L` | Capture the game's live saves as a new set. Defaults: name `mine-<YYYYMMDD-HHMMSS>`, label `my saves`. |
| `add <id> <src>` | `--label L`, `--name N`, `--root R` | Import a set from a directory, an archive (`.zip`, `.tar.gz`, `.7z`, `.rar`), or a single file. `--root` overrides the auto-detected `apply_root`. |
| `apply <id> <set>` | `--replace` | Copy the named set into the live location (auto-snapshotting first when needed) and mark it active. `--replace` mirrors the set instead of merging (see [Apply semantics](#apply-semantics)). |
| `restore <id>` | — | Apply the game's newest snapshot — your own saves. |
| `path <id>` | `--set PATH`, `--auto` | Show the resolved location and all candidates; pin a location with `--set`, or clear the override with `--auto`. |
| `scan <id>` | — | List the save-like files under the resolved and candidate locations, newest first, to help pick a path for `--set` (see [Finding your save location](#finding-your-save-location)). |
| `remove <id> <set>` | — | Delete the named set's directory and database row. Refuses the game's active set. |
| `fetch <id>` | `--list`, `--pick N`, `--password P`, `--url URL` | Download a save posted in the thread's first post and import it as a set. `--list` shows candidates, `--pick N` chooses one. `--password` is accepted but encrypted archives are not yet supported by the extraction backend. |

`apply`, `restore`, and `remove` take a set **name** (the filesystem-safe slug, not
the label). `add` derives the name from `--name`, else a slug of the label, else
`import-<YYYYMMDD-HHMMSS>`; collisions get a `-2`, `-3`, … suffix. Run
`moxie saves list <id>` to see the exact names.

#### Fetching a save from an F95Zone thread

`saves fetch` downloads a save that someone posted in the game's F95Zone thread and
imports it as a set in one step — no manual archive handling:

```bash
moxie saves fetch 42                    # auto-download the only candidate
moxie saves fetch 42 --list             # inspect the candidates, download nothing
moxie saves fetch 42 --pick 2           # download candidate #2
moxie saves fetch 42 --url <thread-url> # override the thread URL (testing)
```

What it does:

1. **Requires F95 cookies.** The thread is read with your F95Zone session, so you
   must be logged into `f95zone.to` in Firefox (the same cookie detection the
   scraper and downloader use). The game needs an F95 thread ID or URL; if it has
   none, run `moxie scrape <id>` first (or pass `--url`).
2. **Scrapes the thread's first post** for save candidates. Two kinds are offered:
   - **`attachment`** — files hosted on F95Zone itself
     (`f95zone.to/attachments/...`), named from the URL.
   - **`external`** — links to a recognized file host (Mega, GoFile, Pixeldrain, …)
     whose link text or filename signals a save (e.g. "100% save", "Full Save",
     "Gallery"). Masked redirect links (`f95zone.to/masked/<host>/...`) carry the
     human name in the link text.
3. **Selects one candidate.**
   - Exactly one candidate auto-downloads.
   - `--list` prints a numbered table (`#`, `KIND`, `HOST`, `FILENAME`, `CONTEXT`)
     and exits without downloading.
   - `--pick N` downloads candidate `N` (1-based). An out-of-range `--pick` is
     rejected.
   - With no `--pick` and more than one candidate, moxie prints the table and
     errors, telling you to re-run with `--pick N`.
   - With no candidates, moxie prints `No save candidates found in the first post.`
     and exits — this is not an error.
4. **Downloads it.** Attachments stream through the authenticated scraper client;
   external links go through the normal downloader, so per-host URL resolution is
   shared with game downloads. `--password` is accepted for forward compatibility
   but encrypted archives are not currently supported by the extraction backend;
   an extraction failure surfaces the underlying error.
5. **Imports it as a set** with source `f95_attachment` or `f95_link`, the origin
   URL (`source_url`), and the game's current version (`game_version`). The set's
   label is the candidate's filename.

On success it prints the new set name and the command to apply it:

```
Fetched 100-save.zip as set 100-save-zip (34 files, 2.1 MB)
Apply it with: moxie saves apply 42 "100-save-zip"
```

Nothing is applied automatically — review with `moxie saves list 42`, then run
`moxie saves apply` when you're ready (your own saves are snapshotted on the first
apply — see [Apply semantics](#apply-semantics)).

> **Known limitation.** Only the thread's **first post** is scanned. Saves posted as
> links to *other* posts (`…/post-XXXXX`) or buried in replies are not discovered
> yet. To use one of those, download it manually and `moxie saves add` it.

#### Adding a fetched or downloaded save

`apply_root` controls which part of the set is copied. When you don't pass `--root`,
moxie derives it from the staged tree:

1. strip single-directory wrapper chains (e.g. an archive nested one level deep);
2. use the deepest directory named `save`/`saves` (case-insensitive) that holds a
   file;
3. otherwise use the deepest directory holding a recognizable save file
   (`*.save`, `persistent`, `*.rpgsave`, `*.rvdata2`, `*.rvdata`, …);
4. otherwise use the directory left after step 1.

#### Example workflow: my saves ↔ 100%

```bash
# 1. Snapshot your current (legit) saves before touching anything
moxie saves snapshot 42

# 2. Import a community 100% save (archive or already-extracted directory)
#    The set name becomes the slug of the label: "100% save" → "100-save"
moxie saves add 42 ~/Downloads/fullsave.zip --label "100% save"
moxie saves list 42                              # confirm the set name

# 3. Apply it. Your own saves are snapshotted automatically on the first apply.
moxie saves apply 42 100-save

# 4. ...play the 100% save...

# 5. Put your own saves back — restore applies the newest snapshot.
moxie saves restore 42

# To keep the 100% playthrough instead, snapshot it first, then switch back
# with an explicit apply of your own set (see the drift caveat above):
#   moxie saves snapshot 42 --label "100% playthrough"
#   moxie saves apply 42 mine-20261012-143000
```

Sample `moxie saves list 42` output:

```
GAME   GAME TITLE    SET                     SOURCE    SIZE       STATUS
--------------------------------------------------------------------------------------------
42     My Game       mine-20261012-143000    snapshot  128.4 KB   active
42     My Game       100-save (100% save)    import    96.1 KB
```

### Desktop app

The Wails desktop app drives the same engine (`internal/saves`) against the same
database, so a set snapshotted in one surface shows up in the others.

- **Game detail → Saves** — per-game management: the resolved live location with its
  provenance and a pinned-path control (Change / Clear), the set list (label, source,
  active badge, recorded game version, size, file count, created date), and the
  actions Apply / Replace / Snapshot / Restore my saves / Import / Scan / Delete.
  *Replace*, *Restore*, and *Delete* ask for confirmation first.
- **Sidebar → Saves** — cross-game overview: every game that has sets, with set counts,
  whether a set is active, and the live save location. Click a row to open the game.
- **Version mismatch** — a set recorded for a different game version is badged
  `(mismatch)`, and applying or restoring it shows the same warning as the CLI.
- **Import** opens a file picker (`.zip`, `.7z`, `.rar`, `.tar`, `.gz`, `.tgz`);
  **Scan** lists candidate directories newest-first and pins one with a click.

Desktop operations are synchronous and show a busy state while they run. `fetch`
(downloading a save from an F95Zone thread) is not yet exposed in the desktop.

## Why

**Non-destructive copy model** — applying a set overwrites same-name files but never
removes live files it does not know about. Combined with the automatic first-apply
snapshot, this means a mis-applied set is always recoverable and your own saves are
never silently destroyed. The `active` flag plus a partial unique index keeps the
"which set is live?" answer unambiguous.

**Staged discovery** — the manager handles saves you make yourself
(snapshot/restore), saves you bring in yourself (`add`), and saves posted in a game's
F95Zone thread (`fetch`). `fetch` reuses the existing cookie-authenticated scraper and
downloader, so thread attachments and file-host links are handled the same way game
downloads are. It scans the OP only today; other-post and reply links are future work.

**Ambiguity is surfaced, not guessed** — save-path detection is the hard part, not
the copying. Wine and Steam/Proton prefixes legitimately keep the same game's saves
in different trees, and Ren'Py builds deliberately hide their save directory name.
Rather than guessing, moxie lists candidates by confidence and gives you a one-line
override (`moxie saves path --set`).

## Limitations and roadmap

- **Untrusted saves are load-at-your-own-risk.** Ren'Py saves are pickled Python
  objects; a set downloaded from a stranger can execute code when the game loads it.
  Only apply saves from sources you trust.
- **`--replace` deletes live files.** Plain apply only overwrites same-name files,
  but `--replace` removes live files the set does not contain. The auto-snapshot
  guard only runs on the first apply — when a set is already active, snapshot your
  progress first (see the drift caveat above) before applying with `--replace`.
- **`saves scan` is heuristic.** It lists files it recognizes as save-like under the
  known candidate locations; an unrecognized save format may be missing from the table.
  It never writes — pin the path you want with `saves path --set`.
- **`saves fetch` scans the first post only.** It discovers save candidates in a
  thread's OP — F95Zone attachments and external file-host links whose text or
  filename signals a save (e.g. "100% save", "Full Save", "Gallery"). Saves posted as
  links to *other* posts (`…/post-XXXXX`) or in replies are not discovered yet; use
  `add` with a manually downloaded archive for those.
- **HTML / Flash games are out of scope.** Their saves live in browser storage with
  no filesystem location to manage.
- **Best-effort paths only.** Ren'Py resolution targets Linux layouts (native, Wine,
  Steam Proton); macOS/Windows parity is not implemented.
