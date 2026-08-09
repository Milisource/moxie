# Research: F95Zone masked-URL captcha mechanism + 2026-08-09 tool A/B round

Status: research complete. Reverse-engineered the F95Zone masked-URL
reCAPTCHA wall end-to-end and A/B-tested the candidate tools from the
captcha/anti-detection round. Verdicts: **no TLS-layer win** (adaptive
rate wall, not fingerprint), **captcha solver design fully validated**
(blocked only on an API key), **one cheap stealth win implemented**.

## The masked-URL mechanism (fully known now)

The masked page (`https://f95zone.to/masked/<host>/<thread>/<id>/...`)
is a 3 KB server-rendered page with no captcha markup:

```html
<a href="#" class="host_link"><span>Continue to Pixeldrain</span></a>
<div id="loading" style="display:none;"></div>
<div id="captcha" style="display:none;"></div>
<div id="error" style="display:none;"></div>
<script src="https://www.google.com/recaptcha/api.js?render=explicit" async defer></script>
<script src="/assets/js/masked.js"></script>
```

`/assets/js/masked.js` (1.2 KB, same session, plain GET) contains the
whole flow:

1. Click `.host_link` → `$.ajax({data: {xhr:1, download:1}})`
2. Server answers `{"status":"ok","msg":"<real URL>"}` — or
   `{"status":"captcha"}` (the wall)
3. On `captcha`: `grecaptcha.render("captcha", {theme:"dark",
   sitekey:"6LcwQ5kUAAAAAAI-_CXQtlnhdMjmFDt-MruZ2gov", callback: fn})`
4. Callback (widget solved) → `$.ajax({data: {xhr:1, download:1,
   captcha:<token>}})` → `ok` → redirect

Key facts for automation:

- **sitekey `6LcwQ5kUAAAAAAI-_CXQtlnhdMjmFDt-MruZ2gov`** — extractable
  from `masked.js` (not the HTML), so the solver chain needs no
  hardcoding.
- **The token POSTs as the `captcha` field**, not `g-recaptcha-response`.
  Verified live 2026-08-09: POST with `captcha=FAKE_TOKEN` returns
  `{"status":"error","msg":"CAPTCHA unsuccessful"}` — the server parses
  the param and rejects the token (a wrong param name would have returned
  the generic `"Please complete the CAPTCHA"` wall).
- The wall is **rate/time-based, not fingerprint-based**: in a 6-round
  A/B (same session), both personas got `ok` for rounds 1-2 and `captcha`
  for rounds 3-6 **in lockstep** — once the rate budget is exhausted,
  everyone is walled regardless of client. It also clears over time (the
  earlier real install succeeded on the Go path minutes after a walled
  browser run).

## A/B round results (live, 2026-08-09)

### 1. tls-client (bogdanfinn) vs stdlib — NO ADVANTAGE, not adopted

| Test | stdlib | tls-client Chrome_146 |
|---|---|---|
| buzzheavier `/download` (4 rounds) | 4/4 pass (204) | 4/4 pass (204) |
| F95Zone masked unwrap (6 rounds) | 3 ok / 3 walled | 2 ok / 4 walled |

The masked wall hits both transports in lockstep (rounds 3-6 both
walled) → adaptive rate limit, not TLS fingerprinting. The h1-only uTLS
"impossible browser" verdict from F95-j3b5 stands as the only reason to
ever touch transports; tls-client adds a heavy dependency
(fhttp/quic-go forks) for zero gain. **Do not adopt.**

### 2. Captcha-solver design — VALIDATED end-to-end, needs only an API key

Pure-Go chain proven live:

1. `POST {xhr:1, download:1}` (session cookie) → `captcha` → get
   `/assets/js/masked.js` → sitekey (probe: `TestMaskedSitekeyExtract`)
2. Solve via 2captcha/CapSolver (`ReCaptchaV2Task`-style: sitekey +
   masked page URL; providers run their own worker IPs, sidestepping
   Google's datacenter-IP refusal to serve the widget)
3. `POST {xhr:1, download:1, captcha:<token>}` → `ok` → real URL
   (param verified: `TestMaskedCaptchaTokenParamProbe`)

Tokens are single-use (~2 min TTL) — solve per attempt, never cache.
Candidate SDKs: `github.com/2captcha/2captcha-go` (official, MIT,
active), `github.com/nukilabs/capsolver` (cheaper). Turnstile support in
both also covers buzzheavier-style gates.

### 3. Stealth flags on the rod engine — WIN, implemented

`bot.sannysoft.com` A/B with the Playwright-cache Chromium
(`TestStealthFlagsLive`):

| | navigator.webdriver | detection table |
|---|---|---|
| current (enable-automation removed) | **true** | 29 passed / 1 failed (WebDriver row) |
| + `--disable-blink-features=AutomationControlled` | **false** | 30 passed / 1 failed (WebGL renderer only) |

The residual WebGL-renderer failure is the headless software-GL
rasterizer string — inherent to headless, minor for reCAPTCHA scoring.
The flag is now set in `applyLaunchFlags` (engine.go). The click flows
re-verified after the change.

## Not tested / deferred

- Real solver round-trip (needs an API key — user decision).
- go-rod/stealth JS: skipped — rod#1208 shows its hardcoded `en-US,en`
  languages can *hurt*; the flag alone already flips the automation
  signal, and the browser-side masked flow clicks the widget natively.
- Camoufox: rejected (Python/Playwright-only, downloaded browser, 2026
  maintenance gap) — see the research round summary.

## Probes kept in the repo (all MOXIE_LIVE-gated)

- `internal/downloader/tls_client_probe_test.go` — tls-client matrix,
  masked A/B, sitekey extraction, captcha param probe
- `internal/browserresolve/stealth_live_test.go` — stealth flags A/B
- `internal/downloader/masked_live_probe_test.go` — original masked-page
  probe (homepage session check + wiring dump)

Dependency note: `github.com/bogdanfinn/tls-client` is imported only by
the probe test; remove it if the probe file is ever dropped.
