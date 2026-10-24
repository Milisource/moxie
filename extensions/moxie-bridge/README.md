# moxie-bridge (browser extension)

Lets moxie use **your** browser to pass F95Zone's `/dl/` hCaptcha check, then
downloads the resolved file itself (progress, resume, archive handling, install
merge — all unchanged).

This is a prototype: load it unpacked, no store listing.

## How it works

1. `moxie download --extension <game>` starts a loopback listener on
   `127.0.0.1` and opens the fresh `/dl/` link in your browser with a fragment:
   `https://f95zone.to/dl/…#moxie=<port>:<token>`.
2. The fragment never leaves the browser (fragments are not sent to servers).
   The content script reads it and tells the background worker which tab to
   watch.
3. You pass the check (click Continue / the hCaptcha). When the tab reaches the
   file host, the worker POSTs that URL to `127.0.0.1:<port>/resolved`.
4. moxie validates the single-use token and downloads the URL itself.

The token is random per resolve and single-use; the listener binds loopback
only and rejects F95Zone-internal URLs (it needs the final host page).

## Install

### Chrome / Chromium / Edge / Brave

1. Copy `manifest.chrome.json` to `manifest.json` in this folder.
2. Open `chrome://extensions`, enable **Developer mode**.
3. **Load unpacked** → select this `extensions/moxie-bridge` folder.

### Firefox

1. Copy `manifest.firefox.json` to `manifest.json` in this folder.
2. Open `about:debugging#/runtime/this-firefox`.
3. **Load Temporary Add-on…** → select `manifest.json`.

Firefox unloads temporary add-ons on restart; repeat step 3 after a restart (or
sign the extension for permanent use).

## Run

```bash
moxie download --extension <id|name>
# also works with --all and --extract
```

moxie prints the port it is listening on, opens the tab, and waits up to
10 minutes for the check. Subsequent mirrors in the fallback chain each re-open
a tab — pass the check on whichever you prefer.

## Notes / limits

- One extension install covers Chrome-family **or** Firefox, matching whichever
  browser you actually download in. Cookies live in your browser, so the check
  sees your real session.
- The extension only runs on `https://f95zone.to/dl/*`; it never reads other
  pages.
- If nothing happens, check `chrome://extensions` → moxie-bridge → **Errors**,
  and that the tab URL ends with `#moxie=<port>:<token>`.
- This does not bypass the check: a human still passes it once per link, which
  is why it works.
