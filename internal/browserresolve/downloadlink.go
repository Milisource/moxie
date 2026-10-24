package browserresolve

import (
	"context"
	"errors"
	"fmt"
)

// ResolveDownloadLinkURL drives F95Zone's download-link page
// (/dl/<postID>/?u=…&k=…&r=…&t=…) in the user's browser and returns the
// destination URL — without downloading anything.
//
// Since the site's 2026-10 download-link change, thread posts no longer
// expose plain /masked/<host>/… URLs. Every download anchor is an opaque,
// short-lived /dl/ link behind a "Just a quick check" page: an invisible
// hCaptcha plus a script-check form. Neither can be satisfied over plain
// HTTP, so the browser clicks Continue; the page then either navigates to
// the host or to the older /masked/ interstitial, which this function also
// clicks through.
//
// When the automated session is met with Cloudflare's "Performing security
// verification" page instead, the check needs a human. The headless attempt
// fails with ErrVerificationPage and the URL is retried once in a visible
// browser window; the user passes the check there and the flow continues by
// itself.
//
// Requires the click-capable Chrome-family engine; auto selection prefers
// rod for /dl/ URLs.
func ResolveDownloadLinkURL(ctx context.Context, dlURL string, opts ...Option) (string, error) {
	o := defaultOptions()
	for _, f := range opts {
		f(&o)
	}
	dest, err := resolveDownloadLinkAttempt(ctx, o, dlURL)
	if err == nil || !errors.Is(err, ErrVerificationPage) {
		return dest, err
	}
	return retryHeadful(ctx, o, err, func(ctx context.Context, o Options) (string, error) {
		return resolveDownloadLinkAttempt(ctx, o, dlURL)
	})
}

// resolveDownloadLinkAttempt is one browser session for a /dl/ link: engine
// selection, profile discovery/copy, then the click flow. The profile copy is
// per attempt so a visible retry starts clean.
func resolveDownloadLinkAttempt(ctx context.Context, o Options, dlURL string) (string, error) {
	eng, err := selectEngine(&o, dlURL)
	if err != nil {
		return "", err
	}
	re, ok := eng.(*rodEngine)
	if !ok {
		return "", fmt.Errorf("browserresolve: download-link unwrap needs the click-capable engine (chrome-family); selected %T", eng)
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

	return re.resolveDownloadLink(ctx, engineRequest{url: dlURL, profileDir: profileCopy, opts: o})
}
