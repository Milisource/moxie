package scraper

import (
	"context"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/mili/moxie/internal/log"
)

// breakerCooldown is how long a tripped circuit breaker refuses requests
// before letting one through again. The breaker is shared by every client of
// a host for the life of the process (the desktop app runs for hours), so it
// must heal on its own; a clearance refresh clears it immediately.
const breakerCooldown = 10 * time.Minute

// f95PacerKey names the pacer shared by every safe client. All of them talk to
// f95zone.to, so this is effectively the per-host limiter for F95Zone.
const f95PacerKey = "f95zone.to"

// hostPacer holds the request pacing, 429 backoff, periodic cooldown and
// circuit-breaker state for one remote host. Every Client created by
// NewClient (and PublicAPI.Client) shares the f95zone.to pacer, so a Discover
// load, a cover fetch and a sync running together are spaced as one stream
// and a 429 seen by one of them slows all of them down.
//
// Slots are reserved under the lock: each caller computes its start time from
// the previous reservation and stores its own before unlocking, then sleeps
// without holding the lock. Concurrent callers therefore get distinct,
// properly spaced slots instead of all waking at once.
type hostPacer struct {
	mu sync.Mutex

	last     time.Time     // start time of the most recently reserved slot
	delay    time.Duration // adaptive inter-request delay (grows on 429)
	reqCount int           // slots reserved so far

	consecutiveBlocks int
	blockedUntil      time.Time // circuit breaker open until this time

	// Tunables. Zero values mean the package defaults; tests shrink them so
	// spacing assertions run in milliseconds.
	jitter      time.Duration // random extra spacing (default maxJitter)
	cooldown    time.Duration // periodic pause length (default cooldownDuration)
	minGapFloor time.Duration // when > 0, replaces the caller's baseDelay
}

func newHostPacer(delay time.Duration) *hostPacer {
	return &hostPacer{delay: delay}
}

var sharedPacers = struct {
	mu sync.Mutex
	m  map[string]*hostPacer
}{m: map[string]*hostPacer{}}

// sharedPacer returns the process-wide pacer for key, creating it on first use.
func sharedPacer(key string) *hostPacer {
	sharedPacers.mu.Lock()
	defer sharedPacers.mu.Unlock()
	p, ok := sharedPacers.m[key]
	if !ok {
		p = newHostPacer(minDelay)
		sharedPacers.m[key] = p
	}
	return p
}

func (p *hostPacer) jitterMax() time.Duration {
	if p.jitter > 0 {
		return p.jitter
	}
	return maxJitter
}

func (p *hostPacer) cooldownLen() time.Duration {
	if p.cooldown > 0 {
		return p.cooldown
	}
	return cooldownDuration
}

// wait reserves the next request slot and sleeps until it starts. baseDelay
// is the caller's minimum spacing (minDelay, or searchMinDelay for search);
// the pacer's adaptive delay wins when larger. Every cooldownInterval-th slot
// gets an extra cooldown pause, counted across all clients of the host.
//
// A caller whose context is cancelled during the wait returns ctx.Err() and
// gives its slot back when nobody reserved a later one, so cancellation never
// moves the schedule backwards past another caller's reservation.
func (p *hostPacer) wait(ctx context.Context, baseDelay time.Duration, urlStr string) error {
	p.mu.Lock()
	now := time.Now()
	if p.minGapFloor > 0 {
		baseDelay = p.minGapFloor
	}
	waitFor := baseDelay
	if p.delay > waitFor {
		waitFor = p.delay
	}

	slot := now
	if earliest := p.last.Add(waitFor); earliest.After(now) {
		slot = earliest
		if j := p.jitterMax(); j > 0 {
			slot = slot.Add(time.Duration(rand.Int63n(int64(j))))
		}
	}
	cooling := p.reqCount > 0 && p.reqCount%cooldownInterval == 0
	if cooling {
		base := now
		if p.last.After(base) {
			base = p.last
		}
		if c := base.Add(p.cooldownLen()); c.After(slot) {
			slot = c
		}
	}

	prevLast := p.last
	p.last = slot
	p.reqCount++
	p.mu.Unlock()

	d := time.Until(slot)
	if d <= 0 {
		return ctx.Err()
	}
	if cooling {
		log.Debug("cooldown pause", "duration", d, "url", urlStr)
	} else {
		log.Debug("pacing wait", "duration", d, "url", urlStr)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		p.mu.Lock()
		if p.last.Equal(slot) {
			p.last = prevLast
			p.reqCount--
		}
		p.mu.Unlock()
		return ctx.Err()
	}
}

// currentDelay returns the adaptive inter-request delay.
func (p *hostPacer) currentDelay() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.delay
}

// isBlocked reports whether the circuit breaker is open.
func (p *hostPacer) isBlocked() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.blockedUntil.IsZero() {
		return false
	}
	if time.Now().Before(p.blockedUntil) {
		return true
	}
	// Half-open: let requests through again, and require a fresh run of
	// blocks before tripping a second time.
	p.blockedUntil = time.Time{}
	p.consecutiveBlocks = 0
	return false
}

// noteSuccess clears the consecutive-block counter and decays the adaptive
// delay towards minDelay. A zero delay (unsafe clients) stays zero.
func (p *hostPacer) noteSuccess() {
	p.mu.Lock()
	p.consecutiveBlocks = 0
	if p.delay > minDelay {
		p.delay = time.Duration(float64(p.delay) * 0.95)
		if p.delay < minDelay {
			p.delay = minDelay
		}
	}
	p.mu.Unlock()
}

// noteThrottled raises the shared adaptive delay after a 429, to the
// server's Retry-After when given, else doubling. It does not touch the
// circuit breaker: that is counted once per logical request by noteBlocked.
func (p *hostPacer) noteThrottled(b *BlockedError) {
	if b.StatusCode != http.StatusTooManyRequests {
		return
	}
	retryAfter := b.RetryAfter
	if retryAfter > maxBackoff {
		retryAfter = maxBackoff
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if retryAfter > 0 {
		p.delay = retryAfter
	} else {
		p.delay = time.Duration(float64(p.delay) * backoffMultiplier)
	}
	if p.delay > maxBackoff {
		p.delay = maxBackoff
	}
}

// noteBlocked records one blocked logical request (call it once per request,
// after the retry loop — not once per attempt). A response carrying
// Retry-After is a cooperative "wait this long" signal: it is honoured by the
// caller and deliberately does not count toward tripping the breaker, so a
// single rate-limit response cannot abort a whole sync run.
func (p *hostPacer) noteBlocked(b *BlockedError, urlStr string) {
	if b.RetryAfter > 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.consecutiveBlocks++
	if p.consecutiveBlocks >= maxConsecutiveBlocks && p.blockedUntil.IsZero() {
		p.blockedUntil = time.Now().Add(breakerCooldown)
		log.Warn("circuit breaker tripped; refusing further requests until cookies are refreshed",
			"status", b.StatusCode,
			"url", urlStr,
			"cooldown", breakerCooldown,
		)
	}
}

// resetBreaker closes the circuit breaker (after a successful clearance
// refresh: the old blocks belonged to the stale session).
func (p *hostPacer) resetBreaker() {
	p.mu.Lock()
	p.consecutiveBlocks = 0
	p.blockedUntil = time.Time{}
	p.mu.Unlock()
}
