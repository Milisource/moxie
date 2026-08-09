# Research: Buzzheavier + Cloudflare — what actually gates the downloads (2026-08-09)

Status: research complete. Corrects the "uTLS NO-GO" verdict from F95-j3b5 and
feeds F95-675j (browser fallback wiring).

## TL;DR

The wall is **Cloudflare Turnstile**, not the TLS fingerprint. The previous
session's "fingerprint unmatchable → uTLS dead" conclusion got the mechanism
wrong; the conclusion (browser required) is still correct.

Live-verified facts (this machine, 2026-08-09):

| Claim | Finding |
|---|---|
| uTLS caps at FF120/Chrome133 | True for the v1.8.2 **release** (Jan 2026). Master has `HelloFirefox_148` since Feb 2026 (PR #362); FF151 work (PR #399) was closed unmerged. Even so, uTLS structurally lags browsers — irrelevant anyway (see next row). |
| cf_clearance is bound to IP+UA+TLS fingerprint | Confirmed by Cloudflare docs + multiple independent 2025-2026 sources. Browser-minted clearance can never be reused by a Go client that doesn't byte-match the minting browser. |
| The resolution wall is the TLS fingerprint | **Wrong.** Share page + HTMX endpoint pass with **stdlib** TLS + valid cookies — intermittently. Same transport got 200/204 and 403 "Just a moment" within seconds → CF serves an **adaptive/rate-limit challenge**, not a fingerprint rejection. |
| The real gate | The share page (captured on a 200) embeds a **Turnstile widget** (sitekey `4bc3e5bee148058d2ed4789415296a65548e2b8e-dirty`). The `/download` endpoint returns 204 with a self-referential `hx-redirect` when no Turnstile proof is present. No Go client can produce a Turnstile proof. |
| Half-parrots are worse than nothing | uTLS FF148 with the http/1.1-only ALPN hack (current `utls.go`) is an **impossible browser** (Firefox TLS that doesn't offer h2). In the A/B probe it was challenged 4/4 times while stdlib passed 4/4 on the same run. |
| fafda.to (real DD host) | **Not Cloudflare-gated** — plain HTTP. But blocked at this network's ASUS router (AiProtection block page `blocking.asus.hns.tm/?cat_id=75`, parental-control category). Unblocking the router makes it reachable; still needs the `v=<token>` from the Turnstile-gated flow. |
| dd.buzzheavier.com / ts.buzzheavier.com | Origins currently **down** (Cloudflare 521/522) — the earlier "file hop challenged" observations may partly have been origin failures. |
| cumulus13/buzzheavier client | Uses the **same HTMX flow moxie implements** (`hx-request`, `priority: u=1, i`, read `Hx-Redirect`). Its own issue tracker: "works great as long as there is no captcha". |

## fafda.to follow-up (2026-08-09, router unblocked — live-verified)

- `http://fafda.to/*` → 301 to HTTPS (Cloudflare-proxied; `server: cloudflare`,
  cf-ray on every response) — but the zone does **not** serve challenges.
  curl passes TLS checks un-challenged; stdlib Go will too.
- `https://fafda.to/d/<shareid>` → **404 `{"error":"not found"}`** (no token).
- `https://fafda.to/d/<shareid>?v=<anything>` → **403 `{"error":"forbidden"}`**
  — the `v` token is enforced at the app layer (JSON API, not CF challenge).
- Root serves `text/plain` "Who's a handsome lizard?"; other paths
  (`/f/`, `/download/`, `/api`, `/health`) → 404/000.
- So the **file hop is Go-able with a plain stdlib client once a valid
  `v=` token exists** — the token still requires the Turnstile-gated flow
  (browser). Hybrid plan: browser mints token (24 h validity per
  buzzheavier's help page) → Go downloads the bytes with resume/progress.
- `dd.buzzheavier.com/developers` (official developer docs) is itself
  challenge-walled (403 "Just a moment"); `ts.buzzheavier.com` is dead
  (status 000); `buzzheavier.com/` root 403s for Go clients.

## What this means for moxie

1. **buzzheavier resolution (token) is browser-only, full stop.** No TLS
   impersonation level helps; the token needs a Turnstile proof.
2. **The uTLS path (F95-j3b5) is dead for buzzheavier** — and the current
   http/1.1-only ALPN hack makes every uTLS profile an impossible browser,
   which CF flags *more* than plain stdlib. Keep uTLS strictly opt-in; if it
   stays, the ALPN fix must eventually be replaced by real h2 (fhttp /
   x/net/http2 over `*utls.UConn` via a `ConnectionState()` wrapper — verified
   to compile against current x/net/http2, which now takes `*tls.Config` in
   `DialTLSContext`), but that work has **no payoff for Turnstile zones**.
3. **The browser fallback (F95-675j) is the answer** — and it must be
   **browser- and OS-agnostic** (Chrome/Chromium/Edge/Brave/Firefox ×
   Linux/macOS/Windows), see `docs/browserresolve.md`. The design:
   - Chrome-family: existing rod engine (extend profile discovery to
     Edge/Brave roots per OS).
   - **Firefox: raw-launch engine, zero new deps** — copy the user's Firefox
     profile (keep `cookies.sqlite` + WAL; skip `.parentlock`, `cache2/`),
     inject download prefs via `user.js` (forward-slash paths even on
     Windows), `firefox --headless -profile <copy> -no-remote <url>`, poll
     the download dir (existing `pollDownloadDir`; add `.part` suffix).
     Never `-private` (private mode changes the TLS fingerprint). Process
     tree teardown per OS (`Setpgid` + `kill(-pid)` / `taskkill /T /F`).
   - Selection: prefer the browser whose store holds `cf_clearance` for the
     host (kooky can tell); then Chrome-family; then Firefox.
   - Turnstile may need headful escalation (user's desktop; `xvfb-run -a`
     on display-less Linux).
4. **fafda.to hybrid** (live-verified after router unblock): the DD host is
   Cloudflare-proxied but challenge-free — `/d/<id>` 404 without token,
   403 `{"error":"forbidden"}` with bad `v=`. Browser mints the token once
   (Turnstile flow) → Go stdlib downloads the file bytes with resume +
   progress. This is the cheapest end-state for buzzheavier.
5. **Torrent track (F95-x8mk)**: the `/torrent` endpoint 403s because of the
   same Turnstile gate — explains why the torrent route failed.

## Verification results (same day, live from this machine)

Independently re-verified each load-bearing claim before acting on it:

| Claim | Verification |
|---|---|
| "uTLS master has FF148" | ✅ `go get @master` → `v1.8.3-0.20260802151714` declares `HelloFirefox_148` (u_common.go:614). FF151 PR #399 still unmerged. |
| "Adaptive challenge, not fingerprint" | ✅ 4-round A/B on `/download` (same cookie, same IP): stdlib **4/4 pass** (204), uTLS Chrome133 h1-only **4/4 challenged** (403 "Just a moment"). The h1-only ALPN hack makes uTLS an impossible browser CF flags *more* than stdlib. |
| "Turnstile widget on the share page" | ✅ Captured a 200 share page (stdlib + cookie, 3rd attempt after 2×403 — adaptive): contains `cf-turnstile` marker AND `hx-get="/<id>/download?t=<server-signed-token>&alt=true"`. |
| "Token required, no Turnstile proof in HTML" | ✅ **Stronger than claimed**: the `t=` token IS in the HTML (server-signed, per page load). `GET /download?t=<token>&alt=true` → 204 + `hx-redirect: https://fafda.to/d/<id>?v=<token>` — **no Turnstile proof needed, browser not required for resolution.** |
| "fafda.to challenge-free, app-layer gated" | ✅ Root 200; `/d/<id>` → 404 `{"error":"not found"}`; `/d/<id>?v=bad` → 403 `{"error":"forbidden"}`; valid `v=` → 503 `{"error":"service unavailable"}` — the **file origin is currently down** (matches dd/ts 521/522). |
| "ts.bzzhr.to dead" | ✅ Connection fails (000); DNS resolves to burritoflakes.com (Cloudflare IPs). `alt=true` redirects to fafda.to instead. |
| Adaptive 403s are per-link/rate dependent | ⚠️ e2yt4zd66jq3 page: 200 on attempt 3. sdkifmiy9zm2 page: 5×403 in one run (then 200 later runs?) — retry with backoff is mandatory; a fresh token per page load. |

**Bottom line**: the token-flow resolution is proven Go-able (F95-hs4y); the file hop needs the origin back (fafda.to 503 — infrastructure, retry/backoff). The browser fallback (F95-675j) remains for true Turnstile hosts (vikingfile, datanodes, mixdrop).

## Probe tooling

Kept in the repo as MOXIE_LIVE-gated tests (commit with the uTLS ALPN fix):
- `internal/downloader/buzz_utls_probe_test.go` — HTMX + file-hop probe, stdlib vs uTLS personas (the A/B found the h1-only "impossible browser" behavior)
- `internal/downloader/buzz_matrix_test.go` — 4-round stdlib vs uTLS matrix (the 4/4 vs 4/4 evidence above)
- `internal/downloader/buzz_pagedump_test.go` — share-page capture with cookie, marker scan (turnstile/hx-get/token)
- `internal/downloader/gofile_live_probe_test.go` — gofile E2E (masked → token → CDN hop)
- `internal/downloader/sizeverify_test.go` — truncation/size verification (offline)

## References

- uTLS releases: v1.8.2 (2026-01-13, Chrome 133 / FF 120), FF148 on master
  (PR #362 merged 2026-02-28), FF151 PR #399 closed unmerged (Parroteer
  verification: real FF 151.0.3 JA4 `t13d1617h2_86a278354501_3cbfd9057e0d`)
- Cloudflare: clearance docs ("securely tied to the specific visitor and
  device"), JA3/JA4 bot-solutions docs, precursor clearance re-evaluation
- Field evidence: trvl#213 (IP+UA+JA3 triple required), dev.to re-challenge
  loop post (2026-06), cf-solver (IP+UA binding claim — zone-dependent)
- cumulus13/buzzheavier: same HTMX flow; "no captcha" caveat in issue #1
- Track D notes (F95-yyes): fafda.to/d/<id>?v=<token>, ts.buzzheavier.com
