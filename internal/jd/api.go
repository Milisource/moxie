package jd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rkosegi/jdownloader-go/jdownloader"
)

// addConfig holds the effective AddLinks options.
type addConfig struct {
	autostart bool
	destDir   string
	pkgName   string
}

// AddOption configures an AddLinks call.
type AddOption func(*addConfig)

// WithAutostart moves added links from the linkgrabber straight into the
// Downloads list and starts them. Defaults to true (the moxie flow wants a
// fire-and-forget sidecar download).
func WithAutostart(v bool) AddOption {
	return func(c *addConfig) { c.autostart = v }
}

// WithDestinationDir overrides the destination folder for the created
// package. When unset, JD uses the device's GeneralSettings
// defaultdownloadfolder.
func WithDestinationDir(dir string) AddOption {
	return func(c *addConfig) { c.destDir = dir }
}

// WithPackageName names the linkgrabber package. When unset, JD picks a
// name from the first link.
func WithPackageName(name string) AddOption {
	return func(c *addConfig) { c.pkgName = name }
}

// AddLinks feeds one or more URLs (or F95Zone thread URLs, which JD's
// decrypter expands) into the device's linkgrabber. Blank entries are
// skipped. See WithAutostart / WithDestinationDir / WithPackageName for the
// linkgrabber behavior. The call is synchronous: it returns once JD has
// accepted the links, not when they finish downloading — track completion
// with WaitDownloads.
func (c *Client) AddLinks(urls []string, opts ...AddOption) error {
	clean := make([]string, 0, len(urls))
	for _, u := range urls {
		if u = strings.TrimSpace(u); u != "" {
			clean = append(clean, u)
		}
	}
	if len(clean) == 0 {
		return fmt.Errorf("jd add links: no URLs provided")
	}
	cfg := addConfig{autostart: true}
	for _, o := range opts {
		o(&cfg)
	}

	grab, err := c.Grabber()
	if err != nil {
		return err
	}
	addOpts := []jdownloader.AddLinksOptions{jdownloader.AddLinksOptionAutostart(cfg.autostart)}
	if cfg.pkgName != "" {
		addOpts = append(addOpts, jdownloader.AddLinksOptionPackage(cfg.pkgName))
	}
	if cfg.destDir != "" {
		addOpts = append(addOpts, jdownloader.AddLinksOptionDestinationDir(cfg.destDir))
	}
	if _, err := grab.Add(clean, addOpts...); err != nil {
		return fmt.Errorf("jd add links: %w", err)
	}
	return nil
}

// QueryDownloads returns a one-shot snapshot of the device's Downloads
// list (downloadsV2/queryLinks). Combine with AddLinks in a manual poll
// loop, or use WaitDownloads for the built-in loop with stall detection.
func (c *Client) QueryDownloads() ([]Download, error) {
	dl, err := c.Downloads()
	if err != nil {
		return nil, err
	}
	links, err := dl.Links()
	if err != nil {
		return nil, fmt.Errorf("jd query downloads: %w", err)
	}
	return toDownloads(links), nil
}

// waitConfig holds the effective WaitDownloads options.
type waitConfig struct {
	pollInterval time.Duration
	stallTimeout time.Duration
	progress     func(Progress)
}

// WaitOption configures a WaitDownloads call.
type WaitOption func(*waitConfig)

// WithPollInterval sets the delay between Downloads list polls.
// Defaults to 5s.
func WithPollInterval(d time.Duration) WaitOption {
	return func(c *waitConfig) { c.pollInterval = d }
}

// WithStallTimeout sets how long WaitDownloads tolerates zero progress
// (aggregate BytesLoaded not increasing) before returning ErrStalled.
// This is the captcha-stall guard: a link parked behind an unsolved
// captcha sits at 0 bytes forever. Defaults to 10m; pass 0 to disable
// stall detection entirely (poll until context cancellation).
func WithStallTimeout(d time.Duration) WaitOption {
	return func(c *waitConfig) { c.stallTimeout = d }
}

// WithProgress registers a callback invoked with a Progress snapshot after
// every poll. The callback must not block: it runs on the WaitDownloads
// goroutine. Use it to forward progress to a TUI spinner or progress bar.
func WithProgress(fn func(Progress)) WaitOption {
	return func(c *waitConfig) { c.progress = fn }
}

// WaitDownloads polls the Downloads list until every link in the batch has
// reached a terminal state (finished, skipped, or disabled) or the
// context is canceled. It returns the final snapshot.
//
// Failure semantics:
//
//   - context cancellation → error wrapping ctx.Err()
//   - no progress for WithStallTimeout → error wrapping ErrStalled
//   - API/transport errors propagate immediately (wrapped)
//
// An empty Downloads list is not treated as done: right after AddLinks with
// autostart, JD may still be processing links in the linkgrabber. The stall
// timeout (default 10m) is the grace period for that transition.
func (c *Client) WaitDownloads(ctx context.Context, opts ...WaitOption) ([]Download, error) {
	cfg := waitConfig{pollInterval: 5 * time.Second, stallTimeout: 10 * time.Minute}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.pollInterval <= 0 {
		return nil, fmt.Errorf("jd wait: poll interval must be positive")
	}
	if cfg.stallTimeout < 0 {
		return nil, fmt.Errorf("jd wait: stall timeout must not be negative")
	}

	// lastProgress is refreshed whenever the aggregate BytesLoaded grows;
	// initialized at start so a batch that never appears stalls too.
	lastProgress := time.Now()
	maxLoaded := int64(0)

	timer := time.NewTimer(cfg.pollInterval)
	defer timer.Stop()

	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("jd wait: %w", err)
		}
		downloads, err := c.QueryDownloads()
		if err != nil {
			return nil, err
		}
		prog := batchProgress(downloads)
		if cfg.progress != nil {
			cfg.progress(prog)
		}
		if prog.BytesLoaded > maxLoaded {
			maxLoaded = prog.BytesLoaded
			lastProgress = time.Now()
		}
		if batchDone(downloads) {
			return downloads, nil
		}
		if cfg.stallTimeout > 0 && time.Since(lastProgress) > cfg.stallTimeout {
			return nil, fmt.Errorf("%w: no progress for %s", ErrStalled, cfg.stallTimeout)
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("jd wait: %w", ctx.Err())
		case <-timer.C:
			timer.Reset(cfg.pollInterval)
		}
	}
}
