# Rich Code Feature Identification Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Expand `ExtractFeaturesFromCode` to return rich feature objects (name + description + layer + user-facing flag) and render them in `mapping.md` with a computed documentation status.

**Architecture:** Add a `CodeFeature` struct to the analyzer types, thread it from extraction through the mapper and cache to the reporter. The LLM prompt is updated to return JSON objects instead of strings. The reporter computes doc status by checking whether any `PageAnalysis` references the feature name.

**Tech Stack:** Go, `encoding/json`, `testify/assert`, existing `analyzer.LLMClient` stub pattern.

**Worktree:** `.worktrees/feat/rich-code-features`
**All commands run from worktree root.**

---

### Task 1: Add `CodeFeature` type to `internal/analyzer/types.go`

**Files:**
- Modify: `internal/analyzer/types.go`
- Test: `internal/analyzer/types_test.go` (create)

**Step 1: Write the failing test**

Create `internal/analyzer/types_test.go`:

```go
package analyzer_test

import (
	"encoding/json"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodeFeature_JSONRoundtrip(t *testing.T) {
	f := analyzer.CodeFeature{
		Name:        "CLI command routing",
		Description: "Provides top-level command structure.",
		Layer:       "cli",
		UserFacing:  true,
	}
	data, err := json.Marshal(f)
	require.NoError(t, err)

	var got analyzer.CodeFeature
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, f, got)
}

func TestCodeFeature_UserFacingFalse_JSONRoundtrip(t *testing.T) {
	f := analyzer.CodeFeature{
		Name:        "token batching",
		Description: "Splits symbol indexes into token-budget-sized chunks.",
		Layer:       "analysis engine",
		UserFacing:  false,
	}
	data, err := json.Marshal(f)
	require.NoError(t, err)

	var got analyzer.CodeFeature
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, f, got)
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/analyzer/... -run TestCodeFeature -v
```

Expected: FAIL — `analyzer.CodeFeature` undefined.

**Step 3: Add `CodeFeature` to `types.go`**

Add above the existing `PageAnalysis` type:

```go
// CodeFeature is a product feature identified from the codebase.
type CodeFeature struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Layer       string `json:"layer"`
	UserFacing  bool   `json:"user_facing"`
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/analyzer/... -run TestCodeFeature -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/analyzer/types.go internal/analyzer/types_test.go
git commit -m "feat(analyzer): add CodeFeature type with JSON roundtrip"
```

---

### Task 2: Update `ExtractFeaturesFromCode` to return `[]CodeFeature`

**Files:**
- Modify: `internal/analyzer/code_features.go`
- Modify: `internal/analyzer/code_features_test.go`

**Step 1: Write the failing test**

In `code_features_test.go`, find the existing test that asserts `[]string` return and update it. Replace the mock response JSON and assertions. The test should assert all four fields on at least one returned feature.

Look at the existing test — it likely has a mock LLM that returns a JSON array of strings. Change the mock to return a JSON array of objects:

```go
// In the existing happy-path test, change the mock response from:
//   `["feature one", "feature two"]`
// to:
`[{"name":"feature one","description":"Does X.","layer":"cli","user_facing":true},{"name":"feature two","description":"Does Y.","layer":"analysis engine","user_facing":false}]`
```

Change the assertion from `assert.Equal(t, []string{"feature one", "feature two"}, got)` to:

```go
require.Len(t, got, 2)
assert.Equal(t, "feature one", got[0].Name)
assert.Equal(t, "Does X.", got[0].Description)
assert.Equal(t, "cli", got[0].Layer)
assert.True(t, got[0].UserFacing)
assert.Equal(t, "feature two", got[1].Name)
assert.False(t, got[1].UserFacing)
```

Also update any test that checks return type or passes the result forward.

**Step 2: Run test to verify it fails**

```bash
go test ./internal/analyzer/... -run TestExtractFeatures -v
```

Expected: FAIL — return type mismatch or JSON parse error.

**Step 3: Update `code_features.go`**

1. Change function signature:
```go
func ExtractFeaturesFromCode(ctx context.Context, client LLMClient, scan *scanner.ProjectScan) ([]CodeFeature, error) {
```

2. Update the LLM prompt (both the `preamblePrompt` for token counting and the real `prompt`) to request objects:

```go
// PROMPT: Identifies product features implemented by a portion of the codebase. Returns a JSON array of objects with name, description, layer, and user_facing fields.
prompt := fmt.Sprintf(`You are analyzing a codebase to identify the product features it implements.

Code files and their exported symbols (format: "file/path: Symbol1, Symbol2"):
%s

Return a JSON array of product features. Each element must have:
- "name": short noun phrase (max 8 words) naming the feature
- "description": 1-2 sentences describing what the feature does and its role in the product
- "layer": a short label for which part of the system owns this (e.g. "cli", "analysis engine", "caching", "reporting") — choose freely based on the code
- "user_facing": true if an end user interacts with this directly, false if it is internal plumbing

Deduplicate and sort by name alphabetically.
Respond with only the JSON array. No markdown code fences. No prose.`, strings.Join(batch, "\n"))
```

Update `preamblePrompt` similarly so `countTokens` sees a representative size.

3. Change the JSON unmarshaling:
```go
var features []CodeFeature
if err := json.Unmarshal([]byte(raw), &features); err != nil {
    return nil, fmt.Errorf("ExtractFeaturesFromCode: invalid JSON response: %w", err)
}
```

4. Change the dedup set from `map[string]struct{}` to `map[string]CodeFeature` keyed on `.Name`:
```go
featSet := make(map[string]CodeFeature)
// ...
for _, f := range features {
    if f.Name != "" {
        featSet[f.Name] = f
    }
}
result := make([]CodeFeature, 0, len(featSet))
for _, f := range featSet {
    result = append(result, f)
}
sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
```

5. Change the nil-guard from `if features == nil { features = []string{} }` to the same for `[]CodeFeature`.

6. Change the empty early return:
```go
return []CodeFeature{}, nil
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/analyzer/... -v
```

Expected: all pass (other packages will fail to compile — ignore for now, fix forward).

**Step 5: Commit**

```bash
git add internal/analyzer/code_features.go internal/analyzer/code_features_test.go
git commit -m "feat(analyzer): ExtractFeaturesFromCode returns []CodeFeature with descriptions"
```

---

### Task 3: Update `FeatureEntry` and mapper internals

**Files:**
- Modify: `internal/analyzer/types.go`
- Modify: `internal/analyzer/mapper.go`
- Modify: `internal/analyzer/mapper_test.go`

**Step 1: Write the failing tests**

In `mapper_test.go`, find the existing test for `MapFeaturesToCode`. Update it to:
- Pass `[]CodeFeature` instead of `[]string`
- Assert `FeatureEntry.Feature` is a `CodeFeature` with correct `.Name`

Example fixture update — wherever the test builds a feature list:
```go
// Before:
features := []string{"feature one", "feature two"}

// After:
features := []analyzer.CodeFeature{
    {Name: "feature one", Description: "Does X.", Layer: "cli", UserFacing: true},
    {Name: "feature two", Description: "Does Y.", Layer: "analysis engine", UserFacing: false},
}
```

Wherever the test asserts `entry.Feature == "feature one"`, change to:
```go
assert.Equal(t, "feature one", entry.Feature.Name)
assert.Equal(t, "cli", entry.Feature.Layer)
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/analyzer/... -run TestMapFeaturesToCode -v
```

Expected: FAIL — type mismatch.

**Step 3: Update `types.go` — change `FeatureEntry.Feature`**

```go
type FeatureEntry struct {
	Feature CodeFeature  // was: string
	Files   []string
	Symbols []string
}
```

**Step 4: Update `mapper.go`**

1. Change `MapFeaturesToCode` signature:
```go
func MapFeaturesToCode(ctx context.Context, client LLMClient, counter TokenCounter, features []CodeFeature, scan *scanner.ProjectScan, tokenBudget int, filesOnly bool, onBatch MapProgressFunc) (FeatureMap, error) {
```

2. Extract names for internal use at the top of the function:
```go
featureNames := make([]string, len(features))
for i, f := range features {
    featureNames[i] = f.Name
}
```

3. Replace all uses of `features []string` inside the function body with `featureNames` (for JSON marshaling, accumulator initialization, batch prompt construction).

4. Update `accToFeatureMap` to accept both the acc map and the original `[]CodeFeature`:
```go
func accToFeatureMap(acc map[string]*accEntry, features []CodeFeature) FeatureMap {
    out := make(FeatureMap, 0, len(features))
    for _, feat := range features {
        e, ok := acc[feat.Name]
        if !ok {
            out = append(out, FeatureEntry{Feature: feat, Files: []string{}, Symbols: []string{}})
            continue
        }
        // ... build files/symbols slices as before ...
        out = append(out, FeatureEntry{Feature: feat, Files: files, Symbols: symbols})
    }
    return out
}
```

5. Update the accumulator initialization to key on `feat.Name`:
```go
for _, feat := range features {
    acc[feat.Name] = &accEntry{ ... }
}
```

6. Update any other internal references from `features[i]` (string) to `featureNames[i]`.

**Step 5: Run tests to verify they pass**

```bash
go test ./internal/analyzer/... -v
```

Expected: analyzer package passes. Other packages still fail to compile — that's expected at this point.

**Step 6: Commit**

```bash
git add internal/analyzer/types.go internal/analyzer/mapper.go internal/analyzer/mapper_test.go
git commit -m "feat(analyzer): FeatureEntry carries CodeFeature; MapFeaturesToCode accepts []CodeFeature"
```

---

### Task 4: Update `codefeatures_cache.go`

**Files:**
- Modify: `internal/cli/codefeatures_cache.go`
- Modify: `internal/cli/codefeatures_cache_test.go`

**Step 1: Write the failing test**

In `codefeatures_cache_test.go`, find the round-trip test. Update it to use `[]analyzer.CodeFeature`:

```go
features := []analyzer.CodeFeature{
    {Name: "feature one", Description: "Does X.", Layer: "cli", UserFacing: true},
    {Name: "feature two", Description: "Does Y.", Layer: "analysis engine", UserFacing: false},
}
```

Assert the round-trip restores all fields:
```go
assert.Equal(t, "Does X.", got[0].Description)
assert.Equal(t, "cli", got[0].Layer)
assert.True(t, got[0].UserFacing)
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/cli/... -run TestCodeFeatures -v
```

Expected: FAIL — type mismatch.

**Step 3: Update `codefeatures_cache.go`**

1. Change the cache struct:
```go
type codeFeaturesCacheFile struct {
	Files    []string                `json:"files"`
	Features []analyzer.CodeFeature  `json:"features"`
}
```

2. Change `loadCodeFeaturesCache` signature and return type:
```go
func loadCodeFeaturesCache(path string, scan *scanner.ProjectScan) ([]analyzer.CodeFeature, bool) {
```

3. Change the nil-guard:
```go
if cache.Features == nil {
    return []analyzer.CodeFeature{}, true
}
```

4. Change `saveCodeFeaturesCache` parameter:
```go
func saveCodeFeaturesCache(path string, scan *scanner.ProjectScan, features []analyzer.CodeFeature) error {
```

Add the import for `analyzer` if not already present.

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/cli/... -run TestCodeFeatures -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli/codefeatures_cache.go internal/cli/codefeatures_cache_test.go
git commit -m "feat(cli): codefeatures cache stores []CodeFeature"
```

---

### Task 5: Update `featuremap_cache.go`

**Files:**
- Modify: `internal/cli/featuremap_cache.go`
- Modify: `internal/cli/featuremap_cache_test.go`

The feature map cache uses a `[]string` feature list as its cache key and stores `Feature string` in each entry. Both need to change.

**Step 1: Write the failing test**

In `featuremap_cache_test.go`, update the round-trip test to pass `[]analyzer.CodeFeature` and assert `FeatureEntry.Feature.Name`, `.Description`, `.Layer`, `.UserFacing` come back intact:

```go
features := []analyzer.CodeFeature{
    {Name: "feature one", Description: "Does X.", Layer: "cli", UserFacing: true},
}
fm := analyzer.FeatureMap{
    {Feature: features[0], Files: []string{"a.go"}, Symbols: []string{"Foo"}},
}
// save then load, assert fm == loaded
assert.Equal(t, "Does X.", loaded[0].Feature.Description)
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/cli/... -run TestFeatureMapCache -v
```

Expected: FAIL.

**Step 3: Update `featuremap_cache.go`**

1. Change cache file struct to store full `CodeFeature` objects as the key list:
```go
type featureMapCacheFile struct {
	Features []analyzer.CodeFeature `json:"features"`
	Entries  []featureMapCacheEntry `json:"entries"`
}
```

2. Change `featureMapCacheEntry.Feature` from `string` to `analyzer.CodeFeature`:
```go
type featureMapCacheEntry struct {
	Feature analyzer.CodeFeature `json:"feature"`
	Files   []string             `json:"files"`
	Symbols []string             `json:"symbols"`
}
```

3. Change `loadFeatureMapCache` to accept `[]analyzer.CodeFeature`:
```go
func loadFeatureMapCache(path string, wantFeatures []analyzer.CodeFeature) (analyzer.FeatureMap, bool) {
```

4. Update the cache key comparison to compare by name only (descriptions may evolve):
```go
wantNames := featureNames(wantFeatures)
cacheNames := featureNames(cacheFeatures(cache.Features))
if !featureSetsEqual(wantNames, cacheNames) {
    return nil, false
}
```

Add a helper:
```go
func featureNames(features []analyzer.CodeFeature) []string {
    names := make([]string, len(features))
    for i, f := range features {
        names[i] = f.Name
    }
    return names
}
```

5. Update `saveFeatureMapCache`:
```go
func saveFeatureMapCache(path string, features []analyzer.CodeFeature, fm analyzer.FeatureMap) error {
```

6. Update the entry construction in `loadFeatureMapCache` to use `CodeFeature`:
```go
fm = append(fm, analyzer.FeatureEntry{
    Feature: e.Feature,
    Files:   files,
    Symbols: symbols,
})
```

7. Update `saveFeatureMapCache` entry construction:
```go
entries[i] = featureMapCacheEntry{Feature: e.Feature, Files: files, Symbols: symbols}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/cli/... -run TestFeatureMapCache -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli/featuremap_cache.go internal/cli/featuremap_cache_test.go
git commit -m "feat(cli): featuremap cache carries CodeFeature metadata"
```

---

### Task 6: Update `analyze.go` and `docsfeaturemap_cache.go`

**Files:**
- Modify: `internal/cli/analyze.go`
- Modify: `internal/cli/docsfeaturemap_cache.go` (check if it uses `[]string` features)
- Modify: `internal/cli/analyze_test.go`
- Modify: `internal/cli/analyze_parallel_test.go`

**Step 1: Check `docsfeaturemap_cache.go` signature**

```bash
grep -n "func load\|func save\|features" internal/cli/docsfeaturemap_cache.go
```

If its functions accept `[]string` features, update them to accept `[]analyzer.CodeFeature` and extract names internally (same pattern as featuremap_cache: compare by name only). Update `docsfeaturemap_cache_test.go` accordingly.

**Step 2: Write the failing tests**

In `analyze_test.go` and `analyze_parallel_test.go`, find anywhere `[]string` feature fixtures are built and change them to `[]analyzer.CodeFeature`:

```go
// Before:
features := []string{"feature one"}

// After:
features := []analyzer.CodeFeature{
    {Name: "feature one", Description: "Does X.", Layer: "cli", UserFacing: true},
}
```

Update `runBothMaps` call sites in tests to pass `[]analyzer.CodeFeature`.

**Step 3: Run tests to verify they fail**

```bash
go test ./internal/cli/... -run TestRunBothMaps -v
go test ./internal/cli/... -run TestAnalyze -v
```

Expected: FAIL — type mismatch.

**Step 4: Update `analyze.go`**

1. Change `runBothMaps` signature:
```go
func runBothMaps(
    ctx context.Context,
    client analyzer.LLMClient,
    counter analyzer.TokenCounter,
    features []analyzer.CodeFeature,  // was: []string
    ...
```

2. Inside `runBothMaps`, the `MapFeaturesToDocs` call still needs `[]string`. Extract names:
```go
featureNames := make([]string, len(features))
for i, f := range features {
    featureNames[i] = f.Name
}
// pass featureNames to MapFeaturesToDocs
fm, err := analyzer.MapFeaturesToDocs(ctx, client, featureNames, pages, ...)
```

3. In the main `RunE` closure, `codeFeatures` is already typed as whatever `ExtractFeaturesFromCode` returns — after Task 2 that's `[]analyzer.CodeFeature`. The cache calls (`saveCodeFeaturesCache`, `loadCodeFeaturesCache`, `saveFeatureMapCache`, `loadFeatureMapCache`, `saveDocsFeatureMapCache`, `loadDocsFeatureMapCache`) should all compile cleanly after Tasks 4 and 5.

4. The `docCoveredFeatures` loop near the end builds a `[]string` from `docsFeatureMap` — that is unaffected.

**Step 5: Run all tests to verify they pass**

```bash
go test ./...
```

Expected: all packages pass. If anything in `cmd/find-the-gaps` fails, fix compilation there too (it likely just calls into `cli` so no logic changes needed).

**Step 6: Commit**

```bash
git add internal/cli/analyze.go internal/cli/analyze_test.go internal/cli/analyze_parallel_test.go internal/cli/docsfeaturemap_cache.go internal/cli/docsfeaturemap_cache_test.go
git commit -m "feat(cli): thread []CodeFeature through analyze pipeline"
```

---

### Task 7: Update `reporter.WriteMapping` to render rich feature output

**Files:**
- Modify: `internal/reporter/reporter.go`
- Modify: `internal/reporter/reporter_test.go`

**Step 1: Write the failing test**

In `reporter_test.go`, find the existing `WriteMapping` test. Update it to assert the new sections appear in `mapping.md`. Use a fixture `FeatureMap` with at least one entry that has a full `CodeFeature`:

```go
mapping := analyzer.FeatureMap{
    {
        Feature: analyzer.CodeFeature{
            Name:        "CLI command routing",
            Description: "Provides top-level command structure.",
            Layer:       "cli",
            UserFacing:  true,
        },
        Files:   []string{"cmd/find-the-gaps/main.go"},
        Symbols: []string{"NewRootCmd"},
    },
}
pages := []analyzer.PageAnalysis{
    {URL: "https://docs.example.com/cli", Features: []string{"CLI command routing"}},
}
```

Assert the output contains:
```go
assert.Contains(t, content, "> Provides top-level command structure.")
assert.Contains(t, content, "**Layer:** cli")
assert.Contains(t, content, "**User-facing:** yes")
assert.Contains(t, content, "**Documentation status:** documented")
assert.Contains(t, content, "https://docs.example.com/cli")
```

Add a second entry with no matching page and assert `**Documentation status:** undocumented`.

**Step 2: Run test to verify it fails**

```bash
go test ./internal/reporter/... -run TestWriteMapping -v
```

Expected: FAIL — output doesn't contain new sections.

**Step 3: Update `reporter.go`**

Replace the feature section rendering in `WriteMapping`. The function signature stays the same — `FeatureEntry.Feature` is now `CodeFeature` so the data is already there.

```go
for _, entry := range mapping {
    fmt.Fprintf(&sb, "### %s\n\n", entry.Feature.Name)
    fmt.Fprintf(&sb, "> %s\n\n", entry.Feature.Description)

    userFacingStr := "no"
    if entry.Feature.UserFacing {
        userFacingStr = "yes"
    }

    // Compute doc status.
    docStatus := "undocumented"
    var docPages []string
    for _, p := range pages {
        for _, f := range p.Features {
            if f == entry.Feature.Name {
                docStatus = "documented"
                docPages = append(docPages, p.URL)
                break
            }
        }
    }

    fmt.Fprintf(&sb, "- **Layer:** %s\n", entry.Feature.Layer)
    fmt.Fprintf(&sb, "- **User-facing:** %s\n", userFacingStr)
    fmt.Fprintf(&sb, "- **Documentation status:** %s\n", docStatus)
    if len(entry.Files) > 0 {
        fmt.Fprintf(&sb, "- **Implemented in:** %s\n", strings.Join(entry.Files, ", "))
    }
    if len(entry.Symbols) > 0 {
        fmt.Fprintf(&sb, "- **Symbols:** %s\n", strings.Join(entry.Symbols, ", "))
    }
    if len(docPages) > 0 {
        fmt.Fprintf(&sb, "- **Documented on:** %s\n", strings.Join(docPages, ", "))
    } else {
        fmt.Fprintf(&sb, "- **Documented on:** _(none)_\n")
    }
    sb.WriteString("\n")
}
```

Note: remove the old `slices.Contains` loop since doc status is now computed inline.

**Step 4: Run all tests**

```bash
go test ./...
```

Expected: all pass.

**Step 5: Check coverage**

```bash
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | grep -E "analyzer|reporter|cli"
```

All modified packages should be at or above 90% statement coverage. If any package dips below, add targeted tests for uncovered branches.

**Step 6: Run linter**

```bash
golangci-lint run
```

Fix any warnings before proceeding.

**Step 7: Commit**

```bash
git add internal/reporter/reporter.go internal/reporter/reporter_test.go
git commit -m "feat(reporter): render description, layer, user-facing, and doc status in mapping.md"
```

---

### Task 8: Final verification

**Step 1: Run full test suite**

```bash
go test ./...
```

Expected: all packages pass, zero failures.

**Step 2: Check coverage across all modified packages**

```bash
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out
```

Check these packages hit ≥90%:
- `internal/analyzer`
- `internal/reporter`
- `internal/cli`

**Step 3: Build the binary**

```bash
go build ./...
```

Expected: succeeds with no errors.

**Step 4: Lint**

```bash
golangci-lint run
```

Expected: clean.

**Step 5: Use finishing-a-development-branch skill to merge**

Run the `superpowers:finishing-a-development-branch` skill to complete the feature.
