# Drift-Detection Cache — Design

**Date:** 2026-04-29
**Status:** Draft (validated via brainstorming, not yet implemented)

## Goals

Make a previously-killed `analyze` run resumable across the drift-detection
stage. After a crash, `Ctrl-C`, or external kill, re-invoking `analyze`
reuses drift results for features that already completed and runs the LLM
investigator+judge only for features whose drift was never finished or whose
inputs changed.

## Non-Goals

- **Mid-feature resume.** A feature halfway through the investigator loop on
  the prior run restarts from scratch. Investigator runs are non-deterministic
  and partial observations aren't worth the persistence complexity.
- **Caching page classification.** The small-tier per-page classify call is
  not cached in v1. Revisit if profiles show it matters.
- **Content hashing.** Set-based invalidation matches the rest of the project.
  Users force a fresh run with the existing `--no-cache` flag.

## Cache Shape

One file per project at `<projectDir>/drift.json`, alongside the existing
`codefeatures.json`, `featuremap.json`, and `docsfeaturemap.json`.

```json
{
  "features": ["AuthLogin", "Frobnicate", "PasswordReset"],
  "entries": [
    {
      "feature": "AuthLogin",
      "files":   ["internal/auth/login.go", "internal/auth/session.go"],
      "pages":   ["https://docs.example.com/auth"],
      "issues": [
        {
          "page":  "https://docs.example.com/auth",
          "issue": "Docs say login takes (email, password); code now requires (email, password, otp)."
        }
      ]
    },
    {
      "feature": "PasswordReset",
      "files":   ["internal/auth/reset.go"],
      "pages":   ["https://docs.example.com/reset"],
      "issues": []
    }
  ]
}
```

Field rules:

- `features` is the sorted list of feature names actually fed to the
  investigator+judge. Informational; lookup is per-feature.
- Per entry, `files` and `pages` are sorted ascending. Together with the
  feature name they form the cache key: a hit requires the current run's
  `entry.Files` and post-classify `pages` to equal the cached values as
  sorted sets.
- `issues` is `[]DriftIssue` (`{page, issue}`). An empty array is a valid,
  useful cached value meaning "investigated, no real drift" — persist it
  the same as a non-empty result.
- Features skipped before the investigator (no files, no pages, all pages
  filtered as release-notes) are NOT written. Their skip path is cheap.

Writes use temp-file + atomic rename, mirroring the other cache helpers, so
a kill mid-write cannot corrupt the file.

## API Changes — `internal/analyzer`

New types:

```go
// CachedDriftEntry is one feature's persisted drift result, used to
// short-circuit investigator+judge when inputs are unchanged. Files and
// Pages must be sorted ascending.
type CachedDriftEntry struct {
    Files  []string
    Pages  []string
    Issues []DriftIssue
}

// DriftFeatureDoneFunc fires after each feature's drift result is decided —
// from a cache hit OR a fresh investigate+judge. Implementations typically
// persist the result so a future run can resume. Files and Pages are sorted
// ascending. Return non-nil to abort detection.
type DriftFeatureDoneFunc func(feature string, files, pages []string, issues []DriftIssue) error
```

New `DetectDrift` signature:

```go
func DetectDrift(
    ctx context.Context,
    tiering LLMTiering,
    featureMap FeatureMap,
    docsMap DocsFeatureMap,
    pageReader func(url string) (string, error),
    repoRoot string,
    cached map[string]CachedDriftEntry,   // NEW; nil means "no cache"
    onFinding DriftProgressFunc,
    onFeatureDone DriftFeatureDoneFunc,    // NEW; nil means "do not notify"
) ([]DriftFinding, error)
```

Per-feature flow inside `DetectDrift`:

1. Resolve `pages` for the feature (existing logic: `docPages` lookup →
   `filterDriftPages` → `classifyDriftPages`). If empty, skip — no cache write.
2. **Cache lookup.** If `cached != nil`, look up `cached[name]`. Hit iff the
   sorted file list and sorted page list both equal the cached values. On
   hit: skip investigator+judge, take cached `Issues`, log `cache hit`.
3. **Fresh path.** Run investigator → judge as today.
4. **Append findings.** If `len(issues) > 0`, append a `DriftFinding` and
   call `onFinding(findings)` exactly as today.
5. **Notify completion.** Call `onFeatureDone(name, sortedFiles, sortedPages,
   issues)` regardless of issue count. This is what lets the CLI persist
   clean-feature cache entries.

Existing call sites (production CLI + tests) pass `nil` for the new params
when caching isn't relevant. Current tests stay green without rewrites.

## CLI Integration

New file `internal/cli/drift_cache.go` mirroring `featuremap_cache.go`:

```go
type driftCacheFile struct {
    Features []string          `json:"features"`
    Entries  []driftCacheEntry `json:"entries"`
}

type driftCacheEntry struct {
    Feature string                `json:"feature"`
    Files   []string              `json:"files"`
    Pages   []string              `json:"pages"`
    Issues  []analyzer.DriftIssue `json:"issues"`
}

func loadDriftCache(path string) (map[string]analyzer.CachedDriftEntry, bool)
func saveDriftCache(path string, current map[string]analyzer.CachedDriftEntry) error
```

`loadDriftCache` returns `(nil, false)` on missing file, parse error, or any
I/O error — same fail-soft pattern as `loadFeatureMapCache`. `saveDriftCache`
writes via temp-file + atomic rename and sorts entries by feature name for
stable diffs.

Integration in `internal/cli/analyze.go` (around the existing `DetectDrift`
call site):

```go
driftCachePath := filepath.Join(projectDir, "drift.json")

var cached map[string]analyzer.CachedDriftEntry
if !noCache {
    if loaded, ok := loadDriftCache(driftCachePath); ok {
        cached = loaded
        log.Infof("using cached drift results (%d features)", len(cached))
    }
}

liveCache := make(map[string]analyzer.CachedDriftEntry, len(featureMap))
hits, fresh := 0, 0

onFeatureDone := func(name string, files, pages []string, issues []analyzer.DriftIssue) error {
    if c, ok := cached[name]; ok && equalSorted(c.Files, files) && equalSorted(c.Pages, pages) {
        hits++
    } else {
        fresh++
    }
    liveCache[name] = analyzer.CachedDriftEntry{Files: files, Pages: pages, Issues: issues}
    return saveDriftCache(driftCachePath, liveCache)
}

driftFindings, err := analyzer.DetectDrift(
    ctx, tiering, featureMap, docsFeatureMap,
    pageReader, repoPath,
    cached, driftOnFinding, onFeatureDone,
)
// ...
log.Infof("drift cache: %d hits, %d fresh", hits, fresh)
```

`--no-cache` semantics: bypass the load (cached stays nil → every feature
runs fresh). The save path still runs, so a `--no-cache` run rebuilds the
cache for next time. Identical to how `--no-cache` works for every other
stage.

## Edge Cases

| Case | Behavior |
| --- | --- |
| Corrupt / unreadable cache file | `loadDriftCache` returns `(nil, false)`; run proceeds cold. Logged at debug. |
| Feature removed upstream | Not in `featureMap` → never copied into `liveCache` → pruned on next save. |
| Feature's files or pages changed | Lookup misses; investigator+judge run fresh; entry overwritten. |
| Crash during `saveDriftCache` | Temp-rename leaves either the prior file intact or the new file intact. |
| Run with `--no-cache` | Read skipped, writes still happen — next run benefits. |
| Two `analyze` runs against same `<projectDir>` | Out of scope (same risk as every existing cache file). |

## Accepted Failure Modes

- A feature interrupted mid-investigation re-runs from scratch on resume.
  Cost: one feature's LLM calls. Acceptable.
- Page classification (small-tier) cost is not cached on resume. Documented;
  revisit if profiles complain.

## Out of Scope

- Cross-machine cache sharing.
- Cache TTLs / expiry.
- Caching the screenshots stage. (That stage already short-circuits via
  `--skip-screenshot-check`; if we want resume there too, it's a separate
  design.)
- A `find-the-gaps cache clear` subcommand. Users delete the file or pass
  `--no-cache`.

## Test Plan

Unit tests:

- `loadDriftCache` / `saveDriftCache`: round-trip; missing file; corrupt
  JSON; sort stability across saves.
- `DetectDrift` with populated `cached`: hit, miss-by-files, miss-by-pages,
  missing key.
- `onFeatureDone` fires on both cache-hit and fresh paths, including for
  features with empty `issues`.
- Existing drift tests keep passing with `nil` for both new params.

Verification (added to `.plans/VERIFICATION_PLAN.md` when implementation
lands):

- Cold run, kill mid-drift, resume → completed features skipped, rest run.
- Edit a file inside one feature; re-run without `--no-cache` → cache hit
  (set-based invalidation does not detect content changes — by design).
- Edit a file, re-run with `--no-cache` → full fresh run; cache rebuilt.
- Add a new feature upstream, re-run → only the new feature is fresh; rest
  are cache hits.

## Implementation Order

1. Add `CachedDriftEntry` and `DriftFeatureDoneFunc` types in
   `internal/analyzer/drift.go`.
2. RED test: `DetectDrift` honors `cached` map (hit / miss / nil).
3. RED test: `DetectDrift` invokes `onFeatureDone` for both cache-hit and
   fresh features.
4. Update `DetectDrift` signature; thread through cache lookup and
   `onFeatureDone` calls. Update existing call sites and tests to pass
   `nil`.
5. RED tests for `loadDriftCache` / `saveDriftCache` in
   `internal/cli/drift_cache_test.go`.
6. Implement `internal/cli/drift_cache.go`.
7. Wire load/save into `internal/cli/analyze.go`; add `hits/fresh` summary
   log.
8. End-to-end manual verification per the scenarios above.
