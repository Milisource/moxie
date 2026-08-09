# internal/jd — JDownloader 2 headless bridge

`jd` is moxie's **opt-in sidecar** integration with JDownloader 2 (JD2).
It drives a headless JD2 instance through the MyJDownloader cloud API
(`api.jdownloader.org`) — moxie never talks to the JVM directly.

**Status: foundation.** The package implements the client surface
(connect / add links / poll downloads / wait with stall detection) and is
fully unit-tested against a protocol-accurate mock of the MyJDownloader
endpoints. It is **not yet wired** into `main.go`, `internal/commands`, or
the TUI — that is a follow-up (see the `F95-scyi` beads issue).

## Why a sidecar at all

moxie's own resolvers (`internal/downloader`) stay the primary download
path. They hit a wall on hosts gated behind Cloudflare/Turnstile challenges
or captchas. JD2 is the only mature, maintained solution for those hosts:

| Host | JD plugin |
|------|-----------|
| buzzheavier, datanodes, vikingfile* | maintained |
| workupload, mixdrop, uploadhaven | maintained |
| krakenfiles (captcha even premium), mediafire, gofile, hexload, pixeldrain, mega | maintained |
| catbox | generic DirectHTTP plugin |
| F95Zone thread URLs | thread decrypter (requires F95Zone account inside JD) |

\* vikingfile uses Turnstile → requires a paid captcha solver (see failure modes).

## Setup

### Option A — Docker (recommended, maintained)

```
docker run -d \
  --name jdownloader-2 \
  -p 5800:5800 \          # optional: web UI for one-time config
  -v /mnt/games:/output \ # downloads land here
  -v jd-cfg:/config \
  -e KEEP_APP_RUNNING=1 \
  -e USER_ID=1000 -e GROUP_ID=1000 \
  jlesage/jdownloader-2
```

`jlesage/jdownloader-2` is actively maintained (Jul 2026). The container
auto-updates JD plugins on the same 7–14 day cycle as the desktop app (see
failure modes). `:5800` exposes a VNC web UI for the one-time
MyJDownloader account binding; afterwards the sidecar runs headless and
moxie only needs the account email/password/device name.

### Option B — local JVM (no container)

Requires Java 11+ (JD2 bundles its own JRE in newer builds):

```
java -Djava.awt.headless=true -Xmx1g -jar JDownloader.jar
```

Run it once interactively (or pre-seed the config JSONs below) to bind the
MyJDownloader account, then keep it running in the background.

### RAM footprint

JD2 is a JVM app — **expect 300–700 MB RSS** (container or local). This is
the reason the bridge is opt-in: users who only hit hosts moxie resolves
natively should never pay the memory floor. Give the JVM `-Xmx1g` (default
heap sizing can balloon past 1 GB on big link lists). On a low-RAM box,
prefer the Docker option and stop the container when the batch is done.

## Required configuration pre-seeding

JD2 persists settings as JSON files in its config dir (`/config/cfg` in
the container). Pre-seeding them avoids the interactive wizard:

1. **MyJDownloaderSettings** — `org.jdownloader.api.myjdownloader.MyJDownloaderSettings.json`

   ```json
   {
     "email": "your-account@example.com",
     "password": "your-myjd-password",
     "devicename": "moxie",
     "autoconnectenabledv2": true,
     "serverhost": "api.jdownloader.org",
     "directconnectmode": "LAN",
     "lasterror": "NONE"
   }
   ```

   The device name here must match `jd.WithDeviceName(...)` (default
   `"moxie"`). The Go structs in `jdownloader-go`'s `settings.go`
   (`DefaultMyJdownloaderSettings()`, `DefaultGeneralSettings()`,
   `DefaultLinkGrabberSettings()`) mirror these files if moxie ever wants
   to generate them programmatically.

2. **GeneralSettings** — `org.jdownloader.settings.GeneralSettings.json`

   ```json
   {
     "defaultdownloadfolder": "/output",
     "maxsimultanedownloadsperhost": 1
   }
   ```

   `defaultdownloadfolder` is the fallback when moxie does not pass
   `jd.WithDestinationDir(...)`.

3. **LinkgrabberSettings** — `org.jdownloader.gui.views.linkgrabber.addlinksdialog.LinkgrabberSettings.json`

   ```json
   {
     "autoextractionenabled": false,
     "linkgrabberautostartenabled": true
   }
   ```

   **Disable auto-extract.** Extraction is moxie's job (`internal/archive`)
   — JD extracting into the same folder races moxie's validate/extract/merge
   pipeline and can half-extract archives.

## Usage flow

```go
c := jd.New(email, password,
    jd.WithDeviceName("moxie"),                          // default
    jd.WithZapLogger(jd.NewZapFromSlog(log.Logger)),     // route JD logs into moxie's sink (TUI-safe)
)

// 1. feed URLs — direct links or F95Zone thread URLs (JD's decrypter expands them)
err := c.AddLinks([]string{threadURL, "https://host/file.zip"},
    jd.WithAutostart(true),                              // default: start immediately
    jd.WithDestinationDir("/mnt/games/SomeGame"),
    jd.WithPackageName("SomeGame"),
)

// 2. poll until the batch is terminal (finished / skipped / disabled)
done, err := c.WaitDownloads(ctx,
    jd.WithPollInterval(5*time.Second),                  // default
    jd.WithStallTimeout(10*time.Minute),                 // default; 0 disables
    jd.WithProgress(func(p jd.Progress) { /* TUI progress line */ }),
)

// 3. done contains the terminal link states → run moxie's validate/extract/merge
```

`QueryDownloads()` gives a one-shot snapshot for manual polling.
`Devices()`/`Ping()` are health checks ("is the sidecar up?").
`Grabber()`/`Downloads()` expose the raw jdownloader-go interfaces for
operations the wrapper doesn't cover yet (clear/remove/force).

The sidecar is driven entirely over the cloud API, so moxie works from any
machine — the JD2 instance does not need to share a network with moxie.

## Failure modes (from the research verdict)

1. **Captcha-gated hosts stall indefinitely.** krakenfiles asks captchas
   even for premium; vikingfile's Turnstile challenge needs a paid solver
   (2captcha/deathbycaptcha, ~$1 per 1k solves; 9kw has no Turnstile
   support). Without a solver configured inside JD, the link sits at
   0 B/s forever. `WaitDownloads` surfaces this as `ErrStalled` after
   `WithStallTimeout` (default 10 min) so moxie can report "stuck behind
   captcha" instead of hanging.

2. **Plugin breakage on a 7–14 day cycle.** JD host plugins break when
   hosts change their markup; JD's auto-update fixes them in waves every
   one to two weeks. Between breakage and fix, the host fails inside JD.
   moxie must not treat a JD failure as final — fall back to the native
   resolver or report the host as temporarily unsupported.

3. **MyJDownloader auth changes.** Password changes, 2FA rollout, or
   account bans invalidate the session. `Connect()` fails with the API
   error message surfaced; the CLI integration should surface "update
   MyJDownloader credentials in config" distinctly from "sidecar offline".

4. **F95Zone thread URLs require an F95Zone account inside JD.** The
   thread decrypter needs the F95Zone cookie/session configured in JD's
   account manager. If it isn't, decryption fails and the link is dropped
   — moxie should prefer passing already-resolved direct download URLs
   and use thread URLs only when the user has configured JD accordingly.

5. **JVM memory floor.** 300–700 MB RSS regardless of load. Never start
   the sidecar implicitly; gate it behind user opt-in (config flag /
   compose manifest) and document the cost.

## Logging

The underlying `jdownloader-go` client requires zap. Use
`jd.NewZapFromSlog(moxieLogger)` to bridge its output into moxie's slog
sink — critical for the TUI, where anything written to stderr would corrupt
the screen. Level filtering stays with slog (`MOXIE_LOG_LEVEL`, log file
mode), so JD debug output automatically lands in moxie's daily log files.

## Dependency

`github.com/rkosegi/jdownloader-go v1.0.3` (Apache-2.0, active). It
implements the documented MyJDownloader protocol: AES-128-CBC encryption
with SHA-256 derived session keys, HMAC-SHA256 URI signatures, and
device-scoped `t_<session>_<device>` calls. The unit tests in
`client_test.go` mock the API at the HTTP layer and re-implement the same
crypto, so every test verifies real encrypted request/response handling —
no live JD2 needed.
