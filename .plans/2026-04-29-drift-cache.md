# Drift-Detection Cache Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make `analyze` resumable across drift detection — a killed run picks back up where it left off, skipping features whose drift was already finished.

**Architecture:** Per-feature cache file at `<projectDir>/drift.json` with set-based invalidation (feature name + sorted files + sorted pages). `analyzer.DetectDrift` gains a `cached` lookup map and an `onFeatureDone` callback. The CLI owns load/save; the analyzer remains pure. `--no-cache` skips the read but still writes a fresh cache for next time.

**Tech Stack:** Go 1.26+, stdlib `encoding/json`, `os`, `path/filepath`, `sort`. Testing via stdlib `testing` plus `github.com/stretchr/testify`. Existing patterns: see `internal/cli/featuremap_cache.go` and `internal/cli/codefeatures_cache.go` for cache-file conventions; see `internal/analyzer/drift.go` for the function being modified.

**Design source:** `.plans/2026-04-29-drift-cache-design.md`. Read it before starting.

---

## Conventions

This project follows TDD strictly (see `CLAUDE.md`). Every task in this plan follows: **RED test → verify fail → minimal code → verify pass → commit**. Do not skip the "verify fail" step — a test that passes immediately is a bug in the test, not a green checkmark.

**Commands you will run constantly:**

```bash
# Run the analyzer test package only (fast feedback)
go test ./internal/analyzer/... -count=1

# Run the CLI test package only
go test ./internal/cli/... -count=1

# Run everything before committing
go test ./... -count=1

# Lint
golangci-lint run

# Format (after every code edit)
gofmt -w . && goimports -w .

# Build
go build ./...
```

**Commit messages** follow the existing convention. Example from this repo:

```
feat(analyzer): persist drift results per feature for resumable runs

- RED: DetectDrift_CachedFeature_SkipsInvestigator
- GREEN: lookup cached map by feature name; equal sorted files+pages → reuse issues
- Status: 32 tests passing, build clean

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
```

Use `feat(analyzer)` for analyzer changes, `feat(cli)` for CLI changes, `test(...)` for test-only commits, `refactor(...)` for non-behavior changes.

---

## Task 1: Add cache types to the analyzer package

**Files:**
- Modify: `internal/analyzer/drift.go` (add types near top of file, after the `const` block)

**Step 1: Add the two new types**

Add after the `const ( ... )` block at line 28:

```go
// CachedDriftEntry is one feature's persisted drift result, used by
// DetectDrift to short-circuit the investigator+judge when inputs are
// unchanged. Files and Pages must be sorted ascending; the lookup compares
// them as sorted sets against the current run's inputs.
type CachedDriftEntry struct {
	Files  []string
	Pages  []string
	Issues []DriftIssue
}

// DriftFeatureDoneFunc fires after DetectDrift decides a feature's drift
// result, whether the result came from a cache hit or a fresh investigate+judge.
// Implementations typically persist the result so a future run can resume.
// Files and Pages are sorted ascending. Return non-nil to abort detection.
type DriftFeatureDoneFunc func(feature string, files, pages []string, issues []DriftIssue) error
```

**Step 2: Verify the package still compiles (no callers yet)**

```bash
go build ./internal/analyzer/...
```

Expected: builds clean (the new types are unused but valid).

**Step 3: Commit**

```bash
git add internal/analyzer/drift.go
git commit -m "$(cat <<'EOF'
feat(analyzer): add CachedDriftEntry and DriftFeatureDoneFunc types

Types only; DetectDrift signature change comes next.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Extend DetectDrift signature with new params (back-compat update)

**Files:**
- Modify: `internal/analyzer/drift.go` (function `DetectDrift` around line 59)
- Modify: `internal/cli/analyze.go` (call site around line 348)
- Modify: `internal/analyzer/drift_test.go` (every existing call site — there are ~10)

**Goal:** Change the signature to accept `cached` and `onFeatureDone`; thread `onFeatureDone` calls into the existing flow on the fresh path. Cache lookup itself is added in Task 3 — for now `cached` is read but not used. Every existing test must still pass with `nil, nil` for the new params.

**Step 1: Update the `DetectDrift` signature and call `onFeatureDone` after each completed feature**

Change the signature in `internal/analyzer/drift.go`:

```go
func DetectDrift(
	ctx context.Context,
	tiering LLMTiering,
	featureMap FeatureMap,
	docsMap DocsFeatureMap,
	pageReader func(url string) (string, error),
	repoRoot string,
	cached map[string]CachedDriftEntry,
	onFinding DriftProgressFunc,
	onFeatureDone DriftFeatureDoneFunc,
) ([]DriftFinding, error) {
```

Inside the per-feature loop, after the `judgeFeatureDrift` call and the existing `if len(issues) > 0` block, add an `onFeatureDone` invocation. Also sort `entry.Files` and `pages` deterministically before passing them. Add this helper near the bottom of `drift.go`:

```go
// sortedCopy returns a sorted copy of s. The input is not modified.
func sortedCopy(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}
```

Add `"sort"` to the imports. Then in the loop, replace the existing `if len(issues) > 0 { ... }` block with:

```go
sortedFiles := sortedCopy(entry.Files)
sortedPages := sortedCopy(pages)

if len(issues) > 0 {
	findings = append(findings, DriftFinding{Feature: entry.Feature.Name, Issues: issues})
	if onFinding != nil {
		if err := onFinding(findings); err != nil {
			return nil, fmt.Errorf("DetectDrift: onFinding: %w", err)
		}
	}
}
if onFeatureDone != nil {
	if err := onFeatureDone(entry.Feature.Name, sortedFiles, sortedPages, issues); err != nil {
		return nil, fmt.Errorf("DetectDrift: onFeatureDone: %w", err)
	}
}
```

(The `cached` parameter is unused for now — Go will tolerate this since map types are reference types and we read it in Task 3. To silence any "declared and not used" warning during this task, also add a leading `_ = cached` line at the top of the function body. Remove that line in Task 3.)

**Step 2: Update the production call site in `internal/cli/analyze.go`**

Around line 348, the existing call is:

```go
driftFindings, err := analyzer.DetectDrift(ctx, tiering, featureMap, docsFeatureMap, pageReader, repoPath, driftOnFinding)
```

Replace with:

```go
driftFindings, err := analyzer.DetectDrift(ctx, tiering, featureMap, docsFeatureMap, pageReader, repoPath, nil, driftOnFinding, nil)
```

The CLI integration with real cache wiring happens in Task 9.

**Step 3: Update all existing test call sites**

Run:

```bash
grep -n "analyzer.DetectDrift" internal/analyzer/drift_test.go
```

For each line, insert `nil,` before the existing trailing `nil` (the `onFinding` arg) and add a trailing `, nil` for `onFeatureDone`. Concretely, every call of the form:

```go
analyzer.DetectDrift(ctx, tiering, featureMap, docsMap, pageReader, repoRoot, nil)
```

Becomes:

```go
analyzer.DetectDrift(ctx, tiering, featureMap, docsMap, pageReader, repoRoot, nil, nil, nil)
```

Where the three trailing `nil`s are: `cached`, `onFinding`, `onFeatureDone`. (Some existing tests pass a non-nil `onFinding` — in that case, the order is `cached=nil, onFinding=<existing>, onFeatureDone=nil`.)

**Step 4: Verify all tests still pass**

```bash
gofmt -w . && goimports -w .
go build ./...
go test ./internal/analyzer/... -count=1
go test ./internal/cli/... -count=1
```

Expected: all green. No new tests yet — this task is purely a signature update with the existing behavior preserved.

**Step 5: Commit**

```bash
git add -u internal/analyzer/drift.go internal/cli/analyze.go internal/analyzer/drift_test.go
git commit -m "$(cat <<'EOF'
refactor(analyzer): extend DetectDrift with cached + onFeatureDone params

Signature now accepts an optional CachedDriftEntry lookup map and a
per-feature completion callback. onFeatureDone fires after every feature
(empty-issues included). Cache lookup itself comes next.

- All existing tests pass nil for the new params.
- Status: build clean, all tests green.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Cache hit short-circuits investigator and judge

**Files:**
- Modify: `internal/analyzer/drift_test.go` (new test)
- Modify: `internal/analyzer/drift.go` (add cache lookup before investigator)

**Step 1: Write the failing test**

Add to `internal/analyzer/drift_test.go`:

```go
func TestDetectDrift_CacheHit_SkipsLLM(t *testing.T) {
	// Investigator and judge stubs panic if invoked — they must not be called
	// when the cache supplies an entry whose files+pages match the current run.
	typical := &driftStubClient{} // empty responses; any call exhausts and panics
	large := &driftStubClient{}   // judge must never run
	small := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil },
	}

	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}},
	}
	pageReader := func(_ string) (string, error) { return "# Auth", nil }

	cached := map[string]analyzer.CachedDriftEntry{
		"auth": {
			Files:  []string{"auth.go"},
			Pages:  []string{"https://docs.example.com/auth"},
			Issues: []analyzer.DriftIssue{{Page: "https://docs.example.com/auth", Issue: "stale signature"}},
		},
	}

	findings, err := analyzer.DetectDrift(
		context.Background(),
		&fakeTiering{small: small, typical: typical, large: large},
		featureMap, docsMap, pageReader, "/repo",
		cached, nil, nil,
	)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, "auth", findings[0].Feature)
	require.Len(t, findings[0].Issues, 1)
	assert.Equal(t, "stale signature", findings[0].Issues[0].Issue)
	assert.Equal(t, 0, typical.calls, "investigator must not run on cache hit")
	assert.Equal(t, 0, large.completeCalls, "judge must not run on cache hit")
}
```

**Step 2: Run the test, watch it fail**

```bash
go test ./internal/analyzer/ -run TestDetectDrift_CacheHit_SkipsLLM -v
```

Expected: FAIL. The investigator runs (`typical.calls > 0`) because cache lookup isn't implemented yet.

**Step 3: Implement cache lookup**

In `internal/analyzer/drift.go`, inside the per-feature loop, after `pages = classifyDriftPages(...)` and before `investigateFeatureDrift(...)`, add:

```go
sortedFiles := sortedCopy(entry.Files)
sortedPages := sortedCopy(pages)

if cached != nil {
	if c, ok := cached[entry.Feature.Name]; ok &&
		equalStringSlice(c.Files, sortedFiles) &&
		equalStringSlice(c.Pages, sortedPages) {
		log.Debugf("  drift cache hit: %s", entry.Feature.Name)
		issues := c.Issues
		if len(issues) > 0 {
			findings = append(findings, DriftFinding{Feature: entry.Feature.Name, Issues: issues})
			if onFinding != nil {
				if err := onFinding(findings); err != nil {
					return nil, fmt.Errorf("DetectDrift: onFinding: %w", err)
				}
			}
		}
		if onFeatureDone != nil {
			if err := onFeatureDone(entry.Feature.Name, sortedFiles, sortedPages, issues); err != nil {
				return nil, fmt.Errorf("DetectDrift: onFeatureDone: %w", err)
			}
		}
		continue
	}
}
```

This means the existing post-judge `sortedFiles := sortedCopy(entry.Files)` and `sortedPages := sortedCopy(pages)` lines from Task 2 are now duplicated. Remove the duplicates from the post-judge block — the variables are already in scope from above.

Add the helper at the bottom of `drift.go`:

```go
// equalStringSlice reports whether a and b are equal element-wise. Both
// must already be sorted; this is not a set comparison.
func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

Also remove the `_ = cached` line added in Task 2.

**Step 4: Run the test, verify it passes**

```bash
go test ./internal/analyzer/ -run TestDetectDrift_CacheHit_SkipsLLM -v
```

Expected: PASS.

**Step 5: Run the full analyzer test suite to ensure no regressions**

```bash
go test ./internal/analyzer/... -count=1
```

Expected: all green.

**Step 6: Commit**

```bash
gofmt -w . && goimports -w .
git add -u internal/analyzer/drift.go internal/analyzer/drift_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): cache hit in DetectDrift skips investigator and judge

- RED: TestDetectDrift_CacheHit_SkipsLLM
- GREEN: per-feature lookup of cached map; equal sorted files+pages → reuse issues
- Status: full analyzer suite green.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Cache miss when files differ recomputes

**Files:**
- Modify: `internal/analyzer/drift_test.go` (new test only)

**Step 1: Write the failing test**

```go
func TestDetectDrift_CacheMissByFiles_RecomputesFresh(t *testing.T) {
	// Cached entry's files don't match the current feature's files → recompute.
	typical := &driftStubClient{
		responses: []analyzer.ChatMessage{
			noteObservation("https://docs.example.com/auth", "old", "new", "drift"),
			driftDone(),
		},
	}
	large := &driftStubClient{completeFunc: judgeJSON("https://docs.example.com/auth", "Login signature changed.")}
	small := &driftStubClient{completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil }}

	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go", "session.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}},
	}
	pageReader := func(_ string) (string, error) { return "# Auth", nil }

	// Cached entry from a prior run when the feature only had auth.go.
	cached := map[string]analyzer.CachedDriftEntry{
		"auth": {
			Files:  []string{"auth.go"}, // mismatch — current run has [auth.go, session.go]
			Pages:  []string{"https://docs.example.com/auth"},
			Issues: nil,
		},
	}

	findings, err := analyzer.DetectDrift(
		context.Background(),
		&fakeTiering{small: small, typical: typical, large: large},
		featureMap, docsMap, pageReader, "/repo",
		cached, nil, nil,
	)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Contains(t, findings[0].Issues[0].Issue, "Login signature changed")
	assert.Greater(t, typical.calls, 0, "investigator must run on cache miss")
}
```

**Step 2: Run, watch it pass already**

```bash
go test ./internal/analyzer/ -run TestDetectDrift_CacheMissByFiles_RecomputesFresh -v
```

Expected: PASS — the cache lookup logic from Task 3 handles this case correctly via `equalStringSlice`.

**Note on TDD discipline:** A test that passes immediately is suspicious. To prove this test is meaningful, temporarily change the cache lookup to skip the `equalStringSlice(c.Files, sortedFiles)` check (e.g., comment out that condition), re-run the test, watch it fail, then restore. Confirm you see RED → GREEN before committing. Do this once for this task and Task 5; you don't need to repeat for every subsequent test if the pattern is identical.

**Step 3: Commit**

```bash
git add internal/analyzer/drift_test.go
git commit -m "$(cat <<'EOF'
test(analyzer): cache miss recomputes when feature's files change

Locks in the file-list invalidation half of the set-based cache key.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Cache miss when pages differ recomputes

**Files:**
- Modify: `internal/analyzer/drift_test.go` (new test only)

**Step 1: Write the failing test**

Same structure as Task 4, but the mismatch is on `Pages`:

```go
func TestDetectDrift_CacheMissByPages_RecomputesFresh(t *testing.T) {
	typical := &driftStubClient{
		responses: []analyzer.ChatMessage{
			noteObservation("https://docs.example.com/auth", "old", "new", "drift"),
			driftDone(),
		},
	}
	large := &driftStubClient{completeFunc: judgeJSON("https://docs.example.com/auth", "Pages drifted.")}
	small := &driftStubClient{completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil }}

	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}},
	}
	pageReader := func(_ string) (string, error) { return "# Auth", nil }

	cached := map[string]analyzer.CachedDriftEntry{
		"auth": {
			Files:  []string{"auth.go"},
			Pages:  []string{"https://docs.example.com/old"}, // mismatch
			Issues: nil,
		},
	}

	findings, err := analyzer.DetectDrift(
		context.Background(),
		&fakeTiering{small: small, typical: typical, large: large},
		featureMap, docsMap, pageReader, "/repo",
		cached, nil, nil,
	)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Greater(t, typical.calls, 0, "investigator must run on page mismatch")
}
```

**Step 2: Run; verify RED-GREEN by temporarily breaking the page-equality check (same technique as Task 4); commit**

```bash
go test ./internal/analyzer/ -run TestDetectDrift_CacheMissByPages_RecomputesFresh -v
git add internal/analyzer/drift_test.go
git commit -m "test(analyzer): cache miss recomputes when feature's pages change

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 6: onFeatureDone fires for every completed feature (fresh, hit, empty-issues)

**Files:**
- Modify: `internal/analyzer/drift_test.go` (one new test covering all three cases)

**Step 1: Write the failing test**

```go
func TestDetectDrift_OnFeatureDone_FiresForAllCompletions(t *testing.T) {
	// Three features:
	// 1. "fresh-with-issues"  — no cache entry, investigator emits an observation, judge issues.
	// 2. "fresh-empty"        — no cache entry, investigator emits zero observations.
	// 3. "cached"             — cache hit, returns prior issues.
	// onFeatureDone must fire exactly 3 times, with the right names and issue counts.
	typical := &driftStubClient{
		responses: []analyzer.ChatMessage{
			// fresh-with-issues:
			noteObservation("https://docs.example.com/a", "old", "new", "drift"),
			driftDone(),
			// fresh-empty:
			driftDone(),
			// cached: investigator must not be invoked.
		},
	}
	large := &driftStubClient{completeFunc: judgeJSON("https://docs.example.com/a", "Drift A.")}
	small := &driftStubClient{completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil }}

	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "fresh-with-issues"}, Files: []string{"a.go"}},
		{Feature: analyzer.CodeFeature{Name: "fresh-empty"}, Files: []string{"b.go"}},
		{Feature: analyzer.CodeFeature{Name: "cached"}, Files: []string{"c.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "fresh-with-issues", Pages: []string{"https://docs.example.com/a"}},
		{Feature: "fresh-empty", Pages: []string{"https://docs.example.com/b"}},
		{Feature: "cached", Pages: []string{"https://docs.example.com/c"}},
	}
	pageReader := func(_ string) (string, error) { return "# Page", nil }

	cached := map[string]analyzer.CachedDriftEntry{
		"cached": {
			Files:  []string{"c.go"},
			Pages:  []string{"https://docs.example.com/c"},
			Issues: []analyzer.DriftIssue{{Page: "https://docs.example.com/c", Issue: "Cached drift."}},
		},
	}

	type record struct {
		name        string
		issuesCount int
	}
	var recorded []record
	onFeatureDone := func(name string, files, pages []string, issues []analyzer.DriftIssue) error {
		recorded = append(recorded, record{name: name, issuesCount: len(issues)})
		return nil
	}

	_, err := analyzer.DetectDrift(
		context.Background(),
		&fakeTiering{small: small, typical: typical, large: large},
		featureMap, docsMap, pageReader, "/repo",
		cached, nil, onFeatureDone,
	)
	require.NoError(t, err)
	require.Len(t, recorded, 3)
	// Order matches featureMap iteration order.
	assert.Equal(t, "fresh-with-issues", recorded[0].name)
	assert.Equal(t, 1, recorded[0].issuesCount)
	assert.Equal(t, "fresh-empty", recorded[1].name)
	assert.Equal(t, 0, recorded[1].issuesCount)
	assert.Equal(t, "cached", recorded[2].name)
	assert.Equal(t, 1, recorded[2].issuesCount)
}
```

**Step 2: Run, expect PASS (since Task 2 wired up `onFeatureDone` on the fresh path AND Task 3 wired it on the hit path)**

```bash
go test ./internal/analyzer/ -run TestDetectDrift_OnFeatureDone_FiresForAllCompletions -v
```

Expected: PASS.

**Step 3: Verify by breaking — comment out the `onFeatureDone` call in either the cache-hit branch or the fresh branch, watch the test fail at the relevant assertion, restore.**

**Step 4: Commit**

```bash
git add internal/analyzer/drift_test.go
git commit -m "test(analyzer): onFeatureDone fires for fresh, empty, and cached features

Locks in that the cache layer can persist every completion, not just
features with non-zero issues.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 7: onFeatureDone error aborts detection

**Files:**
- Modify: `internal/analyzer/drift_test.go` (new test only)

**Step 1: Write the failing test**

```go
func TestDetectDrift_OnFeatureDoneError_Aborts(t *testing.T) {
	typical := &driftStubClient{responses: []analyzer.ChatMessage{driftDone()}}
	large := &driftStubClient{}
	small := &driftStubClient{completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil }}

	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"a.go"}},
		{Feature: analyzer.CodeFeature{Name: "search"}, Files: []string{"b.go"}}, // must not be processed
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://docs.example.com/a"}},
		{Feature: "search", Pages: []string{"https://docs.example.com/b"}},
	}
	pageReader := func(_ string) (string, error) { return "# Page", nil }

	calls := 0
	onFeatureDone := func(_ string, _, _ []string, _ []analyzer.DriftIssue) error {
		calls++
		return errors.New("disk full")
	}

	_, err := analyzer.DetectDrift(
		context.Background(),
		&fakeTiering{small: small, typical: typical, large: large},
		featureMap, docsMap, pageReader, "/repo",
		nil, nil, onFeatureDone,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk full")
	assert.Equal(t, 1, calls, "second feature must not be processed after onFeatureDone error")
}
```

Add `"errors"` to the test file imports if not already present.

**Step 2: Run, expect PASS (the implementation in Task 2 already returns the wrapped error)**

```bash
go test ./internal/analyzer/ -run TestDetectDrift_OnFeatureDoneError_Aborts -v
```

Expected: PASS.

**Step 3: Verify by breaking — temporarily change the `if err := onFeatureDone(...); err != nil { return ... }` block to ignore the error; watch the test fail (calls == 2). Restore.**

**Step 4: Commit**

```bash
git add internal/analyzer/drift_test.go
git commit -m "test(analyzer): onFeatureDone error aborts further drift processing

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 8: CLI cache helpers — load and save

**Files:**
- Create: `internal/cli/drift_cache.go`
- Create: `internal/cli/drift_cache_test.go`

**Step 1: Write the failing tests**

Create `internal/cli/drift_cache_test.go`:

```go
package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDriftCache_FileNotExist_ReturnsFalse(t *testing.T) {
	_, ok := loadDriftCache(filepath.Join(t.TempDir(), "drift.json"))
	assert.False(t, ok)
}

func TestLoadDriftCache_CorruptJSON_ReturnsFalse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drift.json")
	require.NoError(t, os.WriteFile(path, []byte("not json {{{"), 0o644))
	_, ok := loadDriftCache(path)
	assert.False(t, ok)
}

func TestLoadDriftCache_ReadError_ReturnsFalse(t *testing.T) {
	// Pass a directory; ReadFile returns a non-not-exist error.
	dir := t.TempDir()
	_, ok := loadDriftCache(dir)
	assert.False(t, ok)
}

func TestSaveAndLoadDriftCache_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drift.json")
	in := map[string]analyzer.CachedDriftEntry{
		"auth": {
			Files:  []string{"auth.go", "session.go"},
			Pages:  []string{"https://docs.example.com/auth"},
			Issues: []analyzer.DriftIssue{{Page: "https://docs.example.com/auth", Issue: "Stale signature."}},
		},
		"search": {
			Files:  []string{"search.go"},
			Pages:  []string{"https://docs.example.com/search"},
			Issues: []analyzer.DriftIssue{},
		},
	}
	require.NoError(t, saveDriftCache(path, in))

	got, ok := loadDriftCache(path)
	require.True(t, ok)
	require.Len(t, got, 2)

	assert.Equal(t, in["auth"].Files, got["auth"].Files)
	assert.Equal(t, in["auth"].Pages, got["auth"].Pages)
	assert.Equal(t, in["auth"].Issues, got["auth"].Issues)
	assert.Empty(t, got["search"].Issues, "empty issues array must round-trip as empty (or nil)")
}

func TestSaveDriftCache_EntriesSortedByFeatureName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drift.json")
	in := map[string]analyzer.CachedDriftEntry{
		"zebra": {Files: []string{"z.go"}, Pages: []string{"https://docs.example.com/z"}},
		"alpha": {Files: []string{"a.go"}, Pages: []string{"https://docs.example.com/a"}},
		"mango": {Files: []string{"m.go"}, Pages: []string{"https://docs.example.com/m"}},
	}
	require.NoError(t, saveDriftCache(path, in))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	// Cheap structural check: alpha appears before mango appears before zebra.
	str := string(data)
	iAlpha := indexOf(str, "alpha")
	iMango := indexOf(str, "mango")
	iZebra := indexOf(str, "zebra")
	require.True(t, iAlpha >= 0 && iMango >= 0 && iZebra >= 0)
	assert.Less(t, iAlpha, iMango)
	assert.Less(t, iMango, iZebra)
}

func TestSaveDriftCache_AtomicReplace(t *testing.T) {
	// Save twice; second save must fully replace the first without leaving a tmp file.
	path := filepath.Join(t.TempDir(), "drift.json")
	require.NoError(t, saveDriftCache(path, map[string]analyzer.CachedDriftEntry{
		"first": {Files: []string{"f.go"}, Pages: []string{"https://docs.example.com/f"}},
	}))
	require.NoError(t, saveDriftCache(path, map[string]analyzer.CachedDriftEntry{
		"second": {Files: []string{"s.go"}, Pages: []string{"https://docs.example.com/s"}},
	}))

	got, ok := loadDriftCache(path)
	require.True(t, ok)
	require.Len(t, got, 1)
	_, hasSecond := got["second"]
	assert.True(t, hasSecond)

	// No leftover tmp file in the directory.
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	for _, e := range entries {
		assert.Equal(t, "drift.json", e.Name(), "unexpected leftover file: %s", e.Name())
	}
}

// indexOf is strings.Index inlined to avoid an extra import in the test file.
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

**Step 2: Run the tests, watch them fail**

```bash
go test ./internal/cli/ -run TestLoadDriftCache -v
```

Expected: FAIL with "loadDriftCache undefined" / "saveDriftCache undefined".

**Step 3: Implement the helpers**

Create `internal/cli/drift_cache.go`:

```go
package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// driftCacheFile is the on-disk shape of <projectDir>/drift.json. The Features
// list mirrors the sorted entry keys for quick inspection; lookup itself is
// per-feature against Entries.
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

// loadDriftCache reads a drift cache from path. Returns (nil, false) on
// missing file, parse error, or any I/O error — callers proceed cold on miss.
func loadDriftCache(path string) (map[string]analyzer.CachedDriftEntry, bool) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false
	}
	if err != nil {
		return nil, false
	}
	var f driftCacheFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, false
	}
	out := make(map[string]analyzer.CachedDriftEntry, len(f.Entries))
	for _, e := range f.Entries {
		issues := e.Issues
		if issues == nil {
			issues = []analyzer.DriftIssue{}
		}
		out[e.Feature] = analyzer.CachedDriftEntry{
			Files:  e.Files,
			Pages:  e.Pages,
			Issues: issues,
		}
	}
	return out, true
}

// saveDriftCache writes current to path atomically (temp-file + rename).
// Entries are sorted by feature name for stable diffs.
func saveDriftCache(path string, current map[string]analyzer.CachedDriftEntry) error {
	names := make([]string, 0, len(current))
	for k := range current {
		names = append(names, k)
	}
	sort.Strings(names)

	entries := make([]driftCacheEntry, 0, len(names))
	for _, name := range names {
		c := current[name]
		issues := c.Issues
		if issues == nil {
			issues = []analyzer.DriftIssue{}
		}
		entries = append(entries, driftCacheEntry{
			Feature: name,
			Files:   c.Files,
			Pages:   c.Pages,
			Issues:  issues,
		})
	}
	f := driftCacheFile{Features: names, Entries: entries}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".drift-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}
```

**Step 4: Run the tests, verify they pass**

```bash
gofmt -w . && goimports -w .
go test ./internal/cli/ -run TestLoadDriftCache -v
go test ./internal/cli/ -run TestSaveDriftCache -v
go test ./internal/cli/ -run TestSaveAndLoadDriftCache -v
```

Expected: all PASS.

**Step 5: Commit**

```bash
git add internal/cli/drift_cache.go internal/cli/drift_cache_test.go
git commit -m "$(cat <<'EOF'
feat(cli): add drift_cache load/save helpers with atomic write

- RED: 6 tests covering missing file, corrupt JSON, read error, round-trip,
  sort stability, and atomic replace.
- GREEN: load returns map[name]CachedDriftEntry; save uses temp-file + rename.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: Wire the cache into `analyze.go`

**Files:**
- Modify: `internal/cli/analyze.go` (around the existing drift block, lines 329–352)

**Step 1: Replace the existing drift block**

Find the block in `internal/cli/analyze.go` that runs from `log.Infof("detecting documentation drift...")` to the `log.Debugf("drift detection complete: ...")` line. Replace with:

```go
log.Infof("detecting documentation drift...")
pageReader := func(url string) (string, error) {
	path, ok := idx.FilePath(url)
	if !ok {
		return "", fmt.Errorf("page not in cache: %s", url)
	}
	data, err := os.ReadFile(path)
	return string(data), err
}
docCoveredFeatures := make([]string, 0, len(docsFeatureMap))
for _, entry := range docsFeatureMap {
	if len(entry.Pages) > 0 {
		docCoveredFeatures = append(docCoveredFeatures, entry.Feature)
	}
}
driftOnFinding := func(accumulated []analyzer.DriftFinding) error {
	return reporter.WriteGaps(projectDir, featureMap, docCoveredFeatures, accumulated)
}

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
	if c, ok := cached[name]; ok &&
		stringSliceEqual(c.Files, files) &&
		stringSliceEqual(c.Pages, pages) {
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
if err != nil {
	return fmt.Errorf("detect drift: %w", err)
}
log.Infof("drift cache: %d hits, %d fresh", hits, fresh)
log.Debugf("drift detection complete: %d findings", len(driftFindings))
```

**Step 2: Add the helper at the bottom of `internal/cli/analyze.go`** (or in `drift_cache.go` if you prefer — keep close to its only caller):

```go
// stringSliceEqual reports element-wise equality. Both inputs must already be
// sorted; this is not a set comparison.
func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

**Step 3: Build and run the full test suite**

```bash
gofmt -w . && goimports -w .
go build ./...
go test ./... -count=1
golangci-lint run
```

Expected: build clean, all tests green, lint clean.

**Step 4: Manual smoke test against a real fixture**

You need a small Go repo and a docs URL plus Bifrost credentials. If the test fixture exists in `testdata/fixtures/`, use it; otherwise, this verification step requires the maintainer to run it.

```bash
# Cold run
rm -rf <projectDir>
go run ./cmd/find-the-gaps analyze --repo ./testdata/fixtures/known-good --docs-url <url>

# Should produce <projectDir>/drift.json. Inspect it:
cat <projectDir>/drift.json | head -40

# Re-run; expect "drift cache: N hits, 0 fresh".
go run ./cmd/find-the-gaps analyze --repo ./testdata/fixtures/known-good --docs-url <url>

# Re-run with --no-cache; expect "drift cache: 0 hits, N fresh".
go run ./cmd/find-the-gaps analyze --repo ./testdata/fixtures/known-good --docs-url <url> --no-cache
```

If the smoke test isn't possible in your environment, document that and ask the maintainer to run it before merging.

**Step 5: Commit**

```bash
git add -u internal/cli/analyze.go
git commit -m "$(cat <<'EOF'
feat(cli): wire drift cache into analyze; --no-cache bypasses read

- Loads drift.json at start of drift stage; passes cached map to DetectDrift.
- Persists every completed feature via onFeatureDone (incremental resume).
- Logs "drift cache: H hits, F fresh" at end of stage.
- --no-cache skips the read but still writes a fresh cache for next time.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: Final verification

**Step 1: Full test suite + coverage**

```bash
go test ./... -count=1
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -20
```

Expected: all green; analyzer and cli packages at or above the project's 90% target.

**Step 2: Build and lint**

```bash
go build ./...
golangci-lint run
```

Expected: clean.

**Step 3: Update PROGRESS.md**

Append:

```markdown
## Drift Cache - COMPLETE
- Started: <timestamp>
- Tests: <X> passing, 0 failing
- Coverage: <line%>
- Build: ✅ Successful
- Linting: ✅ Clean
- Completed: <timestamp>
- Notes: Per-feature cache at <projectDir>/drift.json. Set-based invalidation (feature name + sorted files + sorted pages). Resume-from-crash works via incremental persist after every completed feature. --no-cache bypasses the read.
```

**Step 4: Open the PR**

The plan is complete. Open a PR against `main` per `CLAUDE.md`:

```bash
gh pr create --base main --title "feat(analyzer): resumable drift detection via per-feature cache" --body "$(cat <<'EOF'
## Summary
- Per-feature drift cache at \`<projectDir>/drift.json\` makes \`analyze\` resumable across the drift stage.
- \`DetectDrift\` gains an optional cached lookup map and an \`onFeatureDone\` callback.
- CLI persists incrementally after every completed feature.
- \`--no-cache\` bypasses the read but still rebuilds the file.

## Design
See \`.plans/2026-04-29-drift-cache-design.md\`.

## Test plan
- [x] Unit tests for cache hit / miss-by-files / miss-by-pages / nil cache.
- [x] Unit tests for onFeatureDone fires across fresh, empty, cached features.
- [x] Unit tests for load/save/atomic-replace/sort-stability.
- [ ] Manual smoke: cold run → re-run shows all hits → \`--no-cache\` shows all fresh.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

Note: per CLAUDE.md, this PR uses a **merge commit** (not squash) and the description should reference closing any related issue if applicable.

---

## Out of scope (for clarity, do NOT do these now)

- Caching page-classification (small-tier per-page LLM calls). Revisit if profiles complain.
- Mid-feature resume (partial observation persistence).
- Content-hash-based invalidation. Set-based is by design; content edits use `--no-cache`.
- A `find-the-gaps cache clear` subcommand.
- Caching the screenshots stage.
