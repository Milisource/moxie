package main

import (
	"context"
	"strconv"
	"sync"
)

// Update concurrency bounds (config key "update-concurrency").
const (
	defaultUpdateConcurrency = 2
	maxUpdateConcurrency     = 4
)

// clampUpdateConcurrency parses the config value, falling back to the
// default for empty/invalid input and clamping to 1..maxUpdateConcurrency.
func clampUpdateConcurrency(v string) int {
	n, err := strconv.Atoi(v)
	if err != nil || n == 0 {
		return defaultUpdateConcurrency
	}
	if n < 1 {
		return 1
	}
	if n > maxUpdateConcurrency {
		return maxUpdateConcurrency
	}
	return n
}

// updateRuns tracks in-flight game updates and installs.
//
// Each game may have at most one run (two runs would extract and merge into
// the same directory), but different games run in parallel, bounded by a
// slot limit: downloads are bandwidth-bound and merges disk-bound, so a
// small limit (default 2) gains most of the speedup without thrashing. The
// app's own self-update takes the whole gate exclusively.
//
// Shared-host politeness lives in the downloader (one process-wide masked
// unwrap limiter, serialised browser fallback), not here.
type updateRuns struct {
	mu        sync.Mutex
	games     map[int64]context.CancelFunc // claimed game IDs → cancel (nil until set)
	exclusive bool                         // self-update holds the gate
	limit     int                          // max concurrent slots
	running   int                          // slots in use
	wake      chan struct{}                // closed when a slot frees up
}

func newUpdateRuns(limit int) *updateRuns {
	if limit < 1 {
		limit = 1
	}
	return &updateRuns{games: map[int64]context.CancelFunc{}, limit: limit, wake: make(chan struct{})}
}

// claimGame reserves gameID. False when that game already has a run or the
// self-update holds the gate.
func (r *updateRuns) claimGame(id int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.exclusive {
		return false
	}
	if _, ok := r.games[id]; ok {
		return false
	}
	r.games[id] = nil
	return true
}

// setCancel publishes the cancel func for a claimed game.
func (r *updateRuns) setCancel(id int64, cancel context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.games[id]; ok {
		r.games[id] = cancel
	}
}

// release drops a game's claim.
func (r *updateRuns) release(id int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.games, id)
}

// claimExclusive takes the whole gate for the self-update. False while any
// game run (or another exclusive holder) is active.
func (r *updateRuns) claimExclusive() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.exclusive || len(r.games) > 0 {
		return false
	}
	r.exclusive = true
	return true
}

func (r *updateRuns) releaseExclusive() {
	r.mu.Lock()
	r.exclusive = false
	r.mu.Unlock()
}

// active reports whether anything holds the gate — scans refuse to run
// while game directories are being written.
func (r *updateRuns) active() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.exclusive || len(r.games) > 0
}

// count returns the number of claimed games.
func (r *updateRuns) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.games)
}

// cancel aborts one game's run. False when it isn't running.
func (r *updateRuns) cancel(id int64) bool {
	r.mu.Lock()
	c, ok := r.games[id]
	r.mu.Unlock()
	if ok && c != nil {
		c()
	}
	return ok
}

// cancelAll aborts every game run and returns how many were running.
func (r *updateRuns) cancelAll() int {
	r.mu.Lock()
	cs := make([]context.CancelFunc, 0, len(r.games))
	for _, c := range r.games {
		if c != nil {
			cs = append(cs, c)
		}
	}
	n := len(r.games)
	r.mu.Unlock()
	for _, c := range cs {
		c()
	}
	return n
}

// setLimit changes the slot limit; waiting runs pick it up immediately.
func (r *updateRuns) setLimit(n int) {
	if n < 1 {
		n = 1
	}
	r.mu.Lock()
	r.limit = n
	close(r.wake)
	r.wake = make(chan struct{})
	r.mu.Unlock()
}

// tryAcquire takes a slot without waiting.
func (r *updateRuns) tryAcquire() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running < r.limit {
		r.running++
		return true
	}
	return false
}

// acquire blocks until a slot is free or ctx is done.
func (r *updateRuns) acquire(ctx context.Context) error {
	for {
		r.mu.Lock()
		if r.running < r.limit {
			r.running++
			r.mu.Unlock()
			return nil
		}
		wake := r.wake
		r.mu.Unlock()
		select {
		case <-wake:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// releaseSlot frees a slot taken by acquire/tryAcquire.
func (r *updateRuns) releaseSlot() {
	r.mu.Lock()
	if r.running > 0 {
		r.running--
	}
	close(r.wake)
	r.wake = make(chan struct{})
	r.mu.Unlock()
}
