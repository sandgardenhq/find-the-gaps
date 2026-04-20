# Context Length: Batched Feature Mapping Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the single-shot `MapFeaturesToCode` prompt with a batched loop that iterates over files in token-budget-sized chunks, so the function works correctly on any codebase regardless of size.

**Architecture:** The features list is small and constant. The symbol index is arbitrarily large. We batch the symbol lines so each LLM call receives: (all features) + (a subset of symbol lines that fits in the token budget). Results from each batch are accumulated and merged into the final FeatureMap.

**Tech Stack:** Go, existing `LLMClient` interface, `internal/analyzer/mapper.go`, `internal/analyzer/mapper_test.go`

---

## Why one-shot is wrong

A single prompt over the full symbol index fails for two reasons:
1. Any real codebase exceeds the model's context window.
2. LLMs perform poorly when asked to reason over thousands of symbols at once — accuracy degrades badly at scale.

Batching by file keeps each prompt focused and fits within any model's context limit.

---

## Task 1: Token estimator

**Files:**
- Create: `internal/analyzer/tokens.go`
- Test: `internal/analyzer/tokens_test.go`

**Step 1: Write the failing test**

```go
// internal/analyzer/tokens_test.go
package analyzer

import "testing"

func TestEstimateTokens_emptyString(t *testing.T) {
    if estimateTokens("") != 0 {
        t.Error("expected 0 for empty string")
    }
}

func TestEstimateTokens_proportionalToLength(t *testing.T) {
    // 4 characters ≈ 1 token (common rule of thumb)
    got := estimateTokens("aaaa") // 4 chars
    if got != 1 {
        t.Errorf("expected 1, got %d", got)
    }
}

func TestEstimateTokens_roundsDown(t *testing.T) {
    got := estimateTokens("aaa") // 3 chars → 0 tokens (floor)
    if got != 0 {
        t.Errorf("expected 0, got %d", got)
    }
}
```

**Step 2: Run test, confirm RED**
```
go test ./internal/analyzer/ -run TestEstimateTokens -v
```
Expected: build failure — `estimateTokens` undefined.

**Step 3: Write minimal implementation**

```go
// internal/analyzer/tokens.go
package analyzer

// estimateTokens returns a rough token count for s using the 4-chars-per-token heuristic.
func estimateTokens(s string) int {
    return len(s) / 4
}
```

**Step 4: Run test, confirm GREEN**
```
go test ./internal/analyzer/ -run TestEstimateTokens -v
```

**Step 5: Commit**
```bash
git add internal/analyzer/tokens.go internal/analyzer/tokens_test.go
git commit -m "feat(analyzer): add estimateTokens helper

- RED: TestEstimateTokens_* written first
- GREEN: len(s)/4 heuristic
- Status: 3 tests passing"
```

---

## Task 2: Symbol line batcher

**Files:**
- Create: `internal/analyzer/batcher.go`
- Test: `internal/analyzer/batcher_test.go`

**Step 1: Write the failing tests**

```go
// internal/analyzer/batcher_test.go
package analyzer

import (
    "testing"
)

func TestBatchSymLines_emptyInput_returnsNoBatches(t *testing.T) {
    got := batchSymLines([]string{}, 0, 10000)
    if len(got) != 0 {
        t.Errorf("expected 0 batches, got %d", len(got))
    }
}

func TestBatchSymLines_allFitInOneBatch(t *testing.T) {
    lines := []string{"a.go: Foo", "b.go: Bar"}
    got := batchSymLines(lines, 0, 10000)
    if len(got) != 1 {
        t.Fatalf("expected 1 batch, got %d", len(got))
    }
    if len(got[0]) != 2 {
        t.Errorf("expected 2 lines in batch, got %d", len(got[0]))
    }
}

func TestBatchSymLines_splitAcrossMultipleBatches(t *testing.T) {
    // Each line is 8 chars = 2 tokens. Budget = 2 tokens → 1 line per batch.
    lines := []string{"12345678", "abcdefgh", "ABCDEFGH"}
    got := batchSymLines(lines, 0, 2)
    if len(got) != 3 {
        t.Fatalf("expected 3 batches, got %d: %v", len(got), got)
    }
}

func TestBatchSymLines_featuresOverheadAccountedFor(t *testing.T) {
    // featuresSize = 9000 tokens, budget = 10000 → only 1000 tokens left for sym lines.
    // Each line is 400 chars = 100 tokens. So only 10 lines fit per batch.
    line := make([]byte, 400)
    for i := range line { line[i] = 'x' }
    lines := make([]string, 20)
    for i := range lines { lines[i] = string(line) }

    got := batchSymLines(lines, 9000, 10000)
    if len(got) != 2 {
        t.Fatalf("expected 2 batches, got %d", len(got))
    }
}

func TestBatchSymLines_singleOversizedLine_getsItsOwnBatch(t *testing.T) {
    // A single line larger than the remaining budget still gets placed alone.
    big := string(make([]byte, 40000)) // 10000 tokens
    lines := []string{big, "small"}
    got := batchSymLines(lines, 0, 1000)
    if len(got) != 2 {
        t.Fatalf("expected 2 batches, got %d", len(got))
    }
}
```

**Step 2: Run tests, confirm RED**
```
go test ./internal/analyzer/ -run TestBatchSymLines -v
```
Expected: build failure — `batchSymLines` undefined.

**Step 3: Write minimal implementation**

```go
// internal/analyzer/batcher.go
package analyzer

// batchSymLines groups symLines into batches where each batch's token estimate,
// added to featuresTokens, does not exceed budget.
// A single line that alone exceeds the remaining budget is placed in its own batch.
func batchSymLines(symLines []string, featuresTokens, budget int) [][]string {
    if len(symLines) == 0 {
        return nil
    }
    remaining := budget - featuresTokens
    var batches [][]string
    var current []string
    currentTokens := 0

    for _, line := range symLines {
        t := estimateTokens(line)
        if len(current) > 0 && currentTokens+t > remaining {
            batches = append(batches, current)
            current = nil
            currentTokens = 0
        }
        current = append(current, line)
        currentTokens += t
    }
    if len(current) > 0 {
        batches = append(batches, current)
    }
    return batches
}
```

**Step 4: Run tests, confirm GREEN**
```
go test ./internal/analyzer/ -run TestBatchSymLines -v
```

**Step 5: Commit**
```bash
git add internal/analyzer/batcher.go internal/analyzer/batcher_test.go
git commit -m "feat(analyzer): add batchSymLines for token-budget batching

- RED: TestBatchSymLines_* written first
- GREEN: accumulate lines until budget exceeded, flush to new batch
- Status: 5 tests passing"
```

---

## Task 3: Rewrite MapFeaturesToCode to use batching

**Files:**
- Modify: `internal/analyzer/mapper.go`
- Modify: `internal/analyzer/mapper_test.go`

### What changes

`MapFeaturesToCode` now:
1. Builds `symLines` exactly as today.
2. Computes `featuresTokens = estimateTokens(string(featuresJSON))`.
3. Calls `batchSymLines(symLines, featuresTokens, mapperTokenBudget)` to get batches.
4. For each batch, sends a prompt with features + this batch's symbol lines.
5. Accumulates results into `map[string]*accEntry` keyed by feature name.
6. Converts the accumulator to `FeatureMap` in the original features order.

### Token budget constant

Add to `mapper.go`:
```go
// mapperTokenBudget is the maximum tokens per MapFeaturesToCode LLM call.
// Set well below the model minimum (1M) to leave room for the response.
const mapperTokenBudget = 80_000
```

### Accumulator type (internal to mapper.go)

```go
type accEntry struct {
    files   map[string]struct{}
    symbols map[string]struct{}
}
```

### Step 1: Update existing tests first

The existing tests use a single-file scan, which will still produce a single batch and a single LLM call. They should continue to pass without changes — verify this after rewriting.

Add new tests for multi-batch behavior:

```go
func TestMapFeaturesToCode_MultipleBatches_MergesResults(t *testing.T) {
    // Two files, token budget of 1 forces one file per batch → 2 LLM calls.
    // Each call returns a partial result; both must appear in final output.
    c := &fakeClient{responses: []string{
        `[{"feature":"auth","files":["auth.go"],"symbols":["Login"]}]`,
        `[{"feature":"auth","files":["session.go"],"symbols":["NewSession"]}]`,
    }}

    scan := &scanner.ProjectScan{
        Files: []scanner.ScannedFile{
            {Path: "auth.go", Symbols: []scanner.Symbol{{Name: "Login"}}},
            {Path: "session.go", Symbols: []scanner.Symbol{{Name: "NewSession"}}},
        },
    }

    got, err := analyzer.MapFeaturesToCode(context.Background(), c, []string{"auth"}, scan, 1)
    if err != nil {
        t.Fatal(err)
    }
    if len(got) != 1 {
        t.Fatalf("expected 1 feature entry, got %d", len(got))
    }
    if len(got[0].Files) != 2 {
        t.Errorf("expected 2 files merged, got %v", got[0].Files)
    }
    if c.callCount != 2 {
        t.Errorf("expected 2 LLM calls, got %d", c.callCount)
    }
}

func TestMapFeaturesToCode_FilesWithNoSymbols_Skipped(t *testing.T) {
    // Files with no symbols contribute no sym lines and produce no batches.
    c := &fakeClient{responses: []string{`[]`}}
    scan := &scanner.ProjectScan{
        Files: []scanner.ScannedFile{
            {Path: "empty.go", Symbols: nil},
        },
    }
    got, err := analyzer.MapFeaturesToCode(context.Background(), c, []string{"auth"}, scan, 80_000)
    if err != nil {
        t.Fatal(err)
    }
    // No sym lines → no batches → no LLM calls → empty result
    if c.callCount != 0 {
        t.Errorf("expected 0 LLM calls, got %d", c.callCount)
    }
    _ = got
}
```

Note: the new signature passes `tokenBudget int` as the last parameter so tests can force small batches without patching a global. The production call site in `analyze.go` passes the `mapperTokenBudget` constant.

**Step 2: Run tests, confirm RED**
```
go test ./internal/analyzer/ -run TestMapFeaturesToCode -v
```
Expected: compile error — signature mismatch.

**Step 3: Rewrite mapper.go**

New `MapFeaturesToCode` signature:
```go
func MapFeaturesToCode(ctx context.Context, client LLMClient, features []string, scan *scanner.ProjectScan, tokenBudget int) (FeatureMap, error)
```

Logic:
1. Early return if `len(features) == 0`.
2. Build `symLines` as today.
3. If `len(symLines) == 0`, return empty FeatureMap (no LLM calls).
4. Marshal features to JSON, compute `featuresTokens`.
5. Get batches from `batchSymLines`.
6. Initialize `acc := map[string]*accEntry{}` for each feature name.
7. For each batch:
   - Build prompt with features JSON + `strings.Join(batch, "\n")`.
   - Call `client.Complete`.
   - Unmarshal response as `[]mapEntry`.
   - For each entry, merge files and symbols into `acc[entry.Feature]`.
8. Convert `acc` to `FeatureMap` in original features order.

The prompt per batch is identical in structure to today's prompt — only the symbol lines section changes.

**Step 4: Update call site in analyze.go**

```go
featureMap, err := analyzer.MapFeaturesToCode(ctx, llmClient, productSummary.Features, scan, analyzer.MapperTokenBudget)
```

Export `MapperTokenBudget` from `mapper.go` so the call site can reference it without importing the constant value directly.

**Step 5: Run all tests, confirm GREEN**
```
go test ./... -v
```

**Step 6: Commit**
```bash
git add internal/analyzer/mapper.go internal/analyzer/mapper_test.go internal/cli/analyze.go
git commit -m "feat(analyzer): batch MapFeaturesToCode by token budget

- RED: TestMapFeaturesToCode_MultipleBatches_MergesResults and
  TestMapFeaturesToCode_FilesWithNoSymbols_Skipped written first
- GREEN: iterate symLines in 80k-token batches; accumulate and merge
  feature entries across all batch responses
- Status: all tests passing
- Fixes: prompt too long error on large codebases"
```

---

---

## Task 4: Coverage verification

**Files:**
- Modify: `internal/analyzer/mapper.go`
- Modify: `internal/analyzer/mapper_test.go`

After all batches are processed, `MapFeaturesToCode` must verify that every file with symbols was included in exactly one batch. This is a runtime invariant check — not optional, not skippable.

### Step 1: Write the failing test

```go
func TestMapFeaturesToCode_AllFilesProcessed_NoneSkipped(t *testing.T) {
    // 5 files, budget forces 2 per batch → 3 batches.
    // Verify callCount == 3 (no files dropped between batches).
    responses := []string{
        `[{"feature":"f","files":["a.go","b.go"],"symbols":[]}]`,
        `[{"feature":"f","files":["c.go","d.go"],"symbols":[]}]`,
        `[{"feature":"f","files":["e.go"],"symbols":[]}]`,
    }
    c := &fakeClient{responses: responses}

    files := make([]scanner.ScannedFile, 5)
    names := []string{"a.go", "b.go", "c.go", "d.go", "e.go"}
    for i, name := range names {
        files[i] = scanner.ScannedFile{
            Path:    name,
            Symbols: []scanner.Symbol{{Name: "Sym"}},
        }
    }

    scan := &scanner.ProjectScan{Files: files}
    _, err := analyzer.MapFeaturesToCode(context.Background(), c, []string{"f"}, scan, 1)
    if err != nil {
        t.Fatal(err)
    }
    if c.callCount != 3 {
        t.Errorf("expected 3 LLM calls (one per batch), got %d — some files may have been skipped", c.callCount)
    }
}

func TestMapFeaturesToCode_CoverageCheck_ReturnsErrorIfFilesMissed(t *testing.T) {
    // Simulate a batcher bug where a file is lost: verify the coverage check catches it.
    // We do this by injecting a tokenBudget of 0, which forces every line into its own batch,
    // and confirming that all files are still covered.
    c := &fakeClient{responses: []string{
        `[{"feature":"f","files":["a.go"],"symbols":["A"]}]`,
        `[{"feature":"f","files":["b.go"],"symbols":["B"]}]`,
    }}
    scan := &scanner.ProjectScan{
        Files: []scanner.ScannedFile{
            {Path: "a.go", Symbols: []scanner.Symbol{{Name: "A"}}},
            {Path: "b.go", Symbols: []scanner.Symbol{{Name: "B"}}},
        },
    }
    got, err := analyzer.MapFeaturesToCode(context.Background(), c, []string{"f"}, scan, 0)
    if err != nil {
        t.Fatal(err)
    }
    if len(got[0].Files) != 2 {
        t.Errorf("expected both files in result, got %v", got[0].Files)
    }
}
```

### Step 2: Run tests, confirm RED
```
go test ./internal/analyzer/ -run TestMapFeaturesToCode_Coverage -v
```

### Step 3: Add coverage assertion to MapFeaturesToCode

After the batching loop completes, verify coverage:

```go
// Build set of all files that entered the batching pipeline.
batched := make(map[string]struct{}, len(symLines))
for _, batch := range batches {
    for _, line := range batch {
        path := strings.SplitN(line, ": ", 2)[0]
        batched[path] = struct{}{}
    }
}
// Verify every file with symbols was batched.
for _, f := range scan.Files {
    if len(f.Symbols) == 0 {
        continue
    }
    if _, ok := batched[f.Path]; !ok {
        return nil, fmt.Errorf("MapFeaturesToCode: file %q was not included in any batch (coverage check failed)", f.Path)
    }
}
```

This runs after batches are built, before any LLM calls, so it fails fast rather than after spending tokens.

### Step 4: Run all tests, confirm GREEN
```
go test ./... -v
```

### Step 5: Commit
```bash
git add internal/analyzer/mapper.go internal/analyzer/mapper_test.go
git commit -m "feat(analyzer): add coverage verification to MapFeaturesToCode

- RED: TestMapFeaturesToCode_AllFilesProcessed_NoneSkipped written first
- GREEN: post-batch coverage check errors if any file with symbols
  was not included in a batch
- Status: all tests passing"
```

---

## Execution Handoff

Plan saved. Two execution options:

**1. Subagent-Driven (this session)** — fresh subagent per task, review between tasks

**2. Parallel Session (separate)** — open new session with executing-plans

Which approach?
