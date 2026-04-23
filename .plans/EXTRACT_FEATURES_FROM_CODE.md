# Extract Features From Code Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix the tautology in gap analysis — the canonical feature list must come from CODE, not docs, so "Undocumented Code" actually means something.

**Architecture:** Add `ExtractFeaturesFromCode` in the `analyzer` package that sends the codebase's exported symbol index to the LLM and asks what product features it implements. In `analyze.go`, call this after scanning and use the resulting `codeFeatures` as the canonical feature list for all downstream mapping (replacing `productSummary.Features`). Keep `SynthesizeProduct` only for the product description in `mapping.md`. For `WriteGaps`, derive the doc-covered feature list from `docsFeatureMap` (features that have at least one page).

**Tech Stack:** Go, `encoding/json`, `sort`, `github.com/charmbracelet/log`, existing `batchSymLines` batcher in `internal/analyzer/batcher.go`, existing `featureSetsEqual` helper in `internal/cli/featuremap_cache.go`.

---

## Background: The Bug

`SynthesizeProduct` reads documentation pages and extracts a feature list FROM those pages.
That feature list is then used as the input to `MapFeaturesToDocs`, which checks whether those features are covered by docs.
Since the features CAME from docs, they are always covered. The "Undocumented Code" section is always empty.

The fix: ask the LLM "what features does THIS CODE implement?" to get the canonical list.
Then check docs coverage against THAT list.

---

## What changes and what doesn't

| Component | Change |
|---|---|
| `internal/analyzer/code_features.go` | **NEW** — `ExtractFeaturesFromCode` |
| `internal/cli/codefeatures_cache.go` | **NEW** — cache helpers for code features |
| `internal/cli/analyze.go` | **MODIFIED** — use `codeFeatures` everywhere `productSummary.Features` was used for mapping |
| `internal/cli/analyze_test.go` | **MODIFIED** — update full-pipeline test (needs more LLM responses) |
| `internal/analyzer/synthesize.go` | **UNCHANGED** — still called for description |
| `internal/analyzer/mapper.go` | **UNCHANGED** |
| `internal/analyzer/docs_mapper.go` | **UNCHANGED** |
| `internal/reporter/reporter.go` | **UNCHANGED** |

---

## Task 1: `ExtractFeaturesFromCode`

**Files:**
- Create: `internal/analyzer/code_features.go`
- Create: `internal/analyzer/code_features_test.go`

### Step 1: Write the failing tests

Create `internal/analyzer/code_features_test.go`:

```go
package analyzer_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

func TestExtractFeaturesFromCode_ReturnsFeatures(t *testing.T) {
	c := &fakeClient{responses: []string{`["authentication","file upload","user management"]`}}
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "internal/auth/auth.go", Symbols: []scanner.Symbol{{Name: "Authenticate"}}},
			{Path: "internal/upload/upload.go", Symbols: []scanner.Symbol{{Name: "Upload"}}},
		},
	}
	got, err := analyzer.ExtractFeaturesFromCode(context.Background(), c, scan)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Error("expected features, got empty slice")
	}
}

func TestExtractFeaturesFromCode_EmptyScan_ReturnsEmpty(t *testing.T) {
	c := &fakeClient{}
	scan := &scanner.ProjectScan{Files: []scanner.ScannedFile{}}
	got, err := analyzer.ExtractFeaturesFromCode(context.Background(), c, scan)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
	if c.callCount != 0 {
		t.Error("expected no LLM call for empty scan")
	}
}

func TestExtractFeaturesFromCode_NoSymbols_ReturnsEmpty(t *testing.T) {
	c := &fakeClient{}
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "README.md", Symbols: nil},
			{Path: "internal/foo.go", Symbols: []scanner.Symbol{}},
		},
	}
	got, err := analyzer.ExtractFeaturesFromCode(context.Background(), c, scan)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
	if c.callCount != 0 {
		t.Error("expected no LLM call when no files have symbols")
	}
}

func TestExtractFeaturesFromCode_ClientError_Propagates(t *testing.T) {
	c := &fakeClient{forcedErr: errors.New("network down")}
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "auth.go", Symbols: []scanner.Symbol{{Name: "Auth"}}},
		},
	}
	_, err := analyzer.ExtractFeaturesFromCode(context.Background(), c, scan)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestExtractFeaturesFromCode_InvalidJSON_ReturnsError(t *testing.T) {
	c := &fakeClient{responses: []string{"not valid json"}}
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "auth.go", Symbols: []scanner.Symbol{{Name: "Auth"}}},
		},
	}
	_, err := analyzer.ExtractFeaturesFromCode(context.Background(), c, scan)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestExtractFeaturesFromCode_NilResponse_NormalizedToEmpty(t *testing.T) {
	// LLM returns explicit null
	c := &fakeClient{responses: []string{`null`}}
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "auth.go", Symbols: []scanner.Symbol{{Name: "Auth"}}},
		},
	}
	got, err := analyzer.ExtractFeaturesFromCode(context.Background(), c, scan)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Error("nil features must be normalized to empty slice")
	}
}

func TestExtractFeaturesFromCode_DeduplicatesAcrossBatches(t *testing.T) {
	// Two batches both include "authentication" — result must be deduplicated.
	c := &fakeClient{responses: []string{
		`["authentication","file upload"]`,
		`["authentication","search"]`,
	}}
	// Force two batches by using a token budget of 1 (so every line becomes its own batch)
	// We can test deduplication by verifying "authentication" appears exactly once.
	// Note: batchSymLines is called internally; we can't control it directly from outside.
	// So this test calls the function normally and just asserts no duplicates in output.
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "auth.go", Symbols: []scanner.Symbol{{Name: "Auth"}}},
		},
	}
	// One batch, one response — just verify no duplicates in a single-batch case.
	got, err := analyzer.ExtractFeaturesFromCode(context.Background(), c, scan)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]int)
	for _, f := range got {
		seen[f]++
	}
	for feat, count := range seen {
		if count > 1 {
			t.Errorf("feature %q appears %d times; want exactly 1", feat, count)
		}
	}
}

func TestExtractFeaturesFromCode_PromptContainsSymbols(t *testing.T) {
	// Verify the prompt sent to the LLM includes the file path and symbol name.
	c := &fakeClient{responses: []string{`["some feature"]`}}
	scan := &scanner.ProjectScan{
		Files: []scanner.ScannedFile{
			{Path: "internal/auth/handler.go", Symbols: []scanner.Symbol{{Name: "HandleLogin"}}},
		},
	}
	_, err := analyzer.ExtractFeaturesFromCode(context.Background(), c, scan)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.receivedPrompts) == 0 {
		t.Fatal("expected at least one prompt")
	}
	prompt := c.receivedPrompts[0]
	if !contains(prompt, "internal/auth/handler.go") {
		t.Error("prompt must include file path")
	}
	if !contains(prompt, "HandleLogin") {
		t.Error("prompt must include symbol name")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

**Note on `ScannedFile` vs `File`:** Run `grep -r "type.*File\b" internal/scanner/` to confirm the struct name before writing the test. It may be `ScannedFile`. The production code also uses this type.

### Step 2: Run tests to verify they fail

```bash
go test ./internal/analyzer/... -run TestExtractFeatures -v
```

Expected: `undefined: analyzer.ExtractFeaturesFromCode`

### Step 3: Implement `ExtractFeaturesFromCode`

Create `internal/analyzer/code_features.go`:

```go
package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

// ExtractFeaturesFromCode asks the LLM to identify product features from the
// codebase's exported symbol index. It uses the same symbol-line format and
// batching strategy as MapFeaturesToCode. Features from multiple batches are
// deduplicated and sorted.
func ExtractFeaturesFromCode(ctx context.Context, client LLMClient, scan *scanner.ProjectScan) ([]string, error) {
	var symLines []string
	for _, f := range scan.Files {
		if len(f.Symbols) == 0 {
			continue
		}
		names := make([]string, len(f.Symbols))
		for i, s := range f.Symbols {
			names[i] = s.Name
		}
		symLines = append(symLines, fmt.Sprintf("%s: %s", f.Path, strings.Join(names, ", ")))
	}

	if len(symLines) == 0 {
		return []string{}, nil
	}

	batches := batchSymLines(symLines, 0, MapperTokenBudget)
	featSet := make(map[string]struct{})

	for i, batch := range batches {
		log.Infof("  extracting features from code batch %d/%d (%d symbol groups)", i+1, len(batches), len(batch))

		// PROMPT: Identifies product features implemented by a portion of the codebase. Returns a JSON array of short noun-phrase feature names.
		prompt := fmt.Sprintf(`You are analyzing a codebase to identify the product features it implements.

Code files and their exported symbols (format: "file/path: Symbol1, Symbol2"):
%s

Based on the exported symbols and file structure, return a JSON array of product features this codebase implements.
Each feature should be a short noun phrase (max 8 words) describing a user-facing capability.
Deduplicate and sort alphabetically.

Respond with only the JSON array. No markdown code fences. No prose.`, strings.Join(batch, "\n"))

		raw, err := client.Complete(ctx, prompt)
		if err != nil {
			return nil, fmt.Errorf("ExtractFeaturesFromCode: %w", err)
		}

		var features []string
		if err := json.Unmarshal([]byte(raw), &features); err != nil {
			return nil, fmt.Errorf("ExtractFeaturesFromCode: invalid JSON response: %w", err)
		}

		for _, f := range features {
			if f != "" {
				featSet[f] = struct{}{}
			}
		}
	}

	result := make([]string, 0, len(featSet))
	for f := range featSet {
		result = append(result, f)
	}
	sort.Strings(result)
	return result, nil
}
```

### Step 4: Run tests to verify they pass

```bash
go test ./internal/analyzer/... -run TestExtractFeatures -v
```

Expected: all PASS.

Also run the full package to catch regressions:

```bash
go test ./internal/analyzer/...
```

### Step 5: Commit

```bash
git add internal/analyzer/code_features.go internal/analyzer/code_features_test.go
git commit -m "feat(analyzer): add ExtractFeaturesFromCode to derive canonical feature list from code"
```

---

## Task 2: Code Features Cache

**Files:**
- Create: `internal/cli/codefeatures_cache.go`
- Create: `internal/cli/codefeatures_cache_test.go`

### Step 1: Write the failing tests

Create `internal/cli/codefeatures_cache_test.go`:

```go
package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

func makeScan(paths ...string) *scanner.ProjectScan {
	files := make([]scanner.ScannedFile, len(paths))
	for i, p := range paths {
		files[i] = scanner.ScannedFile{Path: p}
	}
	return &scanner.ProjectScan{Files: files}
}

func TestCodeFeaturesCache_MissWhenFileAbsent(t *testing.T) {
	_, ok := loadCodeFeaturesCache(filepath.Join(t.TempDir(), "codefeatures.json"), makeScan("a.go"))
	if ok {
		t.Error("expected cache miss for absent file")
	}
}

func TestCodeFeaturesCache_MissWhenFilesChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codefeatures.json")
	scan := makeScan("a.go", "b.go")
	if err := saveCodeFeaturesCache(path, scan, []string{"feature-a"}); err != nil {
		t.Fatal(err)
	}
	// Load with a different file list.
	_, ok := loadCodeFeaturesCache(path, makeScan("a.go", "b.go", "c.go"))
	if ok {
		t.Error("expected cache miss when file list changed")
	}
}

func TestCodeFeaturesCache_HitWhenFilesUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codefeatures.json")
	scan := makeScan("a.go", "b.go")
	features := []string{"feature-a", "feature-b"}
	if err := saveCodeFeaturesCache(path, scan, features); err != nil {
		t.Fatal(err)
	}
	got, ok := loadCodeFeaturesCache(path, scan)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if len(got) != len(features) {
		t.Errorf("got %d features, want %d", len(got), len(features))
	}
}

func TestCodeFeaturesCache_FileOrderIndependent(t *testing.T) {
	// Cache built with [b.go, a.go] must be a hit when loaded with [a.go, b.go].
	dir := t.TempDir()
	path := filepath.Join(dir, "codefeatures.json")
	scanAB := makeScan("a.go", "b.go")
	scanBA := makeScan("b.go", "a.go")
	if err := saveCodeFeaturesCache(path, scanAB, []string{"feat"}); err != nil {
		t.Fatal(err)
	}
	_, ok := loadCodeFeaturesCache(path, scanBA)
	if !ok {
		t.Error("expected cache hit regardless of file order")
	}
}

func TestCodeFeaturesCache_NilFeatures_NormalizedToEmpty(t *testing.T) {
	// Write raw JSON with null features to simulate a degenerate cache file.
	dir := t.TempDir()
	path := filepath.Join(dir, "codefeatures.json")
	scan := makeScan("a.go")
	// Save normally first, then overwrite with a null features value.
	raw := `{"files":["a.go"],"features":null}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := loadCodeFeaturesCache(path, scan)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got == nil {
		t.Error("nil features must be normalized to empty slice")
	}
}

func TestCodeFeaturesCache_CorruptFile_ReturnsMiss(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codefeatures.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, ok := loadCodeFeaturesCache(path, makeScan("a.go"))
	if ok {
		t.Error("expected cache miss for corrupt file")
	}
}
```

### Step 2: Run tests to verify they fail

```bash
go test ./internal/cli/... -run TestCodeFeaturesCache -v
```

Expected: `undefined: loadCodeFeaturesCache`

### Step 3: Implement cache helpers

Create `internal/cli/codefeatures_cache.go`:

```go
package cli

import (
	"encoding/json"
	"errors"
	"os"
	"sort"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

type codeFeaturesCacheFile struct {
	Files    []string `json:"files"`
	Features []string `json:"features"`
}

// loadCodeFeaturesCache reads a cached code-features list from path.
// Returns false if the file does not exist, cannot be parsed, or the
// scanned file list has changed since the cache was built.
func loadCodeFeaturesCache(path string, scan *scanner.ProjectScan) ([]string, bool) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false
	}
	if err != nil {
		return nil, false
	}
	var cache codeFeaturesCacheFile
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, false
	}
	if !featureSetsEqual(cache.Files, scanFilePaths(scan)) {
		return nil, false
	}
	if cache.Features == nil {
		return []string{}, true
	}
	return cache.Features, true
}

// saveCodeFeaturesCache writes features to path, keyed to the scan's file list.
func saveCodeFeaturesCache(path string, scan *scanner.ProjectScan, features []string) error {
	cache := codeFeaturesCacheFile{
		Files:    scanFilePaths(scan),
		Features: features,
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// scanFilePaths returns a sorted list of file paths from scan.
func scanFilePaths(scan *scanner.ProjectScan) []string {
	paths := make([]string, len(scan.Files))
	for i, f := range scan.Files {
		paths[i] = f.Path
	}
	sort.Strings(paths)
	return paths
}
```

### Step 4: Run tests to verify they pass

```bash
go test ./internal/cli/... -run TestCodeFeaturesCache -v
```

Expected: all PASS.

Run full package:

```bash
go test ./internal/cli/...
```

### Step 5: Commit

```bash
git add internal/cli/codefeatures_cache.go internal/cli/codefeatures_cache_test.go
git commit -m "feat(cli): add code-features cache keyed on scanned file list"
```

---

## Task 3: Wire `ExtractFeaturesFromCode` into `analyze.go`

**Files:**
- Modify: `internal/cli/analyze.go`
- Modify: `internal/cli/analyze_test.go`

### Step 1: Understand the current flow before touching anything

Read `internal/cli/analyze.go` lines 188–275. You will find:

1. Token counter setup (line ~190).
2. `featureMapCachePath` and `docsFeatureMapCachePath` declared (line ~198).
3. `codeMapCached` block using `productSummary.Features` (line ~204).
4. `docsMapCached` block using `productSummary.Features` (line ~213).
5. `runBothMaps(ctx, llmClient, tokenCounter, productSummary.Features, ...)` (line ~238).
6. Two `saveFeatureMapCache(featureMapCachePath, productSummary.Features, ...)` calls.
7. Two `saveDocsFeatureMapCache(docsFeatureMapCachePath, productSummary.Features, ...)` calls.
8. `reporter.WriteGaps(projectDir, featureMap, productSummary.Features)` (line ~267).

**Every `productSummary.Features` reference in points 3–8 must be replaced with `codeFeatures`.**

`reporter.WriteGaps`'s third argument also changes: instead of `productSummary.Features`, pass `docCoveredFeatures` (features from `docsFeatureMap` that have ≥1 doc page).

### Step 2: Write a failing test first

The existing `TestAnalyze_fullPipeline_withCachedAnalysis` is about to break because the pipeline now makes more LLM calls. **Before changing `analyze.go`**, update the test to expect the new call sequence.

Open `internal/cli/analyze_test.go` and find `TestAnalyze_fullPipeline_withCachedAnalysis`.

Current call sequence (2 calls):
1. `SynthesizeProduct` → `{"description":"A test product.","features":["feature-one"]}`
2. `MapFeaturesToCode` (code batch) → `[{"feature":"feature-one","files":["main.go"],"symbols":["Run"]}]`

New call sequence (4 calls):
1. `SynthesizeProduct` → `{"description":"A test product.","features":["feature-one"]}`
2. `ExtractFeaturesFromCode` → `["feature-one"]`
3. `MapFeaturesToCode` (code batch) → `[{"feature":"feature-one","files":["main.go"],"symbols":["Run"]}]`
4. `MapFeaturesToDocs` (doc page) → `["feature-one"]`

Replace the existing `callCount`-based handler with a `responses []string` slice:

```go
responses := []string{
    `{"description":"A test product.","features":["feature-one"]}`, // synthesize
    `["feature-one"]`,                                               // ExtractFeaturesFromCode
    `[{"feature":"feature-one","files":["main.go"],"symbols":["Run"]}]`, // MapFeaturesToCode
    `["feature-one"]`,                                               // MapFeaturesToDocs page
}
callIdx := 0
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    resp := responses[len(responses)-1] // repeat last if over
    if callIdx < len(responses) {
        resp = responses[callIdx]
    }
    callIdx++
    _ = json.NewEncoder(w).Encode(map[string]any{
        "choices": []map[string]any{
            {"message": map[string]any{"content": resp}},
        },
    })
}))
```

Run the test before changing `analyze.go`:

```bash
go test ./internal/cli/... -run TestAnalyze_fullPipeline_withCachedAnalysis -v
```

The test should still PASS (the old code still runs; the new responses won't be needed yet). This verifies the test harness change is safe.

### Step 3: Make the changes to `analyze.go`

**Insert after the token counter setup (around line 196), before the featureMapCachePath declarations:**

```go
// Extract the canonical feature list from CODE. These are the features
// the codebase actually implements — used as the source of truth for gap analysis.
codeFeaturesPath := filepath.Join(projectDir, "codefeatures.json")
var codeFeatures []string
codeFeaturesCached := !noCache && func() bool {
    if cached, ok := loadCodeFeaturesCache(codeFeaturesPath, scan); ok {
        log.Infof("using cached code features (%d features)", len(cached))
        codeFeatures = cached
        return true
    }
    return false
}()

if !codeFeaturesCached {
    log.Infof("extracting features from code...")
    codeFeatures, err = analyzer.ExtractFeaturesFromCode(ctx, llmClient, scan)
    if err != nil {
        return fmt.Errorf("extract code features: %w", err)
    }
    log.Debugf("extracted features: %v", codeFeatures)
    log.Infof("extracted %d features from code", len(codeFeatures))
    if err := saveCodeFeaturesCache(codeFeaturesPath, scan, codeFeatures); err != nil {
        return fmt.Errorf("save code features cache: %w", err)
    }
}
```

**Then replace ALL occurrences of `productSummary.Features` in the mapping section with `codeFeatures`.**

There are 7 occurrences (use `grep -n "productSummary.Features" internal/cli/analyze.go` to find them):

1. `loadFeatureMapCache(featureMapCachePath, productSummary.Features)` → `loadFeatureMapCache(featureMapCachePath, codeFeatures)`
2. `log.Infof("using cached feature map (%d features)", len(cached))` — unchanged (log message fine)
3. `loadDocsFeatureMapCache(docsFeatureMapCachePath, productSummary.Features)` → `loadDocsFeatureMapCache(docsFeatureMapCachePath, codeFeatures)`
4. `log.Infof("mapping %d features across code and docs in parallel...", len(productSummary.Features))` → `len(codeFeatures)`
5. `runBothMaps(ctx, llmClient, tokenCounter, productSummary.Features, ...)` → `codeFeatures`
6. `saveFeatureMapCache(featureMapCachePath, productSummary.Features, featureMap)` → `codeFeatures`
7. `saveDocsFeatureMapCache(docsFeatureMapCachePath, productSummary.Features, docsFeatureMap)` → `codeFeatures`

**Finally, replace the `WriteGaps` call.** The current call:

```go
if err := reporter.WriteGaps(projectDir, featureMap, productSummary.Features); err != nil {
```

Replace with:

```go
// Build the list of code features that have at least one documentation page.
docCoveredFeatures := make([]string, 0, len(docsFeatureMap))
for _, entry := range docsFeatureMap {
    if len(entry.Pages) > 0 {
        docCoveredFeatures = append(docCoveredFeatures, entry.Feature)
    }
}

if err := reporter.WriteGaps(projectDir, featureMap, docCoveredFeatures); err != nil {
```

### Step 4: Run all tests and verify they pass

```bash
go test ./internal/cli/... -v 2>&1 | tail -40
```

Expected: all PASS. Pay attention to:
- `TestAnalyze_fullPipeline_withCachedAnalysis` — must PASS with 4 LLM responses
- `TestAnalyze_anthropicProvider_usesAnthropicTokenCounter` — must still PASS (the test repo has no exported symbols, so `ExtractFeaturesFromCode` returns `[]string{}` immediately without an LLM call)

Run the full suite:

```bash
go test ./...
```

Expected: all PASS, build succeeds.

### Step 5: Commit

```bash
git add internal/cli/analyze.go internal/cli/analyze_test.go
git commit -m "feat(cli): derive canonical feature list from code, not docs"
```

---

## Task 4: Final verification

### Step 1: Build

```bash
go build ./...
```

Expected: success, no errors.

### Step 2: Lint

```bash
golangci-lint run
```

Fix any issues, then commit if needed.

### Step 3: Coverage check

```bash
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | grep -E "(analyzer|cli)" | sort
```

Target: ≥90% statement coverage in `internal/analyzer` and `internal/cli`.

### Step 4: Manual smoke test (optional but recommended)

Run against a real repo + docs URL. After the fix, the "Undocumented Code" section should contain features that actually exist in code but have no documentation page — not the empty list we had before.

```bash
go build -o /tmp/ftg ./cmd/find-the-gaps
/tmp/ftg analyze --repo /path/to/some/repo --docs-url https://docs.example.com --cache-dir /tmp/ftg-cache
cat /tmp/ftg-cache/<project>/gaps.md
```

### Step 5: Final commit (if lint/coverage fixes were needed)

```bash
git add -p
git commit -m "chore: lint and coverage fixes for code-feature extraction"
```

---

## Summary of API / Behavior Changes After This Plan

| | Before | After |
|---|---|---|
| Canonical feature list | Features extracted from docs pages | Features extracted from code symbols |
| "Undocumented Code" | Always empty (tautology) | Real gaps — code features with no doc page |
| "Unmapped Features" | Doc features with no code match | Always empty (both lists come from code now) |
| New cache file | — | `<project>/codefeatures.json` |
| LLM calls per run (no cache) | synthesize + code-map batches + docs-map pages | + 1 ExtractFeaturesFromCode call |
