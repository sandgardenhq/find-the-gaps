# Documentation Drift Detection — Design

**Date:** 2026-04-22

## Problem

The tool currently identifies features that are undocumented (in code but not in docs) or unmapped (in docs but not in code). It does not detect cases where a feature IS documented but the documentation is inaccurate, outdated, or incomplete relative to what the code actually does.

Examples of drift the tool currently misses:
- A parameter added to a function but not mentioned in the docs
- A feature removed from code but still described on a doc page
- Incorrect behavior described (wrong defaults, wrong constraints)
- Any other misleading or stale content

## Goal

Add a drift detection pass that compares each documented feature's code implementation against its documentation pages and surfaces specific inaccuracies expressed as documentation feedback — not code diagnostics.

---

## Design

### 1. New types (`internal/analyzer/types.go`)

```go
// DriftIssue is one specific inaccuracy found between a feature's code and its documentation.
type DriftIssue struct {
    Page  string // URL of the doc page the issue is on ("" if cross-page)
    Issue string // description of the inaccuracy in documentation language
}

// DriftFinding groups all drift issues found for one feature.
type DriftFinding struct {
    Feature string
    Issues  []DriftIssue
}
```

Findings are grouped by feature. Each issue optionally names the specific page it came from so the reporter can link back to it.

---

### 2. New `ToolLLMClient` interface (`internal/analyzer/client.go`)

```go
// ChatMessage is a single turn in a tool-use conversation.
type ChatMessage struct {
    Role       string     // "user", "assistant", "tool"
    Content    string
    ToolCalls  []ToolCall // populated when Role == "assistant" and LLM is calling tools
    ToolCallID string     // populated when Role == "tool"
}

// Tool defines a callable function the LLM may invoke.
type Tool struct {
    Name        string
    Description string
    Parameters  map[string]any // JSON Schema object
}

// ToolCall is one invocation requested by the LLM.
type ToolCall struct {
    ID        string
    Name      string
    Arguments string // raw JSON
}

// ToolLLMClient extends LLMClient with a tool-use conversation method.
type ToolLLMClient interface {
    LLMClient
    CompleteWithTools(ctx context.Context, messages []ChatMessage, tools []Tool) (ChatMessage, error)
}
```

`BifrostClient` implements `ToolLLMClient` by mapping these types to the Bifrost SDK's `ChatCompletionRequest` with tool definitions. Existing `Complete` callers are unaffected.

---

### 3. `DetectDrift` function (`internal/analyzer/drift.go`)

```go
func DetectDrift(
    ctx context.Context,
    client ToolLLMClient,
    featureMap FeatureMap,
    docsMap DocsFeatureMap,
    pageIndex *spider.Index, // spider cache index — URL → on-disk .md file
    repoRoot string,
) ([]DriftFinding, error)
```

**Algorithm:**

For each `FeatureEntry` in `featureMap` that has at least one file AND appears in `docsMap` with at least one page:

1. Collect matching page URLs and their summaries (from `PageAnalysis`) for the initial prompt context.
2. Run an agent loop (capped at 8 rounds):
   - Send current conversation + two tool definitions to `CompleteWithTools`
   - If the LLM returns tool calls: execute each, append results, continue loop
   - If the LLM returns a JSON array: parse as `[]DriftIssue`, stop loop
3. Drop features with zero issues.

**Two tools available to the LLM:**

- **`read_file`** — takes `{"path": "repo-relative/path.go"}`, returns file source content. Constrained to `repoRoot`; paths that escape are rejected with an error message returned to the LLM.
- **`read_page`** — takes `{"url": "https://..."}`, looks up the URL in `pageIndex` via `pageIndex.FilePath(url)`, and reads the cached `.md` file from disk. No HTTP calls. If the URL is not in the index, returns a "page not available" message to the LLM.

#### LLM Prompt

```
You are reviewing documentation accuracy for a software feature.

Feature: <name>
Code description: <description>
Implemented in: <file1>, <file2>, ...
Symbols: <symbol1>, <symbol2>, ...

Documentation pages:
- <url1>: <page1 summary>
- <url2>: <page2 summary>

You have tools available to read source files and documentation pages in full.
Use them to investigate as needed before producing your findings.

Identify specific inaccuracies, missing information, or outdated content in the
documentation relative to what the code actually does. This includes:
- Features or behaviors documented but no longer present in code
- Parameters, fields, or requirements not mentioned in docs
- Incorrect descriptions of how something works
- Any other misleading or stale content

Do NOT flag entire features as undocumented — only report inaccuracies or gaps
within documentation that already exists for this feature.

Express each finding as documentation feedback — describe what is wrong or
missing in the docs, not what the code does. One finding per specific issue.

When you are done investigating, return a JSON array of objects:
[{"page": "<url or empty string>", "issue": "<one or two sentences>"}]

If no issues are found, return [].
Respond with only the JSON array. No markdown code fences. No prose.
```

---

### 4. Integration into `analyze.go`

After the existing mapping pass completes, a new drift pass runs:

```go
log.Infof("detecting documentation drift...")
driftFindings, err := analyzer.DetectDrift(ctx, llmClient, featureMap, docsFeatureMap, pageIndex, repoRoot)
if err != nil {
    return fmt.Errorf("detect drift: %w", err)
}
```

`pageIndex` is the `*spider.Index` already in scope from the spider pass — the cache on disk, no new fetching. `repoRoot` is the `--repo` flag value already available in the command.

**Deduplication against missing-docs findings:** `DetectDrift` only runs for features that ARE documented (have at least one matching page in `docsMap`). The "Undocumented Code" section in `gaps.md` covers features with no doc pages at all. These two sets are mutually exclusive by construction — a feature either has pages (drift candidate) or does not (undocumented candidate), never both. The prompt also explicitly instructs the LLM to focus on inaccuracies in existing documentation, not to flag the absence of documentation for features not covered on a page.

`driftFindings` is passed to `reporter.WriteGaps` alongside existing arguments.

No caching for the initial implementation — drift runs fresh each time.

---

### 5. Reporter output (`internal/reporter/reporter.go`)

New section added at the bottom of `gaps.md`:

```markdown
## Stale Documentation

### CLI command routing

- **Page:** https://docs.example.com/cli
  The section on flag parsing does not mention that --quiet suppresses the first-run banner.

- **Page:** https://docs.example.com/cli/flags
  The --repo flag is documented as optional but it is required when --docs-url is provided.

### Token batching

- The documentation does not mention that batches exceeding 80,000 tokens are automatically split and retried.
```

If `driftFindings` is empty or nil the section renders `_None found._`.

---

## Files to Change

| File | Change |
|---|---|
| `internal/analyzer/types.go` | Add `DriftIssue`, `DriftFinding`, `ChatMessage`, `Tool`, `ToolCall` |
| `internal/analyzer/client.go` | Add `ToolLLMClient` interface |
| `internal/analyzer/drift.go` | Create — `DetectDrift`, agent loop, tool implementations |
| `internal/analyzer/bifrost_client.go` | Implement `CompleteWithTools` on `BifrostClient` |
| `internal/cli/analyze.go` | Call `DetectDrift`, pass `driftFindings` to reporter |
| `internal/reporter/reporter.go` | Add "Stale Documentation" section to `WriteGaps` |

## Files to Create (tests)

| File | Change |
|---|---|
| `internal/analyzer/drift_test.go` | TDD tests for `DetectDrift` using stub `ToolLLMClient` |
| `internal/analyzer/bifrost_client_test.go` | Tests for `CompleteWithTools` |
| `internal/reporter/reporter_test.go` | New assertions for "Stale Documentation" section |

## What Does Not Change

- `LLMClient` interface and all existing callers — `ToolLLMClient` is additive
- `ExtractFeaturesFromCode`, `MapFeaturesToCode`, `MapFeaturesToDocs` — unchanged
- `gaps.md` existing sections — new section appended after existing ones
- Spider, scanner, doctor packages — untouched
