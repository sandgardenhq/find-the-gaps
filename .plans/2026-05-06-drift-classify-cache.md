# Drift Classify Cache

Skip `classifyDriftPages` on per-feature cache hits.

## Problem

Today, `internal/analyzer/drift.go:DetectDrift` runs `classifyDriftPages` (a Small-tier LLM call per page, via `isReleaseNotePage`) on every feature *before* checking the per-feature cache. Even when the cache hits and the investigator+judge are skipped, every page in that feature still pays one Small-tier call to be re-classified as "release notes / not release notes". On a resumed run with a fully populated `drift.json`, the user sees:

```
DEBU   drift cache hit: <feature>
DEBU usage: prompt=420 completion=5 ...
DEBU usage: prompt=422 completion=4 ...
DEBU usage: prompt=404 completion=4 ...
```

Those usage lines belong to the *next* feature's `classifyDriftPages` calls. They are pure waste on a cached run.

This was explicitly punted in `.plans/2026-04-29-drift-cache.md:777` ("Caching `classifyDriftPages` results separately. The completion sentinel makes this unnecessary for the skip path.") and acknowledged in `.plans/2026-04-30-skip-drift-on-rerun-design.md:9`. The skip path covers the *all-hits, no-changes* case via the completion sentinel, but mixed/partial caches and resumed runs still pay the classifier on every feature.

## Goal

On a per-feature drift cache hit, make zero LLM calls. Preserve correctness on miss and on input changes.

## Non-goals

- Changing what the classifier does or how it prompts.
- Caching across features (the classifier is per-page; pages can recur across features, but a content-keyed page-level cache is a bigger change — not in scope here).
- Touching the completion-sentinel skip path. That's already correct; this fix only matters when the sentinel can't fire (mixed cache, partial run, recompute-on-priority-miss, etc.).

## Approach

The cache lookup happens at `drift.go:133–155`, gated on `(Files, Pages)` equality. Because `Pages` is the *post-classification* list, the lookup can only run after classification — that's the source of the wasted calls.

Move classification *behind* the cache check, and persist the *pre-classification* list as part of the cache key. The classifier output (post-classification list) does not need to be persisted: nothing downstream consumes it after the run ends; it only matters within the per-feature `investigator+judge` call.

### Cache shape change

Add `FilteredPages` to `CachedDriftEntry` and to `driftCacheEntry` on disk:

```go
type CachedDriftEntry struct {
    Files         []string
    FilteredPages []string   // NEW: post-filter, pre-classify (cache key)
    Pages         []string   // existing: post-classify (kept for forward compat / debugging; cache no longer keys on it)
    Issues        []DriftIssue
}
```

Sorted-ascending invariants and JSON shape: `FilteredPages` follows the same rules as `Files`/`Pages`. New field on disk is additive — old `drift.json` files load with `FilteredPages == nil`.

### Lookup change

In `DetectDrift` (drift.go:113–155), reorder:

```go
// before classify
sortedFiltered := sortedCopy(pages) // pages here is post-filterDriftPages

if cached != nil {
    if c, ok := cached[entry.Feature.Name]; ok &&
        equalStringSlice(c.Files, sortedFiles) &&
        equalStringSlice(c.FilteredPages, sortedFiltered) &&
        !cacheNeedsRecompute(c) {
        // hit: skip classify, investigator, judge
        ...
        continue
    }
}

// miss: classify, then investigate+judge
pages = classifyDriftPages(ctx, classifier, pages, pageReader)
if len(pages) == 0 {
    // persist a "no surviving pages" entry so the next run also skips classify
    onFeatureDone(name, sortedFiles, sortedFiltered, []string{}, nil)
    continue
}
sortedPages := sortedCopy(pages)
...
onFeatureDone(name, sortedFiles, sortedFiltered, sortedPages, issues)
```

`sortedFiles` is computed once before the cache check (it does not depend on classify).

### Persister + onFeatureDone signature

`DriftFeatureDoneFunc` (drift.go:45) currently takes `(feature, files, pages, issues)`. Two options:

- **B1 (preferred):** add a `filteredPages` parameter — `func(feature string, files, filteredPages, pages []string, issues []DriftIssue) error`. Callers in `internal/cli/drift_cache.go` populate both fields when writing.
- **B2:** keep the signature; have the persister recompute `filterDriftPages(pages)` itself when writing. Cheaper diff, but couples the persister to the same filter the analyzer already ran. Reject — the analyzer should hand the persister exactly what it observed.

Going with B1.

### Migration / backward compat

- **Old entries (`FilteredPages == nil`):** treat as a miss. The next run will classify, run investigator+judge, and overwrite the entry with `FilteredPages` populated. One-time miss storm on the first run after upgrade. Acceptable — same magnitude as `cacheNeedsRecompute` rolling out (`drift.go:435`).
- No version bump on the cache file. The field is additive, JSON unmarshal ignores unknown fields, and missing-field handling is explicit at lookup time.

### Stability tradeoff (call out, don't fix)

Today's cache key changes whenever the LLM classifier flips its yes/no on the same page. With this change, the cache key changes only when the *unclassified* (post-`filterDriftPages`) page list changes. That is:

- **More stable** when the classifier is noisy on the same content (a feature unfortunate enough to live near the classifier's decision boundary). Win.
- **Slightly less robust** when a page's content changes such that classification *would* flip but the URL didn't. Today's cache also has this hole — `cacheNeedsRecompute` doesn't fingerprint page content either — so this is not a regression, just an unaddressed limitation.

Plan-of-record: accept this. A page-content fingerprint is a separate plan.

## Empty-survivor caching

Today, when `classifyDriftPages` returns `[]` (every page classified as release notes), the loop `continue`s at drift.go:127 *before* `onFeatureDone` runs. Result: the feature has no cache entry, so the next run re-classifies the same pages and reaches the same empty result.

Fix: when classify reduces to empty, still call `onFeatureDone` with `filteredPages = sortedFiltered`, `pages = []`, `issues = nil`. A cache entry with empty `Pages` and non-empty `FilteredPages` then short-circuits future runs — at hit time, after the cache check passes, the existing `if len(c.Pages) == 0` path simply produces no findings (matching today's behavior).

## Implementation tasks (TDD)

Per CLAUDE.md: every task is RED → GREEN → REFACTOR. Each task ends with a commit. Run `go test ./...`, `go build ./...`, `golangci-lint run` before each commit.

### Task 1 — Add `FilteredPages` to `CachedDriftEntry`

- **RED:** in `internal/analyzer/drift_test.go`, add `TestCachedDriftEntry_FilteredPagesField` asserting the field exists and is sorted-stable through `sortedCopy`.
- **GREEN:** add the field to `CachedDriftEntry`. No behavior change yet.
- **Tests stay green:** all existing cache tests still pass because the field defaults to nil.

### Task 2 — Plumb `FilteredPages` through the on-disk cache

- **RED:** in `internal/cli/drift_cache_test.go`, add `TestDriftCacheRoundTripFilteredPages` writing a cache with `FilteredPages` populated, reading it back, asserting equality. Add `TestDriftCacheLoadOldFileWithoutFilteredPages` reading a fixture JSON without the field, asserting `FilteredPages == nil` and the entry otherwise round-trips.
- **GREEN:** add `FilteredPages` to `driftCacheEntry` (drift_cache.go:25) with `json:"filteredPages,omitempty"`. Update `driftCacheEntriesToMap`, `loadDriftCache`, and `saveDriftCacheComplete` to copy the field with the same nil-normalization treatment as `Files`/`Pages`.

### Task 3 — Extend `DriftFeatureDoneFunc` signature

- **RED:** in `internal/analyzer/drift_test.go`, add a test that captures `onFeatureDone` arguments and asserts `filteredPages` is the post-`filterDriftPages` list (not the post-classification list).
- **GREEN:** update `DriftFeatureDoneFunc` in `internal/analyzer/drift.go` to take `(feature, files, filteredPages, pages, issues)`. Update all call sites:
  - `internal/analyzer/drift.go:148–151` and the post-investigate path at `drift.go:174–177` — pass `sortedFiltered` and `sortedPages` separately.
  - `internal/cli/drift_cache.go:newDriftCachePersister` (drift_cache.go:77) — accept and persist the new parameter.
  - All test stubs that implement the func.
- This is a wide signature change. Land it as one commit.

### Task 4 — Move classify behind the cache check

- **RED:** add `TestDetectDrift_CacheHit_DoesNotCallClassifier` in `internal/analyzer/drift_test.go`. Set up a cached entry with `FilteredPages` matching the post-filter list, wire a `Small()` stub that fails the test on any call, run `DetectDrift`, expect zero classifier calls and the cached issues returned.
- **GREEN:** reorder drift.go:113–155 so `sortedFiles` and `sortedFiltered` are computed before the cache lookup, and `classifyDriftPages` runs only on miss. Cache key uses `FilteredPages`.
- Verify `TestDetectDrift_UsesSmallTypicalLarge` (drift_test.go:724) still passes — the dispatch shape is unchanged on a cold run.

### Task 5 — Persist empty-survivor entries

- **RED:** add `TestDetectDrift_EmptyAfterClassify_StillCachesFilteredPages`. Wire a classifier that returns "yes" for every page (drops everything), assert `onFeatureDone` is called with `filteredPages != nil`, `pages = []`, `issues = nil`. Then re-run with the prior result as the cache; assert zero classifier calls.
- **GREEN:** in drift.go, when `classifyDriftPages` returns empty, call `onFeatureDone` with the empty page list and nil issues before `continue`.

### Task 6 — Verify behavior on stale cache (no `FilteredPages`)

- **RED:** add `TestDetectDrift_CacheWithoutFilteredPages_RecomputesOnce`. Seed the cache with an entry that has populated `Files`/`Pages`/`Issues` but `FilteredPages == nil`. Assert it triggers a miss (classifier *is* called, investigator runs), then assert the persisted entry now has `FilteredPages` populated.
- **GREEN:** the cache-key check at drift.go:134 will already miss because `equalStringSlice(nil, sortedFiltered)` returns false when `sortedFiltered` is non-empty. No extra code needed; this test is a regression guard.

### Task 7 — Update plan docs and progress

- Update `.plans/2026-04-29-drift-cache.md` and `.plans/2026-04-30-skip-drift-on-rerun-plan.md` with a one-line note pointing at this plan as the resolution to the "classify still fires on cache hits" caveat.
- Add a `PROGRESS.md` entry per CLAUDE.md §8.

## Verification

This change is internal — no new CLI flags, no new outputs. Existing scenarios already cover correctness:

- **Scenario 1 (Happy Path)** must still produce zero findings on the known-good fixture.
- **Scenario 3 (Detect Stale Example)** must still detect drift on a modified signature.
- **Scenario 9 (E2E)** is the right place to *observe* the win: run analyze twice in a row against the same fixture+docs and confirm the second run's stdout shows zero "drift cache hit"-adjacent classifier usage lines (`prompt=~400, completion=4–5`).

Add to `.plans/VERIFICATION_PLAN.md` Scenario 9:
- [ ] On a second invocation against an unchanged fixture, the analyze command emits zero LLM usage lines between consecutive `drift cache hit:` log entries.

## Out of scope

- Page-content fingerprinting in `cacheNeedsRecompute`.
- Replacing the LLM classifier with a deterministic URL/heuristic-only check.
- Caching the classifier's per-page decision in a separate URL→bool store (would deduplicate across features but adds a second cache surface; the per-feature persistence above is enough to drop classifier calls to zero on cached runs).

## Risks

- **Signature change to `DriftFeatureDoneFunc`** ripples through every test stub. Mitigated by landing it in one commit (Task 3) so the build is never broken between commits.
- **One-time miss storm** on the first post-upgrade run when existing `drift.json` files lack `FilteredPages`. Acceptable; matches prior cache-format rollouts in this project.
- **Empty-survivor caching (Task 5)** changes how features-with-only-release-notes are recorded. Worth a careful read in code review — these features previously had no cache entry, so behavior in `seedDriftLiveCache` and `driftFindingsFromCache` should be unchanged (issues stays nil → no finding).
