package scraper

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testGap       = 80 * time.Millisecond
	testTolerance = 15 * time.Millisecond
)

// fastPacer is a pacer with a short gap and no jitter, so spacing assertions
// run in milliseconds.
func fastPacer() *hostPacer {
	return &hostPacer{minGapFloor: testGap, jitter: time.Nanosecond}
}

// stampServer records the arrival time of every request whose path starts
// with /paced (ignoring stray probes of the listening port).
func stampServer(t *testing.T, status int) (*httptest.Server, func() []time.Time) {
	t.Helper()
	var mu sync.Mutex
	var stamps []time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/paced") {
			mu.Lock()
			stamps = append(stamps, time.Now())
			mu.Unlock()
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []time.Time {
		mu.Lock()
		defer mu.Unlock()
		out := append([]time.Time(nil), stamps...)
		sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
		return out
	}
}

func assertSpaced(t *testing.T, stamps []time.Time, want int, gap time.Duration) {
	t.Helper()
	if len(stamps) != want {
		t.Fatalf("server saw %d requests, want %d", len(stamps), want)
	}
	for i := 1; i < len(stamps); i++ {
		if d := stamps[i].Sub(stamps[i-1]); d < gap-testTolerance {
			t.Errorf("requests %d and %d only %v apart, want >= %v", i-1, i, d, gap)
		}
	}
}

func fire(t *testing.T, clients []*Client, n int, url string) {
	t.Helper()
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		c := clients[i%len(clients)]
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodGet, url, nil)
			if _, err := c.do(req, minDelay); err != nil {
				t.Errorf("do: %v", err)
			}
		}()
	}
	wg.Wait()
}

// Concurrent goroutines on one client get distinct, spaced slots instead of
// all waking at the same time.
func TestPacer_ConcurrentRequestsOnOneClientAreSpaced(t *testing.T) {
	t.Parallel()
	srv, stamps := stampServer(t, http.StatusOK)
	c := NewClientWithHTTP("", srv.Client())
	c.pacer = fastPacer()

	fire(t, []*Client{c}, 5, srv.URL+"/paced")
	assertSpaced(t, stamps(), 5, testGap)
}

// Two separate clients sharing the host pacer are spaced as one stream.
func TestPacer_SeparateClientsShareSpacing(t *testing.T) {
	t.Parallel()
	srv, stamps := stampServer(t, http.StatusOK)
	shared := fastPacer()
	a := NewClientWithHTTP("", srv.Client())
	b := NewClientWithHTTP("", srv.Client())
	a.pacer, b.pacer = shared, shared

	fire(t, []*Client{a, b}, 6, srv.URL+"/paced")
	assertSpaced(t, stamps(), 6, testGap)
}

// Every safe production client (NewClient, PublicAPI.Client) uses the same
// process-wide f95zone.to pacer; unsafe clients and the cache client do not.
func TestPacer_ProductionClientsShareF95Pacer(t *testing.T) {
	t.Parallel()
	a := NewClient("")
	b := NewClient("xf_user=1")
	api := NewPublicAPIWithCookie("xf_user=1")
	if a.pacer != b.pacer || a.pacer != api.Client.pacer {
		t.Error("safe clients do not share the f95zone.to pacer")
	}
	if a.pacer != sharedPacer(f95PacerKey) {
		t.Error("NewClient is not using the registered f95zone.to pacer")
	}
	if u := NewUnsafeClient(""); u.pacer == a.pacer {
		t.Error("unsafe client joined the shared pacer")
	}
	if api.CacheClient.pacer == a.pacer {
		t.Error("F95Checker cache client joined the shared F95Zone pacer")
	}
	if d := NewUnsafeClient("").Delay(); d != 0 {
		t.Errorf("unsafe client delay = %v, want 0", d)
	}
}

// A 429 seen by one client raises the delay for every client of the host.
func TestPacer_RateLimitRaisesSharedDelay(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	shared := fastPacer()
	a := NewClientWithHTTP("", srv.Client())
	b := NewClientWithHTTP("", srv.Client())
	a.pacer, b.pacer = shared, shared

	// One raw attempt (doOnce + the bookkeeping do() applies) is enough.
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	_, err := a.doOnce(req, 0)
	var be *BlockedError
	if !errors.As(err, &be) {
		t.Fatalf("err = %v, want BlockedError", err)
	}
	a.pacer.noteBlocked(be, srv.URL)

	if d := b.Delay(); d != 7*time.Second {
		t.Errorf("other client's delay after 429 = %v, want 7s", d)
	}
}

// The breaker trips for every client of the host, heals after
// breakerCooldown, and a clearance refresh resets it at once.
func TestPacer_SharedCircuitBreaker(t *testing.T) {
	t.Parallel()
	p := fastPacer()
	for i := 0; i < maxConsecutiveBlocks; i++ {
		p.noteBlocked(&BlockedError{StatusCode: 403}, "x")
	}
	a := &Client{pacer: p}
	b := &Client{pacer: p}
	if !a.Blocked() || !b.Blocked() {
		t.Fatal("breaker did not trip for both clients")
	}

	p.mu.Lock()
	p.blockedUntil = time.Now().Add(-time.Second)
	p.mu.Unlock()
	if b.Blocked() {
		t.Error("breaker did not heal after its cooldown")
	}

	for i := 0; i < maxConsecutiveBlocks; i++ {
		p.noteBlocked(&BlockedError{StatusCode: 403}, "x")
	}
	p.resetBreaker()
	if a.Blocked() {
		t.Error("resetBreaker left the breaker open")
	}
}

// The cooldown pause fires once per cooldownInterval requests across all
// clients, not once per client or per worker.
func TestPacer_CooldownIsProcessWide(t *testing.T) {
	t.Parallel()
	srv, stamps := stampServer(t, http.StatusOK)
	shared := fastPacer()
	shared.cooldown = 400 * time.Millisecond
	shared.reqCount = cooldownInterval // the next slot is due a cooldown
	a := NewClientWithHTTP("", srv.Client())
	b := NewClientWithHTTP("", srv.Client())
	a.pacer, b.pacer = shared, shared

	start := time.Now()
	fire(t, []*Client{a, b}, 4, srv.URL+"/paced")
	got := stamps()
	if len(got) != 4 {
		t.Fatalf("server saw %d requests, want 4", len(got))
	}
	if first := got[0].Sub(start); first < shared.cooldown-testTolerance {
		t.Errorf("first request after %v, want the %v cooldown first", first, shared.cooldown)
	}
	long := 0
	for i := 1; i < len(got); i++ {
		if got[i].Sub(got[i-1]) >= shared.cooldown-testTolerance {
			long++
		}
	}
	if long != 0 {
		t.Errorf("%d extra cooldown-length gaps, want the cooldown to fire once", long)
	}
}

// A caller cancelled while waiting returns promptly, gives back its slot, and
// never moves the schedule behind another caller's reservation.
func TestPacer_CancelledWaitDoesNotCorruptState(t *testing.T) {
	t.Parallel()
	p := fastPacer()
	p.minGapFloor = time.Hour
	before := time.Now()
	p.last = before
	p.reqCount = 1

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	t0 := time.Now()
	err := p.wait(ctx, minDelay, "x")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
	if el := time.Since(t0); el > time.Second {
		t.Errorf("cancelled wait took %v", el)
	}
	if !p.last.Equal(before) || p.reqCount != 1 {
		t.Errorf("state after cancel: last moved %v, reqCount %d; want unchanged", p.last.Sub(before), p.reqCount)
	}

	// With a later reservation behind it, a cancelled waiter must keep its
	// slot rather than pull the schedule back under the other caller.
	ctx2, cancel2 := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.wait(ctx2, minDelay, "first") }()
	time.Sleep(10 * time.Millisecond)
	p.mu.Lock()
	firstSlot := p.last
	p.last = firstSlot.Add(time.Hour) // a second caller reserved after us
	later := p.last
	p.mu.Unlock()
	cancel2()
	<-done
	if !p.last.Equal(later) {
		t.Error("cancelled waiter moved the schedule behind a later reservation")
	}
}

// Unsafe clients keep their zero delay: requests are not spaced.
func TestPacer_UnsafeClientUnpaced(t *testing.T) {
	t.Parallel()
	srv, stamps := stampServer(t, http.StatusOK)
	c := NewUnsafeClient("")
	start := time.Now()
	for i := 0; i < 3; i++ {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/paced", nil)
		if _, err := c.do(req, 0); err != nil {
			t.Fatal(err)
		}
	}
	if len(stamps()) != 3 {
		t.Fatalf("got %d requests", len(stamps()))
	}
	if el := time.Since(start); el > time.Second {
		t.Errorf("unsafe client took %v for 3 requests, want no pacing", el)
	}
}
