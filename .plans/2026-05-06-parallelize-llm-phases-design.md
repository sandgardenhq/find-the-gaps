# Parallelize LLM-Heavy Phases — Design

## Problem

`find-the-gaps analyze` takes far longer than it should because three LLM-heavy phases run their iterations sequentially, even when those iterations are independent. The pipeline already parallelizes code-mapping and docs-mapping against each other (`runBothMaps`), but inside each of the remaining phases, every page or feature waits its turn.

## Phases parallelized

| Phase | Location | Per-iteration cost | Today |
|---|---|---|---|
| Page analysis | `internal/cli/analyze.go:187` | 1 small-tier call per page | serial loop |
| Drift detection | `internal/analyzer/drift.go:127` | classify + tool-using investigate (long) + judge | serial per feature |
| Screenshot detection | `internal/analyzer/screenshot_gaps.go:1160` | relevance pass (vision, batched) + detection | serial per page |

Already parallel and untouched by this design: `MapFeaturesToCode ‖ MapFeaturesToDocs`, mdfetch crawl.

## Concurrency primitive

A single shared helper used three times:

```go
func runParallel[T any](ctx context.Context, items []T, workers int, fn func(context.Context, T) error) error
```

Built on `golang.org/x/sync/errgroup` + a bounded semaphore. First error cancels remaining work via the inherited context.

**Worker count:** the existing `--workers` flag (default 5) governs all three phases, alongside its existing role for mdfetch. One knob keeps the UX simple; users who hit a phase-specific problem can tune it from the same flag.

## Coordination of shared state

### `sync.Mutex` per cache JSON file

The following caches today read-modify-write a single JSON file. Each gets a `sync.Mutex` wrapping the read-modify-write so concurrent workers cannot lose updates:

- `drift.json` — `newDriftCachePersister` and the `liveCache` map it owns
- `screenshots.json` — **new** (see "Screenshot crash-safety" below)
- spider index — `idx.RecordAnalysis(...)` called from the parallel page-analysis loop
- featuremap caches — `saveFeatureMapCache`, `saveDocsFeatureMapCache` invoked from batch callbacks

Atomic temp-file + rename on every persist keeps partial writes off disk.

### `sync.Mutex` on shared in-memory accumulators

Where workers append to a result slice or map (e.g. `result.MissingGaps`, `result.ImageIssues`, the page-analysis `analyses` slice), each accumulator gets a `sync.Mutex`. Critical sections are slice-append only; LLM calls happen outside the lock.

### Single-writer goroutine for the live markdown reports

`gaps.md` and `screenshots.md` are rewritten as new findings stream in. Under parallel workers this would mean either lock contention on the file or last-write-wins overwrites. Each gets a dedicated single-writer goroutine fed by a buffered channel:

```
worker → channel → writer goroutine → atomic write → file
```

The writer **debounces** at ~500ms: a burst of N concurrent finishes coalesces into one write. The writer is the sole owner of the file's bytes — no other goroutine writes there.

`mapping.md` is written exactly once at end-of-run and needs no special coordination.

## gaps.md — eliminate per-finding rebuild

Today `WriteGaps` (reporter.go:77) is invoked from `driftOnFinding` after every drift feature with issues. Each call rebuilds the entire markdown by iterating the full `FeatureMap` to produce three sections:

1. **Undocumented Code** — derived from `mapping` + `allDocFeatures`. Static for the duration of the drift loop.
2. **Unmapped Features** — same inputs. Static.
3. **Stale Documentation** — grows monotonically; bucketed by priority (Large/Medium/Small).

The redesign:

- Sections 1 + 2 are computed **once**, before drift starts, into a static prefix string held in memory by the writer goroutine.
- The writer goroutine owns an in-memory `[]flatDrift` slice. Incoming findings are appended, the Stale section is rebucketed and reformatted, and the file is written as `prefix + stale`.
- Debounced at ~500ms. A no-op rerun (drift cache complete) writes once at the end.

Honest accounting: the on-disk write of a small markdown file is essentially free. The real wins are eliminating the redundant rebuild of sections 1 + 2 on every finding, preventing concurrent overwrites, and reducing rewrite frequency under parallel drift. We do not introduce byte-offset tracking — full-file replace stays the durability story.

## Screenshot crash-safety (new)

Today, screenshot detection persists nothing until end-of-run. A SIGINT mid-pass loses the in-memory findings *and* the LLM work spent generating them. Parallelizing the phase amplifies the loss (more pages partially complete at any moment).

This design brings screenshot detection up to drift's crash-safety bar:

- **`screenshots.json` cache** keyed by page URL + content hash. Stores the full per-page payload: missing gaps, image issues, possibly-covered, audit stats. Full payload (not just user-facing findings) so reruns are byte-reproducible from cache.
- **`onPageDone` callback** persists after every completed page through the same mutex-guarded persister pattern as drift.
- **Completion sentinel** `{Hash, CompletedAt}` mirroring `drift.json`'s; a no-op rerun skips the entire pass.
- **Page-level cache lookup at run start**, before workers are dispatched, so cached pages skip the LLM call entirely.
- **Incremental `screenshots.md`** writes flow through the single-writer goroutine described above. Once per-page persistence exists, the markdown is just a render of the live cache.

## What stays the same

- `mapping.md` — single end-of-run write, no churn problem.
- mdfetch crawl — already parallel via `--workers`.
- Code-features extract / synthesize-product / both-maps — unchanged scope.
- LLM tiering, prompts, schemas — unchanged.

## Testing

Each phase gets a parallel-execution test that asserts:
- All N items complete successfully.
- The first error cancels remaining work.
- Concurrent persistence does not lose updates (race detector + an assertion that the post-run on-disk state contains every completed item).

Run `go test -race ./...` as part of the gate.

## Out of scope

- Cross-phase parallelism (e.g. starting drift before screenshots' page cache is fully warm). Each phase's preconditions today require the previous phase's full output; we keep that contract.
- Per-phase concurrency knobs. One `--workers` flag is the v1.
- Streaming gaps.md as append-only with byte offsets. Full-file atomic replace is the durability story.
