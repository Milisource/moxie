package browserresolve

import (
	"context"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/mili/moxie/internal/log"
)

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
	`//a[contains(concat(' ', normalize-space(@class), ' '), ' host_link ')]`,
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

// clickRecaptchaCheckbox clicks the "I'm not a robot" checkbox inside the
// reCAPTCHA iframe when present and unchecked. Returns true when clicked.
func clickRecaptchaCheckbox(ctx context.Context, page *rod.Page) bool {
	if ctx.Err() != nil {
		return false
	}
	frameEl, err := page.Timeout(clickElementTimeout).ElementX(recaptchaFrameXPath)
	if err != nil {
		return false
	}
	frame, err := frameEl.Frame()
	if err != nil {
		log.Debug("browserresolve: entering recaptcha frame failed", "error", err)
		return false
	}
	box, err := frame.Timeout(clickElementTimeout).ElementX(
		`//*[@id='recaptcha-anchor' and @aria-checked='false']`)
	if err != nil {
		return false // no unchecked checkbox (already solved or a harder challenge)
	}
	if err := box.Click(proto.InputMouseButtonLeft, 1); err != nil {
		log.Debug("browserresolve: recaptcha checkbox click failed", "error", err)
		return false
	}
	log.Info("browserresolve: clicked reCAPTCHA checkbox (headless challenge)")
	return true
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
