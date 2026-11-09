package downloader

import (
	"context"
	"time"
)

// defaultTurnstileSolver is the browser-based solver for Cloudflare
// Turnstile-gated file pages (vikingfile.com's current flow). It loads the
// page in the user's browser, clicks the managed-challenge checkbox when one
// is shown, waits for the page's cloudflareCallback to populate
// #download-link, and returns that URL for the Go path to fetch with browser
// cookies + Referer (preserving progress/resume).
//
// nil = Turnstile-gated pages cannot be resolved (browser fallback disabled).
// Installed by browserresolve.InstallDownloaderFallback through
// SetDefaultTurnstileSolver.
var defaultTurnstileSolver func(ctx context.Context, pageURL string) (string, error)

// SetDefaultTurnstileSolver installs the browser Turnstile solver used by
// every HostResolver. Pass nil to clear. Wired by
// browserresolve.InstallDownloaderFallback at app startup.
func SetDefaultTurnstileSolver(fn func(ctx context.Context, pageURL string) (string, error)) {
	defaultTurnstileSolver = fn
}

// vikingFileTurnstileTimeout bounds a single Turnstile browser solve: page +
// widget load, an optional checkbox click, and the callback XHR. Generous
// because a cold headless browser start plus Cloudflare's widget can take
// tens of seconds, but short enough that a stubborn challenge cannot stall a
// download forever.
const vikingFileTurnstileTimeout = 3 * time.Minute
