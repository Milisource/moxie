// Package browserresolve implements a browser-backed download resolver for
// challenge-graded file hops: downloads whose CDN (Cloudflare Cf-Mitigated,
// Turnstile walls) refuses Go's TLS fingerprint even when a valid
// cf_clearance cookie is attached.
//
// The download runs inside a real headless Chrome launched on a COPY of the
// user's browser profile, so the request carries the same IP, User-Agent,
// and TLS fingerprint the clearance was issued to. The resolved URL is never
// handed to Go's net/http — only the browser can satisfy the challenge.
//
// Flow:
//
//	discover profile → copy to temp dir (lock/cache files skipped)
//	→ launch headless Chrome on the copy (stealth flags, no UA emulation)
//	→ Browser.setDownloadBehavior(allowAndName, eventsEnabled)
//	→ navigate the resolved URL → wait for the download → verify the file
//	→ move it into the destination game dir
//	→ teardown: kill Chrome, delete the profile copy (always)
//
// All failures surface as typed errors (see ErrNoChrome, ErrNoProfile,
// ErrProfileLocked, ErrDownload, ErrChallengePage); the package never
// panics.
package browserresolve

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mili/moxie/internal/log"
)

// Sentinel errors returned by ResolveDownload. Wrap with %w and check with
// errors.Is so callers can degrade gracefully (e.g. fall back to the Go
// resolver or surface a "quit Chrome first" hint).
var (
	// ErrNoBrowser means no supported browser binary could be found (nor
	// started by rod's own discovery covering macOS .app bundles and
	// Playwright caches). Install Chrome/Chromium/Firefox or pass
	// WithBinPath / MOXIE_FIREFOX_BIN.
	ErrNoBrowser = errors.New("browserresolve: no supported browser binary found")
	// ErrNoChrome is the historical name of ErrNoBrowser for the
	// Chrome-family engine; kept as an alias for compatibility.
	ErrNoChrome = ErrNoBrowser
	// ErrNoProfile means no browser profile was found at the override, the
	// MOXIE_CHROME_PROFILE_DIR / MOXIE_FIREFOX_PROFILE_DIR environment
	// variables, or the standard per-OS locations.
	ErrNoProfile = errors.New("browserresolve: no browser profile found")
	// ErrProfileLocked means the live profile could not be copied. On
	// Windows Chrome holds exclusive locks while running — quit Chrome and
	// retry.
	ErrProfileLocked = errors.New("browserresolve: browser profile is locked or in use")
	// ErrDownload means the browser session did not produce a valid file:
	// no download started within the timeout, the download was canceled, or
	// the resulting file failed verification (empty, truncated, too small).
	ErrDownload = errors.New("browserresolve: browser download did not complete")
	// ErrChallengePage means the server responded to the browser with an
	// HTML page instead of a file — a CAPTCHA or challenge the automated
	// session could not clear.
	ErrChallengePage = errors.New("browserresolve: server returned an HTML challenge page instead of a file")
)

// Defaults for the ResolveDownload timeouts. A whole session budgets
// Timeout; DownloadTimeout bounds just the wait for the file to finish
// (a GB-scale download over a slow link should raise DownloadTimeout).
const (
	defaultTimeout         = 10 * time.Minute
	defaultDownloadTimeout = 5 * time.Minute
)

// Options configures a ResolveDownload session. The zero value is not
// usable; start from the defaults inside ResolveDownload and apply
// Option overrides.
type Options struct {
	// ProfileDir overrides automatic profile discovery. It must point at a
	// browser profile root (the directory containing "Local State" and
	// "Default/"), not at a specific profile subdirectory.
	ProfileDir string
	// BinPath overrides the Chrome/Chromium executable. Empty means
	// discovery on PATH, then rod's own search (macOS .app bundles,
	// Playwright caches).
	BinPath string
	// Timeout is the total budget for the whole session, including the
	// profile copy. Defaults to defaultTimeout.
	Timeout time.Duration
	// DownloadTimeout bounds the wait for the download to start and
	// finish. Defaults to defaultDownloadTimeout.
	DownloadTimeout time.Duration
	// Headful forces a visible window (e.g. via xvfb-run on headless
	// Linux) when the challenge refuses the headless fingerprint.
	Headful bool
	// MinSize is the minimum accepted file size in bytes. Zero accepts any
	// non-empty file.
	MinSize int64
	// Browser forces an engine family: BrowserAuto (default), BrowserChrome
	// (rod/CDP), or BrowserFirefox (raw-launch). Auto resolves per URL: the
	// browser holding cookies for the host first, then Chrome-family, then
	// Firefox.
	Browser string
}

// Option mutates an Options value. The With* constructors are the public
// API; nil Options are ignored.
type Option func(*Options)

// WithProfileDir overrides automatic browser profile discovery. The path
// must be the profile root ("User Data" dir containing "Local State" and
// "Default/"), not a profile subdirectory.
func WithProfileDir(dir string) Option { return func(o *Options) { o.ProfileDir = dir } }

// WithBinPath forces a specific Chrome/Chromium executable path.
func WithBinPath(path string) Option { return func(o *Options) { o.BinPath = path } }

// WithTimeout sets the total session budget.
func WithTimeout(d time.Duration) Option { return func(o *Options) { o.Timeout = d } }

// WithDownloadTimeout sets how long to wait for the download to finish.
func WithDownloadTimeout(d time.Duration) Option { return func(o *Options) { o.DownloadTimeout = d } }

// WithHeadful forces a visible browser window instead of --headless=new.
func WithHeadful(v bool) Option { return func(o *Options) { o.Headful = v } }

// WithMinSize sets the minimum accepted file size in bytes (0 = any
// non-empty file).
func WithMinSize(n int64) Option { return func(o *Options) { o.MinSize = n } }

// WithBrowser forces an engine family: BrowserAuto (default), BrowserChrome,
// or BrowserFirefox. WithBrowser("firefox") is equivalent to WithBrowser
// (the option name mirrors the value).
func WithBrowser(name string) Option { return func(o *Options) { o.Browser = name } }

func defaultOptions() Options {
	return Options{Timeout: defaultTimeout, DownloadTimeout: defaultDownloadTimeout}
}

// engine downloads a URL inside a real browser running on a copied profile
// and returns the finished file (still inside the temp download dir) plus
// the server-suggested filename. Implemented by rodEngine and
// firefoxEngine; tests substitute a fake to exercise orchestrator error and
// teardown paths offline.
type engine interface {
	run(ctx context.Context, req engineRequest) (engineResult, error)
	// profileDir locates the engine's live profile (the dir copied for the
	// session), honoring an explicit override.
	profileDir(override string) (string, error)
}

// engineRequest is the input to engine.run.
type engineRequest struct {
	url         string  // resolved download URL (http/https)
	profileDir  string  // copied profile — Chrome's --user-data-dir
	downloadDir string  // fresh temp dir the browser writes the file into
	opts        Options // session options (timeouts, headful, bin path)
}

// engineResult is the output of engine.run.
type engineResult struct {
	path       string // finished file inside downloadDir
	suggested  string // server-suggested filename (may be empty)
	totalBytes int64  // expected size reported by the browser (0 = unknown)
}

// resolver orchestrates a browser-backed download with unconditional
// teardown: the profile copy and the temp download dir are removed on every
// exit path, including engine failures.
type resolver struct {
	engine engine
	opts   Options
}

// ResolveDownload downloads resolvedURL inside a real headless browser and
// returns the path of the finished file inside destDir.
//
// The browser performs the download itself; the URL is never handed to Go's
// net/http. Cloudflare clearance (cf_clearance) is cryptographically bound
// to the IP + User-Agent + TLS fingerprint that solved the challenge, so a
// Go client would still fail it — the whole hop must happen in the browser.
//
// The user's real browser profile is copied to a temporary directory first
// (Chrome refuses a second process on the live profile's SingletonLock and
// ignores --remote-debugging-port against the real profile dir); the copy
// is deleted on every exit path.
//
// Errors are typed (ErrNoChrome, ErrNoProfile, ErrProfileLocked,
// ErrDownload, ErrChallengePage) and the function never panics.
func ResolveDownload(ctx context.Context, resolvedURL, destDir string, opts ...Option) (string, error) {
	o := defaultOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()

	eng, err := selectEngine(&o, resolvedURL)
	if err != nil {
		return "", err
	}
	r := &resolver{engine: eng, opts: o}
	return r.resolve(ctx, resolvedURL, destDir)
}

// copyProfileForSession copies the live profile into a fresh temp dir
// (Chrome hard-refuses a second process on the live profile and Chrome >=
// 136 ignores --remote-debugging-port against the real dir — the copy is
// what the session runs on, private to this call). The caller removes the
// copy when done.
func copyProfileForSession(profileDir string) (string, error) {
	profileCopy, err := os.MkdirTemp("", "moxie-browser-profile-*")
	if err != nil {
		return "", fmt.Errorf("browserresolve: creating profile copy dir: %w", err)
	}
	log.Info("browserresolve: copying browser profile for session",
		"source", profileDir, "copy", profileCopy)
	if err := copyProfile(profileDir, profileCopy); err != nil {
		removeDir("profile copy", profileCopy)
		return "", fmt.Errorf("%w (copying %s): %v", ErrProfileLocked, profileDir, err)
	}
	return profileCopy, nil
}

// resolve implements ResolveDownload on an injectable engine so tests can
// drive the orchestrator offline.
func (r *resolver) resolve(ctx context.Context, resolvedURL, destDir string) (string, error) {
	if err := validateDownloadURL(resolvedURL); err != nil {
		return "", err
	}
	if strings.TrimSpace(destDir) == "" {
		return "", fmt.Errorf("browserresolve: empty destination directory")
	}

	var profileDir string
	var err error
	profileDir, err = r.engine.profileDir(r.opts.ProfileDir)
	if err != nil {
		return "", err
	}

	// Copy the live profile first. Chrome hard-refuses a second process on
	// the live profile (SingletonLock) and Chrome >= 136 ignores
	// --remote-debugging-port against the real profile dir — the copy is
	// what the download session runs on, and it is private to this call.
	profileCopy, err := copyProfileForSession(profileDir)
	if err != nil {
		return "", err
	}
	defer removeDir("profile copy", profileCopy)

	downloadDir, err := os.MkdirTemp("", "moxie-browser-download-*")
	if err != nil {
		return "", fmt.Errorf("browserresolve: creating download dir: %w", err)
	}
	defer removeDir("download dir", downloadDir)

	res, err := r.engine.run(ctx, engineRequest{
		url:         resolvedURL,
		profileDir:  profileCopy,
		downloadDir: downloadDir,
		opts:        r.opts,
	})
	if err != nil {
		return "", err
	}

	if err := verifyDownloadedFile(res.path, r.opts.MinSize, res.totalBytes); err != nil {
		return "", err
	}

	name := sanitizeFilename(res.suggested)
	if name == "" {
		name = filepath.Base(res.path)
	}
	log.Info("browserresolve: download complete", "file", name, "dest_dir", destDir)
	return moveFile(res.path, destDir, name)
}

// validateDownloadURL rejects non-http(s) URLs — a browser download only
// makes sense for the network protocols the downloader deals with.
func validateDownloadURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("browserresolve: invalid download URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("browserresolve: unsupported URL scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("browserresolve: URL has no host")
	}
	return nil
}

// removeDir deletes a temporary directory. Cleanup failures are logged, not
// fatal — the browser may still hold files open briefly (Windows).
func removeDir(label, dir string) {
	if err := os.RemoveAll(dir); err != nil {
		log.Warn("browserresolve: failed to remove temp dir", "dir", label, "path", dir, "error", err)
	}
}
