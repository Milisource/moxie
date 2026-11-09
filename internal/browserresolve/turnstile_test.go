package browserresolve

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeTurnstileDOM implements turnstileDOM with a scriptable page flow so
// the link-polling loop can be exercised without launching a browser.
//
// The three behaviours mirror the real page:
//   - auto-pass: the widget solves itself and the link appears immediately;
//   - managed:   the link only appears after the checkbox is clicked;
//   - neither:   no checkbox and no link (a stubborn/failed challenge).
type fakeTurnstileDOM struct {
	autoPass  bool // href populated without any click
	managed   bool // href populated only after a successful checkbox click
	clickable bool // the managed-challenge checkbox is present/clickable

	clicked   bool
	hrefReads int
	clicks    int
}

const fakeTurnstileLink = "https://vikingfile.com/d/Q4GEdYpgph/CyanBrain-1.1.4.zip"

func (f *fakeTurnstileDOM) downloadLinkHref() string {
	f.hrefReads++
	if f.autoPass || (f.managed && f.clicked) {
		return fakeTurnstileLink
	}
	return ""
}

func (f *fakeTurnstileDOM) clickTurnstileCheckbox(context.Context) bool {
	f.clicks++
	if !f.clickable {
		return false
	}
	f.clicked = true
	return true
}

func TestWaitForTurnstileDownloadLink_AutoPass(t *testing.T) {
	t.Parallel()
	dom := &fakeTurnstileDOM{autoPass: true}

	got, err := waitForTurnstileDownloadLink(context.Background(), dom, time.Second, time.Millisecond)
	if err != nil {
		t.Fatalf("waitForTurnstileDownloadLink = %v, want nil", err)
	}
	if got != fakeTurnstileLink {
		t.Errorf("link = %q, want %q", got, fakeTurnstileLink)
	}
	// The link is already there, so the checkbox must never be clicked.
	if dom.clicks != 0 {
		t.Errorf("checkbox clicked %d times on an auto-pass page, want 0", dom.clicks)
	}
}

func TestWaitForTurnstileDownloadLink_ManagedChallenge(t *testing.T) {
	t.Parallel()
	dom := &fakeTurnstileDOM{managed: true, clickable: true}

	got, err := waitForTurnstileDownloadLink(context.Background(), dom, 2*time.Second, time.Millisecond)
	if err != nil {
		t.Fatalf("waitForTurnstileDownloadLink = %v, want nil", err)
	}
	if got != fakeTurnstileLink {
		t.Errorf("link = %q, want %q", got, fakeTurnstileLink)
	}
	if !dom.clicked || dom.clicks == 0 {
		t.Errorf("managed challenge: checkbox not clicked (clicked=%v clicks=%d)", dom.clicked, dom.clicks)
	}
	// Once the checkbox is cleared, later iterations must not re-click it.
	if dom.clicks > 1 {
		t.Errorf("checkbox clicked %d times, want exactly 1", dom.clicks)
	}
}

// A managed challenge whose checkbox never appears (or fails to click) must
// surface the challenge error rather than hang or return a bogus URL.
func TestWaitForTurnstileDownloadLink_Timeout(t *testing.T) {
	t.Parallel()
	dom := &fakeTurnstileDOM{managed: true, clickable: false}

	_, err := waitForTurnstileDownloadLink(context.Background(), dom, 20*time.Millisecond, time.Millisecond)
	if !errors.Is(err, ErrChallengePage) {
		t.Fatalf("waitForTurnstileDownloadLink = %v, want ErrChallengePage", err)
	}
	if dom.hrefReads == 0 {
		t.Error("polling loop never read the download link")
	}
}

func TestWaitForTurnstileDownloadLink_ContextCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := waitForTurnstileDownloadLink(ctx, &fakeTurnstileDOM{}, time.Minute, time.Millisecond)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waitForTurnstileDownloadLink = %v, want context.Canceled", err)
	}
}
