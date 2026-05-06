# Skip Drift Detection on Re-run — Design

**Date:** 2026-04-30

## Problem

Today, every `find-the-gaps analyze` run executes the drift-detection pass even when nothing has changed since the last successful run. Two costs follow from this:

1. The drift loop iterates every documented feature and calls `classifyDriftPages` (a Small-tier LLM call) before consulting `drift.json`. Even on a full per-feature cache hit, those classify calls fire. *(Resolved for mixed/partial caches in `.plans/2026-05-06-drift-classify-cache.md` — the per-feature cache now also short-circuits the classifier. The completion sentinel still covers the all-hits-no-changes fast path.)*
2. `gaps.md` is rewritten on every run regardless of whether its contents changed. The user has manually verified `gaps.md` and doesn't want a no-op re-run to touch it.

The user's request: when the prior run produced a complete drift result and `gaps.md` exists on disk, skip the drift pass entirely and leave `gaps.md` alone.

## Goal

Add a "complete" sentinel to `drift.json` and a CLI-layer skip path in `analyze.go` so that re-running `analyze` with unchanged inputs does not invoke the drift analyzer or rewrite `gaps.md`. Any change to upstream inputs (featureMap, docsFeatureMap) — or any pass-through of `--no-cache` — must defeat the skip.

## Trigger

Skip drift only when **all** of the following hold:

1. `--no-cache` is not set.
2. The upstream `featuremap.json` AND `docsfeaturemap.json` were both cache hits this run. (If either was recomputed, drift inputs may have changed even if the hash happens to coincide — we do not trust it.)
3. `drift.json` loads cleanly and carries a `complete` sentinel whose hash matches a freshly computed hash of the current `featureMap` and `docsFeatureMap`.
4. `gaps.md` exists at `<projectDir>/gaps.md`.

If any check fails or errors, fall through to the normal drift path. Skipping is intentionally fail-open — we never skip on uncertainty.

---

## Design

### 1. Sentinel shape (`internal/cli/drift_cache.go`)

Extend `driftCacheFile`:

```go
type driftCacheFile struct {
    Features []string          `json:"features"`
    Entries  []driftCacheEntry `json:"entries"`
    Complete *driftComplete    `json:"complete,omitempty"`
}

type driftComplete struct {
    Hash        string    `json:"hash"`
    CompletedAt time.Time `json:"completedAt"`
}
```

`Complete` is `omitempty` so old `drift.json` files (no sentinel) load with `Complete == nil` and are treated as not-complete. No migration needed.

### 2. Hash function

```go
func computeDriftInputHash(fm analyzer.FeatureMap, dm analyzer.DocsFeatureMap) string
```

The hash covers exactly the facts the drift pass consumes from upstream:

- For each feature in `featureMap`, in feature-name order: `(name, sorted Files, sorted Symbols)`.
- For each feature in `docsMap`, in feature-name order: `(name, sorted Pages)`.

Encode as canonical JSON (deterministic key order, no whitespace), then SHA-256, then hex. The function is pure and independent of map iteration order.

We do not hash the `drift.json` entries themselves. The sentinel attests the *inputs* — outputs are derived.

### 3. New cache helpers

```go
// Returns the full file (entries + sentinel). Existing loadDriftCache stays.
func loadDriftCacheFile(path string) (driftCacheFile, bool)

// Rebuilds DriftFindings from the per-feature cache, used only when skipping.
func driftFindingsFromCache(c map[string]analyzer.CachedDriftEntry, fm analyzer.FeatureMap) []analyzer.DriftFinding
```

`saveDriftCache` grows an optional `complete *driftComplete` parameter (or a sibling `saveDriftCacheComplete`). The per-feature `newDriftCachePersister` continues to write entries without a sentinel during the loop. The sentinel is stamped exactly once, after `DetectDrift` returns without error, by a final write from `analyze.go`.

### 4. Skip block in `internal/cli/analyze.go`

```go
driftSkipped := false
var driftFindings []analyzer.DriftFinding

if !noCache && codeMapCached && docsMapCached {
    if file, ok := loadDriftCacheFile(driftCachePath); ok && file.Complete != nil {
        wantHash := computeDriftInputHash(featureMap, docsFeatureMap)
        if file.Complete.Hash == wantHash {
            gapsPath := filepath.Join(projectDir, "gaps.md")
            if _, err := os.Stat(gapsPath); err == nil {
                driftFindings = driftFindingsFromCache(toMap(file.Entries), featureMap)
                driftSkipped = true
                log.Infof("drift: cache complete, skipping (hash %s)", shortHash(wantHash))
            }
        }
    }
}

if !driftSkipped {
    // existing DetectDrift block, unchanged
    // on success, stamp the sentinel:
    _ = saveDriftCacheComplete(driftCachePath, liveCache, &driftComplete{
        Hash:        computeDriftInputHash(featureMap, docsFeatureMap),
        CompletedAt: time.Now(),
    })
}
```

Findings are rebuilt purely in-memory from `drift.json`. Downstream consumers (`reporter.WriteGaps`, `site.Build`) see a populated `[]DriftFinding` byte-identical to what a full run would have produced.

### 5. Downstream writers when skipped

```go
if !driftSkipped {
    if err := reporter.WriteGaps(projectDir, featureMap, docCoveredFeatures, driftFindings); err != nil {
        return fmt.Errorf("write gaps: %w", err)
    }
}
// reporter.WriteMapping unchanged — cheap, deterministic.
// reporter.WriteScreenshots gated only by --skip-screenshot-check.
// site.Build unchanged — runs unless --no-site.
```

Rationale:

- **`WriteGaps`** is skipped because the file would be byte-identical and the user wants `gaps.md` left alone on no-op re-runs. Trade-off: any manual edits to `gaps.md` survive until the next non-skipped run, identical to today's behavior.
- **`WriteMapping`** is small and deterministic; skipping it would need its own sentinel for marginal value.
- **`site.Build`** is a separate pipeline; user's request was about drift. Out of scope.

### 6. Stdout

The `reports:` block annotates the cached gaps line:

```
  <projectDir>/gaps.md (cached, drift unchanged)
```

Mirrors the existing `(skipped)` convention for `screenshots.md` and `site/`.

---

## Files to Change

| File | Change |
|---|---|
| `internal/cli/drift_cache.go` | Add `driftComplete`, `loadDriftCacheFile`, `driftFindingsFromCache`, `computeDriftInputHash`, `saveDriftCacheComplete` |
| `internal/cli/analyze.go` | Skip block + sentinel stamp after successful `DetectDrift`; conditional `WriteGaps`; updated stdout |

## Files to Create (tests)

| File | Change |
|---|---|
| `internal/cli/drift_cache_test.go` | Add tests for `computeDriftInputHash` (determinism, order-independence, sensitivity), `driftCacheFile` round-trip with sentinel, back-compat load of old shape |
| `internal/cli/analyze_test.go` | Integration: clean run → second run skips drift, third run after mutating featureMap input does NOT skip; `gaps.md` deletion between runs forces re-run |

## Test Plan (TDD order)

1. **RED**: `computeDriftInputHash` deterministic — same inputs produce the same hex hash. Different `Files` slices produce a different hash. Map insertion order does not affect the hash.
2. **RED**: `loadDriftCacheFile` reads back a `complete` sentinel written by `saveDriftCacheComplete`.
3. **RED**: Old `drift.json` (no `complete` field) loads with `Complete == nil`.
4. **RED**: `driftFindingsFromCache` rebuilds findings only for features still present in `featureMap` (dropped features stay dropped).
5. **RED**: Integration — second `analyze` run with unchanged inputs does NOT call the investigator (use a counting stub `ToolLLMClient`).
6. **RED**: Integration — second run after mutating a feature's `Files` DOES call the investigator (hash mismatch).
7. **RED**: Integration — `gaps.md` deleted between runs forces full drift pass.

Each RED → minimal GREEN → refactor; commit after each cycle.

## What Does Not Change

- `internal/analyzer/drift.go` — skip is a CLI-layer concern; the analyzer stays oblivious.
- `DetectDrift` signature and behavior.
- The per-feature `drift.json` entry shape (`Files`, `Pages`, `Issues`).
- `--no-cache` semantics.
- `reporter.WriteMapping`, `reporter.WriteScreenshots`, `site.Build`.

## Failure Modes (intentional)

- Sentinel present, hash mismatches → run drift normally.
- Sentinel absent → run drift normally.
- `gaps.md` missing → run drift normally and rewrite.
- `drift.json` parse error → run drift normally (existing `loadDriftCache` returns `(nil, false)` on any I/O / parse error).
- Either upstream map cache miss → run drift normally.
- `--no-cache` → run drift normally.
