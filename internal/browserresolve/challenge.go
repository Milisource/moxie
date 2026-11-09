package browserresolve

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/mili/moxie/internal/log"
)

// verificationWaitTimeout bounds how long a visible browser window waits for
// the user to complete a bot-verification check before the session is
// abandoned. The solvers that call into this package are themselves bounded
// (5 min in InstallDownloaderFallback), so a headless attempt, the visible
// retry, and this wait all fit inside one download.
var verificationWaitTimeout = 2 * time.Minute

// verificationPollInterval is the pause between checks for the user having
// cleared the verification page.
const verificationPollInterval = time.Second

// verificationMarkers are the phrasing served by Cloudflare's managed
// challenge. Matching is case-insensitive on title + body text.
//
// Deliberately no generic "verify you are human" marker: F95Zone's own
// "Just a quick check" page carries similar wording, and the click flow — not
// the gate — handles that page. Treating it as a wall would deadlock the
// solver in a wait that can never clear.
var verificationMarkers = []string{
	"performing security verification",
	"this website uses a security service to protect against malicious bots",
	"checking your browser",
	"enable javascript and cookies to continue",
	"just a moment",
}

// looksLikeVerification reports whether a page title/body pair is a
// bot-verification interstitial. Split out from the rod-backed helpers so the
// phrasing can be unit-tested without a browser.
func looksLikeVerification(title, body string) bool {
	text := strings.ToLower(title + "\n" + body)
	for _, marker := range verificationMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// isVerificationPage reports whether the page is currently a
// bot-verification interstitial rather than the /dl/ check page or the host
// landing page.
func isVerificationPage(page *rod.Page) bool {
	info, err := page.Info()
	title := ""
	if err == nil {
		title = info.Title
	}
	body := ""
	if el, err := page.Timeout(time.Second).Element("body"); err == nil {
		if text, err := el.Text(); err == nil {
			body = text
		}
	}
	return looksLikeVerification(title, body)
}

// canShowBrowserWindow reports whether a visible browser window can reach a
// human: always true off Linux, and on Linux only with a display server.
// Headful Chrome on a display-less box would land in an invisible X server,
// so the caller returns the manual `--open` fallback instead.
func canShowBrowserWindow() bool {
	if runtime.GOOS != "linux" {
		return true
	}
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

// verificationGate handles a verification page for one browser session. A
// headless session cannot pass the check (Cloudflare binds clearance to a
// human interaction), so it returns ErrVerificationPage and the caller
// retries with a visible window. A headful session asks the user to finish
// the check and waits for the page to move on.
func verificationGate(ctx context.Context, page *rod.Page, headful bool) error {
	if !isVerificationPage(page) {
		return nil
	}
	if !headful {
		title := ""
		if info, err := page.Info(); err == nil {
			title = info.Title
		}
		return fmt.Errorf("%w (page title=%q)", ErrVerificationPage, title)
	}
	log.Warn("browserresolve: the site is showing a security check — complete it in the browser window; the download continues by itself once it passes")
	if err := waitForVerificationClearance(ctx, page, verificationWaitTimeout); err != nil {
		return err
	}
	log.Info("browserresolve: security check passed, continuing")
	return nil
}

// waitForVerificationClearance polls until the page is no longer a
// verification interstitial, the wait budget elapses, or the context dies.
func waitForVerificationClearance(ctx context.Context, page *rod.Page, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	for {
		if err := ctx.Err(); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return fmt.Errorf("%w: the check was not completed before the session deadline", ErrVerificationPage)
			}
			return err
		}
		if !isVerificationPage(page) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w: the check was not completed within %s", ErrVerificationPage, wait)
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("%w: the check was not completed before the session deadline", ErrVerificationPage)
			}
			return ctx.Err()
		case <-time.After(verificationPollInterval):
		}
	}
}

// retryHeadful retries a solver in a visible browser window after the
// headless session hit a verification page. The URL solvers copy the profile
// per attempt, so the visible session starts from a clean copy (and its
// clearance cookies die with it, like every other session).
func retryHeadful(ctx context.Context, opts Options, cause error, attempt func(context.Context, Options) (string, error)) (string, error) {
	if opts.Headful {
		return "", cause
	}
	if !canShowBrowserWindow() {
		return "", fmt.Errorf("%w; no display is available to show the check — open the link in your own browser (`moxie download --open <id>`) and retry", cause)
	}
	log.Warn("browserresolve: automated session hit a security verification page; retrying in a visible browser window — complete the check there and the download continues automatically")
	opts.Headful = true
	dest, err := attempt(ctx, opts)
	if err != nil && errors.Is(err, ErrVerificationPage) {
		return "", fmt.Errorf("%w — the check was not completed in the browser window; open the link in your own browser with `moxie download --open <id>` (or `--extension`) and retry", err)
	}
	return dest, err
}
