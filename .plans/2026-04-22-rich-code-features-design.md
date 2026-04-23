# Rich Code Feature Identification — Design

**Date:** 2026-04-22
**Branch:** to be created

## Problem

`ExtractFeaturesFromCode` currently returns `[]string` — short noun phrases only. The mapping report shows feature names with no context about what each feature does, what layer it belongs to, or whether it is user-facing. The report also does not show documentation status inline.

## Goal

Enrich each identified feature with a description, ownership layer, and user-facing flag at extraction time. Surface all of this, plus a computed documentation status, in `mapping.md`.

---

## Design

### 1. New `CodeFeature` type (`internal/analyzer`)

```go
type CodeFeature struct {
    Name        string `json:"name"`
    Description string `json:"description"`
    Layer       string `json:"layer"`
    UserFacing  bool   `json:"user_facing"`
}
```

Replaces `string` everywhere features flow through the system.

### 2. Updated `ExtractFeaturesFromCode` signature

```go
func ExtractFeaturesFromCode(ctx context.Context, client LLMClient, scan *scanner.ProjectScan) ([]CodeFeature, error)
```

#### Updated LLM prompt

```
You are analyzing a codebase to identify the product features it implements.

Code files and their exported symbols (format: "file/path: Symbol1, Symbol2"):
<symbols>

Return a JSON array of product features. Each element must have:
- "name": short noun phrase (max 8 words) naming the feature
- "description": 1-2 sentences describing what the feature does and its role in the product
- "layer": a short label for which part of the system owns this (e.g. "cli", "analysis engine",
  "caching", "reporting") — choose freely based on the code
- "user_facing": true if an end user interacts with this directly, false if it is internal plumbing

Deduplicate and sort by name alphabetically.
Respond with only the JSON array. No markdown code fences. No prose.
```

Response parsing changes from `[]string` to `[]CodeFeature`. Deduplication is keyed on `Name`.

### 3. `FeatureMapEntry` carries full `CodeFeature`

```go
type FeatureMapEntry struct {
    Feature  CodeFeature // was: string
    Files    []string
    Symbols  []string
}
```

`MapFeaturesToCode` accepts `[]CodeFeature` and extracts `.Name` internally for prompt construction, accumulator keys, and result assembly.

`MapFeaturesToDocs` and the docs side of the pipeline extract `.Name` from the slice — they do not need the rich struct.

`analyze.go` and `runBothMaps` carry `[]CodeFeature` throughout instead of `[]string`.

### 4. Cache update (`internal/cli/codefeatures_cache.go`)

```go
type codeFeaturesCacheFile struct {
    Files    []string      `json:"files"`
    Features []CodeFeature `json:"features"`
}
```

JSON round-trips cleanly. Cache invalidation logic (keyed on scanned file paths) is unchanged.

### 5. Enhanced `mapping.md`

Documentation status is computed in the reporter by checking whether any `PageAnalysis.Features` contains the feature name — a set lookup, no LLM call.

Status values: `documented` | `undocumented`.

Each feature section renders as:

```markdown
### CLI command routing

> Provides the top-level command structure and flag parsing for the CLI binary.

- **Layer:** cli
- **User-facing:** yes
- **Documentation status:** undocumented
- **Implemented in:** cmd/find-the-gaps/main.go, internal/cli/analyze.go
- **Symbols:** NewRootCmd, RunAnalyze
- **Documented on:** _(none)_
```

`WriteMapping` receives the enriched `FeatureMap` — `Description`, `Layer`, and `UserFacing` are already on each `FeatureMapEntry.Feature`, so no new parameters are needed.

---

## Files to Change

| File | Change |
|---|---|
| `internal/analyzer/code_features.go` | Return `[]CodeFeature`; update prompt and JSON parsing |
| `internal/analyzer/mapper.go` | `FeatureMapEntry.Feature` becomes `CodeFeature`; accept `[]CodeFeature` |
| `internal/cli/analyze.go` | Thread `[]CodeFeature` instead of `[]string` |
| `internal/cli/codefeatures_cache.go` | Cache `[]CodeFeature` instead of `[]string` |
| `internal/reporter/reporter.go` | Render description, layer, user-facing, doc status in `WriteMapping` |

## Files to Update (tests)

| File | Change |
|---|---|
| `internal/analyzer/code_features_test.go` | Expect `[]CodeFeature`; assert all fields |
| `internal/analyzer/mapper_test.go` | Pass `[]CodeFeature`; assert `FeatureMapEntry.Feature` fields |
| `internal/cli/analyze_test.go` | Update feature fixtures to `CodeFeature` |
| `internal/cli/codefeatures_cache_test.go` | Round-trip `[]CodeFeature` |
| `internal/reporter/reporter_test.go` | Assert new sections render in `mapping.md` |

---

## What Does Not Change

- `MapFeaturesToDocs` signature — it works with feature name strings extracted from `[]CodeFeature`
- Gap detection logic in `WriteGaps` — still driven by feature names
- Batching, token budget, and retry logic in the mapper
