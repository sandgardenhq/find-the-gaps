# Docs Feature Mapping Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Map each product feature to the documentation pages that cover it, processing pages individually in parallel alongside the existing code feature mapping.

**Architecture:** A new `MapFeaturesToDocs` function in `internal/analyzer/` mirrors `MapFeaturesToCode` but operates on cached doc pages rather than code symbols. Each page gets its own LLM call (never batched) with content truncated to fit the token budget. In `analyze.go`, both maps run concurrently via goroutines after synthesis completes.

**Tech Stack:** Go stdlib `sync` + channels for parallelism; existing `LLMClient` and `TokenCounter` interfaces; local tiktoken (`countTokens`) for fast content sizing before each call.

---

## Background: What already exists

- `internal/analyzer/mapper.go` — `MapFeaturesToCode`: batches *code symbols* into 80k-token chunks, sends per-batch LLM calls, accumulates `FeatureMap` (`[]FeatureEntry{Feature, Files, Symbols}`).
- `internal/spider/cache.go` — `Index.All()` returns `map[string]string` (URL → local `.md` filepath); pages already fetched and cached before this step runs.
- `internal/cli/analyze.go` (lines 146–167) — calls `MapFeaturesToCode` sequentially after synthesis. This is where we add parallel execution.
- `internal/cli/featuremap_cache.go` — pattern to copy for the new docs cache.
- `countTokens(s string) int` (tokens.go:30) — package-private tiktoken estimator, no network call, fast.

---

## Task 1: Add `DocsFeatureEntry` and `DocsFeatureMap` to types.go

**Files:**
- Modify: `internal/analyzer/types.go`

### Step 1: Write the failing test

Create `internal/analyzer/docs_mapper_test.go`:

```go
package analyzer_test

import (
	"encoding/json"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDocsFeatureMapRoundtrips(t *testing.T) {
	fm := analyzer.DocsFeatureMap{
		{Feature: "authentication", Pages: []string{"https://example.com/auth"}},
		{Feature: "search", Pages: []string{}},
	}
	data, err := json.Marshal(fm)
	require.NoError(t, err)

	var got analyzer.DocsFeatureMap
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, fm, got)
}
```

### Step 2: Run test to verify it fails

```bash
go test ./internal/analyzer/... -run TestDocsFeatureMapRoundtrips -v
```

Expected: `FAIL` — `analyzer.DocsFeatureMap undefined`

### Step 3: Add types to types.go

Append to `internal/analyzer/types.go`:

```go
// DocsFeatureEntry maps one product feature to the documentation pages that cover it.
type DocsFeatureEntry struct {
	Feature string   `json:"feature"`
	Pages   []string `json:"pages"`
}

// DocsFeatureMap is the complete feature-to-docs mapping for a project.
type DocsFeatureMap []DocsFeatureEntry
```

### Step 4: Run test to verify it passes

```bash
go test ./internal/analyzer/... -run TestDocsFeatureMapRoundtrips -v
```

Expected: `PASS`

### Step 5: Commit

```bash
git add internal/analyzer/types.go internal/analyzer/docs_mapper_test.go
git commit -m "feat(analyzer): add DocsFeatureEntry and DocsFeatureMap types

- RED: TestDocsFeatureMapRoundtrips written first
- GREEN: Two new types appended to types.go
- Status: 1 test passing, build successful"
```

---

## Task 2: Implement `mapPageToFeatures` (single page → matched features)

**Files:**
- Create: `internal/analyzer/docs_mapper.go`
- Modify: `internal/analyzer/docs_mapper_test.go`

This private function handles one doc page: truncates content if needed, calls LLM, parses result.

### Step 1: Write the failing tests

Add to `internal/analyzer/docs_mapper_test.go`:

```go
// --- helpers ---

type fakeClient struct {
	response string
	err      error
}

func (f *fakeClient) Complete(_ context.Context, _ string) (string, error) {
	return f.response, f.err
}

type fakeCounter struct{ count int }

func (f *fakeCounter) CountTokens(_ context.Context, _ string) (int, error) {
	return f.count, nil
}

// --- mapPageToFeatures tests ---

func TestMapPageToFeatures_HappyPath(t *testing.T) {
	features := []string{"authentication", "search", "billing"}
	featuresJSON, _ := json.Marshal(features)
	client := &fakeClient{response: `["authentication","search"]`}
	counter := &fakeCounter{count: 100}

	got, err := analyzer.ExportedMapPageToFeatures(
		context.Background(), client, counter,
		features, featuresJSON, 50, 10_000,
		"https://example.com/auth", "This page covers login and search.",
	)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"authentication", "search"}, got)
}

func TestMapPageToFeatures_EmptyResponse(t *testing.T) {
	features := []string{"authentication"}
	featuresJSON, _ := json.Marshal(features)
	client := &fakeClient{response: `[]`}
	counter := &fakeCounter{count: 10}

	got, err := analyzer.ExportedMapPageToFeatures(
		context.Background(), client, counter,
		features, featuresJSON, 20, 10_000,
		"https://example.com/other", "Unrelated content.",
	)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestMapPageToFeatures_InvalidJSON(t *testing.T) {
	features := []string{"authentication"}
	featuresJSON, _ := json.Marshal(features)
	client := &fakeClient{response: `not json`}
	counter := &fakeCounter{count: 10}

	_, err := analyzer.ExportedMapPageToFeatures(
		context.Background(), client, counter,
		features, featuresJSON, 20, 10_000,
		"https://example.com/page", "content",
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid JSON")
}

func TestMapPageToFeatures_ContentTruncatedWhenOverBudget(t *testing.T) {
	// The function must truncate content so the prompt fits the budget.
	// We verify truncation happened by checking the prompt sent to the client.
	features := []string{"authentication"}
	featuresJSON, _ := json.Marshal(features)

	var capturedPrompt string
	client := &fakeCaptureClient{
		capture: func(p string) { capturedPrompt = p },
		resp:    `["authentication"]`,
	}
	counter := &fakeCounter{count: 10}

	// Content has 10k characters; budget allows only ~100 content tokens (~400 chars)
	largeContent := strings.Repeat("word ", 2_000) // ~10k chars
	_, err := analyzer.ExportedMapPageToFeatures(
		context.Background(), client, counter,
		features, featuresJSON,
		50,    // featureTokens
		200,   // tokenBudget — very small to force truncation
		"https://example.com/big", largeContent,
	)
	require.NoError(t, err)
	// Prompt must be shorter than the full content
	assert.Less(t, len(capturedPrompt), len(largeContent))
}
```

Add `fakeCaptureClient` helper:

```go
type fakeCaptureClient struct {
	capture func(string)
	resp    string
}

func (f *fakeCaptureClient) Complete(_ context.Context, prompt string) (string, error) {
	f.capture(prompt)
	return f.resp, nil
}
```

Add `"strings"` and `"context"` to the import block in `docs_mapper_test.go`.

> **Note on `ExportedMapPageToFeatures`:** The real implementation is a private function `mapPageToFeatures`. Export it under a test-only alias in `internal/analyzer/export_test.go` (a file that only exists in test builds — see Step 3b below). This is the standard Go pattern for testing private functions from `package foo_test`.

### Step 2: Run tests to verify they fail

```bash
go test ./internal/analyzer/... -run "TestMapPageToFeatures" -v
```

Expected: `FAIL` — `analyzer.ExportedMapPageToFeatures undefined`

### Step 3a: Create `internal/analyzer/docs_mapper.go`

```go
package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/charmbracelet/log"
)

// DocsMapperPageBudget is the maximum tokens per mapPageToFeatures LLM call.
// Lower than MapperTokenBudget because we never batch pages — each call carries
// one page's full content plus the feature list.
const DocsMapperPageBudget = 40_000

// DocsMapProgressFunc is called with the accumulated results after each page is processed.
// It is called sequentially (from the result-drain goroutine), so implementations do not
// need to be goroutine-safe. Returning a non-nil error aborts the mapping.
type DocsMapProgressFunc func(partial DocsFeatureMap) error

// mapPageToFeatures asks the LLM which features from the canonical list are
// covered by a single documentation page. Content is truncated to fit the budget.
//
// featureTokens is the pre-computed token count of featuresJSON (caller computes once).
// tokenBudget is the per-call ceiling.
func mapPageToFeatures(
	ctx context.Context,
	client LLMClient,
	features []string,
	featuresJSON []byte,
	featureTokens int,
	tokenBudget int,
	pageURL, content string,
) ([]string, error) {
	const promptOverhead = 400
	available := tokenBudget - featureTokens - promptOverhead
	if available < 100 {
		// Feature list alone is too large for the budget — nothing we can do.
		return []string{}, nil
	}

	// Use the fast local tiktoken estimator to decide whether to truncate.
	contentTokens := countTokens(content)
	if contentTokens > available {
		// Truncate by character ratio. ~4 chars per token for English prose.
		keepChars := int(float64(len(content)) * float64(available) / float64(contentTokens))
		if keepChars < 0 {
			keepChars = 0
		}
		if keepChars > len(content) {
			keepChars = len(content)
		}
		content = content[:keepChars]
	}

	// PROMPT: Maps a single documentation page to the canonical product features it covers. Returns a JSON array of matching feature strings only.
	prompt := fmt.Sprintf(`You are analyzing a documentation page to identify which product features it covers.

Product features:
%s

Documentation page URL: %s

Documentation page content:
%s

Return a JSON array of feature strings (exact matches from the list above) that this page covers.
Only include features that are clearly addressed on this page.
Respond with only the JSON array. No markdown code fences. No prose.`,
		string(featuresJSON), pageURL, content)

	raw, err := client.Complete(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("mapPageToFeatures %s: %w", pageURL, err)
	}

	var matched []string
	if err := json.Unmarshal([]byte(raw), &matched); err != nil {
		return nil, fmt.Errorf("mapPageToFeatures %s: invalid JSON response: %w", pageURL, err)
	}
	if matched == nil {
		matched = []string{}
	}
	return matched, nil
}

// pageResult is the outcome of processing one doc page.
type pageResult struct {
	url      string
	features []string
	err      error
}

// MapFeaturesToDocs maps each product feature to the documentation pages that cover it.
// Each page is processed by an individual LLM call. Up to workers pages are processed
// concurrently. pages is a map of URL → local file path (as returned by spider.Crawl).
// onPage, if non-nil, is called with the accumulated results after each page completes.
func MapFeaturesToDocs(
	ctx context.Context,
	client LLMClient,
	features []string,
	pages map[string]string,
	workers int,
	tokenBudget int,
	onPage DocsMapProgressFunc,
) (DocsFeatureMap, error) {
	if len(features) == 0 || len(pages) == 0 {
		return emptyDocsFeatureMap(features), nil
	}

	featuresJSON, _ := json.Marshal(features)
	featureTokens := countTokens(string(featuresJSON))

	resultCh := make(chan pageResult, len(pages))
	sem := make(chan struct{}, workers)

	total := len(pages)
	var wg sync.WaitGroup
	for url, filePath := range pages {
		url, filePath := url, filePath
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			content, err := readPageContent(filePath)
			if err != nil {
				resultCh <- pageResult{url: url, err: err}
				return
			}
			log.Infof("  mapping page %s", url)
			matched, err := mapPageToFeatures(ctx, client, features, featuresJSON, featureTokens, tokenBudget, url, content)
			resultCh <- pageResult{url: url, features: matched, err: err}
		}()
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Accumulate: feature → set of pages that cover it.
	// The drain loop is sequential, so onPage can be called here without locking.
	acc := make(map[string]map[string]struct{}, len(features))
	for _, feat := range features {
		acc[feat] = make(map[string]struct{})
	}

	completed := 0
	for res := range resultCh {
		if res.err != nil {
			log.Warnf("docs mapping: skipping %s: %v", res.url, res.err)
			continue
		}
		for _, feat := range res.features {
			if _, known := acc[feat]; known {
				acc[feat][res.url] = struct{}{}
			}
		}
		completed++
		log.Infof("  [%d/%d] %s → %d features matched", completed, total, res.url, len(res.features))
		if onPage != nil {
			partial := docsAccToFeatureMap(acc, features)
			if err := onPage(partial); err != nil {
				return nil, fmt.Errorf("MapFeaturesToDocs: onPage: %w", err)
			}
		}
	}

	return docsAccToFeatureMap(acc, features), nil
}

func readPageContent(filePath string) (string, error) {
	import_os_content, err := readFile(filePath)
	if err != nil {
		return "", err
	}
	return import_os_content, nil
}

func docsAccToFeatureMap(acc map[string]map[string]struct{}, features []string) DocsFeatureMap {
	out := make(DocsFeatureMap, 0, len(features))
	for _, feat := range features {
		pages := make([]string, 0, len(acc[feat]))
		for p := range acc[feat] {
			pages = append(pages, p)
		}
		sort.Strings(pages)
		out = append(out, DocsFeatureEntry{Feature: feat, Pages: pages})
	}
	return out
}

func emptyDocsFeatureMap(features []string) DocsFeatureMap {
	out := make(DocsFeatureMap, 0, len(features))
	for _, feat := range features {
		out = append(out, DocsFeatureEntry{Feature: feat, Pages: []string{}})
	}
	return out
}
```

> **Wait** — `readPageContent` calls a helper `readFile`. Replace the body of `readPageContent` with a direct `os.ReadFile` call:

```go
import "os"

func readPageContent(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
```

Remove the `readFile` placeholder above and add `"os"` to the import block.

### Step 3b: Create `internal/analyzer/export_test.go`

This file exposes private functions for black-box tests. It only compiles during `go test`.

```go
package analyzer

// ExportedMapPageToFeatures exposes mapPageToFeatures for black-box tests.
// The TokenCounter parameter is accepted but unused — mapPageToFeatures uses the
// fast local countTokens estimator; the interface is kept for API symmetry.
func ExportedMapPageToFeatures(
	ctx context.Context,
	client LLMClient,
	_ TokenCounter,
	features []string,
	featuresJSON []byte,
	featureTokens int,
	tokenBudget int,
	pageURL, content string,
) ([]string, error) {
	return mapPageToFeatures(ctx, client, features, featuresJSON, featureTokens, tokenBudget, pageURL, content)
}
```

Add the missing `"context"` import.

### Step 4: Run tests to verify they pass

```bash
go test ./internal/analyzer/... -run "TestMapPageToFeatures" -v
```

Expected: all `PASS`

### Step 5: Verify build

```bash
go build ./...
```

Expected: no errors.

### Step 6: Commit

```bash
git add internal/analyzer/docs_mapper.go internal/analyzer/export_test.go internal/analyzer/docs_mapper_test.go
git commit -m "feat(analyzer): implement mapPageToFeatures with token-aware truncation

- RED: TestMapPageToFeatures_* tests written first
- GREEN: mapPageToFeatures + MapFeaturesToDocs skeleton added
- Status: 4 tests passing, build successful"
```

---

## Task 3: Test and complete `MapFeaturesToDocs` (orchestration layer)

**Files:**
- Modify: `internal/analyzer/docs_mapper_test.go`

The orchestrator (`MapFeaturesToDocs`) already exists from Task 2 but needs direct tests that verify parallel fan-out and result aggregation.

### Step 1: Write the failing tests

Add to `internal/analyzer/docs_mapper_test.go`:

```go
func TestMapFeaturesToDocs_AggregatesAcrossPages(t *testing.T) {
	// Three pages, two features. Each page covers different features.
	// Verifies parallel processing and correct aggregation.
	features := []string{"auth", "search", "billing"}

	// Set up temp dir with three page files.
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
		return p
	}

	pages := map[string]string{
		"https://example.com/auth":    write("auth.md", "covers auth"),
		"https://example.com/search":  write("search.md", "covers search"),
		"https://example.com/billing": write("billing.md", "covers billing"),
	}

	// Client responds based on the URL in the prompt.
	client := &fakeDynamicClient{responses: map[string]string{
		"https://example.com/auth":    `["auth"]`,
		"https://example.com/search":  `["search"]`,
		"https://example.com/billing": `["billing"]`,
	}}

	fm, err := analyzer.MapFeaturesToDocs(
		context.Background(), client,
		features, pages, 2, 10_000, nil,
	)
	require.NoError(t, err)
	require.Len(t, fm, 3)

	byFeature := make(map[string][]string)
	for _, e := range fm {
		byFeature[e.Feature] = e.Pages
	}
	assert.Equal(t, []string{"https://example.com/auth"}, byFeature["auth"])
	assert.Equal(t, []string{"https://example.com/search"}, byFeature["search"])
	assert.Equal(t, []string{"https://example.com/billing"}, byFeature["billing"])
}

func TestMapFeaturesToDocs_SkipsMissingFile(t *testing.T) {
	features := []string{"auth"}
	pages := map[string]string{
		"https://example.com/missing": "/tmp/does-not-exist-ftg-test.md",
	}
	client := &fakeClient{response: `["auth"]`}

	fm, err := analyzer.MapFeaturesToDocs(
		context.Background(), client,
		features, pages, 1, 10_000, nil,
	)
	require.NoError(t, err) // errors are logged, not returned
	require.Len(t, fm, 1)
	assert.Empty(t, fm[0].Pages)
}

func TestMapFeaturesToDocs_EmptyFeatures(t *testing.T) {
	fm, err := analyzer.MapFeaturesToDocs(
		context.Background(), nil,
		[]string{}, map[string]string{"https://x.com": "/tmp/x.md"}, 1, 10_000, nil,
	)
	require.NoError(t, err)
	assert.Empty(t, fm)
}

func TestMapFeaturesToDocs_EmptyPages(t *testing.T) {
	fm, err := analyzer.MapFeaturesToDocs(
		context.Background(), nil,
		[]string{"auth"}, map[string]string{}, 1, 10_000, nil,
	)
	require.NoError(t, err)
	require.Len(t, fm, 1)
	assert.Empty(t, fm[0].Pages)
}

func TestMapFeaturesToDocs_OnPageCalledAfterEachResult(t *testing.T) {
	// onPage must be called once per successfully processed page (not for errors/skips).
	features := []string{"auth", "search"}
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
		return p
	}
	pages := map[string]string{
		"https://example.com/auth":   write("auth.md", "auth content"),
		"https://example.com/search": write("search.md", "search content"),
	}
	client := &fakeDynamicClient{responses: map[string]string{
		"https://example.com/auth":   `["auth"]`,
		"https://example.com/search": `["search"]`,
	}}

	var callCount int
	onPage := func(partial analyzer.DocsFeatureMap) error {
		callCount++
		require.Len(t, partial, 2, "partial must always contain all features")
		return nil
	}

	_, err := analyzer.MapFeaturesToDocs(
		context.Background(), client,
		features, pages, 2, 10_000, onPage,
	)
	require.NoError(t, err)
	assert.Equal(t, 2, callCount, "onPage should be called once per successfully processed page")
}

func TestMapFeaturesToDocs_OnPageErrorAborts(t *testing.T) {
	features := []string{"auth"}
	dir := t.TempDir()
	p := filepath.Join(dir, "auth.md")
	require.NoError(t, os.WriteFile(p, []byte("auth content"), 0o644))

	client := &fakeClient{response: `["auth"]`}
	boom := errors.New("disk full")

	_, err := analyzer.MapFeaturesToDocs(
		context.Background(), client,
		features, map[string]string{"https://example.com/auth": p},
		1, 10_000,
		func(_ analyzer.DocsFeatureMap) error { return boom },
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk full")
}
```

Add `"errors"` to the import block in `docs_mapper_test.go`.
```

Add `fakeDynamicClient` helper (responds based on URL substring in prompt):

```go
type fakeDynamicClient struct {
	responses map[string]string // url substring → JSON response
}

func (f *fakeDynamicClient) Complete(_ context.Context, prompt string) (string, error) {
	for url, resp := range f.responses {
		if strings.Contains(prompt, url) {
			return resp, nil
		}
	}
	return `[]`, nil
}
```

Add `"os"`, `"path/filepath"` to the test file imports.

Export `MapFeaturesToDocs` in `export_test.go` — but wait, it's already public (capital M). No export needed.

### Step 2: Run tests to verify they fail

```bash
go test ./internal/analyzer/... -run "TestMapFeaturesToDocs" -v
```

Expected: some `FAIL` (the new tests likely expose a compilation issue if any, or logic gaps).

### Step 3: Fix any issues found

Run the tests. If they pass, great — the implementation from Task 2 was correct. If not, fix the minimal issue (likely a nil map read or missing `os` import in `docs_mapper.go`).

### Step 4: Run all analyzer tests

```bash
go test ./internal/analyzer/... -v
```

Expected: all `PASS`

### Step 5: Check coverage

```bash
go test -coverprofile=coverage.out ./internal/analyzer/...
go tool cover -func=coverage.out | grep docs_mapper
```

Target: ≥90% statement coverage on `docs_mapper.go`.

### Step 6: Commit

```bash
git add internal/analyzer/docs_mapper_test.go internal/analyzer/docs_mapper.go
git commit -m "test(analyzer): add MapFeaturesToDocs integration tests

- RED: TestMapFeaturesToDocs_* tests written before fixes
- GREEN: orchestration logic verified correct
- Status: all analyzer tests passing"
```

---

## Task 4: Add `docsfeaturemap_cache`

**Files:**
- Create: `internal/cli/docsfeaturemap_cache.go`
- Create: `internal/cli/docsfeaturemap_cache_test.go`

Mirrors `featuremap_cache.go` exactly but for `DocsFeatureMap`.

### Step 1: Write the failing tests

Create `internal/cli/docsfeaturemap_cache_test.go`:

```go
package cli

import (
	"path/filepath"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDocsFeatureMapCache_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "docsfeaturemap.json")

	features := []string{"auth", "search"}
	fm := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://example.com/auth"}},
		{Feature: "search", Pages: []string{"https://example.com/search", "https://example.com/home"}},
	}

	require.NoError(t, saveDocsFeatureMapCache(path, features, fm))

	got, ok := loadDocsFeatureMapCache(path, features)
	require.True(t, ok)
	require.Len(t, got, 2)
	assert.Equal(t, fm[0].Feature, got[0].Feature)
	assert.ElementsMatch(t, fm[0].Pages, got[0].Pages)
	assert.ElementsMatch(t, fm[1].Pages, got[1].Pages)
}

func TestDocsFeatureMapCache_StaleOnFeatureChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "docsfeaturemap.json")

	features := []string{"auth"}
	fm := analyzer.DocsFeatureMap{{Feature: "auth", Pages: []string{}}}
	require.NoError(t, saveDocsFeatureMapCache(path, features, fm))

	_, ok := loadDocsFeatureMapCache(path, []string{"auth", "new-feature"})
	assert.False(t, ok, "cache should be invalid when features change")
}

func TestDocsFeatureMapCache_MissingFile(t *testing.T) {
	_, ok := loadDocsFeatureMapCache("/tmp/does-not-exist-ftg.json", []string{"auth"})
	assert.False(t, ok)
}

func TestDocsFeatureMapCache_NilPagesNormalized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "docsfeaturemap.json")

	features := []string{"auth"}
	fm := analyzer.DocsFeatureMap{{Feature: "auth", Pages: nil}}
	require.NoError(t, saveDocsFeatureMapCache(path, features, fm))

	got, ok := loadDocsFeatureMapCache(path, features)
	require.True(t, ok)
	assert.NotNil(t, got[0].Pages, "nil pages should be normalized to empty slice on load")
}
```

### Step 2: Run tests to verify they fail

```bash
go test ./internal/cli/... -run "TestDocsFeatureMapCache" -v
```

Expected: `FAIL` — `loadDocsFeatureMapCache`, `saveDocsFeatureMapCache` undefined.

### Step 3: Create `internal/cli/docsfeaturemap_cache.go`

```go
package cli

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

type docsFeatureMapCacheFile struct {
	Features []string                  `json:"features"`
	Entries  []docsFeatureMapCacheEntry `json:"entries"`
}

type docsFeatureMapCacheEntry struct {
	Feature string   `json:"feature"`
	Pages   []string `json:"pages"`
}

// loadDocsFeatureMapCache reads a cached DocsFeatureMap from path.
// Returns false if the file does not exist, cannot be parsed, or wantFeatures
// does not match the features the cache was built from (order-insensitive).
func loadDocsFeatureMapCache(path string, wantFeatures []string) (analyzer.DocsFeatureMap, bool) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false
	}
	if err != nil {
		return nil, false
	}
	var cache docsFeatureMapCacheFile
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, false
	}
	if !featureSetsEqual(cache.Features, wantFeatures) {
		return nil, false
	}
	fm := make(analyzer.DocsFeatureMap, 0, len(cache.Entries))
	for _, e := range cache.Entries {
		pages := e.Pages
		if pages == nil {
			pages = []string{}
		}
		fm = append(fm, analyzer.DocsFeatureEntry{Feature: e.Feature, Pages: pages})
	}
	return fm, true
}

// saveDocsFeatureMapCache writes fm to path as JSON, recording features so that
// a future load can detect stale caches when the feature set changes.
func saveDocsFeatureMapCache(path string, features []string, fm analyzer.DocsFeatureMap) error {
	entries := make([]docsFeatureMapCacheEntry, len(fm))
	for i, e := range fm {
		pages := e.Pages
		if pages == nil {
			pages = []string{}
		}
		entries[i] = docsFeatureMapCacheEntry{Feature: e.Feature, Pages: pages}
	}
	cache := docsFeatureMapCacheFile{Features: features, Entries: entries}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
```

> **Note:** `featureSetsEqual` is already defined in `featuremap_cache.go` in the same package — no need to duplicate it.

### Step 4: Run tests to verify they pass

```bash
go test ./internal/cli/... -run "TestDocsFeatureMapCache" -v
```

Expected: all `PASS`

### Step 5: Commit

```bash
git add internal/cli/docsfeaturemap_cache.go internal/cli/docsfeaturemap_cache_test.go
git commit -m "feat(cli): add docsfeaturemap cache

- RED: TestDocsFeatureMapCache_* written first
- GREEN: loadDocsFeatureMapCache / saveDocsFeatureMapCache implemented
- Status: 4 tests passing, build successful"
```

---

## Task 5: Wire parallel execution into `analyze.go`

**Files:**
- Modify: `internal/cli/analyze.go`

Replace the sequential `MapFeaturesToCode` call (lines 146–167) with a goroutine that runs concurrently with a new `MapFeaturesToDocs` call. Both check their caches before hitting the LLM.

### Step 1: Write the failing test

The existing `analyze.go` has no unit test — only the end-to-end CLI tests in `cmd/find-the-gaps/testdata/`. Write a focused unit test that verifies both maps are returned after a run.

Create `internal/cli/analyze_parallel_test.go`:

```go
package cli

import (
	"context"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunBothMapsInParallel verifies that runBothMaps returns results from
// both MapFeaturesToCode and MapFeaturesToDocs concurrently.
func TestRunBothMapsInParallel(t *testing.T) {
	// runBothMaps is the extracted function we'll create in Step 3.
	codeMap, docsMap, err := runBothMaps(
		context.Background(),
		&stubLLMClient{
			codeResp: `[{"feature":"auth","files":["auth.go"],"symbols":["Login"]}]`,
			docsResp: `["auth"]`,
		},
		analyzer.NewTiktokenCounter(),
		[]string{"auth"},
		stubScan(),
		map[string]string{
			"https://example.com/auth": writeTempFile(t, "auth content"),
		},
		2,      // workers
		10_000, // docsTokenBudget
		nil,    // onCodeBatch
		nil,    // onDocsPage
	)
	require.NoError(t, err)
	require.Len(t, codeMap, 1)
	assert.Equal(t, "auth", codeMap[0].Feature)
	require.Len(t, docsMap, 1)
	assert.Equal(t, "auth", docsMap[0].Feature)
}
```

Add stub helpers at the bottom of the file:

```go
import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
	scannertypes "github.com/sandgardenhq/find-the-gaps/internal/scanner/types"
)

type stubLLMClient struct {
	codeResp string
	docsResp string
	calls    int
}

func (s *stubLLMClient) Complete(_ context.Context, prompt string) (string, error) {
	s.calls++
	// Heuristic: code mapping prompts contain "Code symbols"; docs prompts contain "Documentation page URL"
	if strings.Contains(prompt, "Code symbols") {
		return s.codeResp, nil
	}
	return s.docsResp, nil
}

func stubScan() *scanner.ProjectScan {
	return &scanner.ProjectScan{
		RepoPath:  "/fake",
		ScannedAt: time.Now(),
		Files: []scanner.ScannedFile{
			{
				Path:     "auth.go",
				Language: "go",
				Symbols:  []scannertypes.Symbol{{Name: "Login", Kind: scannertypes.KindFunc}},
			},
		},
	}
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "page.md")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	return p
}
```

### Step 2: Run test to verify it fails

```bash
go test ./internal/cli/... -run "TestRunBothMapsInParallel" -v
```

Expected: `FAIL` — `runBothMaps undefined`

### Step 3: Extract `runBothMaps` and update `analyze.go`

Add the following function to `analyze.go` (before `newAnalyzeCmd`):

```go
type bothMapsResult struct {
	codeMap analyzer.FeatureMap
	err     error
}

type docsMapsResult struct {
	docsMap analyzer.DocsFeatureMap
	err     error
}

// runBothMaps executes MapFeaturesToCode and MapFeaturesToDocs concurrently.
// It returns when both complete or either returns an error.
// onCodeBatch and onDocsPage are passed through to the respective mappers for
// intermediate persistence; either may be nil.
func runBothMaps(
	ctx context.Context,
	client analyzer.LLMClient,
	counter analyzer.TokenCounter,
	features []string,
	scan *scanner.ProjectScan,
	pages map[string]string,
	workers int,
	docsTokenBudget int,
	onCodeBatch analyzer.MapProgressFunc,
	onDocsPage analyzer.DocsMapProgressFunc,
) (analyzer.FeatureMap, analyzer.DocsFeatureMap, error) {
	codeCh := make(chan bothMapsResult, 1)
	docsCh := make(chan docsMapsResult, 1)

	go func() {
		fm, err := analyzer.MapFeaturesToCode(ctx, client, counter, features, scan, analyzer.MapperTokenBudget, onCodeBatch)
		codeCh <- bothMapsResult{fm, err}
	}()

	go func() {
		fm, err := analyzer.MapFeaturesToDocs(ctx, client, features, pages, workers, docsTokenBudget, onDocsPage)
		docsCh <- docsMapsResult{fm, err}
	}()

	codeRes := <-codeCh
	docsRes := <-docsCh

	if codeRes.err != nil {
		return nil, nil, codeRes.err
	}
	if docsRes.err != nil {
		return nil, nil, docsRes.err
	}
	return codeRes.codeMap, docsRes.docsMap, nil
}
```

> **Why ignore the `tokenCounter` parameter in `runBothMaps`?** The `analyze.go` caller selects the right counter for code mapping based on provider. Pass `tokenCounter` from the caller instead of hardcoding `NewTiktokenCounter()`. Update the signature to include `counter analyzer.TokenCounter` and pass it to `MapFeaturesToCode`.

Replace the code mapping block in `RunE` (lines 146–167 in `analyze.go`) with:

```go
featureMapCachePath := filepath.Join(projectDir, "featuremap.json")
docsFeatureMapCachePath := filepath.Join(projectDir, "docsfeaturemap.json")

var featureMap analyzer.FeatureMap
var docsFeatureMap analyzer.DocsFeatureMap

codeMapCached := !noCache && func() bool {
    if cached, ok := loadFeatureMapCache(featureMapCachePath, productSummary.Features); ok {
        log.Infof("using cached feature map (%d features)", len(cached))
        featureMap = cached
        return true
    }
    return false
}()

docsMapCached := !noCache && func() bool {
    if cached, ok := loadDocsFeatureMapCache(docsFeatureMapCachePath, productSummary.Features); ok {
        log.Infof("using cached docs feature map (%d features)", len(cached))
        docsFeatureMap = cached
        return true
    }
    return false
}()

if !codeMapCached || !docsMapCached {
    log.Infof("mapping %d features across code and docs in parallel...", len(productSummary.Features))

    freshCodeMap, freshDocsMap, mapErr := runBothMaps(
        ctx, llmClient, tokenCounter, productSummary.Features,
        scan, pages, workers, analyzer.DocsMapperPageBudget,
        func(partial analyzer.FeatureMap) error {
            return saveFeatureMapCache(featureMapCachePath, productSummary.Features, partial)
        },
        func(partial analyzer.DocsFeatureMap) error {
            return saveDocsFeatureMapCache(docsFeatureMapCachePath, productSummary.Features, partial)
        },
    )
    if mapErr != nil {
        return fmt.Errorf("map features: %w", mapErr)
    }

    if !codeMapCached {
        featureMap = freshCodeMap
        if err := saveFeatureMapCache(featureMapCachePath, productSummary.Features, featureMap); err != nil {
            return fmt.Errorf("save feature map cache: %w", err)
        }
    }
    if !docsMapCached {
        docsFeatureMap = freshDocsMap
        if err := saveDocsFeatureMapCache(docsFeatureMapCachePath, productSummary.Features, docsFeatureMap); err != nil {
            return fmt.Errorf("save docs feature map cache: %w", err)
        }
    }
}

log.Debug("feature mapping complete", "code", len(featureMap), "docs", len(docsFeatureMap))
```

> **Important:** Update `runBothMaps` signature to accept `counter analyzer.TokenCounter` and the `tokenBudget int` for docs, and pass the counter to `MapFeaturesToCode`:

The final `runBothMaps` signature (matching the function added above) is:

```go
func runBothMaps(
    ctx context.Context,
    client analyzer.LLMClient,
    counter analyzer.TokenCounter,
    features []string,
    scan *scanner.ProjectScan,
    pages map[string]string,
    workers int,
    docsTokenBudget int,
    onCodeBatch analyzer.MapProgressFunc,
    onDocsPage analyzer.DocsMapProgressFunc,
) (analyzer.FeatureMap, analyzer.DocsFeatureMap, error)
```

> **Note on partial cache:** When one map is cached and one is not, only the missing map runs. The test in Step 1 covers both uncached. Cache-hit paths are covered by `TestDocsFeatureMapCache_RoundTrip` and `TestFeatureMapCache` in `featuremap_cache_test.go`.

### Step 4: Run all tests

```bash
go test ./internal/cli/... -v
```

Expected: all `PASS`

### Step 5: Build

```bash
go build ./...
```

Expected: no errors.

### Step 6: Lint

```bash
golangci-lint run
```

Expected: clean.

### Step 7: Check coverage

```bash
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -5
```

### Step 8: Commit

```bash
git add internal/cli/analyze.go internal/cli/analyze_parallel_test.go
git commit -m "feat(cli): run code and docs feature mapping in parallel

- RED: TestRunBothMapsInParallel written first
- GREEN: runBothMaps goroutine fan-out; both caches checked before dispatch
- Status: all tests passing, build clean"
```

---

## Task 6: Full test suite and coverage gate

### Step 1: Run all tests with coverage

```bash
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

### Step 2: Check per-package coverage

Target packages and their minimums:
- `internal/analyzer` — ≥90% (new `docs_mapper.go` must be covered)
- `internal/cli` — ≥90% (new `docsfeaturemap_cache.go` must be covered)

If coverage is below threshold on a specific file, write additional test cases for the uncovered lines before proceeding.

### Step 3: Final lint

```bash
golangci-lint run
```

### Step 4: Update PROGRESS.md

```markdown
## Task: Docs Feature Mapping - COMPLETE
- Started: 2026-04-21
- Tests: all passing, 0 failing
- Coverage: see go tool cover output
- Build: ✅ Successful
- Linting: ✅ Clean
- Completed: [timestamp]
- Notes: Each doc page is processed by an individual LLM call (not batched).
  Content is truncated to DocsMapperPageBudget (40k tokens) minus feature list tokens.
  Both code and docs mapping run concurrently after synthesis completes.
  Results cached to docsfeaturemap.json; stale when feature set changes.
```

### Step 5: Final commit

```bash
git add PROGRESS.md
git commit -m "docs: update PROGRESS.md for docs feature mapping

- All tests passing
- Coverage ≥90% on all touched packages"
```

---

## Implementation Notes

### Token budget rationale

`DocsMapperPageBudget = 40_000` is half of `MapperTokenBudget`. The code mapper batches many files into one call (80k budget used fully). Each docs page call uses: feature list tokens (fixed, typically 1–5k) + page content tokens (variable). 40k leaves ample room for large documentation pages without approaching model limits.

### Why tiktoken for truncation, not the provider counter

The provider counter (Anthropic API) makes a network call. Calling it once per doc page for every run would be slow and costly. The local tiktoken estimate (`countTokens`) is accurate enough for truncation decisions — we're aiming for "roughly fits" not "exactly fits". The code mapper uses provider counting only for batch-splitting decisions where exact counts matter; for page truncation a ~10% overestimate is fine.

### Why both maps run concurrently, not just docs-within-pages

The two maps are independent. `MapFeaturesToCode` does many sequential batched LLM calls (one per code batch). `MapFeaturesToDocs` does many concurrent LLM calls (one per page). Running them in parallel halves wall-clock time on the analysis step when neither is cached.

### Partial cache handling

If the code map is cached but the docs map is stale (or vice versa), only the missing map runs. This avoids re-running the expensive code mapping when only new docs pages have been added.
