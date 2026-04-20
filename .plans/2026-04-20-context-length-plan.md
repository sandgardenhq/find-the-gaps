# Context Length: Batched Feature Mapping Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the single-shot `MapFeaturesToCode` prompt with a batched loop that iterates over files in token-budget-sized chunks, so the function works correctly on any codebase regardless of size.

**Architecture:** The features list is small and constant. The symbol index is arbitrarily large. We batch the symbol lines so each LLM call receives: (all features) + (a subset of symbol lines that fits in the token budget). Results from each batch are accumulated and merged into the final FeatureMap.

**Tech Stack:** Go, existing `LLMClient` interface, `internal/analyzer/mapper.go`, `internal/analyzer/mapper_test.go`, `github.com/tiktoken-go/tokenizer` (local BPE, for OpenAI/Ollama and internal batcher estimates), `github.com/anthropics/anthropic-sdk-go` (for Anthropic exact token counts via API)

---

## Why one-shot is wrong

A single prompt over the full symbol index fails for two reasons:
1. Any real codebase exceeds the model's context window.
2. LLMs perform poorly when asked to reason over thousands of symbols at once — accuracy degrades badly at scale.

Batching by file keeps each prompt focused and fits within any model's context limit.

---

## Task 1: Provider-specific TokenCounter

**Files:**
- Create: `internal/analyzer/tokens.go`
- Test: `internal/analyzer/tokens_test.go`

**Design:**

`TokenCounter` is a public interface used by `MapFeaturesToCode` to validate that an assembled batch prompt fits within the model's context window before sending it. Provider-specific because Anthropic, OpenAI, and Ollama tokenize differently.

Two implementations:
- `TiktokenCounter` — local cl100k_base BPE, zero network calls. Used for OpenAI and Ollama.
- `AnthropicCounter` — calls `POST /v1/messages/count_tokens` via the official Go SDK. Exact counts for Claude.

`tokens.go` also defines an unexported `countTokens(s string) int` helper (tiktoken, no context) used by the batcher for fast initial batch sizing estimates. This is separate from the `TokenCounter` interface, which handles full-prompt validation.

**Step 1: Add dependencies**
```bash
go get github.com/tiktoken-go/tokenizer
go get github.com/anthropics/anthropic-sdk-go
```

**Step 2: Write the failing tests**

```go
// internal/analyzer/tokens_test.go
package analyzer_test

import (
    "context"
    "testing"

    "github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestTiktokenCounter_emptyString_returnsZero(t *testing.T) {
    c := analyzer.NewTiktokenCounter()
    n, err := c.CountTokens(context.Background(), "")
    if err != nil {
        t.Fatal(err)
    }
    if n != 0 {
        t.Errorf("expected 0, got %d", n)
    }
}

func TestTiktokenCounter_nonEmptyString_returnsPositive(t *testing.T) {
    c := analyzer.NewTiktokenCounter()
    n, err := c.CountTokens(context.Background(), "hello world")
    if err != nil {
        t.Fatal(err)
    }
    if n <= 0 {
        t.Errorf("expected positive token count, got %d", n)
    }
}

func TestTiktokenCounter_longerString_moreTokens(t *testing.T) {
    c := analyzer.NewTiktokenCounter()
    short, _ := c.CountTokens(context.Background(), "hello")
    long, _ := c.CountTokens(context.Background(), "hello world this is a longer sentence with many words")
    if long <= short {
        t.Errorf("expected longer string to have more tokens: short=%d long=%d", short, long)
    }
}
```

Note: `AnthropicCounter` requires a live API key and makes network calls — it is covered by integration tests only, not here.

**Step 3: Run tests, confirm RED**
```
go test ./internal/analyzer/ -run TestTiktokenCounter -v
```
Expected: build failure — `analyzer.NewTiktokenCounter` undefined.

**Step 4: Write implementation**

```go
// internal/analyzer/tokens.go
package analyzer

import (
    "context"
    "fmt"

    "github.com/anthropics/anthropic-sdk-go"
    "github.com/tiktoken-go/tokenizer"
)

// TokenCounter counts the tokens in a text string for a specific LLM provider.
// Used by MapFeaturesToCode to validate batch prompts before sending.
type TokenCounter interface {
    CountTokens(ctx context.Context, text string) (int, error)
}

var defaultEnc = mustGetEncoder()

func mustGetEncoder() tokenizer.Codec {
    enc, err := tokenizer.Get(tokenizer.Cl100kBase)
    if err != nil {
        panic("tiktoken: failed to load cl100k_base: " + err.Error())
    }
    return enc
}

// countTokens is a fast package-private estimator using embedded cl100k_base.
// Used by batchSymLines for initial sizing only — no network calls.
func countTokens(s string) int {
    if s == "" {
        return 0
    }
    ids, _, _ := defaultEnc.Encode(s)
    return len(ids)
}

// tiktokenCounter implements TokenCounter using the local cl100k_base vocabulary.
type tiktokenCounter struct{}

// NewTiktokenCounter returns a TokenCounter backed by the embedded cl100k_base vocabulary.
// Use this for OpenAI and Ollama providers.
func NewTiktokenCounter() TokenCounter { return &tiktokenCounter{} }

func (c *tiktokenCounter) CountTokens(_ context.Context, text string) (int, error) {
    return countTokens(text), nil
}

// anthropicCounter implements TokenCounter using the Anthropic token counting API.
type anthropicCounter struct {
    client *anthropic.Client
    model  string
}

// NewAnthropicCounter returns a TokenCounter that calls POST /v1/messages/count_tokens.
// Gives exact token counts for Claude models.
func NewAnthropicCounter(apiKey, model string) TokenCounter {
    client := anthropic.NewClient(anthropic.WithAPIKey(apiKey))
    return &anthropicCounter{client: &client, model: model}
}

func (c *anthropicCounter) CountTokens(ctx context.Context, text string) (int, error) {
    resp, err := c.client.Messages.CountTokens(ctx, anthropic.MessageCountTokensParams{
        Model: anthropic.Model(c.model),
        Messages: []anthropic.MessageParam{
            anthropic.NewUserMessage(anthropic.NewTextBlock(text)),
        },
    })
    if err != nil {
        return 0, fmt.Errorf("anthropic count tokens: %w", err)
    }
    return int(resp.InputTokens), nil
}
```

**Step 5: Run tests, confirm GREEN**
```
go test ./internal/analyzer/ -run TestTiktokenCounter -v
```

**Step 6: Commit**
```bash
git add internal/analyzer/tokens.go internal/analyzer/tokens_test.go go.mod go.sum
git commit -m "feat(analyzer): add TokenCounter interface with tiktoken and Anthropic implementations

- RED: TestTiktokenCounter_* written first
- GREEN: local tiktoken counter + Anthropic API counter
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
    // budget=0 means remaining=0, so any line with ≥1 token flushes the current batch.
    // Every non-empty line gets its own batch regardless of actual token count.
    lines := []string{"alpha bravo", "charlie delta", "echo foxtrot"}
    got := batchSymLines(lines, 0, 0)
    if len(got) != 3 {
        t.Fatalf("expected 3 batches, got %d: %v", len(got), got)
    }
}

func TestBatchSymLines_featuresOverheadAccountedFor(t *testing.T) {
    // When featuresTokens == budget, remaining == 0, so each line gets its own batch.
    // This verifies the featuresTokens parameter is actually subtracted from the budget.
    lines := []string{"alpha", "bravo", "charlie"}
    budget := 10000
    got := batchSymLines(lines, budget, budget) // featuresTokens consumes entire budget
    if len(got) != 3 {
        t.Fatalf("expected 3 batches (1 line each), got %d", len(got))
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
        t := countTokens(line)
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
2. Computes `featuresTokens = countTokens(string(featuresJSON))` (local tiktoken estimate).
3. Calls `batchSymLines(symLines, featuresTokens, tokenBudget)` to get initial batches (tiktoken estimates).
4. For each batch:
   a. Assembles the full prompt text (features + batch sym lines).
   b. Calls `counter.CountTokens(ctx, promptText)` to get the provider-exact token count.
   c. If the count exceeds `tokenBudget` AND the batch has more than one line, splits the batch in half and retries from step 4a with each half.
   d. Once the prompt fits, sends it to the LLM.
5. Accumulates results into `map[string]*accEntry` keyed by feature name.
6. Converts the accumulator to `FeatureMap` in the original features order.

The split-and-retry in step 4c is O(log N) per oversized batch. For Anthropic, it means O(batches) API calls for token counting, not O(sym lines). For OpenAI/Ollama, `TiktokenCounter` returns immediately with no network calls, so tiktoken estimates and exact counts agree and splitting rarely occurs.

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

The existing tests must be updated to compile after the signature change. In `mapper_test.go`:
- All existing calls to `analyzer.MapFeaturesToCode(...)` currently pass 4 arguments.
- Add `analyzer.NewTiktokenCounter()` as the 3rd argument and `analyzer.MapperTokenBudget` as the 6th.
- Without this, the package will not compile and no tests will run.

Add a `fakeCounter` to `mapper_test.go` for tests that need budget-based splitting:

```go
// fakeCounter returns a fixed token count for every input, regardless of content.
type fakeCounter struct{ n int }
func (f *fakeCounter) CountTokens(_ context.Context, _ string) (int, error) { return f.n, nil }
```

Add new tests for multi-batch behavior:

```go
func TestMapFeaturesToCode_MultipleBatches_MergesResults(t *testing.T) {
    // budget=1 forces one sym line per batch (batchSymLines), and fakeCounter always
    // returns 0 so no split-and-retry occurs. Result: 2 batches, 2 LLM calls.
    c := &fakeClient{responses: []string{
        `[{"feature":"auth","files":["auth.go"],"symbols":["Login"]}]`,
        `[{"feature":"auth","files":["session.go"],"symbols":["NewSession"]}]`,
    }}
    counter := &fakeCounter{n: 0} // always fits, no retry

    scan := &scanner.ProjectScan{
        Files: []scanner.ScannedFile{
            {Path: "auth.go", Symbols: []scanner.Symbol{{Name: "Login"}}},
            {Path: "session.go", Symbols: []scanner.Symbol{{Name: "NewSession"}}},
        },
    }

    got, err := analyzer.MapFeaturesToCode(context.Background(), c, counter, []string{"auth"}, scan, 1)
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

func TestMapFeaturesToCode_CounterOverBudget_SplitsBatch(t *testing.T) {
    // fakeCounter returns a count over budget, forcing the mapper to split every
    // 2-line batch into 1-line batches. Verifies split-and-retry logic.
    c := &fakeClient{responses: []string{
        `[{"feature":"auth","files":["auth.go"],"symbols":["Login"]}]`,
        `[{"feature":"auth","files":["session.go"],"symbols":["NewSession"]}]`,
    }}
    // Counter always says "over budget" — but the batcher already put 2 lines per batch.
    // The mapper must split them into 1-line batches and retry.
    // We use a counter that returns 999999 (always over) until the batch is 1 line.
    counter := &splitForcingCounter{budget: 80_000}

    scan := &scanner.ProjectScan{
        Files: []scanner.ScannedFile{
            {Path: "auth.go", Symbols: []scanner.Symbol{{Name: "Login"}}},
            {Path: "session.go", Symbols: []scanner.Symbol{{Name: "NewSession"}}},
        },
    }

    got, err := analyzer.MapFeaturesToCode(context.Background(), c, counter, []string{"auth"}, scan, 80_000)
    if err != nil {
        t.Fatal(err)
    }
    if len(got[0].Files) != 2 {
        t.Errorf("expected 2 files merged, got %v", got[0].Files)
    }
    if c.callCount != 2 {
        t.Errorf("expected 2 LLM calls after forced split, got %d", c.callCount)
    }
}

// splitForcingCounter returns over-budget when the input has more than ~50 chars,
// simulating a provider that reports a large batch as too long.
type splitForcingCounter struct{ budget int }
func (s *splitForcingCounter) CountTokens(_ context.Context, text string) (int, error) {
    if len(text) > 50 {
        return s.budget + 1, nil // over budget → triggers split
    }
    return 1, nil // single-line prompt always fits
}

func TestMapFeaturesToCode_FilesWithNoSymbols_Skipped(t *testing.T) {
    // Files with no symbols contribute no sym lines and produce no batches.
    c := &fakeClient{responses: []string{`[]`}}
    scan := &scanner.ProjectScan{
        Files: []scanner.ScannedFile{
            {Path: "empty.go", Symbols: nil},
        },
    }
    got, err := analyzer.MapFeaturesToCode(context.Background(), c, analyzer.NewTiktokenCounter(), []string{"auth"}, scan, 80_000)
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

Note: the production call site in `analyze.go` passes the right `TokenCounter` for the configured provider.

**Step 2: Run tests, confirm RED**
```
go test ./internal/analyzer/ -run TestMapFeaturesToCode -v
```
Expected: compile error — signature mismatch.

**Step 3: Rewrite mapper.go**

New `MapFeaturesToCode` signature:
```go
func MapFeaturesToCode(ctx context.Context, client LLMClient, counter TokenCounter, features []string, scan *scanner.ProjectScan, tokenBudget int) (FeatureMap, error)
```

Logic:
1. Early return if `len(features) == 0`.
2. Build `symLines` as today.
3. If `len(symLines) == 0`, return empty FeatureMap (no LLM calls).
4. Marshal features to JSON, compute `featuresTokens = countTokens(string(featuresJSON))`.
5. Get initial batches from `batchSymLines(symLines, featuresTokens, tokenBudget)`.
6. Initialize `acc := map[string]*accEntry{}` for each feature name.
7. For each batch (use an index-based loop, not range, since batches may grow):
   - Build `promptText` with features JSON + `strings.Join(batch, "\n")`.
   - Call `counter.CountTokens(ctx, promptText)` to get the provider-exact count.
   - If count > tokenBudget AND len(batch) > 1: split batch in half, insert both halves back into the queue, continue.
   - Call `client.Complete(ctx, promptText)`.
   - Unmarshal response as `[]mapEntry`.
   - For each entry, merge files and symbols into `acc[entry.Feature]`.
8. Convert `acc` to `FeatureMap` in original features order.

The prompt per batch is identical in structure to today's prompt — only the symbol lines section changes.

**Step 4: Update call site in analyze.go**

Create the right `TokenCounter` for the configured provider:

```go
var counter analyzer.TokenCounter
switch cfg.LLMProvider {
case "anthropic":
    counter = analyzer.NewAnthropicCounter(cfg.LLMAPIKey, cfg.LLMModel)
default:
    counter = analyzer.NewTiktokenCounter() // OpenAI, Ollama
}

featureMap, err := analyzer.MapFeaturesToCode(ctx, llmClient, counter, productSummary.Features, scan, analyzer.MapperTokenBudget)
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
    // budget=1 is below any real featuresTokens, so remaining is negative and every
    // sym line lands in its own batch → 5 files = 5 LLM calls.
    // This verifies the batcher processes every file regardless of budget pressure.
    responses := []string{
        `[{"feature":"f","files":["a.go"],"symbols":[]}]`,
        `[{"feature":"f","files":["b.go"],"symbols":[]}]`,
        `[{"feature":"f","files":["c.go"],"symbols":[]}]`,
        `[{"feature":"f","files":["d.go"],"symbols":[]}]`,
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
    counter := &fakeCounter{n: 0} // always fits, no split-and-retry
    _, err := analyzer.MapFeaturesToCode(context.Background(), c, counter, []string{"f"}, scan, 1)
    if err != nil {
        t.Fatal(err)
    }
    if c.callCount != 5 {
        t.Errorf("expected 5 LLM calls (one per file), got %d — some files may have been skipped", c.callCount)
    }
}

func TestMapFeaturesToCode_TinyBudget_AllFilesStillCovered(t *testing.T) {
    // With budget=0, remaining goes negative and every line lands in its own batch.
    // Verifies that even with extreme fragmentation, all files appear in the merged result.
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
    counter := &fakeCounter{n: 0} // always fits
    got, err := analyzer.MapFeaturesToCode(context.Background(), c, counter, []string{"f"}, scan, 0)
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
go test ./internal/analyzer/ -run "TestMapFeaturesToCode_AllFilesProcessed|TestMapFeaturesToCode_TinyBudget" -v
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

- RED: TestMapFeaturesToCode_AllFilesProcessed_NoneSkipped,
  TestMapFeaturesToCode_TinyBudget_AllFilesStillCovered written first
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
