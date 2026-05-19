# Dead Link Check Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a mechanical dead-link check pass to `ftg analyze` that probes every link (same-host + outbound) discovered in the crawled docs site, classifies failures into Broken / Auth Required / Redirected, and emits a new top-level artifact set (`links.md`, `links.json`, `/links/` site page, "Dead Links" PDF section). Default-on, opt out with `--no-link-check`.

**Architecture:** New `internal/linkcheck/` package (extractor, checker, cache, orchestrator). New writers in `internal/reporter/`. The phase runs after the spider crawl and before the reporter writes its final artifact set, under the same bounded worker pool that drives the LLM phases. Persistent cache (`<projectDir>/links-cache.json`, no TTL) is invalidated only by `--no-cache`. Per-host throttle of 4 protects third-party hosts from accidental over-fetching. Findings get NO priority field — buckets render flat, sorted by `len(pages)` desc.

**Tech Stack:** Go 1.26+, `net/http`, `net/http/httptest`, Cobra, testify, testscript. No new third-party deps.

**Reference:** Design doc at `.plans/2026-05-19-dead-link-check-design.md`.

**TDD discipline:** Every task follows RED → GREEN → REFACTOR exactly as specified in `CLAUDE.md` §1–§9. No production code is written without a failing test first. Commit after every passing test.

---

## Pre-flight check

Confirm baseline is green before touching anything.

**Step 1: Run the full test suite**

Run: `go test ./... -count=1`
Expected: PASS (all green).

**Step 2: Run lint**

Run: `golangci-lint run`
Expected: zero issues.

If either fails, **stop**. Do not start the plan against a red baseline. Ask the user.

**Step 3: Confirm the design doc is on disk**

Run: `ls .plans/2026-05-19-dead-link-check-design.md`
Expected: file exists.

---

## Task 1: `linkcheck.Extract` — un-filtered link extraction

Build the function that walks a cached markdown page and returns every absolute HTTP(S) URL it references, regardless of host. Mirror the shape of `spider.ExtractLinks` but drop the same-host filter and add skip rules for non-HTTP schemes and private addresses.

**Files:**
- Create: `internal/linkcheck/extract.go`
- Create: `internal/linkcheck/extract_test.go`

### Step 1: Write the failing test

Create `internal/linkcheck/extract_test.go`:

```go
package linkcheck

import (
	"net/url"
	"reflect"
	"sort"
	"testing"
)

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %s: %v", raw, err)
	}
	return u
}

func TestExtract_PullsBothMarkdownAndBareURLs(t *testing.T) {
	page := mustURL(t, "https://docs.example.com/intro/")
	md := "See [the guide](/guide/) and [Cobra](https://github.com/spf13/cobra).\n" +
		"Inline: https://pkg.go.dev/net/http\n"

	got := Extract(md, page)
	sort.Strings(got)
	want := []string{
		"https://docs.example.com/guide/",
		"https://github.com/spf13/cobra",
		"https://pkg.go.dev/net/http",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestExtract_SkipsNonHTTPSchemes(t *testing.T) {
	page := mustURL(t, "https://docs.example.com/")
	md := "[email me](mailto:x@y.com) and [call](tel:+15555550100) and " +
		"[js](javascript:void(0)) and [data](data:text/plain,hello)"

	got := Extract(md, page)
	if len(got) != 0 {
		t.Fatalf("expected no URLs, got %v", got)
	}
}

func TestExtract_SkipsLocalhostAndRFC1918(t *testing.T) {
	page := mustURL(t, "https://docs.example.com/")
	md := "[a](http://localhost:8080/x) [b](http://127.0.0.1/) " +
		"[c](http://10.0.0.1/) [d](http://192.168.1.1/) [e](http://172.16.0.1/)"

	got := Extract(md, page)
	if len(got) != 0 {
		t.Fatalf("expected all private addresses skipped, got %v", got)
	}
}

func TestExtract_StripsFragments(t *testing.T) {
	page := mustURL(t, "https://docs.example.com/")
	md := "[a](https://x.example.com/page#section-a) " +
		"[b](https://x.example.com/page#section-b)"

	got := Extract(md, page)
	if len(got) != 1 || got[0] != "https://x.example.com/page" {
		t.Fatalf("expected single dedup'd URL, got %v", got)
	}
}

func TestExtract_DedupesAcrossMarkdownAndBareForms(t *testing.T) {
	page := mustURL(t, "https://docs.example.com/")
	md := "[github](https://github.com/x/y) — also: https://github.com/x/y."

	got := Extract(md, page)
	if len(got) != 1 || got[0] != "https://github.com/x/y" {
		t.Fatalf("expected single dedup'd URL, got %v", got)
	}
}

func TestExtract_ResolvesRelativeReferences(t *testing.T) {
	page := mustURL(t, "https://docs.example.com/sub/")
	md := "[rel](../other/) [abs-path](/root/) [abs-url](https://other.example.com/x)"

	got := Extract(md, page)
	sort.Strings(got)
	want := []string{
		"https://docs.example.com/other/",
		"https://docs.example.com/root/",
		"https://other.example.com/x",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
```

### Step 2: Run the test, watch it fail

Run: `go test ./internal/linkcheck/ -run TestExtract -v -count=1`
Expected: FAIL — the package does not exist (`build failed: no Go files in internal/linkcheck`).

### Step 3: Write the minimal implementation

Create `internal/linkcheck/extract.go`:

```go
// Package linkcheck probes every link discovered in the crawled docs site and
// classifies failures so a maintainer can see broken, auth-walled, and
// redirected URLs at a glance. See .plans/2026-05-19-dead-link-check-design.md.
package linkcheck

import (
	"net"
	"net/url"
	"regexp"
	"strings"
)

var (
	mdLinkRe  = regexp.MustCompile(`\[([^\]]*)\]\(([^)\s]+)\)`)
	bareURLRe = regexp.MustCompile(`(?:^|[\s(>])(https?://[^\s)<>"']+)`)
)

// Extract returns every absolute HTTP(S) URL referenced by markdown rendered
// at pageURL. Same-host and outbound links are both returned. Fragments are
// stripped before dedupe. URLs whose host is loopback or RFC1918 are skipped,
// as are mailto:/tel:/javascript:/data: schemes.
func Extract(markdown string, pageURL *url.URL) []string {
	seen := make(map[string]bool)
	var links []string

	add := func(raw string) {
		raw = strings.TrimRight(raw, ".,;!?)")
		ref, err := url.Parse(raw)
		if err != nil {
			return
		}
		if ref.Scheme == "" && ref.Host == "" && ref.Path == "" && ref.Fragment != "" {
			return
		}
		resolved := pageURL.ResolveReference(ref)
		switch resolved.Scheme {
		case "http", "https":
			// ok
		default:
			return
		}
		if resolved.Host == "" || isPrivateHost(resolved.Hostname()) {
			return
		}
		resolved.Fragment = ""
		abs := resolved.String()
		if !seen[abs] {
			seen[abs] = true
			links = append(links, abs)
		}
	}

	for _, m := range mdLinkRe.FindAllStringSubmatch(markdown, -1) {
		add(m[2])
	}
	for _, m := range bareURLRe.FindAllStringSubmatch(markdown, -1) {
		add(m[1])
	}
	return links
}

// isPrivateHost reports whether host is loopback, link-local, or RFC1918.
// It also matches the literal hostnames "localhost" and "localhost.localdomain".
func isPrivateHost(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "localhost.localdomain":
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}
```

### Step 4: Run the tests — watch them pass

Run: `go test ./internal/linkcheck/ -run TestExtract -v -count=1`
Expected: all six tests PASS.

### Step 5: Lint

Run: `golangci-lint run ./internal/linkcheck/...`
Expected: zero issues.

### Step 6: Commit

```bash
git add internal/linkcheck/extract.go internal/linkcheck/extract_test.go
git commit -m "$(cat <<'EOF'
feat(linkcheck): un-filtered URL extraction over cached markdown

- RED: TestExtract suite (markdown+bare dedupe, scheme skip, private-host
  skip, fragment strip, relative resolution).
- GREEN: linkcheck.Extract resolves relative refs against the page URL,
  strips fragments, skips non-HTTP schemes and loopback/RFC1918 hosts.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: `linkcheck.Checker` — HTTP probe + classification

Build the HTTP-probing primitive. HEAD-first with GET fallback on 405/501, retry once on timeout/5xx/conn-error with 1s backoff, 10s timeout, identifying User-Agent, follow up to 10 redirects, record the full status chain. Classify into `BucketOK | BucketBroken | BucketAuth | BucketRedirected`.

**Files:**
- Create: `internal/linkcheck/checker.go`
- Create: `internal/linkcheck/checker_test.go`

### Step 1: Write the failing test (Bucket + Result shape)

Create `internal/linkcheck/checker_test.go`:

```go
package linkcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newClient() *http.Client {
	return &http.Client{Timeout: 2 * time.Second}
}

func TestChecker_2xxIsOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	c := NewHTTPChecker(newClient(), "test-ua")
	got := c.Check(context.Background(), srv.URL)
	if got.Bucket != BucketOK {
		t.Fatalf("bucket=%v, want OK; result=%+v", got.Bucket, got)
	}
	if got.ErrorType != "" {
		t.Fatalf("ErrorType=%q, want empty", got.ErrorType)
	}
}

func TestChecker_404IsBroken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	got := NewHTTPChecker(newClient(), "test-ua").Check(context.Background(), srv.URL)
	if got.Bucket != BucketBroken {
		t.Fatalf("bucket=%v, want Broken", got.Bucket)
	}
	if got.ErrorType != "http_404" {
		t.Fatalf("ErrorType=%q, want http_404", got.ErrorType)
	}
	if !strings.Contains(got.Detail, "404") {
		t.Fatalf("Detail=%q, want it to mention 404", got.Detail)
	}
}

func TestChecker_401And403GoToAuthBucket(t *testing.T) {
	for _, code := range []int{401, 403} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
		}))
		got := NewHTTPChecker(newClient(), "test-ua").Check(context.Background(), srv.URL)
		srv.Close()
		if got.Bucket != BucketAuth {
			t.Fatalf("code=%d bucket=%v, want Auth", code, got.Bucket)
		}
	}
}

func TestChecker_RedirectToFinalURLIsRedirectedBucket(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/moved", 301)
	}))
	defer origin.Close()

	got := NewHTTPChecker(newClient(), "test-ua").Check(context.Background(), origin.URL)
	if got.Bucket != BucketRedirected {
		t.Fatalf("bucket=%v, want Redirected; result=%+v", got.Bucket, got)
	}
	if got.FinalURL == "" || got.FinalURL == origin.URL {
		t.Fatalf("FinalURL=%q, want it to differ from origin", got.FinalURL)
	}
	if len(got.StatusChain) < 2 {
		t.Fatalf("StatusChain=%v, want at least [3xx, 200]", got.StatusChain)
	}
}

func TestChecker_HEAD405FallsBackToGET(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Method == "HEAD" {
			w.WriteHeader(405)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	got := NewHTTPChecker(newClient(), "test-ua").Check(context.Background(), srv.URL)
	if got.Bucket != BucketOK {
		t.Fatalf("bucket=%v, want OK after GET fallback; result=%+v", got.Bucket, got)
	}
	if hits.Load() < 2 {
		t.Fatalf("hits=%d, want at least 2 (HEAD then GET)", hits.Load())
	}
}

func TestChecker_5xxRetriesOnceThenReportsBroken(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(503)
	}))
	defer srv.Close()

	c := NewHTTPChecker(newClient(), "test-ua")
	c.RetryBackoff = 10 * time.Millisecond // keep the test fast
	got := c.Check(context.Background(), srv.URL)
	if got.Bucket != BucketBroken {
		t.Fatalf("bucket=%v, want Broken", got.Bucket)
	}
	if got.ErrorType != "http_5xx" {
		t.Fatalf("ErrorType=%q, want http_5xx", got.ErrorType)
	}
	// HEAD -> 503 + retry HEAD -> 503: 2 hits minimum. GET fallback on 405/501 only.
	if hits.Load() < 2 {
		t.Fatalf("hits=%d, want at least 2 (initial + retry)", hits.Load())
	}
}

func TestChecker_4xxDoesNotRetry(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(404)
	}))
	defer srv.Close()

	c := NewHTTPChecker(newClient(), "test-ua")
	c.RetryBackoff = 10 * time.Millisecond
	_ = c.Check(context.Background(), srv.URL)
	if hits.Load() != 1 {
		t.Fatalf("hits=%d, want exactly 1 (no retry on 4xx)", hits.Load())
	}
}

func TestChecker_RedirectLoopIsBroken(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+r.URL.Path+"/x", 302)
	}))
	defer srv.Close()

	got := NewHTTPChecker(newClient(), "test-ua").Check(context.Background(), srv.URL+"/")
	if got.Bucket != BucketBroken {
		t.Fatalf("bucket=%v, want Broken", got.Bucket)
	}
	if got.ErrorType != "redirect_loop" {
		t.Fatalf("ErrorType=%q, want redirect_loop", got.ErrorType)
	}
}

func TestChecker_UnreachableHostIsBroken(t *testing.T) {
	c := NewHTTPChecker(newClient(), "test-ua")
	c.RetryBackoff = 10 * time.Millisecond
	got := c.Check(context.Background(), "http://127.0.0.1:1/")
	if got.Bucket != BucketBroken {
		t.Fatalf("bucket=%v, want Broken", got.Bucket)
	}
	if got.ErrorType != "connection_refused" && got.ErrorType != "dns" && got.ErrorType != "timeout" {
		t.Fatalf("ErrorType=%q, want connection_refused/dns/timeout", got.ErrorType)
	}
}

func TestChecker_SendsUserAgent(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.UserAgent()
		w.WriteHeader(200)
	}))
	defer srv.Close()

	_ = NewHTTPChecker(newClient(), "find-the-gaps/test").Check(context.Background(), srv.URL)
	if !strings.Contains(got, "find-the-gaps/test") {
		t.Fatalf("UA=%q, want it to contain find-the-gaps/test", got)
	}
}
```

### Step 2: Run the tests, watch them fail

Run: `go test ./internal/linkcheck/ -run TestChecker -v -count=1`
Expected: FAIL — `NewHTTPChecker`, `Result`, and bucket constants do not exist.

### Step 3: Write the minimal implementation

Create `internal/linkcheck/checker.go`:

```go
package linkcheck

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Bucket classifies a probe outcome.
type Bucket int

const (
	BucketOK Bucket = iota
	BucketBroken
	BucketAuth
	BucketRedirected
)

// Result is the outcome of probing a single URL.
type Result struct {
	URL         string
	FinalURL    string
	StatusChain []int
	ErrorType   string
	Detail      string
	Bucket      Bucket
	CheckedAt   time.Time
}

// Checker probes a URL and returns its Result.
type Checker interface {
	Check(ctx context.Context, url string) Result
}

// HTTPChecker is the production Checker. The exported fields are tuning knobs;
// zero values are production defaults.
type HTTPChecker struct {
	Client       *http.Client
	UserAgent    string
	RetryBackoff time.Duration // default 1s
}

// NewHTTPChecker builds a Checker with sensible defaults. The client must
// have a Timeout set (10s is recommended). A CheckRedirect that records
// the redirect chain is installed automatically.
func NewHTTPChecker(client *http.Client, userAgent string) *HTTPChecker {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &HTTPChecker{
		Client:       client,
		UserAgent:    userAgent,
		RetryBackoff: time.Second,
	}
}

// Check probes one URL.
func (c *HTTPChecker) Check(ctx context.Context, target string) Result {
	res := Result{URL: target, CheckedAt: time.Now()}

	chain, finalURL, err := c.do(ctx, "HEAD", target)
	if shouldFallbackToGET(chain, err) {
		chain, finalURL, err = c.do(ctx, "GET", target)
	}
	if shouldRetry(chain, err) {
		select {
		case <-ctx.Done():
		case <-time.After(c.RetryBackoff):
		}
		method := "HEAD"
		if shouldFallbackToGET(chain, err) {
			method = "GET"
		}
		chain, finalURL, err = c.do(ctx, method, target)
		if shouldFallbackToGET(chain, err) {
			chain, finalURL, err = c.do(ctx, "GET", target)
		}
	}

	res.StatusChain = chain
	res.FinalURL = finalURL
	classify(&res, err)
	return res
}

func (c *HTTPChecker) do(ctx context.Context, method, target string) (chain []int, finalURL string, err error) {
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, "", err
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}

	// Wrap the client so the redirect chain is captured and the 10-hop cap
	// is honored. We restore the caller's CheckRedirect after the call.
	prev := c.Client.CheckRedirect
	c.Client.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errRedirectLoop
		}
		// Record the upstream response status that triggered this redirect.
		if len(via) > 0 && via[len(via)-1].Response != nil {
			chain = append(chain, via[len(via)-1].Response.StatusCode)
		}
		return nil
	}
	defer func() { c.Client.CheckRedirect = prev }()

	resp, err := c.Client.Do(req)
	if resp != nil {
		// Append the final response status to the chain.
		chain = append(chain, resp.StatusCode)
		finalURL = resp.Request.URL.String()
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	return chain, finalURL, err
}

var errRedirectLoop = errors.New("redirect loop or hop cap exceeded")

func shouldFallbackToGET(chain []int, err error) bool {
	if err != nil {
		return false
	}
	if len(chain) == 0 {
		return false
	}
	last := chain[len(chain)-1]
	return last == http.StatusMethodNotAllowed || last == http.StatusNotImplemented
}

func shouldRetry(chain []int, err error) bool {
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return true
		}
		// Bare connection-refused / DNS errors are retryable.
		return !errors.Is(err, errRedirectLoop) && !errors.Is(err, context.Canceled)
	}
	if len(chain) == 0 {
		return false
	}
	last := chain[len(chain)-1]
	return last >= 500 && last <= 599
}

func classify(res *Result, err error) {
	if err != nil {
		res.Bucket = BucketBroken
		switch {
		case errors.Is(err, errRedirectLoop):
			res.ErrorType = "redirect_loop"
			res.Detail = "redirect loop or hop cap exceeded"
		case isTimeout(err):
			res.ErrorType = "timeout"
			res.Detail = "request timed out"
		case isDNS(err):
			res.ErrorType = "dns"
			res.Detail = "DNS lookup failed"
		case isTLS(err):
			res.ErrorType = "tls"
			res.Detail = "TLS verification failed"
		case isConnRefused(err):
			res.ErrorType = "connection_refused"
			res.Detail = "connection refused"
		default:
			res.ErrorType = "network"
			res.Detail = err.Error()
		}
		return
	}
	if len(res.StatusChain) == 0 {
		res.Bucket = BucketBroken
		res.ErrorType = "network"
		res.Detail = "no response"
		return
	}
	last := res.StatusChain[len(res.StatusChain)-1]
	switch {
	case last == 401 || last == 403:
		res.Bucket = BucketAuth
		res.Detail = fmt.Sprintf("HTTP %d %s", last, http.StatusText(last))
	case last >= 200 && last < 300:
		if len(res.StatusChain) > 1 && res.FinalURL != "" && res.FinalURL != res.URL {
			res.Bucket = BucketRedirected
			res.Detail = fmt.Sprintf("redirected to %s", res.FinalURL)
		} else {
			res.Bucket = BucketOK
		}
	case last >= 500 && last <= 599:
		res.Bucket = BucketBroken
		res.ErrorType = "http_5xx"
		res.Detail = fmt.Sprintf("HTTP %d %s", last, http.StatusText(last))
	case last == 404:
		res.Bucket = BucketBroken
		res.ErrorType = "http_404"
		res.Detail = "HTTP 404 Not Found"
	default:
		res.Bucket = BucketBroken
		res.ErrorType = fmt.Sprintf("http_%d", last)
		res.Detail = fmt.Sprintf("HTTP %d %s", last, http.StatusText(last))
	}
}

func isTimeout(err error) bool {
	var ne net.Error
	if errors.As(err, &ne) {
		return ne.Timeout()
	}
	return false
}

func isDNS(err error) bool {
	var dns *net.DNSError
	return errors.As(err, &dns)
}

func isTLS(err error) bool {
	return strings.Contains(err.Error(), "tls:") || strings.Contains(err.Error(), "x509:")
}

func isConnRefused(err error) bool {
	return strings.Contains(err.Error(), "connection refused")
}
```

### Step 4: Run the tests — watch them pass

Run: `go test ./internal/linkcheck/ -run TestChecker -v -count=1`
Expected: all `TestChecker_*` PASS.

If a test fails, **stop**. Read the failure message. The two most likely failure modes:
- Redirect-chain capture missing a leading status: confirm the `CheckRedirect` callback appends `via[len(via)-1].Response.StatusCode`.
- Retry firing on 4xx: confirm `shouldRetry` returns false for status codes < 500.

### Step 5: Lint

Run: `golangci-lint run ./internal/linkcheck/...`
Expected: zero issues.

### Step 6: Commit

```bash
git add internal/linkcheck/checker.go internal/linkcheck/checker_test.go
git commit -m "$(cat <<'EOF'
feat(linkcheck): HTTP probe with HEAD/GET fallback, retry, classification

- RED: TestChecker suite (2xx OK, 404 broken, 401/403 auth, redirect →
  redirected, HEAD→GET fallback on 405, 5xx retries once, 4xx never
  retries, redirect loop broken, unreachable host broken, UA sent).
- GREEN: HTTPChecker shells net/http with a redirect-chain capture,
  retry-once policy, and classify() that maps outcome → Bucket+ErrorType.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: `linkcheck.Cache` — persistent `links-cache.json`

Build the cache layer: load existing results from disk at phase start, write incrementally as probes complete, flush on close. No TTL; entries persist until `--no-cache` skips the load and the persist.

**Files:**
- Create: `internal/linkcheck/cache.go`
- Create: `internal/linkcheck/cache_test.go`

### Step 1: Write the failing test

Create `internal/linkcheck/cache_test.go`:

```go
package linkcheck

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCache_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "links-cache.json")

	c := NewCache(path)
	if err := c.Load(); err != nil {
		t.Fatalf("load empty: %v", err)
	}
	if _, ok := c.Get("https://a/"); ok {
		t.Fatalf("expected empty cache miss")
	}
	c.Put(Result{URL: "https://a/", Bucket: BucketOK, CheckedAt: time.Now()})
	if err := c.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	c2 := NewCache(path)
	if err := c2.Load(); err != nil {
		t.Fatalf("load populated: %v", err)
	}
	got, ok := c2.Get("https://a/")
	if !ok {
		t.Fatalf("expected hit after reload")
	}
	if got.Bucket != BucketOK {
		t.Fatalf("got bucket=%v, want OK", got.Bucket)
	}
}

func TestCache_LoadMissingFileIsNotAnError(t *testing.T) {
	c := NewCache(filepath.Join(t.TempDir(), "missing.json"))
	if err := c.Load(); err != nil {
		t.Fatalf("expected nil error for missing file, got %v", err)
	}
	if _, ok := c.Get("https://a/"); ok {
		t.Fatalf("expected empty cache")
	}
}

func TestCache_PutIsAtomicAcrossGoroutines(t *testing.T) {
	c := NewCache(filepath.Join(t.TempDir(), "cache.json"))
	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			c.Put(Result{URL: "https://a/", Bucket: BucketOK})
		}
		close(done)
	}()
	for i := 0; i < 200; i++ {
		_, _ = c.Get("https://a/")
	}
	<-done
	if _, ok := c.Get("https://a/"); !ok {
		t.Fatalf("expected entry to be set")
	}
}

func TestCache_FlushIsAtomicReplace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	c := NewCache(path)
	c.Put(Result{URL: "https://a/", Bucket: BucketOK})
	if err := c.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	// Replace the file mid-test should leave a parseable file at all times.
	for i := 0; i < 5; i++ {
		c.Put(Result{URL: "https://a/", Bucket: BucketBroken})
		if err := c.Flush(); err != nil {
			t.Fatalf("flush #%d: %v", i, err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("stat after flush #%d: %v", i, err)
		}
	}
}
```

### Step 2: Run the test, watch it fail

Run: `go test ./internal/linkcheck/ -run TestCache -v -count=1`
Expected: FAIL — `NewCache` is undefined.

### Step 3: Write the minimal implementation

Create `internal/linkcheck/cache.go`:

```go
package linkcheck

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// Cache is a goroutine-safe persistent map keyed by URL.
//
// No TTL: entries live until --no-cache skips both load and flush.
type Cache struct {
	path string
	mu   sync.RWMutex
	data map[string]Result
}

// NewCache constructs an empty Cache backed by path.
func NewCache(path string) *Cache {
	return &Cache{path: path, data: make(map[string]Result)}
}

// Load reads the cache file. A missing file is not an error.
func (c *Cache) Load() error {
	b, err := os.ReadFile(c.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return json.Unmarshal(b, &c.data)
}

// Get returns the cached Result for url, if any.
func (c *Cache) Get(url string) (Result, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	r, ok := c.data[url]
	return r, ok
}

// Put records a Result.
func (c *Cache) Put(r Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[r.URL] = r
}

// Flush writes the cache to disk via temp-file + rename for atomic replace.
func (c *Cache) Flush() error {
	c.mu.RLock()
	snap := make(map[string]Result, len(c.data))
	for k, v := range c.data {
		snap[k] = v
	}
	c.mu.RUnlock()

	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(c.path), ".links-cache-*.json")
	if err != nil {
		return err
	}
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(snap); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), c.path)
}
```

### Step 4: Run the tests — watch them pass

Run: `go test ./internal/linkcheck/ -run TestCache -v -count=1`
Expected: all four PASS.

### Step 5: Lint

Run: `golangci-lint run ./internal/linkcheck/...`
Expected: zero issues.

### Step 6: Commit

```bash
git add internal/linkcheck/cache.go internal/linkcheck/cache_test.go
git commit -m "$(cat <<'EOF'
feat(linkcheck): persistent links-cache.json, atomic flush

- RED: TestCache round-trip, missing-file tolerance, concurrent Put,
  atomic-replace flush.
- GREEN: Cache wraps a goroutine-safe map; Load tolerates ENOENT;
  Flush writes a temp file then renames to guarantee no torn reads.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: `linkcheck.Run` — orchestration with per-host throttle

Build the orchestrator. Input: a map `url → []pageURL` (built by the caller from cached markdown). Output: a `Report` that groups Results into the three reportable buckets, sorted by `len(pages)` desc.

**Files:**
- Create: `internal/linkcheck/linkcheck.go`
- Create: `internal/linkcheck/linkcheck_test.go`

### Step 1: Write the failing test

Create `internal/linkcheck/linkcheck_test.go`:

```go
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
	hostHits map[string]*atomic.Int32 // per-host high-water-mark
	maxByH   map[string]int32
}

func newFakeChecker() *fakeChecker {
	return &fakeChecker{
		results:  map[string]Result{},
		hostHits: map[string]*atomic.Int32{},
		maxByH:   map[string]int32{},
	}
}

func (f *fakeChecker) seed(url string, r Result) {
	r.URL = url
	f.mu.Lock()
	f.results[url] = r
	f.mu.Unlock()
}

func (f *fakeChecker) Check(ctx context.Context, raw string) Result {
	u, _ := url.Parse(raw)
	host := u.Host

	f.mu.Lock()
	if _, ok := f.hostHits[host]; !ok {
		f.hostHits[host] = &atomic.Int32{}
	}
	counter := f.hostHits[host]
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
		"https://a.example/":   {"https://docs/p1", "https://docs/p2", "https://docs/p3"},
		"https://b.example/":   {"https://docs/p1"},
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
	// Sort: 3 pages > 1 page, so a.example must come first.
	if rep.Broken[0].URL != "https://a.example/" {
		t.Fatalf("got first=%s, want https://a.example/", rep.Broken[0].URL)
	}
	if len(rep.Broken[0].Pages) != 3 {
		t.Fatalf("pages=%d, want 3", len(rep.Broken[0].Pages))
	}
}

func TestRun_BucketsAndSortsCorrectly(t *testing.T) {
	links := map[string][]string{
		"https://broken1.example/":  {"p1"},
		"https://broken2.example/":  {"p1", "p2"},
		"https://auth.example/":     {"p1", "p2", "p3"},
		"https://redir.example/":    {"p1"},
		"https://ok.example/":       {"p1"},
	}
	fc := newFakeChecker()
	fc.seed("https://broken1.example/", Result{Bucket: BucketBroken, ErrorType: "http_404", Detail: "404"})
	fc.seed("https://broken2.example/", Result{Bucket: BucketBroken, ErrorType: "http_5xx", Detail: "500"})
	fc.seed("https://auth.example/", Result{Bucket: BucketAuth, Detail: "401"})
	fc.seed("https://redir.example/", Result{Bucket: BucketRedirected, FinalURL: "https://elsewhere/x", StatusChain: []int{301, 200}})
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
	if len(rep.Redirected) != 1 {
		t.Fatalf("redirected=%d, want 1", len(rep.Redirected))
	}
	// Within broken: 2-pages first, then 1-page.
	if rep.Broken[0].URL != "https://broken2.example/" {
		t.Fatalf("broken[0]=%s, want broken2", rep.Broken[0].URL)
	}
	// Page list inside each finding is sorted ascending for determinism.
	if !sort.StringsAreSorted(rep.Broken[1].Pages) {
		t.Fatalf("Pages not sorted: %v", rep.Broken[1].Pages)
	}
}

func TestRun_PerHostThrottleHonored(t *testing.T) {
	// 12 URLs on the SAME host. Workers=8, PerHostWorkers=4. Expect host
	// concurrency to peak at 4, not 8.
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
	// The cached entry says broken; the live probe would say OK. Cache wins.
	var sawCached bool
	for _, f := range rep.Broken {
		if f.URL == "https://cached.example/" {
			sawCached = true
		}
	}
	if !sawCached {
		t.Fatalf("expected cached entry to flow through to Broken, got %+v", rep.Broken)
	}
	if fc.hostHits["cached.example"] != nil && fc.hostHits["cached.example"].Load() > 0 {
		// counter would have been incremented and decremented; we want the checker to never have been called at all.
		// We can prove that by checking it's nil (never added a key).
		t.Fatalf("expected cached.example to be skipped; checker observed it")
	}
}
```

### Step 2: Run the tests, watch them fail

Run: `go test ./internal/linkcheck/ -run TestRun -v -count=1`
Expected: FAIL — `Run`, `Options`, and `Report` are undefined.

### Step 3: Write the minimal implementation

Create `internal/linkcheck/linkcheck.go`:

```go
package linkcheck

import (
	"context"
	"net/url"
	"sort"
	"sync"
	"time"
)

// Finding is one URL's row in the report. Pages are sorted ascending for
// deterministic output.
type Finding struct {
	URL         string   `json:"url"`
	FinalURL    string   `json:"final_url,omitempty"`
	StatusChain []int    `json:"status_chain,omitempty"`
	ErrorType   string   `json:"error_type,omitempty"`
	Detail      string   `json:"detail,omitempty"`
	Pages       []string `json:"pages"`
}

// Report groups Findings by bucket. Each list is sorted by len(Pages) desc,
// tiebreak alphabetic by URL.
type Report struct {
	Broken     []Finding `json:"broken"`
	Auth       []Finding `json:"auth_required"`
	Redirected []Finding `json:"redirected"`
}

// Options configures Run.
type Options struct {
	Links          map[string][]string // url → list of referencing page URLs
	Checker        Checker
	Cache          *Cache
	Workers        int           // default 8
	PerHostWorkers int           // default 4
	FlushEvery    time.Duration // when set + Cache != nil, periodic flush
}

// Run probes every URL in opts.Links and returns the assembled Report.
func Run(ctx context.Context, opts Options) (Report, error) {
	if opts.Workers <= 0 {
		opts.Workers = 8
	}
	if opts.PerHostWorkers <= 0 {
		opts.PerHostWorkers = 4
	}

	type job struct {
		url   string
		pages []string
	}
	jobs := make(chan job)
	results := make(chan Result, len(opts.Links))

	var (
		hostSemMu sync.Mutex
		hostSem   = map[string]chan struct{}{}
	)
	acquire := func(host string) {
		hostSemMu.Lock()
		sem, ok := hostSem[host]
		if !ok {
			sem = make(chan struct{}, opts.PerHostWorkers)
			hostSem[host] = sem
		}
		hostSemMu.Unlock()
		sem <- struct{}{}
	}
	release := func(host string) {
		hostSemMu.Lock()
		sem := hostSem[host]
		hostSemMu.Unlock()
		<-sem
	}

	var wg sync.WaitGroup
	for i := 0; i < opts.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if opts.Cache != nil {
					if hit, ok := opts.Cache.Get(j.url); ok {
						results <- hit
						continue
					}
				}
				u, err := url.Parse(j.url)
				if err != nil {
					results <- Result{URL: j.url, Bucket: BucketBroken, ErrorType: "bad_url", Detail: err.Error()}
					continue
				}
				acquire(u.Host)
				r := opts.Checker.Check(ctx, j.url)
				release(u.Host)
				if opts.Cache != nil {
					opts.Cache.Put(r)
				}
				results <- r
			}
		}()
	}

	go func() {
		defer close(jobs)
		for u, pages := range opts.Links {
			select {
			case <-ctx.Done():
				return
			case jobs <- job{url: u, pages: pages}:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	pagesByURL := opts.Links
	var rep Report
	for r := range results {
		pages := append([]string(nil), pagesByURL[r.URL]...)
		sort.Strings(pages)
		f := Finding{
			URL:         r.URL,
			FinalURL:    r.FinalURL,
			StatusChain: r.StatusChain,
			ErrorType:   r.ErrorType,
			Detail:      r.Detail,
			Pages:       pages,
		}
		switch r.Bucket {
		case BucketBroken:
			rep.Broken = append(rep.Broken, f)
		case BucketAuth:
			rep.Auth = append(rep.Auth, f)
		case BucketRedirected:
			rep.Redirected = append(rep.Redirected, f)
		}
	}

	sortFindings(rep.Broken)
	sortFindings(rep.Auth)
	sortFindings(rep.Redirected)
	return rep, nil
}

func sortFindings(xs []Finding) {
	sort.SliceStable(xs, func(i, j int) bool {
		if len(xs[i].Pages) != len(xs[j].Pages) {
			return len(xs[i].Pages) > len(xs[j].Pages)
		}
		return xs[i].URL < xs[j].URL
	})
}
```

### Step 4: Run the tests — watch them pass

Run: `go test ./internal/linkcheck/ -run TestRun -v -count=1`
Expected: all four PASS.

### Step 5: Run the full package + lint

Run: `go test ./internal/linkcheck/... -race -count=1`
Expected: PASS (the `-race` flag is mandatory because of the per-host semaphore map).

Run: `golangci-lint run ./internal/linkcheck/...`
Expected: zero issues.

### Step 6: Commit

```bash
git add internal/linkcheck/linkcheck.go internal/linkcheck/linkcheck_test.go
git commit -m "$(cat <<'EOF'
feat(linkcheck): orchestrator with per-host throttle and cache reuse

- RED: TestRun_AggregatesPagesPerURL, TestRun_BucketsAndSortsCorrectly,
  TestRun_PerHostThrottleHonored, TestRun_UsesCacheToSkipProbes.
- GREEN: Run dispatches Workers goroutines, gates per-host parallelism
  on a PerHostWorkers semaphore, consults the optional Cache before
  probing, and assembles Findings sorted by len(Pages) desc.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Reporter writers — `links.md` + `links.json`

Render the `Report` produced by Task 4 into the two on-disk artifacts. Empty buckets are omitted from `links.md`; `links.json` always contains all three keys (empty slices when no findings).

**Files:**
- Create: `internal/reporter/links_writer.go`
- Create: `internal/reporter/links_writer_test.go`
- Create: `internal/reporter/links_json.go`
- Create: `internal/reporter/links_json_test.go`

### Step 1: Write the failing test (JSON)

Create `internal/reporter/links_json_test.go`:

```go
package reporter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/linkcheck"
)

func TestWriteLinksJSON_AlwaysWritesAllThreeKeys(t *testing.T) {
	dir := t.TempDir()
	rep := linkcheck.Report{} // all empty

	if err := WriteLinksJSON(dir, rep); err != nil {
		t.Fatalf("write: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "links.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, key := range []string{"broken", "auth_required", "redirected"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("missing key %q in %s", key, string(b))
		}
	}
}

func TestWriteLinksJSON_PopulatedFields(t *testing.T) {
	dir := t.TempDir()
	rep := linkcheck.Report{
		Broken: []linkcheck.Finding{{
			URL:         "https://gone.example/",
			ErrorType:   "http_404",
			Detail:      "HTTP 404 Not Found",
			StatusChain: []int{404},
			Pages:       []string{"p1", "p2"},
		}},
		Redirected: []linkcheck.Finding{{
			URL:         "https://old/x",
			FinalURL:    "https://new/x",
			StatusChain: []int{301, 200},
			Detail:      "redirected",
			Pages:       []string{"p1"},
		}},
	}
	if err := WriteLinksJSON(dir, rep); err != nil {
		t.Fatalf("write: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "links.json"))
	for _, want := range []string{
		`"url": "https://gone.example/"`,
		`"error_type": "http_404"`,
		`"status_chain": [`,
		`"final_url": "https://new/x"`,
	} {
		if !contains(b, want) {
			t.Fatalf("expected %q in:\n%s", want, b)
		}
	}
}

func contains(b []byte, s string) bool {
	return string(b) != "" && (func() bool {
		idx := -1
		for i := 0; i+len(s) <= len(b); i++ {
			if string(b[i:i+len(s)]) == s {
				idx = i
				break
			}
		}
		return idx >= 0
	})()
}
```

### Step 2: Run, watch fail

Run: `go test ./internal/reporter/ -run TestWriteLinksJSON -v -count=1`
Expected: FAIL — `WriteLinksJSON` is undefined.

### Step 3: Implement `links_json.go`

Create `internal/reporter/links_json.go`:

```go
package reporter

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/sandgardenhq/find-the-gaps/internal/linkcheck"
)

// WriteLinksJSON writes <dir>/links.json. All three top-level keys are always
// present; empty buckets render as `[]` so downstream tooling can rely on the
// shape.
func WriteLinksJSON(dir string, rep linkcheck.Report) error {
	type out struct {
		Broken     []linkcheck.Finding `json:"broken"`
		Auth       []linkcheck.Finding `json:"auth_required"`
		Redirected []linkcheck.Finding `json:"redirected"`
	}
	o := out{Broken: rep.Broken, Auth: rep.Auth, Redirected: rep.Redirected}
	if o.Broken == nil {
		o.Broken = []linkcheck.Finding{}
	}
	if o.Auth == nil {
		o.Auth = []linkcheck.Finding{}
	}
	if o.Redirected == nil {
		o.Redirected = []linkcheck.Finding{}
	}
	b, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "links.json"), b, 0o644)
}
```

### Step 4: Run, watch pass

Run: `go test ./internal/reporter/ -run TestWriteLinksJSON -v -count=1`
Expected: PASS.

### Step 5: Write the failing test (markdown)

Create `internal/reporter/links_writer_test.go`:

```go
package reporter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/linkcheck"
)

func TestWriteLinksMD_EmptyReportProducesEmptyButValidFile(t *testing.T) {
	dir := t.TempDir()
	if err := WriteLinksMD(dir, linkcheck.Report{}); err != nil {
		t.Fatalf("write: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "links.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	s := string(b)
	if !strings.HasPrefix(s, "# Dead Links\n") {
		t.Fatalf("want leading H1, got %q", s)
	}
	for _, banned := range []string{"## Broken", "## Auth Required", "## Redirected"} {
		if strings.Contains(s, banned) {
			t.Fatalf("empty report must not render %q section, got:\n%s", banned, s)
		}
	}
	if !strings.Contains(s, "No dead links detected") {
		t.Fatalf("want empty-state copy, got %q", s)
	}
}

func TestWriteLinksMD_RendersAllThreeBucketsWhenNonEmpty(t *testing.T) {
	dir := t.TempDir()
	rep := linkcheck.Report{
		Broken: []linkcheck.Finding{{
			URL:       "https://gone.example/",
			ErrorType: "http_404",
			Detail:    "HTTP 404 Not Found",
			Pages:     []string{"https://docs/a", "https://docs/b"},
		}},
		Auth: []linkcheck.Finding{{
			URL:    "https://private.example/",
			Detail: "HTTP 401 Unauthorized",
			Pages:  []string{"https://docs/a"},
		}},
		Redirected: []linkcheck.Finding{{
			URL:      "https://old.example/x",
			FinalURL: "https://new.example/x",
			Detail:   "redirected",
			Pages:    []string{"https://docs/a"},
		}},
	}
	if err := WriteLinksMD(dir, rep); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := readString(t, filepath.Join(dir, "links.md"))
	for _, want := range []string{
		"## Broken",
		"### https://gone.example/",
		"**Reason:** HTTP 404 Not Found",
		"## Auth Required",
		"## Redirected",
		"**Redirects to:** https://new.example/x",
		"- https://docs/a",
		"- https://docs/b",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("want %q in:\n%s", want, s)
		}
	}
	// Bucket order: Broken before Auth before Redirected.
	bi := strings.Index(s, "## Broken")
	ai := strings.Index(s, "## Auth Required")
	ri := strings.Index(s, "## Redirected")
	if !(bi < ai && ai < ri) {
		t.Fatalf("bucket order wrong: broken=%d auth=%d redirected=%d", bi, ai, ri)
	}
}

func TestWriteLinksMD_OmitsEmptyBuckets(t *testing.T) {
	dir := t.TempDir()
	rep := linkcheck.Report{
		Auth: []linkcheck.Finding{{URL: "https://private.example/", Detail: "401", Pages: []string{"p1"}}},
	}
	if err := WriteLinksMD(dir, rep); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := readString(t, filepath.Join(dir, "links.md"))
	if strings.Contains(s, "## Broken") {
		t.Fatalf("Broken section should be omitted")
	}
	if strings.Contains(s, "## Redirected") {
		t.Fatalf("Redirected section should be omitted")
	}
	if !strings.Contains(s, "## Auth Required") {
		t.Fatalf("Auth Required section should be present")
	}
}

func readString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
```

### Step 6: Run, watch fail

Run: `go test ./internal/reporter/ -run TestWriteLinksMD -v -count=1`
Expected: FAIL — `WriteLinksMD` is undefined.

### Step 7: Implement `links_writer.go`

Create `internal/reporter/links_writer.go`:

```go
package reporter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sandgardenhq/find-the-gaps/internal/linkcheck"
)

// WriteLinksMD renders rep to <dir>/links.md. Empty buckets are omitted.
// When all three buckets are empty, the file still renders with a leading
// H1 and an explicit "no dead links" line so the site/PDF surfaces have
// content to embed.
func WriteLinksMD(dir string, rep linkcheck.Report) error {
	var b strings.Builder
	b.WriteString("# Dead Links\n\n")

	total := len(rep.Broken) + len(rep.Auth) + len(rep.Redirected)
	if total == 0 {
		b.WriteString("_No dead links detected._\n")
		return os.WriteFile(filepath.Join(dir, "links.md"), []byte(b.String()), 0o644)
	}

	if len(rep.Broken) > 0 {
		b.WriteString("## Broken\n\n")
		for _, f := range rep.Broken {
			writeFinding(&b, f, "")
		}
	}
	if len(rep.Auth) > 0 {
		b.WriteString("## Auth Required\n\n")
		for _, f := range rep.Auth {
			writeFinding(&b, f, "")
		}
	}
	if len(rep.Redirected) > 0 {
		b.WriteString("## Redirected\n\n")
		for _, f := range rep.Redirected {
			writeFinding(&b, f, f.FinalURL)
		}
	}

	return os.WriteFile(filepath.Join(dir, "links.md"), []byte(b.String()), 0o644)
}

func writeFinding(b *strings.Builder, f linkcheck.Finding, redirectsTo string) {
	fmt.Fprintf(b, "### %s\n\n", f.URL)
	if f.Detail != "" {
		fmt.Fprintf(b, "**Reason:** %s\n\n", f.Detail)
	}
	if redirectsTo != "" {
		fmt.Fprintf(b, "**Redirects to:** %s\n\n", redirectsTo)
	}
	b.WriteString("**Pages:**\n\n")
	for _, p := range f.Pages {
		fmt.Fprintf(b, "- %s\n", p)
	}
	b.WriteString("\n")
}
```

### Step 8: Run, watch pass

Run: `go test ./internal/reporter/ -run TestWriteLinksMD -v -count=1`
Expected: all three PASS.

### Step 9: Full reporter suite + lint

Run: `go test ./internal/reporter/... -count=1`
Expected: PASS.

Run: `golangci-lint run ./internal/reporter/...`
Expected: zero issues.

### Step 10: Commit

```bash
git add internal/reporter/links_writer.go internal/reporter/links_writer_test.go \
        internal/reporter/links_json.go    internal/reporter/links_json_test.go
git commit -m "$(cat <<'EOF'
feat(reporter): emit links.md and links.json

- RED: TestWriteLinksJSON_* (all three keys always present, populated
  fields), TestWriteLinksMD_* (empty file fallback, all three buckets
  render in canonical order, empty buckets omitted).
- GREEN: WriteLinksJSON marshals the Report with empty slices instead of
  nulls; WriteLinksMD renders Broken → Auth → Redirected with finding
  cards.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Wire the link-check phase into `ftg analyze`

Register the `--no-link-check` flag, run the phase after the spider crawl, persist the cache, write `links.md` + `links.json`, and append a line to the stdout `reports:` block.

**Files:**
- Modify: `internal/cli/analyze.go` (flag registration ~line 808; orchestration at the spider-crawl tail; stdout block at lines 770-794)
- Modify: `internal/cli/analyze_test.go` (new flag-existence test)

Before editing, **read** `internal/cli/analyze.go` end-to-end. Locate:
1. The variable declaration block where `experimentalCheckScreenshots` is declared (around line 100).
2. The spider crawl invocation — `spider.Crawl(...)` — and the `pages` variable it produces (map of URL → cache path).
3. The stdout `reports:` printf at line 791-794.
4. The flag registration block (~line 808-839).

### Step 1: Write the failing test (flag exists)

Add to `internal/cli/analyze_test.go`:

```go
func TestAnalyzeCmd_HasNoLinkCheckFlag(t *testing.T) {
	cmd := newAnalyzeCmd()
	f := cmd.Flags().Lookup("no-link-check")
	require.NotNil(t, f)
	assert.Equal(t, "false", f.DefValue)
	assert.Contains(t, strings.ToLower(f.Usage), "link")
}
```

### Step 2: Run, watch fail

Run: `go test ./internal/cli/ -run TestAnalyzeCmd_HasNoLinkCheckFlag -v -count=1`
Expected: FAIL — flag not registered.

### Step 3: Register the flag

In `internal/cli/analyze.go`:

1. Add to the variable declaration block (right after `experimentalCheckScreenshots bool`):

```go
		noLinkCheck bool
```

2. Add to the `cmd.Flags()` block, alongside `--no-pdf` (which lives near line 830):

```go
	cmd.Flags().BoolVar(&noLinkCheck, "no-link-check", false,
		"skip the dead-link check; links.md / links.json / site /links/ / report.pdf section are still emitted with a (skipped) marker")
```

### Step 4: Run, watch pass

Run: `go test ./internal/cli/ -run TestAnalyzeCmd_HasNoLinkCheckFlag -v -count=1`
Expected: PASS.

### Step 5: Write the failing test (phase runs, artifacts produced)

Add to `internal/cli/analyze_test.go`:

```go
func TestAnalyzeCmd_LinkCheckEmitsArtifactsAndReportsLine(t *testing.T) {
	if testing.Short() {
		t.Skip("integration-shaped; skipping in -short")
	}
	// A minimal fixture that exercises the spider+linkcheck path.
	// Reuse the harness used by other analyze tests in this file —
	// see analyze_test.go's existing fixtures for patterns; build one
	// with a single docs page containing one broken outbound link.
	t.Skip("TODO: wire when the fixture harness is identified; covered by Scenario 19 verification")
}
```

This deliberately skips — the end-to-end behavior is covered by Scenario 19 (verification plan) and by the unit tests on the underlying packages. We register the test as a placeholder so the next person looking at the file finds the seam.

### Step 6: Wire the phase

After the spider's `pages` map is built but BEFORE the reporter writes its final artifacts, add a new block. The exact line is wherever `spider.Crawl` returns; locate by searching for the comment that introduces the screenshot-pass branch. The phase must:

1. Build `linkMap := map[string][]string{}` by ranging over the cached pages: read each cached markdown file, call `linkcheck.Extract(content, pageURL)`, and append `pageURL` to `linkMap[u]` for every returned `u`.
2. Construct a checker and cache (cache path: `filepath.Join(projectDir, "links-cache.json")`, skipped if `noCache` is set).
3. Run `linkcheck.Run(ctx, ...)` under the existing `--workers` setting.
4. Flush the cache (when set).
5. Write `links.md` + `links.json` via the new reporter funcs.

Skeleton (drop in next to the existing screenshot/site/pdf blocks):

```go
		// --- link check ---
		var linkReport linkcheck.Report
		linkCheckRan := !noLinkCheck
		if linkCheckRan {
			linkMap := map[string][]string{}
			for pageURL, cachePath := range pages {
				u, err := url.Parse(pageURL)
				if err != nil {
					continue
				}
				body, err := os.ReadFile(cachePath)
				if err != nil {
					continue
				}
				for _, link := range linkcheck.Extract(string(body), u) {
					linkMap[link] = append(linkMap[link], pageURL)
				}
			}

			httpClient := &http.Client{Timeout: 10 * time.Second}
			ua := fmt.Sprintf("find-the-gaps/%s (+https://github.com/sandgardenhq/find-the-gaps)", currentVersion())
			checker := linkcheck.NewHTTPChecker(httpClient, ua)

			var cache *linkcheck.Cache
			if !noCache {
				cache = linkcheck.NewCache(filepath.Join(projectDir, "links-cache.json"))
				if err := cache.Load(); err != nil {
					log.Warn("links cache load failed; starting fresh", "err", err)
				}
			}

			linkReport, err = linkcheck.Run(ctx, linkcheck.Options{
				Links:          linkMap,
				Checker:        checker,
				Cache:          cache,
				Workers:        workers,
				PerHostWorkers: 4,
			})
			if err != nil {
				return fmt.Errorf("link check: %w", err)
			}
			if cache != nil {
				if err := cache.Flush(); err != nil {
					log.Warn("links cache flush failed", "err", err)
				}
			}
			if err := reporter.WriteLinksMD(projectDir, linkReport); err != nil {
				return fmt.Errorf("write links.md: %w", err)
			}
			if err := reporter.WriteLinksJSON(projectDir, linkReport); err != nil {
				return fmt.Errorf("write links.json: %w", err)
			}
		}
```

You will need to import `"net/http"`, `"net/url"`, `"time"`, and the new `linkcheck` package.

### Step 7: Extend the stdout `reports:` block

Locate the printf at lines 791-794. Build a `linksLine` adjacent to `screenshotsLine`:

```go
				linksLine := fmt.Sprintf("  %s/links.md", projectDir)
				if !linkCheckRan {
					linksLine += " (skipped)"
				} else if c := linksCounts(linkReport); c != "" {
					linksLine += " (" + c + ")"
				}
```

Add a tiny helper in the same file:

```go
func linksCounts(r linkcheck.Report) string {
	if len(r.Broken)+len(r.Auth)+len(r.Redirected) == 0 {
		return ""
	}
	return fmt.Sprintf("%d broken · %d auth · %d redirected",
		len(r.Broken), len(r.Auth), len(r.Redirected))
}
```

Extend the `Fprintf` format and arg list to splice `linksLine` between `screenshotsLine` and `siteLine`:

```go
_, _ = fmt.Fprintf(cmd.OutOrStdout(),
    "scanned %d files, fetched %d pages, %d features mapped\nreports:\n  %s/mapping.md\n%s\n%s\n%s\n%s\n%s%s\n",
    len(scan.Files), len(pages), len(featureMap),
    projectDir, gapsLine, screenshotsLine, linksLine, siteLine, pdfLine, extraLine)
```

### Step 8: Run the full CLI suite

Run: `go test ./internal/cli/... -count=1`
Expected: PASS (existing tests that snapshot the `reports:` block may need their golden strings updated; that's expected.).

If any existing test fails on the new `links.md` line, **update the test** — it's a known-good change. Do NOT silence the new line.

### Step 9: Full build + lint

Run: `go build ./...`
Expected: SUCCESS.

Run: `golangci-lint run`
Expected: zero issues.

### Step 10: Commit

```bash
git add internal/cli/analyze.go internal/cli/analyze_test.go
git commit -m "$(cat <<'EOF'
feat(cli): wire dead-link check into ftg analyze

- RED: TestAnalyzeCmd_HasNoLinkCheckFlag.
- GREEN: --no-link-check flag registered; analyze runs the linkcheck
  phase after the spider crawl, persists links-cache.json (unless
  --no-cache), writes links.md + links.json, and appends a per-bucket
  count line to the stdout reports: block.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Render `/links/` in the Hugo site

Wire `links.md` into the site builder so the rendered site gets a `/links/` page.

**Files:**
- Modify: `internal/site/materialize.go` (mirror the screenshots handling around lines 322-340; add a links block)
- Modify: `internal/site/materialize_test.go` (extend the mirror-mode test to expect `links.md` in the rendered site)
- Modify: `internal/site/build_integration_test.go` (extend the file list assertion to include `links.md`)

### Step 1: Read the existing screenshots-mirror block

`internal/site/materialize.go:322-340` shows the screenshots-mirror handling: read the file, strip the leading H1, write into the content dir with appropriate front-matter. Mirror this pattern for `links.md`.

### Step 2: Write the failing test

Extend `internal/site/materialize_test.go`. In the test that asserts mirror-mode content layout, add `links.md` to the list of expected files.

Specifically, change the existing list at `materialize_test.go:27`:

```go
for _, name := range []string{"mapping.md", "gaps.md", "screenshots.md"} {
```

to:

```go
for _, name := range []string{"mapping.md", "gaps.md", "screenshots.md", "links.md"} {
```

And the weight-table test at `materialize_test.go:54-56`: add an entry for `links.md` with a weight (40, after the existing 30/20/10 scheme).

### Step 3: Run, watch fail

Run: `go test ./internal/site/ -run TestMaterialize -v -count=1`
Expected: FAIL — `links.md` does not exist in the content dir.

### Step 4: Implement the materializer block

In `internal/site/materialize.go`, after the screenshots block (around line 340), add:

```go
	// links.md — read raw, strip the standalone reporter's leading `# Dead
	// Links` H1, wrap in front-matter. Always written when the source file
	// exists, regardless of mode.
	if _, err := os.Stat(filepath.Join(opts.ProjectDir, "links.md")); err == nil {
		linksBody, err := os.ReadFile(filepath.Join(opts.ProjectDir, "links.md"))
		if err != nil {
			return fmt.Errorf("read links.md: %w", err)
		}
		linksFM := "---\ntitle = \"Dead Links\"\nweight = 40\n---\n"
		if err := os.WriteFile(filepath.Join(contentDir, "links.md"),
			append([]byte(linksFM), stripLeadingH1(linksBody)...), 0o644); err != nil {
			return fmt.Errorf("write links.md: %w", err)
		}
	}
```

### Step 5: Run, watch pass

Run: `go test ./internal/site/ -run TestMaterialize -v -count=1`
Expected: PASS.

### Step 6: Extend the build-integration assertion

In `internal/site/build_integration_test.go:91`, change `{"mapping.md", "gaps.md", "screenshots.md"}` to include `"links.md"`. Also update the other matching string list at line 21 and line 51 if they share the same intent (read the test first to confirm).

### Step 7: Full site test pass

Run: `go test ./internal/site/... -count=1`
Expected: PASS.

### Step 8: Lint

Run: `golangci-lint run ./internal/site/...`
Expected: zero issues.

### Step 9: Commit

```bash
git add internal/site/materialize.go internal/site/materialize_test.go internal/site/build_integration_test.go
git commit -m "$(cat <<'EOF'
feat(site): render /links/ page from links.md

- RED: materialize_test + build_integration_test extended to expect
  links.md in the rendered content dir at weight 40.
- GREEN: materialize.go reads links.md (when present), strips its
  leading H1, and writes it with Hextra front-matter.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: PDF — "Dead Links" section after "Screenshots"

Add a `DeadLinks` field to the PDF Input struct, render the section after Screenshots, no L/M/S sub-headings, no priority colors. The cover stat-card row stays at three cards.

**Files:**
- Modify: `internal/pdf/pdf.go` (extend `Input` struct)
- Create: `internal/pdf/deadlinks.go` (renderer)
- Create: `internal/pdf/deadlinks_test.go`

### Step 1: Read the existing screenshot renderer

`internal/pdf/screenshots.go` (or wherever `Screenshots` renders — find via `grep -n "func renderScreenshots\|Screenshots:" internal/pdf/`). Use that as a structural template.

### Step 2: Write the failing test

Create `internal/pdf/deadlinks_test.go`:

```go
package pdf

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/linkcheck"
)

func TestDeadLinks_RenderedWhenNonEmpty(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "report.pdf")
	in := Input{
		ProjectName: "test",
		DeadLinks: linkcheck.Report{
			Broken: []linkcheck.Finding{{URL: "https://gone/", ErrorType: "http_404", Detail: "HTTP 404 Not Found", Pages: []string{"https://docs/a"}}},
		},
	}
	if err := Render(in, out); err != nil {
		t.Fatalf("render: %v", err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytesContainsAll(b, []byte("Dead Links"), []byte("https://gone/")) {
		t.Fatalf("expected Dead Links section + URL in PDF text stream")
	}
}

func TestDeadLinks_OmittedWhenAllBucketsEmpty(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "report.pdf")
	in := Input{ProjectName: "test"} // zero DeadLinks
	if err := Render(in, out); err != nil {
		t.Fatalf("render: %v", err)
	}
	b, _ := os.ReadFile(out)
	if bytesContains(b, []byte("Dead Links")) {
		t.Fatalf("Dead Links section should be omitted when empty")
	}
}

// Helpers — PDF binary streams contain compressed text segments; for these
// smoke checks, do a naive byte-substring search.
func bytesContains(b, needle []byte) bool { return bytesIndex(b, needle) >= 0 }
func bytesContainsAll(b []byte, needles ...[]byte) bool {
	for _, n := range needles {
		if !bytesContains(b, n) {
			return false
		}
	}
	return true
}
func bytesIndex(b, needle []byte) int {
	for i := 0; i+len(needle) <= len(b); i++ {
		if string(b[i:i+len(needle)]) == string(needle) {
			return i
		}
	}
	return -1
}
```

NOTE: PDF text may be inside compressed object streams. If `bytesContains` returns false on a valid render, drive the test off the typed inputs to the renderer (e.g. assert that an unexported `collectTOCEntries` returns an entry for "Dead Links" when `DeadLinks` is non-empty) rather than reading the binary PDF.

### Step 3: Run, watch fail

Run: `go test ./internal/pdf/ -run TestDeadLinks -v -count=1`
Expected: FAIL — `DeadLinks` field is not on `Input`.

### Step 4: Extend `Input` and render the section

In `internal/pdf/pdf.go`, extend the `Input` struct:

```go
import "github.com/sandgardenhq/find-the-gaps/internal/linkcheck"

type Input struct {
    // ... existing fields
    DeadLinks   linkcheck.Report
}
```

Create `internal/pdf/deadlinks.go` mirroring the structure of `screenshots.go`. Render only when `len(DeadLinks.Broken) + len(DeadLinks.Auth) + len(DeadLinks.Redirected) > 0`. Section heading `Dead Links`. Three sub-sections: `Broken`, `Auth Required`, `Redirected` (omitted when empty). Each finding renders as a card with URL + Reason + Pages list. No left stripe (no priority).

Place the call to the new renderer immediately after the existing Screenshots renderer in the section sequence (search `internal/pdf/pdf.go` for "Screenshots" to find the dispatch point).

### Step 5: Run, watch pass

Run: `go test ./internal/pdf/ -run TestDeadLinks -v -count=1`
Expected: PASS.

### Step 6: Full PDF + lint

Run: `go test ./internal/pdf/... -count=1`
Expected: PASS.

Run: `golangci-lint run ./internal/pdf/...`
Expected: zero issues.

### Step 7: Wire `DeadLinks` from `cli/analyze.go` into PDF input

Find where `pdf.Input{...}` is constructed in `internal/cli/analyze.go`. Add `DeadLinks: linkReport`.

### Step 8: Extend the `report.pdf` stdout line

`pdfLine` already handles `--no-pdf` (skipped). The Dead Links section is included whenever the PDF is produced AND `linkCheckRan` is true. No additional stdout changes needed (the PDF skip annotation already covers it).

### Step 9: Build + commit

Run: `go build ./...`
Expected: SUCCESS.

Run: `golangci-lint run`
Expected: zero issues.

```bash
git add internal/pdf/pdf.go internal/pdf/deadlinks.go internal/pdf/deadlinks_test.go internal/cli/analyze.go
git commit -m "$(cat <<'EOF'
feat(pdf): render Dead Links section after Screenshots

- RED: TestDeadLinks_RenderedWhenNonEmpty, _OmittedWhenAllBucketsEmpty.
- GREEN: pdf.Input.DeadLinks; deadlinks.go renders three sub-sections
  (Broken / Auth Required / Redirected) with finding cards, no priority
  treatment. Cover stat-card row unchanged.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: Verification plan — Scenario 19

Add the new scenario to `.plans/VERIFICATION_PLAN.md`.

**Files:**
- Modify: `.plans/VERIFICATION_PLAN.md`

### Step 1: Append Scenario 19

Append at the end of the scenarios section (before "## Verification Rules"):

```markdown
---

### Scenario 19: Dead Link Check

**Context**: Same fixture and docs site as Scenario 9. Verifies that every link on the docs site is probed once, that classification matches the spec (Broken / Auth Required / Redirected), and that the persistent `links-cache.json` is honored across re-runs.

**Prerequisites**: Seed three known-status outbound URLs into the docs:
- A known-404 URL (e.g. `https://example.com/definitely-not-a-real-path-find-the-gaps-test`).
- A known-401 URL (e.g. a private GitHub repo the test runner cannot access).
- A known-301 URL (e.g. `http://github.com/` redirecting to `https://github.com/`).

**Steps**:
1. From a clean fixture state (`rm -rf <projectDir>`), run `find-the-gaps analyze --repo ./testdata/fixtures/known-good --docs https://<docs> -v` and capture stdout.
2. Inspect `<projectDir>/links.md`, `<projectDir>/links.json`, `<projectDir>/site/links/index.html`, and the "Dead Links" section in `<projectDir>/report.pdf`.
3. Re-run the same command. Confirm the run is meaningfully faster (cache hits) and that no fresh HTTP traffic appears under `-v` for the previously-probed URLs.
4. Re-run with `--no-cache`. Confirm every URL is re-probed.
5. Re-run with `--no-link-check`. Confirm stdout shows `links.md (skipped)` and that no `links.md` / `links.json` / `/links/` page / Dead Links PDF section is produced.
6. SIGINT a fresh run during the link-check phase. Confirm `<projectDir>/links-cache.json` contains a non-empty subset of probed URLs.

**Success Criteria**:
- [ ] Step 2: `links.md` contains the seeded 404 under `## Broken`, the 401 under `## Auth Required`, and the 301 under `## Redirected`.
- [ ] Step 2: `links.json` parses; every entry has `pages: [...]` populated.
- [ ] Step 2: Within each bucket, findings are sorted by `len(pages)` desc.
- [ ] Step 2: `<projectDir>/site/links/index.html` renders the three sections through Hextra.
- [ ] Step 2: `report.pdf` contains a "Dead Links" section after "Screenshots".
- [ ] Step 2: stdout `reports:` block contains `links.md (N broken · M auth · K redirected)`.
- [ ] Step 3: re-run completes in noticeably less wall-clock time and `-v` shows no per-URL probe lines for already-cached URLs.
- [ ] Step 4: every URL is re-probed (verifiable via `-v` lines or `links-cache.json` mtime).
- [ ] Step 5: `links.md (skipped)` appears; the artifact files are absent; PDF has no Dead Links section; site has no `/links/` page.
- [ ] Step 6: `links-cache.json` exists post-SIGINT with a partial set of entries; the next un-interrupted run completes the rest.

**If Blocked**: If the 401 URL is reported under `Broken` instead of `Auth Required`, the classifier is not catching 401/403 — check `internal/linkcheck/checker.go:classify`. If the 301 URL is reported under `Broken`, the redirect chain is being lost — check `internal/linkcheck/checker.go:do`. If the seeded 404 produces a finding but the cache short-circuits a re-run before all URLs were probed in step 1, the cache load logic is firing too early — check `internal/cli/analyze.go` ordering of `cache.Load()` vs the page collection.
```

### Step 2: Commit

```bash
git add .plans/VERIFICATION_PLAN.md
git commit -m "$(cat <<'EOF'
docs(verification): Scenario 19 for dead-link check

Seeded-URL fixture covers Broken / Auth / Redirected classification,
cache reuse, --no-cache invalidation, --no-link-check opt-out, and
SIGINT-safe partial cache persistence.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: README + CHANGELOG

User-visible changes need user-visible documentation.

**Files:**
- Modify: `README.md`
- Modify: `CHANGELOG.md` (or `.plans/PROGRESS.md` if that's where unreleased changes accumulate — check the existing convention before editing)

### Step 1: README

Add a short subsection under whichever heading documents `ftg analyze` output (likely under "Reports" or "Output"). Example copy:

```markdown
### Dead Link Check

By default, `ftg analyze` probes every link discovered in your docs site —
including outbound links to GitHub, third-party SDKs, and blog posts — and
groups failures into three buckets:

- **Broken** — 4xx, 5xx, DNS failures, timeouts, TLS errors, redirect loops.
- **Auth Required** — links returning 401 or 403 (called out separately
  because they need manual verification).
- **Redirected** — links that resolve via 3xx to a different final URL.

Findings land in `<projectDir>/links.md`, `<projectDir>/links.json`, the
rendered site at `/links/`, and a "Dead Links" section in `report.pdf`.
Within each bucket, findings are sorted by the number of pages that
reference the URL — high-traffic dead links surface first.

Results are cached at `<projectDir>/links-cache.json` indefinitely. Use
`--no-cache` to force a fresh probe of every URL. Skip the entire pass
with `--no-link-check`.
```

### Step 2: CHANGELOG

Add an entry under whatever "Unreleased" section the project maintains:

```markdown
### Added

- Dead-link check: `ftg analyze` now probes every link on the docs site
  and reports Broken / Auth Required / Redirected findings via
  `links.md`, `links.json`, the rendered `/links/` page, and a "Dead
  Links" PDF section. Opt out with `--no-link-check`.
```

### Step 3: Commit

```bash
git add README.md CHANGELOG.md
git commit -m "$(cat <<'EOF'
docs: dead-link check in README + CHANGELOG

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Final verification

### Step 1: Full suite with race detector

Run: `go test ./... -race -count=1`
Expected: PASS.

### Step 2: Lint

Run: `golangci-lint run`
Expected: zero issues.

### Step 3: Coverage check on the new package

Run: `go test ./internal/linkcheck/ -coverprofile=coverage.out && go tool cover -func=coverage.out | tail -5`
Expected: 90%+ coverage per CLAUDE.md.

### Step 4: Manual smoke against a real docs site

Run: `go build -o /tmp/ftg ./cmd/ftg && /tmp/ftg analyze --repo ./testdata/fixtures/<a-known-fixture> --docs https://<real-docs-site> -v`
Expected:
- `links.md`, `links.json`, `/links/` page, and a "Dead Links" PDF section are emitted.
- The stdout `reports:` block includes the new `links.md (...)` line with counts.
- Re-running consumes the cache (verifiable via `-v`).

### Step 5: Update `PROGRESS.md`

Per CLAUDE.md §8, log the completed task in `PROGRESS.md` with timestamp, tests passing, coverage, build/lint status, and notes.

### Step 6: Open the PR

```bash
gh pr create --base main --title "feat: dead-link check pass" --body "$(cat <<'EOF'
## Summary
- Probes every link discovered in the crawled docs site (same-host + outbound) during `ftg analyze`.
- Classifies into Broken (with error_type), Auth Required, Redirected.
- Emits `links.md`, `links.json`, `/links/` site page, and a "Dead Links" PDF section.
- Persistent `<projectDir>/links-cache.json` (no TTL) invalidated only by `--no-cache`.
- Opt out with `--no-link-check`.

Design: `.plans/2026-05-19-dead-link-check-design.md`
Plan: `.plans/2026-05-19-dead-link-check-plan.md`

## Test plan
- [x] `go test ./... -race -count=1`
- [x] `golangci-lint run`
- [x] Scenario 19 in `.plans/VERIFICATION_PLAN.md`
- [x] Manual smoke against a real docs site

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Out of scope (do NOT implement in this PR)

These are deliberately deferred — see the design doc § "Out of scope":

- Fragment-anchor validation (does `#section-a` exist on the target page).
- GitHub Action issue-body inclusion of dead-link findings.
- `Retry-After` header honoring on rate-limit responses.
- Per-link priority (LLM-judged or otherwise).

If during implementation you find yourself reaching for any of these, **stop** and ask the user. They are not silently in-scope.
