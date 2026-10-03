package main

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestClampUpdateConcurrency(t *testing.T) {
	cases := map[string]int{"": 2, "x": 2, "0": 2, "-3": 1, "1": 1, "3": 3, "4": 4, "9": 4}
	for in, want := range cases {
		if got := clampUpdateConcurrency(in); got != want {
			t.Errorf("clampUpdateConcurrency(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestUpdateRunsClaims(t *testing.T) {
	r := newUpdateRuns(2)
	if !r.claimGame(1) || !r.claimGame(2) {
		t.Fatal("distinct games must both claim")
	}
	if r.claimGame(1) {
		t.Fatal("duplicate claim for game 1 accepted")
	}
	if r.claimExclusive() {
		t.Fatal("exclusive claimed while games run")
	}
	r.release(1)
	r.release(2)
	if r.active() {
		t.Fatal("still active after releases")
	}
	if !r.claimExclusive() {
		t.Fatal("exclusive refused on an idle gate")
	}
	if r.claimGame(3) {
		t.Fatal("game claimed during exclusive hold")
	}
	r.releaseExclusive()
	if !r.claimGame(3) {
		t.Fatal("game refused after exclusive release")
	}
}

func TestUpdateRunsCancel(t *testing.T) {
	r := newUpdateRuns(1)
	r.claimGame(1)
	r.claimGame(2)
	ctx1, c1 := context.WithCancel(context.Background())
	ctx2, c2 := context.WithCancel(context.Background())
	defer c1()
	defer c2()
	r.setCancel(1, c1)
	r.setCancel(2, c2)

	if !r.cancel(1) {
		t.Fatal("cancel(1) = false for a running game")
	}
	if ctx1.Err() == nil || ctx2.Err() != nil {
		t.Fatal("cancel(1) must cancel only game 1")
	}
	if r.cancel(9) {
		t.Fatal("cancel of a game that isn't running reported true")
	}
	if n := r.cancelAll(); n != 2 || ctx2.Err() == nil {
		t.Fatalf("cancelAll = %d (ctx2 err %v), want 2 and cancelled", n, ctx2.Err())
	}
}

// A queued run waiting for a slot must give up when cancelled.
func TestUpdateRunsAcquireCancelled(t *testing.T) {
	r := newUpdateRuns(1)
	if !r.tryAcquire() {
		t.Fatal("first slot refused")
	}
	if r.tryAcquire() {
		t.Fatal("slot beyond the limit granted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.acquire(ctx) }()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("acquire succeeded after cancel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("acquire did not return after cancel")
	}
}

// Many workers through the slots never exceed the limit, and raising the
// limit wakes waiters immediately.
func TestUpdateRunsConcurrencyLimit(t *testing.T) {
	for _, limit := range []int{1, 2, 3} {
		r := newUpdateRuns(limit)
		var cur, peak int32
		var wg sync.WaitGroup
		for i := 0; i < 12; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := r.acquire(context.Background()); err != nil {
					t.Error(err)
					return
				}
				n := atomic.AddInt32(&cur, 1)
				for {
					p := atomic.LoadInt32(&peak)
					if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				atomic.AddInt32(&cur, -1)
				r.releaseSlot()
			}()
		}
		wg.Wait()
		if peak > int32(limit) {
			t.Errorf("limit %d: peak concurrency %d", limit, peak)
		}
	}

	r := newUpdateRuns(1)
	r.tryAcquire()
	got := make(chan struct{})
	go func() {
		r.acquire(context.Background())
		close(got)
	}()
	time.Sleep(20 * time.Millisecond)
	r.setLimit(2)
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("raising the limit did not wake the waiter")
	}
}
