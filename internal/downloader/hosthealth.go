package downloader

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mili/moxie/internal/log"
)

// Some hosts impose a server-side download speed cap. pixeldrain, for example,
// throttles free downloads to 1 MiB/s once its 24h transfer quota (5 GB
// regular / 6 GB filesystem) is exceeded — a 1 GB release then takes ~17
// minutes instead of seconds. Grinding through that is worse than trying the
// next host, so moxie probes the host before the transfer starts and, when a
// cap is reported, fails the attempt with ErrHostThrottled so the caller's
// link-fallback loop moves on. The host is also marked throttled so link
// ranking deprioritises it for the rest of the batch.

// ErrHostThrottled is returned (wrapped in *ThrottledError) when a host
// reports a server-side download speed cap and another host should be tried.
var ErrHostThrottled = errors.New("host is throttling downloads")

// ThrottledError reports a host-side download speed cap.
// errors.Is(err, ErrHostThrottled) is true.
type ThrottledError struct {
	Host  string
	Limit int64 // bytes per second; 0 = unknown
}

func (e *ThrottledError) Error() string {
	if e.Limit > 0 {
		return fmt.Sprintf("%s is throttling downloads to %s (free transfer quota exceeded) — trying another host", e.Host, humanRate(e.Limit))
	}
	return fmt.Sprintf("%s is throttling downloads — trying another host", e.Host)
}

func (e *ThrottledError) Is(target error) bool { return target == ErrHostThrottled }

// humanRate formats a bytes-per-second rate for user-facing messages.
func humanRate(bps int64) string {
	switch {
	case bps >= 1<<20:
		return fmt.Sprintf("%.1f MB/s", float64(bps)/(1<<20))
	case bps >= 1<<10:
		return fmt.Sprintf("%.0f KB/s", float64(bps)/1024)
	default:
		return fmt.Sprintf("%d B/s", bps)
	}
}

const (
	// throttlePenalty dwarfs every positive host tier (best is +25) and any
	// platform priority, so a throttled host sinks below working alternatives.
	throttlePenalty = 1000
	// throttleTTL bounds how long a detected cap deprioritises a host. The
	// pixeldrain free window is a 24h sliding window, but re-probing is cheap
	// and authoritative, so a shorter TTL recovers promptly once the window
	// clears or the IP changes.
	throttleTTL = 30 * time.Minute
)

var throttledHosts sync.Map // normalized host label -> time.Time (expiry)

// MarkHostThrottled records that host is currently serving capped downloads.
// A zero/negative duration falls back to throttleTTL.
func MarkHostThrottled(host string, d time.Duration) {
	host = normalizeHostLabel(host)
	if host == "" {
		return
	}
	if d <= 0 {
		d = throttleTTL
	}
	throttledHosts.Store(host, time.Now().Add(d))
}

// HostThrottled reports whether host is currently marked as throttled.
func HostThrottled(host string) bool {
	host = normalizeHostLabel(host)
	v, ok := throttledHosts.Load(host)
	if !ok {
		return false
	}
	expiry, ok := v.(time.Time)
	if !ok || time.Now().After(expiry) {
		throttledHosts.Delete(host)
		return false
	}
	return true
}

// ClearHostThrottle removes a throttle mark — the probe found no cap, or a
// test needs a clean slate.
func ClearHostThrottle(host string) { throttledHosts.Delete(normalizeHostLabel(host)) }

func normalizeHostLabel(h string) string { return strings.ToLower(strings.TrimSpace(h)) }

// throttleAllowed reports whether the pre-download rejection is disabled so a
// capped host still downloads (slowly). Escape hatch for the rare game whose
// only source is throttled: MOXIE_ALLOW_THROTTLED=1.
func throttleAllowed() bool { return os.Getenv("MOXIE_ALLOW_THROTTLED") == "1" }

// checkHostThrottle probes host for a server-side download cap before the
// transfer starts. When a cap is detected the host is marked throttled and
// (unless MOXIE_ALLOW_THROTTLED=1) ErrHostThrottled is returned so the caller
// falls through to the next link. Probe failures are non-fatal: an unknown
// state must not block a download.
func (r *HostResolver) checkHostThrottle(ctx context.Context, host, resolvedURL string) error {
	label := normalizeHostLabel(host)
	if label == "" {
		label = normalizeHostLabel(IdentifyHostInURL(resolvedURL))
	}
	switch label {
	case "pixeldrain":
		limit, limited, err := r.probePixeldrainLimit(ctx, resolvedURL)
		if err != nil {
			log.Debug("pixeldrain throttle probe failed; proceeding", "error", err)
			return nil
		}
		if !limited {
			ClearHostThrottle("pixeldrain")
			return nil
		}
		MarkHostThrottled("pixeldrain", throttleTTL)
		if throttleAllowed() {
			log.Warn("pixeldrain download is throttled; MOXIE_ALLOW_THROTTLED=1 set — proceeding slowly",
				"limit_bytes_per_sec", limit)
			return nil
		}
		log.Warn("host is throttling downloads; trying another link",
			"host", "pixeldrain", "limit_bytes_per_sec", limit)
		return &ThrottledError{Host: "pixeldrain", Limit: limit}
	default:
		return nil
	}
}
