package browserresolve

import (
	"context"
	"fmt"
)

// ResolveMaskedURL drives the F95Zone masked download interstitial in the
// user's browser and returns the real destination URL — without
// downloading anything (the Go path fetches the destination with
// progress/resume). Session cookies are injected from every browser store
// (kooky), so any browser can hold the session.
//
// Requires the click-capable Chrome-family engine (raw-launch Firefox has
// no DOM access); auto selection prefers rod for /masked/ URLs. The flow:
// open the masked page → click "Continue" (host_link) → click the
// reCAPTCHA checkbox if the widget appears (headless sessions get the
// checkbox challenge) → return the URL the page navigates to.
func ResolveMaskedURL(ctx context.Context, maskedURL string, opts ...Option) (string, error) {
	o := defaultOptions()
	for _, f := range opts {
		f(&o)
	}
	eng, err := selectEngine(&o, maskedURL)
	if err != nil {
		return "", err
	}
	re, ok := eng.(*rodEngine)
	if !ok {
		return "", fmt.Errorf("browserresolve: masked unwrap needs the click-capable engine (chrome-family); selected %T", eng)
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

	return re.resolveMasked(ctx, engineRequest{url: maskedURL, profileDir: profileCopy, opts: o})
}
