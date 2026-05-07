# Parallelize LLM-Heavy Phases — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Cut wall-clock time on `find-the-gaps analyze` by running the three serial LLM-heavy phases (page analysis, drift detection, screenshot detection) in parallel under one bounded worker pool, with safe coordination of shared caches and report files.

**Architecture:** A new `runParallel[T]` helper (`errgroup` + bounded semaphore) wraps each phase's loop. Shared cache JSONs and in-memory accumulators get `sync.Mutex` guards. `gaps.md` and `screenshots.md` move to a single-writer-goroutine + debounce pattern fed by a buffered channel; `gaps.md` additionally gets a precomputed static prefix so the unchanging Undocumented Code + Unmapped Features sections aren't rebuilt per finding. Screenshot detection gains a per-page `screenshots-cache.json` cache + completion sentinel mirroring `drift.json` so partial runs do not lose work. (The cache is at a distinct path from the reporter's user-visible `screenshots.json` flat findings file — the two writers cannot share a filename without clobbering each other's shape.)

**Tech Stack:** Go 1.26+, `golang.org/x/sync/errgroup`, stdlib `sync`, existing project conventions (testify, testscript, `go test -race ./...`).

**Reference design:** [`.plans/2026-05-06-parallelize-llm-phases-design.md`](./2026-05-06-parallelize-llm-phases-design.md)

**TDD discipline:** Per `CLAUDE.md`, every task follows RED → verify-RED → GREEN → verify-GREEN → REFACTOR → commit. No production code lands without a failing test that fails for the right reason. Run `go test -race ./...` before each commit that introduces concurrency.

---

## Task 1: `runParallel` helper

**Files:**
- Create: `internal/parallel/parallel.go`
- Create: `internal/parallel/parallel_test.go`

**Why first:** Every phase parallelization depends on this primitive. Building it standalone with tests means later tasks only call it.

**Step 1: Write the failing test**

`internal/parallel/parallel_test.go`:

```go
package parallel_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sandgardenhq/find-the-gaps/internal/parallel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun_runsAllItems(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	var seen sync.Map
	err := parallel.Run(context.Background(), items, 3, func(_ context.Context, n int) error {
		seen.Store(n, true)
		return nil
	})
	require.NoError(t, err)
	for _, n := range items {
		_, ok := seen.Load(n)
		assert.Truef(t, ok, "item %d not seen", n)
	}
}

func TestRun_capsConcurrency(t *testing.T) {
	const workers = 3
	const items = 12
	var inFlight int32
	var peak int32
	work := make([]int, items)
	err := parallel.Run(context.Background(), work, workers, func(_ context.Context, _ int) error {
		cur := atomic.AddInt32(&inFlight, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if cur <= p || atomic.CompareAndSwapInt32(&peak, p, cur) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		return nil
	})
	require.NoError(t, err)
	assert.LessOrEqual(t, peak, int32(workers))
}

func TestRun_firstErrorCancelsRest(t *testing.T) {
	items := make([]int, 50)
	for i := range items {
		items[i] = i
	}
	var ran int32
	sentinel := errors.New("boom")
	err := parallel.Run(context.Background(), items, 4, func(ctx context.Context, n int) error {
		atomic.AddInt32(&ran, 1)
		if n == 0 {
			return sentinel
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
			return nil
		}
	})
	require.ErrorIs(t, err, sentinel)
	assert.Less(t, atomic.LoadInt32(&ran), int32(50))
}

func TestRun_zeroWorkersDefaultsToOne(t *testing.T) {
	var ran int32
	err := parallel.Run(context.Background(), []int{1, 2}, 0, func(_ context.Context, _ int) error {
		atomic.AddInt32(&ran, 1)
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, int32(2), ran)
}

func TestRun_emptySliceIsNoop(t *testing.T) {
	var ran int32
	err := parallel.Run(context.Background(), []int{}, 4, func(_ context.Context, _ int) error {
		atomic.AddInt32(&ran, 1)
		return nil
	})
	require.NoError(t, err)
	assert.Zero(t, ran)
}
```

**Step 2: Run test to verify it fails**

```
go test -race ./internal/parallel/...
```

Expected: FAIL — package does not compile (`internal/parallel/parallel.go` does not exist).

**Step 3: Write minimal implementation**

`internal/parallel/parallel.go`:

```go
// Package parallel provides a small bounded-concurrency helper used by the
// LLM-heavy analyze phases. Cancellation is propagated through the supplied
// context; the first non-nil error from fn cancels remaining work and is
// returned.
package parallel

import (
	"context"

	"golang.org/x/sync/errgroup"
)

// Run executes fn for every element of items with at most workers in flight.
// A workers value <= 0 is treated as 1 (serial). The context handed to fn is
// cancelled as soon as any fn returns a non-nil error.
func Run[T any](ctx context.Context, items []T, workers int, fn func(context.Context, T) error) error {
	if len(items) == 0 {
		return nil
	}
	if workers <= 0 {
		workers = 1
	}
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(workers)
	for _, item := range items {
		item := item
		g.Go(func() error {
			if gctx.Err() != nil {
				return gctx.Err()
			}
			return fn(gctx, item)
		})
	}
	return g.Wait()
}
```

**Step 4: Run test to verify it passes**

```
go test -race ./internal/parallel/...
```

Expected: PASS, all five tests.

**Step 5: Commit**

```bash
git add internal/parallel/parallel.go internal/parallel/parallel_test.go
git commit -m "feat(parallel): add bounded concurrency helper

- RED: tests for cap, cancel-on-first-error, zero/empty edge cases
- GREEN: errgroup.SetLimit-based Run[T]
- Status: 5 tests passing under -race"
```

---

## Task 2: Parallelize the page-analysis loop

**Files:**
- Modify: `internal/cli/analyze.go:182-214` (the page-analysis `for url, filePath := range pages` loop)
- Modify or extend: `internal/cli/analyze_parallel_test.go` — existing parallel-mapping test file is a good neighbour; add new tests there.

**Context:**
- `analyzer.AnalyzePage` is the per-page LLM call (small tier).
- `idx.RecordAnalysis` is already concurrent-safe (`internal/spider/cache.go:42` declares `sync.Mutex`).
- Today's loop builds `analyses []analyzer.PageAnalysis` and increments `freshCount` and `pageNum` in serial. Under parallel workers, those need a mutex.

**Step 1: Write the failing test**

In `internal/cli/analyze_parallel_test.go`, add a test that:
1. Stubs `tieringFactory` with a fake `LLMClient` whose response handler records the order of incoming URLs and blocks briefly so multiple are in-flight at once.
2. Runs `analyze` with `--workers=4` against a fixture with 8 pages.
3. Asserts every page's analysis is recorded in `idx` and `analyses` length is 8.
4. Asserts the recorded order is NOT identical to the spider's `pages` map iteration order (i.e. pages truly ran out-of-order; the existing `TestRunBothMaps_RunsConcurrently` pattern is the model).

(If asserting concurrency directly is brittle, use the pattern from Task 1's `TestRun_capsConcurrency`: have the fake client track peak-in-flight and assert it is `>= 2`.)

**Step 2: Run test to verify it fails**

```
go test -race -run TestAnalyze_pageAnalysisRunsConcurrently ./internal/cli/...
```

Expected: FAIL — peak-in-flight observed is 1.

**Step 3: Write minimal implementation**

In `internal/cli/analyze.go`, replace the serial `for url, filePath := range pages { ... }` block with:

```go
type pageJob struct {
    url      string
    filePath string
}
jobs := make([]pageJob, 0, len(pages))
for url, filePath := range pages {
    if summary, features, isDocs, ok := idx.Analysis(url); ok {
        log.Debug("page cache hit", "url", url)
        analyses = append(analyses, analyzer.PageAnalysis{URL: url, Summary: summary, Features: features, IsDocs: isDocs})
        continue
    }
    jobs = append(jobs, pageJob{url: url, filePath: filePath})
}

var (
    analysesMu sync.Mutex
    pageNum    atomic.Int32
)
log.Infof("analyzing %d pages...", len(pages))
err = parallel.Run(ctx, jobs, workers, func(ctx context.Context, j pageJob) error {
    content, readErr := os.ReadFile(j.filePath)
    if readErr != nil {
        return nil // preserve current "log and skip unreadable" behavior
    }
    n := pageNum.Add(1)
    log.Infof("  [%d] %s", n, j.url)
    pa, analyzeErr := analyzer.AnalyzePage(ctx, tiering, j.url, string(content))
    if analyzeErr != nil {
        log.Warnf("skipping %s: %v", j.url, analyzeErr)
        return nil // preserve current "log and skip" behavior
    }
    if recErr := idx.RecordAnalysis(j.url, pa.Summary, pa.Features, pa.IsDocs); recErr != nil {
        return fmt.Errorf("record analysis: %w", recErr)
    }
    analysesMu.Lock()
    analyses = append(analyses, pa)
    analysesMu.Unlock()
    return nil
})
if err != nil {
    return fmt.Errorf("analyze pages: %w", err)
}
freshCount := int(pageNum.Load())
```

Add imports: `sync`, `sync/atomic`, `github.com/sandgardenhq/find-the-gaps/internal/parallel`.

**Step 4: Run test + full suite under race**

```
go test -race -run TestAnalyze_pageAnalysisRunsConcurrently ./internal/cli/...
go test -race ./...
```

Expected: PASS for the new test and all pre-existing tests.

**Step 5: Commit**

```bash
git add internal/cli/analyze.go internal/cli/analyze_parallel_test.go
git commit -m "perf(analyze): parallelize page-analysis loop

- RED: peak-in-flight test asserts >= 2 concurrent AnalyzePage calls
- GREEN: route the per-page LLM call through parallel.Run, mutex-guard the analyses slice
- Status: tests passing under -race"
```

---

## Task 3: Extract gaps.md static prefix

**Files:**
- Modify: `internal/reporter/reporter.go` — split `WriteGaps` so the unchanging sections become a separately addressable function.
- Modify: `internal/reporter/reporter_test.go` — pin the new prefix function's output.

**Why now:** The single-writer goroutine in Task 4 needs the static prefix as a value it can reuse. Doing the extraction first keeps the diff in Task 4 about wiring, not formatting.

**Step 1: Write the failing test**

Add to `reporter_test.go`:

```go
func TestBuildGapsStaticPrefix_includesUndocumentedAndUnmapped(t *testing.T) {
    mapping := analyzer.FeatureMap{
        {Feature: analyzer.CodeFeature{Name: "alpha", UserFacing: true}, Files: []string{"a.go"}},
        {Feature: analyzer.CodeFeature{Name: "beta"}, Files: []string{"b.go"}},
    }
    docFeatures := []string{"gamma"}
    got := reporter.BuildGapsStaticPrefix(mapping, docFeatures)
    assert.Contains(t, got, "## Undocumented Code")
    assert.Contains(t, got, "alpha")
    assert.Contains(t, got, "beta")
    assert.Contains(t, got, "## Unmapped Features")
    assert.Contains(t, got, "gamma")
    assert.NotContains(t, got, "## Stale Documentation")
}
```

**Step 2: Run test to verify it fails**

```
go test -run TestBuildGapsStaticPrefix ./internal/reporter/...
```

Expected: FAIL — `BuildGapsStaticPrefix` undefined.

**Step 3: Write minimal implementation**

In `reporter.go`, extract the first three sections of `WriteGaps` (Undocumented Code user-facing + not user-facing + Unmapped Features) into:

```go
// BuildGapsStaticPrefix renders the portion of gaps.md whose contents do
// not change as drift findings stream in: Undocumented Code (split by
// user-facing) and Unmapped Features. The Stale Documentation section is
// rendered separately by BuildGapsStaleSection.
func BuildGapsStaticPrefix(mapping analyzer.FeatureMap, allDocFeatures []string) string {
    // ... extracted body ending right before "## Stale Documentation" ...
}

// BuildGapsStaleSection renders the Stale Documentation section in priority
// order (Large → Medium → Small). It is the only part of gaps.md that
// changes during a drift run.
func BuildGapsStaleSection(drift []analyzer.DriftFinding) string { ... }
```

Update `WriteGaps` to compose `BuildGapsStaticPrefix(...) + "\n## Stale Documentation\n\n" + BuildGapsStaleSection(...)` so its existing behavior is byte-identical.

**Step 4: Run tests**

```
go test -race ./internal/reporter/...
go test -race ./...
```

Expected: PASS, including the existing reporter golden tests (which validate `WriteGaps` byte-for-byte).

**Step 5: Commit**

```bash
git add internal/reporter/reporter.go internal/reporter/reporter_test.go
git commit -m "refactor(reporter): extract gaps.md static prefix and stale section

- RED: BuildGapsStaticPrefix test pins per-section content
- GREEN: split WriteGaps into prefix + stale builders; behavior unchanged
- Status: existing golden tests still pass"
```

---

## Task 4: Single-writer goroutine for `gaps.md`

**Files:**
- Create: `internal/reporter/gaps_writer.go`
- Create: `internal/reporter/gaps_writer_test.go`

**What this is:** A `GapsWriter` type owns the file's bytes. Callers `Push(findings)`; the writer debounces (~500ms) and rewrites atomically. `Close()` flushes pending state and waits for the writer goroutine to exit.

**Step 1: Write the failing test**

`gaps_writer_test.go` should cover:

1. **Coalesces bursts** — push 5 finding slices in quick succession; assert exactly 1 write hit disk before the debounce window elapses.
2. **Final flush on Close** — push, immediately Close; assert the final state is on disk.
3. **Atomic replace** — observer never sees a half-written file (use a tight reader loop checking parse-ability in a goroutine).
4. **Concurrent Push under -race** — N goroutines call Push; final on-disk content matches the last Push.

```go
func TestGapsWriter_coalescesBursts(t *testing.T) {
    dir := t.TempDir()
    w := reporter.NewGapsWriter(dir, "<static prefix>", 50*time.Millisecond)
    for i := 0; i < 5; i++ {
        w.Push([]analyzer.DriftFinding{{Feature: fmt.Sprintf("f%d", i), Issues: []analyzer.DriftIssue{{Issue: "x", Priority: analyzer.PriorityMedium}}}})
    }
    require.NoError(t, w.Close())
    bytes, err := os.ReadFile(filepath.Join(dir, "gaps.md"))
    require.NoError(t, err)
    assert.Contains(t, string(bytes), "f4")
}

// ... plus the other three scenarios.
```

**Step 2: Run test to verify it fails**

```
go test -race -run TestGapsWriter ./internal/reporter/...
```

Expected: FAIL — `NewGapsWriter` undefined.

**Step 3: Write minimal implementation**

`internal/reporter/gaps_writer.go`:

```go
package reporter

import (
    "os"
    "path/filepath"
    "sync"
    "time"

    "github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// GapsWriter owns gaps.md for the duration of a drift run. Workers Push
// accumulated findings; the goroutine debounces writes so a burst of
// concurrent finishes coalesces into one atomic file replacement.
type GapsWriter struct {
    dir       string
    prefix    string
    debounce  time.Duration

    mu       sync.Mutex
    latest   []analyzer.DriftFinding // sole copy of "what the file should currently say"
    dirty    bool

    wakeup   chan struct{}
    done     chan struct{}
    closeOnce sync.Once
    closed    chan struct{}
}

func NewGapsWriter(dir, prefix string, debounce time.Duration) *GapsWriter {
    w := &GapsWriter{
        dir:      dir,
        prefix:   prefix,
        debounce: debounce,
        wakeup:   make(chan struct{}, 1),
        done:     make(chan struct{}),
        closed:   make(chan struct{}),
    }
    go w.loop()
    return w
}

// Push replaces the writer's current view of all findings. Caller passes the
// full accumulated slice each time (not just the new entries) — the writer is
// the source of truth for on-disk state, not a delta accumulator.
func (w *GapsWriter) Push(findings []analyzer.DriftFinding) {
    w.mu.Lock()
    w.latest = append(w.latest[:0], findings...)
    w.dirty = true
    w.mu.Unlock()
    select {
    case w.wakeup <- struct{}{}:
    default:
    }
}

func (w *GapsWriter) Close() error {
    w.closeOnce.Do(func() { close(w.closed) })
    <-w.done
    return nil
}

func (w *GapsWriter) loop() {
    defer close(w.done)
    var timer *time.Timer
    arm := func() {
        if timer == nil {
            timer = time.NewTimer(w.debounce)
            return
        }
        timer.Reset(w.debounce)
    }
    var fire <-chan time.Time
    for {
        select {
        case <-w.wakeup:
            arm()
            fire = timer.C
        case <-fire:
            fire = nil
            w.flush()
        case <-w.closed:
            w.flush()
            return
        }
    }
}

func (w *GapsWriter) flush() {
    w.mu.Lock()
    if !w.dirty {
        w.mu.Unlock()
        return
    }
    findings := append([]analyzer.DriftFinding(nil), w.latest...)
    w.dirty = false
    w.mu.Unlock()

    body := w.prefix + "\n## Stale Documentation\n\n" + BuildGapsStaleSection(findings)
    tmp := filepath.Join(w.dir, "gaps.md.tmp")
    final := filepath.Join(w.dir, "gaps.md")
    if err := os.WriteFile(tmp, []byte(body), 0o644); err == nil {
        _ = os.Rename(tmp, final)
    }
}
```

**Step 4: Run tests**

```
go test -race -run TestGapsWriter ./internal/reporter/...
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/reporter/gaps_writer.go internal/reporter/gaps_writer_test.go
git commit -m "feat(reporter): add debounced single-writer GapsWriter

- RED: tests for burst coalescing, final flush, atomic replace, concurrent push
- GREEN: GapsWriter goroutine + debounce timer + atomic temp+rename
- Status: passing under -race"
```

---

## Task 5: Wire `GapsWriter` into the analyze command

**Files:**
- Modify: `internal/cli/analyze.go` — replace the `driftOnFinding` callback with a `GapsWriter.Push` call; ensure `Close()` runs before the final `WriteGaps` end-of-run path.

**Step 1: Write the failing test**

Add `internal/cli/analyze_gaps_writer_test.go` asserting that during an analyze run with multiple drift findings, `gaps.md` is rewritten at most ceil(total_run_seconds / debounce) times — implemented by stubbing the file watcher with a counter on `os.Rename` (or by injecting a debounce of 500ms and observing only one rename for a 5-finding run that completes inside 500ms).

**Step 2: Run test to verify it fails**

```
go test -race -run TestAnalyze_gapsMdDebounced ./internal/cli/...
```

Expected: FAIL — current code calls `WriteGaps` once per finding.

**Step 3: Write minimal implementation**

In `analyze.go`, around the drift block:

```go
prefix := reporter.BuildGapsStaticPrefix(featureMap, docCoveredFeatures)
gapsWriter := reporter.NewGapsWriter(projectDir, prefix, 500*time.Millisecond)
defer gapsWriter.Close()

driftOnFinding := func(accumulated []analyzer.DriftFinding) error {
    gapsWriter.Push(accumulated)
    return nil
}
```

Remove the trailing `if !driftSkipped { reporter.WriteGaps(...) }` only if the `Close()` flush guarantees the same final state — otherwise keep both, but ensure the final `WriteGaps` runs after `gapsWriter.Close()` so the bytes are deterministic.

**Step 4: Run tests**

```
go test -race ./...
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli/analyze.go internal/cli/analyze_gaps_writer_test.go
git commit -m "perf(analyze): route drift findings through GapsWriter

- RED: assert <= 1 gaps.md rewrite per debounce window
- GREEN: build static prefix once, push findings to debounced writer
- Status: -race clean"
```

---

## Task 6: Mutex-guard the drift cache persister

**Files:**
- Modify: `internal/cli/drift_cache.go` — add `sync.Mutex` around the read-modify-write inside `newDriftCachePersister` and any helper that mutates `liveCache`.
- Modify: `internal/cli/drift_cache_test.go` — add a concurrent-callers test.

**Why now:** Task 7 turns drift detection into N parallel callers of this persister. Adding the lock under a failing race-detector test is the textbook RED → GREEN.

**Step 1: Write the failing test**

```go
func TestDriftCachePersister_concurrentCallersDoNotLoseUpdates(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "drift.json")
    live := map[string]analyzer.CachedDriftEntry{}
    cached := map[string]analyzer.CachedDriftEntry{}
    var hits, fresh int
    persist := newDriftCachePersister(cached, live, path, &hits, &fresh)

    var wg sync.WaitGroup
    for i := 0; i < 32; i++ {
        i := i
        wg.Add(1)
        go func() {
            defer wg.Done()
            name := fmt.Sprintf("f%02d", i)
            _ = persist(name, []string{"a.go"}, []string{"p"}, []string{"p"}, nil)
        }()
    }
    wg.Wait()

    file, ok := loadDriftCacheFile(path)
    require.True(t, ok)
    assert.Len(t, file.Entries, 32)
}
```

**Step 2: Run test to verify it fails**

```
go test -race -run TestDriftCachePersister_concurrent ./internal/cli/...
```

Expected: FAIL — race detector flags concurrent writes to `live`, or final `Entries` length < 32.

**Step 3: Write minimal implementation**

Wrap the persister's body in a `sync.Mutex` captured in the closure:

```go
func newDriftCachePersister(cached, live map[string]analyzer.CachedDriftEntry, path string, hits, fresh *int) func(...) error {
    var mu sync.Mutex
    return func(name string, files, filteredPages, pages []string, issues []analyzer.DriftIssue) error {
        mu.Lock()
        defer mu.Unlock()
        // existing body, unchanged
    }
}
```

**Step 4: Run tests**

```
go test -race ./internal/cli/...
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli/drift_cache.go internal/cli/drift_cache_test.go
git commit -m "fix(drift): serialize cache persister against concurrent callers

- RED: 32 concurrent persister calls trip -race / drop entries
- GREEN: sync.Mutex around the read-modify-write
- Status: -race clean"
```

---

## Task 7: Parallelize drift detection

**Files:**
- Modify: `internal/analyzer/drift.go` — replace the serial feature loop in `DetectDrift` with a `parallel.Run`.
- Modify: `internal/analyzer/drift_test.go` — add a peak-in-flight test.

**Constraints:**
- Worker count: take a new `workers int` parameter on `DetectDrift`. Caller passes the same `--workers` value used elsewhere.
- `findings []DriftFinding` and `onFinding` callback need a mutex; the existing `onFinding` semantics ("called with the accumulated slice") are preserved.
- `onFeatureDone` is already mutex-guarded after Task 6.

**Step 1: Write the failing test**

Add to `drift_test.go`:

```go
func TestDetectDrift_runsFeaturesConcurrently(t *testing.T) {
    // Stub investigator that records peak-in-flight using atomic counters.
    // Build a featureMap with 8 features, all with both files and pages.
    // Call DetectDrift with workers=4.
    // Assert peak-in-flight >= 2.
}
```

**Step 2: Run test to verify it fails**

```
go test -race -run TestDetectDrift_runsFeaturesConcurrently ./internal/analyzer/...
```

Expected: FAIL — peak-in-flight is 1.

**Step 3: Write minimal implementation**

Convert the loop:

```go
type driftJob struct{ entry FeatureMapEntry }
jobs := make([]driftJob, 0, len(featureMap))
for _, e := range featureMap {
    jobs = append(jobs, driftJob{entry: e})
}

var (
    findingsMu sync.Mutex
    findings   []DriftFinding
)
err := parallel.Run(ctx, jobs, workers, func(ctx context.Context, job driftJob) error {
    // existing per-feature body, but mutex-guarded around findings appends
    // and onFinding calls. onFeatureDone is already safe after Task 6.
})
```

`onFinding` must observe a stable accumulated slice — pass a copy under the lock:

```go
findingsMu.Lock()
findings = append(findings, DriftFinding{...})
snapshot := append([]DriftFinding(nil), findings...)
findingsMu.Unlock()
if onFinding != nil { onFinding(snapshot) }
```

**Step 4: Run tests**

```
go test -race ./...
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/analyzer/drift.go internal/analyzer/drift_test.go internal/cli/analyze.go
git commit -m "perf(drift): run features in parallel under bounded worker pool

- RED: peak-in-flight test for 8 features with workers=4
- GREEN: parallel.Run + findings mutex; onFinding receives stable snapshots
- Status: -race clean, full test suite green"
```

---

## Task 8: Add `screenshots-cache.json` cache types + helpers

**Files:**
- Create: `internal/cli/screenshots_cache.go` (mirror `drift_cache.go` structure)
- Create: `internal/cli/screenshots_cache_test.go`

**Cache shape:**

```go
type screenshotsCacheFile struct {
    Pages    []string                  `json:"pages"`
    Entries  []screenshotsCacheEntry   `json:"entries"`
    Complete *screenshotsComplete      `json:"complete,omitempty"`
}

type screenshotsCacheEntry struct {
    URL         string                       `json:"url"`
    ContentHash string                       `json:"contentHash"`
    Stats       analyzer.ScreenshotPageStats `json:"stats"`
    Missing     []analyzer.ScreenshotGap     `json:"missing"`
    Possibly    []analyzer.ScreenshotGap     `json:"possiblyCovered"`
    ImageIssues []analyzer.ImageIssue        `json:"imageIssues"`
}

type screenshotsComplete struct {
    Hash        string    `json:"hash"`
    CompletedAt time.Time `json:"completedAt"`
}
```

**Step 1: Write the failing test**

Tests should cover:
- Save, load, round-trip equality.
- `loadScreenshotsCache` returning `ok=false` on missing file or bad JSON.
- Concurrent persister callers (32 goroutines, last-write-wins; under -race, no data loss).
- Completion sentinel save + read-back.

**Step 2: Run test to verify it fails**

```
go test -race -run TestScreenshotsCache ./internal/cli/...
```

Expected: FAIL — types do not exist.

**Step 3: Write minimal implementation**

Implement the file shape, `loadScreenshotsCache`, `saveScreenshotsCache`, `newScreenshotsCachePersister(live map[string]screenshotsCacheEntry, path string) func(entry) error`, and `saveScreenshotsCacheComplete`. Mutex inside the persister mirrors `drift_cache.go` after Task 6.

**Step 4: Run tests**

```
go test -race ./internal/cli/...
```

**Step 5: Commit**

```bash
git add internal/cli/screenshots_cache.go internal/cli/screenshots_cache_test.go
git commit -m "feat(screenshots): add per-page cache + completion sentinel

- RED: round-trip + concurrent-persister tests
- GREEN: cache file shape, persister with mutex, completion writer
- Status: -race clean"
```

---

## Task 9: Wire screenshot cache into `DetectScreenshotGaps` (still serial)

**Files:**
- Modify: `internal/analyzer/screenshot_gaps.go` — accept an `onPageDone` callback and a `cached` lookup map.
- Modify: `internal/cli/analyze.go` — load the cache before dispatch; check the completion sentinel; wire the persister into the call.

**Step 1: Write the failing test**

In `screenshot_gaps_integration_test.go`, add a test that runs `DetectScreenshotGaps` against 5 pages with a fake LLM that records call counts. Pre-populate `cached` with entries for 3 of the pages; assert only 2 fresh LLM calls happen.

**Step 2: Run test to verify it fails**

Expected: FAIL — `cached` parameter does not exist on `DetectScreenshotGaps`.

**Step 3: Write minimal implementation**

Extend the signature:

```go
func DetectScreenshotGaps(
    ctx context.Context,
    client LLMClient,
    pages []DocPage,
    cached map[string]ScreenshotsCachedPage, // keyed by URL+ContentHash
    onPageDone func(url string, entry ScreenshotsCachedPage) error,
    progress ScreenshotProgressFunc,
) (ScreenshotResult, error)
```

For each page, hash the content; look up `cached`; if hit, append cached results to `result` and invoke `progress`; if miss, run the existing pipeline and call `onPageDone` with the freshly computed entry.

In `analyze.go`, mirror the drift wiring: load the cache, check completion sentinel against an input hash, skip the pass if complete, otherwise pass `cached` and a persister into `DetectScreenshotGaps`. Stamp the completion sentinel after success.

**Step 4: Run tests**

```
go test -race ./...
```

**Step 5: Commit**

```bash
git add internal/analyzer/screenshot_gaps.go internal/cli/analyze.go internal/analyzer/screenshot_gaps_integration_test.go
git commit -m "feat(screenshots): per-page cache + completion sentinel in analyze pipeline

- RED: cache-hit test asserts 2 fresh calls out of 5 pages
- GREEN: DetectScreenshotGaps accepts cached + onPageDone; analyze wires persistence
- Status: -race clean"
```

---

## Task 10: Parallelize screenshot detection

**Files:**
- Modify: `internal/analyzer/screenshot_gaps.go` — wrap the page loop in `parallel.Run`; mutex-guard the result accumulator slices.
- Modify: `internal/analyzer/screenshot_gaps_test.go` — add the peak-in-flight test.

**Step 1: Write the failing test**

Same pattern as Task 7's drift test: 8 pages, workers=4, assert peak-in-flight >= 2.

**Step 2: Run test to verify it fails**

Expected: FAIL — peak-in-flight is 1.

**Step 3: Write minimal implementation**

```go
var resultMu sync.Mutex
err := parallel.Run(ctx, pages, workers, func(ctx context.Context, page DocPage) error {
    // existing per-page body, but appends to result.* go through resultMu.
    // onPageDone is already mutex-guarded after Task 8.
})
```

`progress` callback must be invoked under the lock or via an atomic counter so the (done, total) values are monotonic.

**Step 4: Run tests**

```
go test -race ./...
```

**Step 5: Commit**

```bash
git add internal/analyzer/screenshot_gaps.go internal/analyzer/screenshot_gaps_test.go
git commit -m "perf(screenshots): run pages in parallel under bounded worker pool

- RED: peak-in-flight test for 8 pages with workers=4
- GREEN: parallel.Run + result mutex; progress counter atomic
- Status: -race clean"
```

---

## Task 11: Single-writer goroutine for `screenshots.md`

**Files:**
- Create: `internal/reporter/screenshots_writer.go`
- Create: `internal/reporter/screenshots_writer_test.go`
- Modify: `internal/cli/analyze.go` — replace end-of-run `WriteScreenshots` with `Push` calls during the pass + `Close()` at the end.

**Pattern:** Identical shape to `GapsWriter` (Task 4). Owns `screenshots.md`. `Push(result analyzer.ScreenshotResult)` replaces the current view; debounce; atomic replace.

**Step 1: Write the failing test**

Mirror Task 4's tests for the new writer type.

**Step 2: Run test to verify it fails**

Expected: FAIL — type does not exist.

**Step 3: Write minimal implementation**

Copy `gaps_writer.go` shape with `analyzer.ScreenshotResult` as the payload and `WriteScreenshots`'s body inlined into the `flush` step.

**Step 4: Wire in `analyze.go`**

Replace the end-of-run single `reporter.WriteScreenshots(...)` call. Per-page workers Push the running result snapshot (taken under the result mutex from Task 10) into the writer; final `Close()` flushes.

**Step 5: Tests + commit**

```
go test -race ./...
```

```bash
git add internal/reporter/screenshots_writer.go internal/reporter/screenshots_writer_test.go internal/cli/analyze.go
git commit -m "feat(reporter): debounced single-writer ScreenshotsWriter

- RED: tests for burst coalescing, final flush, atomic replace, concurrent push
- GREEN: writer goroutine + debounce; analyze pushes mid-run
- Status: -race clean"
```

---

## Task 12: End-to-end verification + docs

**Files:**
- Modify: `.plans/VERIFICATION_PLAN.md` — add a "Scenario 15: Parallel execution under load" describing how to manually exercise the new flag.
- Run: full verification scenarios 1, 2, 3, 9 (the LLM-call-heavy ones) end-to-end against a real fixture.

**Step 1: Add a verification scenario**

Append to `.plans/VERIFICATION_PLAN.md`:

```
### Scenario 15: Parallel Execution

**Steps**:
1. Run `find-the-gaps analyze --repo <fixture> --docs-url <url> --workers=8 -v`.
2. Compare the wall-clock time against `--workers=1` on the same fixture (cold cache for both).
3. Assert reports/gaps.md, screenshots.md, mapping.md, drift.json, screenshots.json, and screenshots-cache.json are byte-identical (after sort) to the serial run.
4. SIGINT the parallel run mid-screenshot-pass. Re-run. Assert a partial screenshots-cache.json is loaded and only un-cached pages run again.

**Success Criteria**:
- [ ] Parallel run is meaningfully faster on the chosen fixture.
- [ ] Output reports do not differ in content (only finding order may differ within a priority bucket; assert via sorted comparison).
- [ ] Crash-recover: SIGINT then re-run results in <= original page count of fresh LLM calls.
```

**Step 2: Run end-to-end against a fixture**

Per `.plans/VERIFICATION_PLAN.md` Scenarios 1, 2, 3, 9. Capture timings.

**Step 3: Update PROGRESS.md**

Per CLAUDE.md rule #8.

**Step 4: Final commit**

```bash
git add .plans/VERIFICATION_PLAN.md PROGRESS.md
git commit -m "docs: verification scenario for parallel analyze execution

- Scenario 15 covers parallel speedup and crash-recover via screenshots-cache.json"
```

---

## Notes for the executing engineer

- **`go test -race ./...` is the gate** for every commit in this plan. If race detector fires, do not proceed — fix it first.
- **Coverage:** Per CLAUDE.md, ≥90% statement coverage per package. Run `go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out` and inspect that the new files (`internal/parallel/...`, the writer goroutines, the screenshots cache) clear the bar.
- **Linter:** `golangci-lint run` must be clean before each commit.
- **PROGRESS.md:** Update after each task with timestamp, tests-passing count, coverage delta.
- **Order matters.** Tasks 3, 4, 5 must land before Task 7 (drift parallelization writes through `GapsWriter`). Tasks 8, 9 must land before Task 10 (parallel screenshots writes through `screenshotsCachePersister`). Task 11 can land any time after Task 10.
- **Don't add `ftgignore`-style escape hatches or feature flags.** Per CLAUDE.md "no backwards-compatibility hacks" — flip behavior in place.
