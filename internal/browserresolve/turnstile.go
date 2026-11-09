package browserresolve

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/mili/moxie/internal/log"
)

// Cloudflare Turnstile flow (vikingfile.com/f/<hash> as of 2026-10):

//	GET /f/<hash> → HTML renders a Turnstile widget (#captcha) whose
//	callback is the page's cloudflareCallback(token).
//	On success cloudflareCallback XHR-POSTs cf-turnstile-response=<token>
//	to the same URL, parses the JSON {"link": "..."} response, and sets
//	#download-link.href (the anchor starts hidden and href-less).
//
// NOTE (2026-10-04): live verification showed this host is NOT passable with
// the rod/CDP engine — the widget renders in a closed shadow root and
// Cloudflare answers "Verification failed". This solver is therefore dormant
// (vikingfile is scored as a hard wall); see F95-ob9m for the full findings.

// ResolveTurnstileURL drives a Cloudflare Turnstile-gated file page in the
// user's browser and returns the download URL the page reveals from its
// client-side callback — without downloading anything (the Go path fetches
// the URL with browser cookies + Referer, preserving progress/resume).
//
// It mirrors ResolveMaskedURL: session cookies are injected from every
// browser store (kooky), the managed-challenge checkbox is clicked when one
// is shown, and the page's own widget callback does the rest. Requires the
// click-capable Chrome-family engine (raw-launch Firefox has no DOM access).
func ResolveTurnstileURL(ctx context.Context, pageURL string, opts ...Option) (string, error) {
	o := defaultOptions()
	for _, f := range opts {
		if f != nil {
			f(&o)
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	eng, err := selectEngine(&o, pageURL)
	if err != nil {
		return "", err
	}
	re, ok := eng.(*rodEngine)
	if !ok {
		return "", fmt.Errorf("browserresolve: Turnstile unwrap needs the click-capable engine (chrome-family); selected %T", eng)
	}

	profileDir := o.ProfileDir
	if profileDir == "" {
		profileDir, err = re.profileDir("")
		if err != nil {
			return "", err
		}
	}
	profileCopy, err := copyProfileForSession(profileDir)
	if err != nil {
		return "", err
	}
	defer removeDir("profile copy", profileCopy)

	return re.resolveTurnstile(ctx, engineRequest{url: pageURL, profileDir: profileCopy, opts: o})
}

// resolveTurnstile opens the page, clears the Turnstile widget when a
// managed challenge is shown, and waits for #download-link to receive a
// non-empty href from the page's cloudflareCallback.
func (e *rodEngine) resolveTurnstile(ctx context.Context, req engineRequest) (string, error) {
	b, page, cleanup, err := e.open(ctx, req)
	if err != nil {
		return "", err
	}
	defer cleanup()

	// Insurance against pages that auto-start a download (or a stray click):
	// they land in a temp dir instead of the user's default download folder.
	if err := (proto.BrowserSetDownloadBehavior{
		Behavior:         proto.BrowserSetDownloadBehaviorBehaviorAllowAndName,
		BrowserContextID: b.BrowserContextID,
		DownloadPath:     os.TempDir(),
	}).Call(b); err != nil {
		log.Debug("browserresolve: turnstile flow download behavior failed", "error", err)
	}

	link, err := waitForTurnstileDownloadLink(ctx, rodTurnstileDOM{page: page}, turnstileLinkPollTimeout, turnstilePollInterval)
	if err != nil {
		return "", fmt.Errorf("browserresolve: Turnstile unwrap in browser: %w", err)
	}
	log.Info("browserresolve: Turnstile unwrap in browser succeeded", "url", link)
	return link, nil
}

// turnstileFrameXPath locates the Cloudflare Turnstile widget iframe. The
// interstitial host serves the widget from challenges.cloudflare.com; a
// managed challenge renders the "Verify you are human" checkbox inside it.
var turnstileFrameXPath = `//iframe[contains(translate(@src, 'ABCDEFGHIJKLMNOPQRSTUVWXYZ', 'abcdefghijklmnopqrstuvwxyz'), 'challenges.cloudflare.com')]`

// turnstileCheckboxXPaths are the managed-challenge checkbox candidates,
// tried in order. Turnstile has shipped several DOM shapes (a native
// checkbox, a role="checkbox" div, and a labelled control), so no single
// selector is reliable across widget versions.
var turnstileCheckboxXPaths = []string{
	`//input[@type='checkbox']`,
	`//*[@role='checkbox']`,
	`//label[contains(translate(., 'ABCDEFGHIJKLMNOPQRSTUVWXYZ', 'abcdefghijklmnopqrstuvwxyz'), 'verify you are human')]`,
	`//*[contains(@class, 'cb-c')]`,
}

const (
	// turnstileLinkPollTimeout bounds how long the flow waits for
	// #download-link to be populated after the widget is cleared. The
	// callback fires a XHR, so the link lags the token by a round-trip; a
	// cold headless start plus the widget load can take tens of seconds.
	turnstileLinkPollTimeout = 45 * time.Second
	// turnstilePollInterval is the pause between link/checkbox checks.
	turnstilePollInterval = 250 * time.Millisecond
	// turnstileClickTimeout bounds each checkbox-selector search inside the
	// Turnstile iframe.
	turnstileClickTimeout = 2 * time.Second
)

// turnstileDOM abstracts the two DOM interactions of the Turnstile link
// flow so the polling loop can be unit-tested without launching a browser.
type turnstileDOM interface {
	// downloadLinkHref returns the current href of #download-link, or "" while
	// the anchor is still hidden/href-less.
	downloadLinkHref() string
	// clickTurnstileCheckbox clicks the managed-challenge checkbox inside the
	// Turnstile iframe when present, reporting whether it clicked one.
	clickTurnstileCheckbox(ctx context.Context) bool
}

// rodTurnstileDOM is the real turnstileDOM backed by the rod page.
type rodTurnstileDOM struct{ page *rod.Page }

// downloadLinkHref reads #download-link's href via a single JS evaluation.
// It never waits: the polling loop owns the timing, so each read is a cheap
// point-in-time snapshot.
func (d rodTurnstileDOM) downloadLinkHref() string {
	res, err := d.page.Eval(`() => {
		const el = document.getElementById('download-link');
		return el ? (el.getAttribute('href') || '') : '';
	}`)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(res.Value.Str())
}

// clickTurnstileCheckbox enters the Turnstile iframe and clicks the
// managed-challenge checkbox when one is present. A non-interactive
// (auto-pass) widget has no checkbox, so this reports false and the polling
// loop keeps waiting for the link instead.
func (d rodTurnstileDOM) clickTurnstileCheckbox(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	found, frameEl, err := d.page.HasX(turnstileFrameXPath)
	if err != nil || !found {
		return false
	}
	frame, err := frameEl.Frame()
	if err != nil {
		log.Debug("browserresolve: entering Turnstile frame failed", "error", err)
		return false
	}
	for _, xpath := range turnstileCheckboxXPaths {
		box, err := frame.Timeout(turnstileClickTimeout).ElementX(xpath)
		if err != nil {
			continue
		}
		if err := box.Click(proto.InputMouseButtonLeft, 1); err != nil {
			log.Debug("browserresolve: Turnstile checkbox click failed", "xpath", xpath, "error", err)
			continue
		}
		log.Info("browserresolve: clicked Turnstile checkbox (managed challenge)")
		return true
	}
	return false
}

// waitForTurnstileDownloadLink polls until #download-link has a non-empty
// href (populated by the page's cloudflareCallback), clicking the Turnstile
// managed-challenge checkbox once if one appears, or until timeout/ctx
// cancellation. The link is returned for the Go path to download.
func waitForTurnstileDownloadLink(ctx context.Context, dom turnstileDOM, timeout, interval time.Duration) (string, error) {
	if interval <= 0 {
		interval = turnstilePollInterval
	}
	deadline := time.Now().Add(timeout)
	clicked := false
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if href := dom.downloadLinkHref(); href != "" {
			return href, nil
		}
		if !clicked && dom.clickTurnstileCheckbox(ctx) {
			clicked = true
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("%w: Turnstile page did not reveal a download link within %s", ErrChallengePage, timeout)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(interval):
		}
	}
}
