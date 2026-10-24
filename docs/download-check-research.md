# F95Zone download check — hCaptcha research (2026-10-24)

> Why automated downloads stopped working, what the check actually is, and
> every option found for getting past it — including one clean server-side
> path (donor DDL) and why the shipped tools all choose a human-in-the-loop.

Related: [`scraper.md`](scraper.md#host-detection) (link parsing),
[`browserresolve.md`](browserresolve.md) (browser engines).

---

## What the check is

Every download anchor on a thread is now
`https://f95zone.to/dl/<postID>/?u=<user>&k=<linkKey>&r=<ts>&t=<token>`.
The page (`data-template="f95dl_check"`) stacks two independent checks:

1. **hCaptcha** — the site's own widget (`/js/f95vibecoded/downloadlinks/captcha.js`)
   renders a **visible** hCaptcha with sitekey `c3266d70-c784-4c86-9842-af98deb61432`.
   The `?captcha=xenforo` alternative renders XenForo's **invisible** hCaptcha
   (sitekey `f1592dee-67b6-4670-9062-649933011aa2`) plus a hidden
   `h-captcha-response` field.
2. **Script check** — `data-code` is a base64-encoded, reversed
   `<pageTime>.<random>` string; `page.js` decodes it after ~2.1 s into the
   hidden `w` field, then submits the form (`_xfToken`, `had_check=1`, `w`).

Verified by direct POST:

| Submitted | Result |
|---|---|
| `_xfToken` + `w` (decoded code) | `The check wasn't passed. Please try again.` |
| `_xfToken` + `w` + `had_check=1` | same |
| no `w`, no token | `The check couldn't be fetched…` |
| hidden duplicate anchor's `k` | `That link is no longer in the post.` |

So the script code is **not** a captcha bypass; an hCaptcha token is mandatory.
`r`/`t` are request-time values, **not** an expiry: a 40-minute-old link still
opens the check page correctly.

### Automation result

With a logged-in session, both Playwright (Chromium) and rod (user's Chrome
profile) reach the page. Neither can obtain a token:

- Playwright: clicking Continue → `The check wasn't passed`;
  `hcaptcha.execute()` → `challenge-expired` immediately.
- rod: the copied Chrome profile trips Cloudflare's managed challenge before
  the check page even renders; headful/xvfb makes no difference.
- The page's hCaptcha is a normal widget (not passive mode), so there is no
  "invisible auto-pass" to inherit.

This matches hCaptcha's own 2026 position: active challenges are explicitly
built to stay effective against browser-use agents, and challenges rotate when
solve rates rise ([hCaptcha blog, 2026-02](https://www.hcaptcha.com/blog/hcaptcha-captchas-are-highly-effective-against-bots-and-agents-in-2026)).

---

## What comparable tools do

| Tool | Approach | Notes |
|---|---|---|
| **F95Checker** 11.2 (commits 2026-10-17…24) | Integrated **Qt WebEngine** window; user solves the captcha; cookies are harvested and the request retried automatically (`Solve captcha inline and retry automatically`) | Stores `/dl/` links as-is. Also strips attachment signing params (`?h=…`) and blackholes the attachments IP that enabled signed URLs |
| **XLibrary** 3.2/3.3 | **Browser extension** + catalog server; "Download from this thread" runs in the user's logged-in browser | Human passes the check in their own browser; the app receives the resulting file/link |
| **moxie** (this repo) | `download --open` opens the fresh `/dl/` link in the default browser; `--watch` picks up the finished archive | CLI prints this path when every automatic link fails |

None of them solves the hCaptcha server-side. That is the signal: the
maintained tools treat this as a human gate, not a scraping problem.

---

## Options evaluated

### 1. Human-in-the-loop (recommended default)

Open the link in the user's own browser, where the check passes normally:

```bash
moxie download --open <id>            # open best fresh link
moxie download --open --watch <id>    # ...and auto-install the archive
```

Pros: reliable, no extra dependency, no ToS problem. Cons: one click per
download. This is what F95Checker/XLibrary effectively do, with more UI.

### 2. Donor DDL — the clean server-side path

F95Zone runs a **Donor Direct Download** service at `/sam/dddl.php`. It is a
cookie-authenticated JSON endpoint that returns direct file URLs, i.e. **no
hCaptcha**:

```http
POST /sam/dddl.php?raw=1        (form-encoded, session cookie)
thread_id=<threadID>
→ {"status":"ok","msg":{
     "session":"<id>",
     "files":{"<section>":{"<title>":{"file_id":"…","filename":"…","size":"…","date":"…","hash":"…"}}}}}
```

```http
POST /sam/dddl.php?raw=1
thread_id=<threadID>&file=<file_id>&session=<session>
→ {"status":"ok","msg":{"url":"https://…/file","cookie":"<cookie header>"}}
```

Non-donor accounts get `{"status":"error","msg":"Not a donor"}` (verified with
the current test account). F95Checker uses this for its in-app download list.

This is the only fully automated, officially-provided path. It is
capability-gated on a paid donor subscription and should degrade cleanly to
the human path. Tracked as a follow-up in beads.

### 3. hCaptcha accessibility cookie

hCaptcha's official accommodation: sign up once at
`hcaptcha.com/accessibility`, receive an encrypted cookie, and "automatically
pass or receive an accessibility challenge" on participating sites. The cookie
is usable several times per day and must be refreshed periodically
([hCaptcha accessibility](https://www.hcaptcha.com/accessibility)).

This is a real per-account credential, not a bypass. moxie must not
auto-provision it; a user who already has one could supply it as a normal
browser cookie. Not implemented (eligibility/ToS are the user's call).

### 4. Commercial solver APIs

Services like 2Captcha, CapSolver and Anti-Captcha accept `sitekey` + page URL
and return a token, which is injected into `h-captcha-response` before submit.
Technically compatible with the page's form, but:

- per-solve cost and an external account/API key;
- hCaptcha actively profiles and rotates challenges against solver traffic;
- almost certainly against F95Zone's rules and hCaptcha's terms.

Not recommended, not implemented.

### 5. Anti-detect browser + residential proxy

The usual "avoid the captcha" stack (patched fingerprint, residential IP,
human-like pacing). Since the account here is already a real, logged-in
session on a residential IP and still gets the active widget, this would only
reduce challenge frequency, not remove it. No advantage over option 1 for a
single-user desktop tool.

### 6. Browser-extension bridge (XLibrary model)

A small extension performs the click inside the user's real browser and hands
the resolved URL back to moxie (e.g. via a localhost callback). Best UX if the
project wants full automation **with** a human check, but it is a separate
distribution surface (extension store, permissions, updates) — not worth it
for the current CLI/desktop scope.

---

## Decisions for moxie

1. **Keep the browser attempt** (`browserresolve.ResolveDownloadLinkURL`): it
   is the correct flow and will work on setups hCaptcha does not flag
   (e.g. a real non-CDP browser, or when the user solves the widget).
2. **Always offer `--open`** on failure with the exact command (implemented).
3. **Add donor DDL** support behind a capability probe; it is the only clean
   automated path. Filed as a follow-up.
4. **Do not** ship captcha-solver integrations or accessibility-cookie
   automation.
5. **Strip attachment signatures** from stored cover/save URLs (implemented):
   unsigned `attachments.f95zone.to` URLs still work, and stored signed URLs
   would expire before a later fetch. F95Checker applies the same fix.
