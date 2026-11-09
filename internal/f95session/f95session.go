// Package f95session keeps moxie's F95Zone requests acceptable to Cloudflare.
//
// Cloudflare binds cf_clearance to the User-Agent that solved its challenge.
// moxie borrows the user's browser cookies, so every F95Zone request must
// carry the UA of the browser that minted the clearance — a fixed UA breaks
// the moment that browser updates (2026-10-27: Firefox 159 made every thread
// fetch a 403 while moxie still claimed Firefox 140).
//
// Install wires two things into the scraper and the downloader:
//
//   - a UA provider reporting the UA that matches the clearance in use
//     (browser.F95UserAgent), and
//   - a clearance refresher, called once when a request is challenged anyway
//     (clearance expired, never visited the site, …). It mints a fresh
//     clearance headless with Camoufox or the system Firefox (mint.go) and
//     keeps it — persisted, so later runs reuse it — until the user's own
//     browser solves a newer one.
package f95session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mili/moxie/internal/browser"
	"github.com/mili/moxie/internal/config"
	"github.com/mili/moxie/internal/downloader"
	"github.com/mili/moxie/internal/log"
	"github.com/mili/moxie/internal/scraper"
)

// configKey turns the clearance minter off ("off") — the UA matching stays.
const configKey = "f95_clearance"

// envVar overrides configKey.
const envVar = "MOXIE_F95_CLEARANCE"

// minRefreshInterval stops a run that keeps getting challenged from launching
// a browser over and over: within the interval the previous outcome is reused.
const minRefreshInterval = 2 * time.Minute

var installOnce sync.Once

// Install wires the UA provider and the clearance refresher into the scraper
// and downloader packages and loads a previously minted clearance. Safe to
// call more than once.
func Install() {
	installOnce.Do(func() {
		loadPersisted()
		scraper.SetUserAgentProvider(UserAgent)
		downloader.SetF95UserAgentProvider(UserAgent)
		if mintingEnabled() {
			scraper.SetClearanceRefresher(Refresh)
			downloader.SetF95ClearanceRefresher(Refresh)
		}
	})
}

// UserAgent returns the UA matching the F95Zone clearance currently in use,
// or "" (callers then use their default).
func UserAgent() string {
	return browser.F95UserAgent()
}

func mintingEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(envVar)))
	if v == "" {
		if cfg, err := config.ReadConfig(); err == nil {
			v = strings.ToLower(strings.TrimSpace(cfg.Get(configKey)))
		}
	}
	switch v {
	case "off", "0", "false", "no":
		return false
	}
	return true
}

var refreshState struct {
	mu     sync.Mutex
	at     time.Time
	cookie string
	err    error
}

// Refresh obtains a clearance Cloudflare accepts and returns the F95Zone
// Cookie header to use with it. Concurrent callers share one mint; repeat
// calls within minRefreshInterval reuse the last outcome. A clearance the
// user's browser solves later still wins automatically: GetF95Cookies and
// F95UserAgent prefer whichever clearance is newer.
func Refresh(ctx context.Context) (string, error) {
	refreshState.mu.Lock()
	defer refreshState.mu.Unlock()
	if !refreshState.at.IsZero() && time.Since(refreshState.at) < minRefreshInterval {
		return refreshState.cookie, refreshState.err
	}
	cookie, err := refresh(ctx)
	refreshState.at, refreshState.cookie, refreshState.err = time.Now(), cookie, err
	return cookie, err
}

func refresh(ctx context.Context) (string, error) {
	cl, err := Mint(ctx)
	if err != nil {
		return "", err
	}
	browser.SetF95Clearance(cl)
	persist(cl)
	return browser.GetF95Cookies()
}

func statePath() string {
	return filepath.Join(config.ConfigDir(), "f95-clearance.json")
}

// persist stores the minted clearance (0600) so the next moxie run starts
// with it instead of being challenged again.
func persist(cl browser.F95Clearance) {
	b, err := json.Marshal(cl)
	if err != nil {
		return
	}
	path := statePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
	}
}

// loadPersisted installs a still-valid clearance from a previous run.
func loadPersisted() {
	b, err := os.ReadFile(statePath())
	if err != nil {
		return
	}
	var cl browser.F95Clearance
	if json.Unmarshal(b, &cl) != nil {
		return
	}
	if cl.Value == "" || (!cl.Expires.IsZero() && time.Now().After(cl.Expires)) {
		_ = os.Remove(statePath())
		return
	}
	browser.SetF95Clearance(cl)
	log.Debug("F95Zone clearance loaded", "expires", cl.Expires)
}
