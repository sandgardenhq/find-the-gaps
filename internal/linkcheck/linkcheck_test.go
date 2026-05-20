package linkcheck

import (
	"context"
	"net/url"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeChecker struct {
	mu       sync.Mutex
	results  map[string]Result
	hostHits map[string]*atomic.Int32
	maxByH   map[string]int32
}

func newFakeChecker() *fakeChecker {
	return &fakeChecker{
		results:  map[string]Result{},
		hostHits: map[string]*atomic.Int32{},
		maxByH:   map[string]int32{},
	}
}

func (f *fakeChecker) seed(u string, r Result) {
	r.URL = u
	f.mu.Lock()
	f.results[u] = r
	f.mu.Unlock()
}

func (f *fakeChecker) Check(ctx context.Context, raw string) Result {
	u, _ := url.Parse(raw)
	host := u.Host

	f.mu.Lock()
	counter, ok := f.hostHits[host]
	if !ok {
		counter = &atomic.Int32{}
		f.hostHits[host] = counter
	}
	f.mu.Unlock()

	cur := counter.Add(1)
	defer counter.Add(-1)

	f.mu.Lock()
	if cur > f.maxByH[host] {
		f.maxByH[host] = cur
	}
	f.mu.Unlock()

	time.Sleep(20 * time.Millisecond)

	f.mu.Lock()
	r := f.results[raw]
	f.mu.Unlock()
	r.URL = raw
	return r
}

func TestRun_AggregatesPagesPerURL(t *testing.T) {
	links := map[string][]string{
		"https://a.example/": {"https://docs/p1", "https://docs/p2", "https://docs/p3"},
		"https://b.example/": {"https://docs/p1"},
	}
	fc := newFakeChecker()
	fc.seed("https://a.example/", Result{Bucket: BucketBroken, ErrorType: "http_404", Detail: "404"})
	fc.seed("https://b.example/", Result{Bucket: BucketBroken, ErrorType: "http_404", Detail: "404"})

	rep, err := Run(context.Background(), Options{
		Links:          links,
		Checker:        fc,
		Workers:        4,
		PerHostWorkers: 4,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(rep.Broken) != 2 {
		t.Fatalf("broken=%d, want 2", len(rep.Broken))
	}
	if rep.Broken[0].URL != "https://a.example/" {
		t.Fatalf("got first=%s, want https://a.example/", rep.Broken[0].URL)
	}
	if len(rep.Broken[0].Pages) != 3 {
		t.Fatalf("pages=%d, want 3", len(rep.Broken[0].Pages))
	}
}

func TestRun_BucketsAndSortsCorrectly(t *testing.T) {
	links := map[string][]string{
		"https://broken1.example/": {"p1"},
		"https://broken2.example/": {"p1", "p2"},
		"https://auth.example/":    {"p1", "p2", "p3"},
		"https://ok.example/":      {"p1"},
	}
	fc := newFakeChecker()
	fc.seed("https://broken1.example/", Result{Bucket: BucketBroken, ErrorType: "http_404", Detail: "404"})
	fc.seed("https://broken2.example/", Result{Bucket: BucketBroken, ErrorType: "http_5xx", Detail: "500"})
	fc.seed("https://auth.example/", Result{Bucket: BucketAuth, Detail: "401"})
	fc.seed("https://ok.example/", Result{Bucket: BucketOK})

	rep, err := Run(context.Background(), Options{Links: links, Checker: fc, Workers: 4, PerHostWorkers: 4})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(rep.Broken) != 2 {
		t.Fatalf("broken=%d, want 2", len(rep.Broken))
	}
	if len(rep.Auth) != 1 {
		t.Fatalf("auth=%d, want 1", len(rep.Auth))
	}
	if rep.Broken[0].URL != "https://broken2.example/" {
		t.Fatalf("broken[0]=%s, want broken2", rep.Broken[0].URL)
	}
	if !sort.StringsAreSorted(rep.Broken[1].Pages) {
		t.Fatalf("Pages not sorted: %v", rep.Broken[1].Pages)
	}
}

func TestRun_PerHostThrottleHonored(t *testing.T) {
	links := map[string][]string{}
	for i := 0; i < 12; i++ {
		u := "https://same.example/" + string(rune('a'+i))
		links[u] = []string{"p1"}
	}
	fc := newFakeChecker()
	for u := range links {
		fc.seed(u, Result{Bucket: BucketOK})
	}

	_, err := Run(context.Background(), Options{Links: links, Checker: fc, Workers: 8, PerHostWorkers: 4})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := fc.maxByH["same.example"]; got > 4 {
		t.Fatalf("per-host high-water=%d, want <= 4", got)
	}
}

func TestRun_UsesCacheToSkipProbes(t *testing.T) {
	links := map[string][]string{
		"https://cached.example/": {"p1"},
		"https://fresh.example/":  {"p2"},
	}
	fc := newFakeChecker()
	fc.seed("https://cached.example/", Result{Bucket: BucketOK})
	fc.seed("https://fresh.example/", Result{Bucket: BucketBroken, ErrorType: "http_404", Detail: "404"})

	cache := NewCache(t.TempDir() + "/cache.json")
	cache.Put(Result{URL: "https://cached.example/", Bucket: BucketBroken, ErrorType: "http_404", Detail: "cached-as-broken"})

	rep, err := Run(context.Background(), Options{
		Links:          links,
		Checker:        fc,
		Cache:          cache,
		Workers:        4,
		PerHostWorkers: 4,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var sawCached bool
	for _, f := range rep.Broken {
		if f.URL == "https://cached.example/" {
			sawCached = true
		}
	}
	if !sawCached {
		t.Fatalf("expected cached entry to flow through to Broken, got %+v", rep.Broken)
	}
	// Checker must never have been called for the cached URL.
	if _, called := fc.hostHits["cached.example"]; called {
		t.Fatalf("expected cached.example to be skipped; checker observed it")
	}
}

// cancellingChecker cancels the supplied context the first time Check is
// invoked, then returns the configured Result. Subsequent invocations also
// see the canceled ctx but happily return the same Result — the harness
// under test must not persist results obtained under a canceled ctx.
type cancellingChecker struct {
	cancel context.CancelFunc
	result Result
	once   sync.Once
}

func (c *cancellingChecker) Check(_ context.Context, target string) Result {
	c.once.Do(func() { c.cancel() })
	r := c.result
	r.URL = target
	return r
}

// TestRun_SkipsCachePutWhenCtxCanceledDuringCheck pins that results produced
// after ctx cancellation are NOT persisted to the cache. Without the guard,
// in-flight probes interrupted by SIGINT get classified as Broken/network
// and poison the cache, so resume runs report those URLs as broken without
// ever re-probing them.
func TestRun_SkipsCachePutWhenCtxCanceledDuringCheck(t *testing.T) {
	links := map[string][]string{"https://a.example/": {"p1"}}
	ctx, cancel := context.WithCancel(context.Background())
	fc := &cancellingChecker{
		cancel: cancel,
		result: Result{Bucket: BucketBroken, ErrorType: "network", Detail: "ctx canceled"},
	}
	cache := NewCache(t.TempDir() + "/cache.json")

	_, err := Run(ctx, Options{
		Links:          links,
		Checker:        fc,
		Cache:          cache,
		Workers:        1,
		PerHostWorkers: 1,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, ok := cache.Get("https://a.example/"); ok {
		t.Fatalf("cache must not retain results obtained after ctx cancellation")
	}
}
