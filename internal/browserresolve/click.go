package browserresolve

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/mili/moxie/internal/log"
)

// maskedHostLinkXPath is the F95Zone masked "Continue to <host>" link.
const maskedHostLinkXPath = `//a[contains(concat(' ', normalize-space(@class), ' '), ' host_link ')]`

// Download triggers, in priority order. The first match on the current page
// is clicked; after a click the loop re-checks, because a masked "Continue"
// lands on the host's page, which usually has its own download button.
//
// Covered flows:
//   - F95Zone masked download interstitial: "Continue to <host>" (the
//     reCAPTCHA-gated unwrap Go's masked-URL path cannot pass — F95Zone
//     answers every unwrap POST with {"status":"captcha"} since 2026-08).
//   - File-host free-download buttons (vikingfile/datanodes form POSTs,
//     pixeldrain icon buttons) — only the rod engine can click; raw-launch
//     Firefox has no DOM access.
var downloadTriggerXPaths = []string{
	// F95Zone masked page: <a href="#" class="host_link">Continue to X</a>
	maskedHostLinkXPath,
	// Free-download buttons/links by text (case-insensitive translate).
	`//button[contains(translate(., 'ABCDEFGHIJKLMNOPQRSTUVWXYZ', 'abcdefghijklmnopqrstuvwxyz'), 'free download')]`,
	`//a[contains(translate(., 'ABCDEFGHIJKLMNOPQRSTUVWXYZ', 'abcdefghijklmnopqrstuvwxyz'), 'free download')]`,
	`//button[contains(translate(., 'ABCDEFGHIJKLMNOPQRSTUVWXYZ', 'abcdefghijklmnopqrstuvwxyz'), 'download')]`,
	`//a[contains(translate(., 'ABCDEFGHIJKLMNOPQRSTUVWXYZ', 'abcdefghijklmnopqrstuvwxyz'), 'download')]`,
	// Download-submit inputs (file hosts that use <input type="submit">).
	`//input[contains(translate(@value, 'ABCDEFGHIJKLMNOPQRSTUVWXYZ', 'abcdefghijklmnopqrstuvwxyz'), 'download')]`,
	// Icon-only download controls (pixeldrain) — match by href/id.
	`//a[contains(translate(@href, 'ABCDEFGHIJKLMNOPQRSTUVWXYZ', 'abcdefghijklmnopqrstuvwxyz'), '/download')]`,
	`//*[contains(translate(normalize-space(@id), 'ABCDEFGHIJKLMNOPQRSTUVWXYZ', 'abcdefghijklmnopqrstuvwxyz'), 'download')]`,
}

// recaptchaFrameXPath locates the reCAPTCHA widget iframe. Headless
// sessions often get the "I'm not a robot" checkbox challenge instead of
// the invisible auto-pass — the checkbox must be clicked before the
// page's own flow (e.g. the masked Continue) can proceed.
var recaptchaFrameXPath = `//iframe[contains(translate(@src, 'ABCDEFGHIJKLMNOPQRSTUVWXYZ', 'abcdefghijklmnopqrstuvwxyz'), 'recaptcha')]`

// clickElementTimeout bounds each per-selector search. Auto-download flows
// bail out via the tracker's started() check, so a long search only happens
// on genuinely click-required pages.
const clickElementTimeout = 2 * time.Second

// firstClickElementTimeout is the bound for the very first selector search:
// the tab was just opened, and the page load (cold browser start, masked
// interstitial fetch) can take a few seconds before the DOM is searchable.
const firstClickElementTimeout = 12 * time.Second

// clickSettleTimeout bounds the wait after a successful click: the click
// usually triggers a navigation (masked Continue → host page), and element
// searches during a page load time out. Settle first, then search the new
// page.
const clickSettleTimeout = 8 * time.Second

// maxDownloadClicks bounds the click chain (masked Continue → reCAPTCHA
// checkbox → host download button = 2-3 clicks).
const maxDownloadClicks = 5

// clickDownloadTriggers drives click-required download flows: F95Zone
// masked "Continue" interstitials, the reCAPTCHA checkbox challenge that
// gates them in headless sessions, and file-host free-download buttons.
// It returns as soon as a download started (tracker), the context expires,
// or no trigger is left to click. Auto-download pages are never delayed
// beyond the first tracker check.
func clickDownloadTriggers(ctx context.Context, page *rod.Page, tr *downloadTracker) {
	first := true
	for click := 0; click < maxDownloadClicks; click++ {
		if ctx.Err() != nil || tr.started() {
			return
		}
		clicked := false
		for _, xpath := range downloadTriggerXPaths {
			if ctx.Err() != nil || tr.started() {
				return
			}
			timeout := clickElementTimeout
			if first {
				timeout = firstClickElementTimeout
				first = false
			}
			el, err := page.Timeout(timeout).ElementX(xpath)
			if err != nil {
				continue // not found on this page (yet)
			}
			if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
				log.Debug("browserresolve: download trigger click failed", "xpath", xpath, "error", err)
				continue
			}
			log.Debug("browserresolve: clicked download trigger", "xpath", xpath)
			clicked = true
			break
		}
		if clicked {
			if waitForDownloadOrSettle(ctx, tr, clickSettleTimeout) {
				return
			}
			continue
		}
		// No download trigger — a reCAPTCHA checkbox may be blocking the
		// page's flow (headless sessions get the challenge instead of the
		// invisible auto-pass).
		if clickRecaptchaCheckbox(ctx, page) {
			if waitForDownloadOrSettle(ctx, tr, clickSettleTimeout) {
				return
			}
			continue
		}
		log.Debug("browserresolve: no download trigger on page", "url", pageURL(page))
		return // the wait loop takes over
	}
}

// recaptchaCheckboxPollTimeout bounds how long clickRecaptchaCheckbox waits
// for the widget to appear. After the masked Continue click, masked.js only
// renders the widget into #captcha once the server answers
// {"status":"captcha"} — and the google iframe can take several seconds to
// load in a cold headless browser. A single bounded search (the old
// behaviour) failed the wall path live on 2026-08-09: "no Continue link or
// reCAPTCHA checkbox on the masked page".
const recaptchaCheckboxPollTimeout = 10 * time.Second

// recaptchaCheckboxPollInterval is the pause between widget searches.
const recaptchaCheckboxPollInterval = 500 * time.Millisecond

// clickRecaptchaCheckbox clicks the "I'm not a robot" checkbox inside the
// reCAPTCHA iframe when present and unchecked. The widget may take seconds
// to render after the Continue click, so it is polled for up to
// recaptchaCheckboxPollTimeout. Returns true when clicked.
func clickRecaptchaCheckbox(ctx context.Context, page *rod.Page) bool {
	if ctx.Err() != nil {
		return false
	}
	deadline := time.Now().Add(recaptchaCheckboxPollTimeout)
	for {
		if ctx.Err() != nil {
			return false
		}
		frameEl, err := page.Timeout(clickElementTimeout).ElementX(recaptchaFrameXPath)
		if err == nil {
			frame, err := frameEl.Frame()
			if err != nil {
				log.Debug("browserresolve: entering recaptcha frame failed", "error", err)
				return false
			}
			box, err := frame.Timeout(clickElementTimeout).ElementX(
				`//*[@id='recaptcha-anchor' and @aria-checked='false']`)
			if err == nil {
				if err := box.Click(proto.InputMouseButtonLeft, 1); err != nil {
					log.Debug("browserresolve: recaptcha checkbox click failed", "error", err)
					return false
				}
				log.Info("browserresolve: clicked reCAPTCHA checkbox (headless challenge)")
				return true
			}
			// iframe present but no unchecked checkbox yet (still loading
			// or already solved — keep polling until the deadline).
			log.Debug("browserresolve: recaptcha iframe present, checkbox not found", "error", err)
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(recaptchaCheckboxPollInterval):
		}
	}
}

// waitForDownloadOrSettle blocks until a download starts (tracker) or the
// context expires (returns true), or the settle window elapses (false).
func waitForDownloadOrSettle(ctx context.Context, tr *downloadTracker, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return true
		case <-timer.C:
			return false
		default:
			if tr.started() {
				return true
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
}

// pageURL returns the page's current URL for diagnostics, or "" when the
// page is gone.
func pageURL(page *rod.Page) string {
	info, err := page.Info()
	if err != nil {
		return ""
	}
	return info.URL
}

// clickMaskedFlow drives the F95Zone masked interstitial to its
// destination: waits for the page (the first search gets the full load
// timeout), clicks the Continue link (host_link), clicks the reCAPTCHA
// checkbox if the widget appears (headless sessions get the checkbox
// challenge), and returns the destination URL as soon as the page
// navigates away from the interstitial. Only masked-page triggers are
// used — generic download buttons must NOT be clicked here: the
// destination page's own buttons would start downloads.
func clickMaskedFlow(ctx context.Context, page *rod.Page, maskedURL string) (string, error) {
	first := true
	for click := 0; click < maxDownloadClicks; click++ {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		// A fast redirect (the unwrap answered ok on the first click — no
		// captcha) can complete before the settle below — check first.
		if dest, ok := waitForMaskedDestination(ctx, page, maskedURL, 2*time.Second); ok {
			return dest, nil
		}
		timeout := clickElementTimeout
		if first {
			timeout = firstClickElementTimeout
			first = false
		}
		el, err := page.Timeout(timeout).ElementX(maskedHostLinkXPath)
		if err == nil {
			if err := el.Click(proto.InputMouseButtonLeft, 1); err == nil {
				log.Debug("browserresolve: masked continue clicked")
				if dest, ok := waitForMaskedDestination(ctx, page, maskedURL, clickSettleTimeout); ok {
					return dest, nil
				}
				// The click hit the captcha wall: masked.js keeps the
				// host_link in the DOM but fades it out (clicks fail from
				// here on) and renders the reCAPTCHA widget into #captcha.
				// Poll for the widget before re-clicking the link — the
				// live wall path (2026-08-09) died here because the widget
				// never got a chance to load.
				if clickRecaptchaCheckbox(ctx, page) {
					if dest, ok := waitForMaskedDestination(ctx, page, maskedURL, clickSettleTimeout); ok {
						return dest, nil
					}
				}
				continue
			}
			log.Debug("browserresolve: masked continue click failed", "error", err)
		}
		// The Continue click hit the captcha wall — the widget renders
		// into #captcha; solve the checkbox if present.
		if clickRecaptchaCheckbox(ctx, page) {
			if dest, ok := waitForMaskedDestination(ctx, page, maskedURL, clickSettleTimeout); ok {
				return dest, nil
			}
			continue
		}
		log.Debug("browserresolve: no masked trigger on page", "url", pageURL(page))
		return "", fmt.Errorf("no Continue link or reCAPTCHA checkbox on the masked page")
	}
	return "", fmt.Errorf("masked page did not navigate to the real host within %d clicks", maxDownloadClicks)
}

// waitForMaskedDestination polls the page URL until it leaves the masked
// interstitial (different URL, off f95zone.to) or the window elapses.
func waitForMaskedDestination(ctx context.Context, page *rod.Page, maskedURL string, d time.Duration) (string, bool) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", false
		case <-timer.C:
			return "", false
		default:
			if dest := destinationAfterMasked(page, maskedURL); dest != "" {
				return dest, true
			}
			time.Sleep(250 * time.Millisecond)
		}
	}
}

// destinationAfterMasked returns the current page URL once it differs from
// the masked URL and is off f95zone.to (the interstitial host) — i.e. the
// page navigated to the real destination. Empty until then; a session
// login wall or the "back to f95zone" page stays on f95zone.to and never
// qualifies.
func destinationAfterMasked(page *rod.Page, maskedURL string) string {
	info, err := page.Info()
	if err != nil {
		return ""
	}
	if info.URL == "" || info.URL == maskedURL {
		return ""
	}
	u, err := url.Parse(info.URL)
	if err != nil {
		return ""
	}
	if strings.EqualFold(u.Hostname(), "f95zone.to") {
		return ""
	}
	return info.URL
}
