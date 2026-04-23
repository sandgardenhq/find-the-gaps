# Documentation Drift Detection — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a drift detection pass that compares each documented feature's code against its doc pages and surfaces specific inaccuracies expressed as documentation feedback. Results appear in a new "Stale Documentation" section of `gaps.md`.

**Architecture:** New `DetectDrift` function in `internal/analyzer/drift.go` uses a new `ToolLLMClient` interface (agentic loop with `read_file` and `read_page` tools). `BifrostClient` implements the interface. `analyze.go` wires it in after the existing mapping pass. `WriteGaps` renders the new section.

**Tech Stack:** Go, Bifrost SDK (`schemas.ChatTool`, `schemas.ChatAssistantMessageToolCall`), `encoding/json`, testify.

**Worktree:** `.worktrees/feat/drift-detection`
**All commands run from worktree root.**

**Design doc:** `.plans/2026-04-22-drift-detection-design.md`

---

### Task 1: Add new types to `internal/analyzer/types.go`

**Files:**
- Modify: `internal/analyzer/types.go`
- Modify: `internal/analyzer/types_test.go`

**Step 1: Write failing tests**

Add to `internal/analyzer/types_test.go`:

```go
func TestDriftFinding_JSONRoundtrip(t *testing.T) {
    f := analyzer.DriftFinding{
        Feature: "CLI command routing",
        Issues: []analyzer.DriftIssue{
            {Page: "https://docs.example.com/cli", Issue: "The --repo flag is not mentioned."},
        },
    }
    data, err := json.Marshal(f)
    require.NoError(t, err)
    var got analyzer.DriftFinding
    require.NoError(t, json.Unmarshal(data, &got))
    assert.Equal(t, f, got)
}

func TestToolCall_Fields(t *testing.T) {
    tc := analyzer.ToolCall{ID: "call_1", Name: "read_file", Arguments: `{"path":"foo.go"}`}
    assert.Equal(t, "call_1", tc.ID)
    assert.Equal(t, "read_file", tc.Name)
}

func TestChatMessage_Fields(t *testing.T) {
    msg := analyzer.ChatMessage{Role: "user", Content: "hello"}
    assert.Equal(t, "user", msg.Role)
}
```

**Step 2: Run to confirm RED**

```bash
go test ./internal/analyzer/... -run "TestDriftFinding|TestToolCall|TestChatMessage" -v
```

Expected: FAIL — types undefined.

**Step 3: Add types to `types.go`**

Add after `DocsFeatureMap`:

```go
// ChatMessage is one turn in a tool-use conversation.
type ChatMessage struct {
    Role       string     // "user", "assistant", "tool"
    Content    string
    ToolCalls  []ToolCall // set when Role=="assistant" and LLM requests tools
    ToolCallID string     // set when Role=="tool" (response to a tool call)
}

// Tool defines a callable function the LLM may invoke during drift detection.
type Tool struct {
    Name        string
    Description string
    Parameters  map[string]any // JSON Schema object
}

// ToolCall is one tool invocation requested by the LLM.
type ToolCall struct {
    ID        string
    Name      string
    Arguments string // raw JSON
}

// DriftIssue is one specific inaccuracy found between a feature's code and its documentation.
type DriftIssue struct {
    Page  string `json:"page"`  // URL of the doc page ("" if cross-page)
    Issue string `json:"issue"` // inaccuracy described in documentation language
}

// DriftFinding groups all drift issues found for one feature.
type DriftFinding struct {
    Feature string
    Issues  []DriftIssue
}
```

**Step 4: Run to confirm GREEN**

```bash
go test ./internal/analyzer/... -run "TestDriftFinding|TestToolCall|TestChatMessage" -v
```

**Step 5: Commit**

```bash
git add internal/analyzer/types.go internal/analyzer/types_test.go
git commit -m "feat(analyzer): add DriftFinding, DriftIssue, ChatMessage, Tool, ToolCall types"
```

---

### Task 2: Add `ToolLLMClient` interface to `internal/analyzer/client.go`

**Files:**
- Modify: `internal/analyzer/client.go`
- No separate test file needed — the interface is verified by Task 3's implementation.

**Step 1: Add the interface**

```go
// ToolLLMClient extends LLMClient with a multi-turn tool-use conversation.
// The caller sends messages and tool definitions; the LLM may request tool
// calls; the caller executes them and continues the conversation until the
// LLM returns a final non-tool response.
type ToolLLMClient interface {
    LLMClient
    CompleteWithTools(ctx context.Context, messages []ChatMessage, tools []Tool) (ChatMessage, error)
}
```

**Step 2: Verify package still compiles**

```bash
go build ./internal/analyzer/...
```

**Step 3: Commit**

```bash
git add internal/analyzer/client.go
git commit -m "feat(analyzer): add ToolLLMClient interface for tool-use conversations"
```

---

### Task 3: Implement `CompleteWithTools` on `BifrostClient`

**Files:**
- Modify: `internal/analyzer/bifrost_client.go`
- Modify: `internal/analyzer/bifrost_client_test.go`

**Step 1: Write the failing test**

Add to `bifrost_client_test.go`:

```go
func TestBifrostClient_CompleteWithTools_ReturnsFinalContent(t *testing.T) {
    // Simulate LLM returning a non-tool final answer directly.
    fake := &fakeBifrostRequester{
        resp: &schemas.BifrostChatResponse{
            Choices: []schemas.BifrostChatResponseChoice{
                {
                    Message: schemas.BifrostChatResponseChoiceMessage{
                        Role: string(schemas.ChatMessageRoleAssistant),
                        Content: &schemas.ChatMessageContent{
                            ContentStr: schemas.Ptr(`[{"page":"https://x.com","issue":"Missing param."}]`),
                        },
                    },
                    FinishReason: string(schemas.BifrostFinishReasonStop),
                },
            },
        },
    }
    client := newBifrostClientWithFake(fake, schemas.Anthropic, "claude-sonnet-4-6")
    msgs := []analyzer.ChatMessage{{Role: "user", Content: "check this"}}
    tools := []analyzer.Tool{{Name: "read_file", Description: "reads a file", Parameters: map[string]any{"type": "object"}}}
    got, err := client.CompleteWithTools(context.Background(), msgs, tools)
    if err != nil {
        t.Fatal(err)
    }
    if got.Role != "assistant" {
        t.Errorf("expected role 'assistant', got %q", got.Role)
    }
    if !strings.Contains(got.Content, "Missing param") {
        t.Errorf("expected content to contain 'Missing param', got %q", got.Content)
    }
}

func TestBifrostClient_CompleteWithTools_ReturnsToolCalls(t *testing.T) {
    // Simulate LLM requesting a tool call.
    fake := &fakeBifrostRequester{
        resp: &schemas.BifrostChatResponse{
            Choices: []schemas.BifrostChatResponseChoice{
                {
                    Message: schemas.BifrostChatResponseChoiceMessage{
                        Role: string(schemas.ChatMessageRoleAssistant),
                        ToolCalls: []schemas.ChatAssistantMessageToolCall{
                            {
                                ID: schemas.Ptr("call_1"),
                                Function: schemas.ChatAssistantMessageToolCallFunction{
                                    Name:      "read_file",
                                    Arguments: `{"path":"main.go"}`,
                                },
                            },
                        },
                    },
                    FinishReason: string(schemas.BifrostFinishReasonToolCalls),
                },
            },
        },
    }
    client := newBifrostClientWithFake(fake, schemas.Anthropic, "claude-sonnet-4-6")
    msgs := []analyzer.ChatMessage{{Role: "user", Content: "check this"}}
    tools := []analyzer.Tool{{Name: "read_file", Description: "reads a file", Parameters: map[string]any{"type": "object"}}}
    got, err := client.CompleteWithTools(context.Background(), msgs, tools)
    if err != nil {
        t.Fatal(err)
    }
    if len(got.ToolCalls) != 1 {
        t.Fatalf("expected 1 tool call, got %d", len(got.ToolCalls))
    }
    if got.ToolCalls[0].Name != "read_file" {
        t.Errorf("expected tool name 'read_file', got %q", got.ToolCalls[0].Name)
    }
    if got.ToolCalls[0].ID != "call_1" {
        t.Errorf("expected tool call ID 'call_1', got %q", got.ToolCalls[0].ID)
    }
}
```

Note: add `"strings"` import if needed.

**Step 2: Run to confirm RED**

```bash
go test ./internal/analyzer/... -run "TestBifrostClient_CompleteWithTools" -v
```

Expected: FAIL — method undefined.

**Step 3: Implement `CompleteWithTools` on `BifrostClient`**

Add to `bifrost_client.go`:

```go
// CompleteWithTools sends a multi-turn conversation with tool definitions and
// returns the LLM's next message (which may contain tool call requests or a
// final text response).
func (c *BifrostClient) CompleteWithTools(ctx context.Context, messages []ChatMessage, tools []Tool) (ChatMessage, error) {
    // Convert our ChatMessage slice to Bifrost schema messages.
    bifrostMsgs := make([]schemas.ChatMessage, 0, len(messages))
    for _, m := range messages {
        bm := schemas.ChatMessage{}
        switch m.Role {
        case "user":
            bm.Role = schemas.ChatMessageRoleUser
            bm.Content = &schemas.ChatMessageContent{ContentStr: schemas.Ptr(m.Content)}
        case "assistant":
            bm.Role = schemas.ChatMessageRoleAssistant
            if len(m.ToolCalls) > 0 {
                calls := make([]schemas.ChatAssistantMessageToolCall, len(m.ToolCalls))
                for i, tc := range m.ToolCalls {
                    id := tc.ID
                    calls[i] = schemas.ChatAssistantMessageToolCall{
                        ID: &id,
                        Function: schemas.ChatAssistantMessageToolCallFunction{
                            Name:      tc.Name,
                            Arguments: tc.Arguments,
                        },
                    }
                }
                bm.ToolCalls = calls
            } else {
                bm.Content = &schemas.ChatMessageContent{ContentStr: schemas.Ptr(m.Content)}
            }
        case "tool":
            bm.Role = schemas.ChatMessageRoleTool
            id := m.ToolCallID
            bm.ToolCallID = &id
            bm.Content = &schemas.ChatMessageContent{ContentStr: schemas.Ptr(m.Content)}
        }
        bifrostMsgs = append(bifrostMsgs, bm)
    }

    // Convert our Tool slice to Bifrost ChatTool slice.
    bifrostTools := make([]schemas.ChatTool, len(tools))
    for i, t := range tools {
        paramsJSON, _ := json.Marshal(t.Parameters)
        var params schemas.ToolFunctionParameters
        _ = json.Unmarshal(paramsJSON, &params)
        desc := t.Description
        bifrostTools[i] = schemas.ChatTool{
            Type: schemas.ChatToolTypeFunction,
            Function: &schemas.ChatToolFunction{
                Name:        t.Name,
                Description: &desc,
                Parameters:  &params,
            },
        }
    }

    bifrostCtx := schemas.NewBifrostContext(ctx, schemas.NoDeadline)
    resp, bifrostErr := c.client.ChatCompletionRequest(bifrostCtx, &schemas.BifrostChatRequest{
        Provider: c.provider,
        Model:    c.model,
        Input:    bifrostMsgs,
        Tools:    bifrostTools,
    })
    if bifrostErr != nil {
        if bifrostErr.Error != nil {
            return ChatMessage{}, fmt.Errorf("bifrost tool completion: %s", bifrostErr.Error.Message)
        }
        return ChatMessage{}, fmt.Errorf("bifrost tool completion: unknown error")
    }
    if len(resp.Choices) == 0 {
        return ChatMessage{}, fmt.Errorf("bifrost tool completion: no choices returned")
    }

    choice := resp.Choices[0]
    result := ChatMessage{Role: "assistant"}

    if len(choice.Message.ToolCalls) > 0 {
        calls := make([]ToolCall, len(choice.Message.ToolCalls))
        for i, tc := range choice.Message.ToolCalls {
            id := ""
            if tc.ID != nil {
                id = *tc.ID
            }
            calls[i] = ToolCall{ID: id, Name: tc.Function.Name, Arguments: tc.Function.Arguments}
        }
        result.ToolCalls = calls
    } else if choice.Message.Content != nil && choice.Message.Content.ContentStr != nil {
        result.Content = *choice.Message.Content.ContentStr
    }

    return result, nil
}
```

Add `"encoding/json"` to imports if not already present.

**Step 4: Run tests**

```bash
go test ./internal/analyzer/... -run "TestBifrostClient_CompleteWithTools" -v
```

**Step 5: Commit**

```bash
git add internal/analyzer/bifrost_client.go internal/analyzer/bifrost_client_test.go
git commit -m "feat(analyzer): implement CompleteWithTools on BifrostClient"
```

---

### Task 4: Create `internal/analyzer/drift.go` with `DetectDrift`

**Files:**
- Create: `internal/analyzer/drift.go`
- Create: `internal/analyzer/drift_test.go`

**Step 1: Write failing tests**

Create `internal/analyzer/drift_test.go`:

```go
package analyzer_test

import (
    "context"
    "testing"

    "github.com/sandgardenhq/find-the-gaps/internal/analyzer"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// stubToolClient is a ToolLLMClient test double.
type stubToolClient struct {
    // responses is consumed in order; last element is reused when exhausted.
    responses []analyzer.ChatMessage
    calls     int
    // completeFunc used by Complete (existing interface).
    completeFunc func(ctx context.Context, prompt string) (string, error)
}

func (s *stubToolClient) Complete(ctx context.Context, prompt string) (string, error) {
    if s.completeFunc != nil {
        return s.completeFunc(ctx, prompt)
    }
    return "", nil
}

func (s *stubToolClient) CompleteWithTools(ctx context.Context, messages []analyzer.ChatMessage, tools []analyzer.Tool) (analyzer.ChatMessage, error) {
    idx := s.calls
    if idx >= len(s.responses) {
        idx = len(s.responses) - 1
    }
    s.calls++
    return s.responses[idx], nil
}

func TestDetectDrift_NoDocumentedFeatures_ReturnsEmpty(t *testing.T) {
    client := &stubToolClient{}
    featureMap := analyzer.FeatureMap{
        {Feature: analyzer.CodeFeature{Name: "auth", UserFacing: true}, Files: []string{"auth.go"}},
    }
    docsMap := analyzer.DocsFeatureMap{} // no pages mapped — auth is undocumented, not a drift candidate
    pageReader := func(url string) (string, error) { return "", nil }

    findings, err := analyzer.DetectDrift(context.Background(), client, featureMap, docsMap, pageReader, "/repo")
    require.NoError(t, err)
    assert.Empty(t, findings)
}

func TestDetectDrift_DocumentedFeature_ReturnsIssues(t *testing.T) {
    client := &stubToolClient{
        responses: []analyzer.ChatMessage{
            {
                Role:    "assistant",
                Content: `[{"page":"https://docs.example.com/auth","issue":"Email requirement not documented."}]`,
            },
        },
    }
    featureMap := analyzer.FeatureMap{
        {
            Feature: analyzer.CodeFeature{Name: "auth", Description: "Handles user auth.", UserFacing: true},
            Files:   []string{"auth.go"},
            Symbols: []string{"Login"},
        },
    }
    docsMap := analyzer.DocsFeatureMap{
        {Feature: "auth", Pages: []string{"https://docs.example.com/auth"}},
    }
    pageReader := func(url string) (string, error) { return "# Auth\nLogin with username.", nil }

    findings, err := analyzer.DetectDrift(context.Background(), client, featureMap, docsMap, pageReader, "/repo")
    require.NoError(t, err)
    require.Len(t, findings, 1)
    assert.Equal(t, "auth", findings[0].Feature)
    require.Len(t, findings[0].Issues, 1)
    assert.Equal(t, "https://docs.example.com/auth", findings[0].Issues[0].Page)
    assert.Contains(t, findings[0].Issues[0].Issue, "Email requirement")
}

func TestDetectDrift_LLMReturnsEmptyArray_FeatureDropped(t *testing.T) {
    client := &stubToolClient{
        responses: []analyzer.ChatMessage{{Role: "assistant", Content: "[]"}},
    }
    featureMap := analyzer.FeatureMap{
        {Feature: analyzer.CodeFeature{Name: "search"}, Files: []string{"search.go"}},
    }
    docsMap := analyzer.DocsFeatureMap{
        {Feature: "search", Pages: []string{"https://docs.example.com/search"}},
    }
    pageReader := func(url string) (string, error) { return "# Search docs", nil }

    findings, err := analyzer.DetectDrift(context.Background(), client, featureMap, docsMap, pageReader, "/repo")
    require.NoError(t, err)
    assert.Empty(t, findings, "features with no issues should be dropped")
}

func TestDetectDrift_ToolCall_ExecutedAndContinued(t *testing.T) {
    // First response: LLM requests read_file tool.
    // Second response: LLM returns final JSON after seeing tool result.
    client := &stubToolClient{
        responses: []analyzer.ChatMessage{
            {
                Role: "assistant",
                ToolCalls: []analyzer.ToolCall{
                    {ID: "call_1", Name: "read_file", Arguments: `{"path":"auth.go"}`},
                },
            },
            {
                Role:    "assistant",
                Content: `[{"page":"","issue":"The docs omit that Login returns a JWT token."}]`,
            },
        },
    }
    featureMap := analyzer.FeatureMap{
        {
            Feature: analyzer.CodeFeature{Name: "auth", Description: "Handles user auth."},
            Files:   []string{"auth.go"},
        },
    }
    docsMap := analyzer.DocsFeatureMap{
        {Feature: "auth", Pages: []string{"https://docs.example.com/auth"}},
    }
    pageReader := func(url string) (string, error) { return "# Auth page", nil }

    findings, err := analyzer.DetectDrift(context.Background(), client, featureMap, docsMap, pageReader, t.TempDir())
    require.NoError(t, err)
    require.Len(t, findings, 1)
    assert.Contains(t, findings[0].Issues[0].Issue, "JWT token")
}

func TestDetectDrift_ReadFile_OutsideRepo_ReturnsError(t *testing.T) {
    // LLM requests a path that escapes the repo root — tool should return an
    // error message to the LLM, not panic or expose files.
    client := &stubToolClient{
        responses: []analyzer.ChatMessage{
            {
                Role:      "assistant",
                ToolCalls: []analyzer.ToolCall{{ID: "call_1", Name: "read_file", Arguments: `{"path":"../../../etc/passwd"}`}},
            },
            {Role: "assistant", Content: "[]"},
        },
    }
    featureMap := analyzer.FeatureMap{
        {Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
    }
    docsMap := analyzer.DocsFeatureMap{{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}}}
    pageReader := func(url string) (string, error) { return "# Auth", nil }

    // Must not error — path rejection is communicated back to the LLM as a tool result.
    _, err := analyzer.DetectDrift(context.Background(), client, featureMap, docsMap, pageReader, t.TempDir())
    assert.NoError(t, err)
}
```

**Step 2: Run to confirm RED**

```bash
go test ./internal/analyzer/... -run "TestDetectDrift" -v
```

Expected: FAIL — `DetectDrift` undefined.

**Step 3: Implement `drift.go`**

Create `internal/analyzer/drift.go`:

```go
package analyzer

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "strings"

    "github.com/charmbracelet/log"
)

const driftMaxRounds = 8

// DetectDrift compares each documented feature's code against its doc pages
// and returns a list of specific inaccuracies expressed as documentation feedback.
//
// Only features that have both code files AND at least one matching doc page are
// checked — features with no pages are undocumented (handled by WriteGaps), not
// drift candidates.
//
// pageReader reads the cached content of a doc page by URL. repoRoot is the
// absolute path to the repository root, used to constrain read_file access.
func DetectDrift(
    ctx context.Context,
    client ToolLLMClient,
    featureMap FeatureMap,
    docsMap DocsFeatureMap,
    pageReader func(url string) (string, error),
    repoRoot string,
) ([]DriftFinding, error) {
    // Index docsMap by feature name for fast lookup.
    docPages := make(map[string][]string, len(docsMap))
    for _, entry := range docsMap {
        if len(entry.Pages) > 0 {
            docPages[entry.Feature] = entry.Pages
        }
    }

    tools := driftTools()
    var findings []DriftFinding

    for _, entry := range featureMap {
        if len(entry.Files) == 0 {
            continue
        }
        pages, ok := docPages[entry.Feature.Name]
        if !ok || len(pages) == 0 {
            continue
        }

        log.Infof("  checking drift for feature %q (%d pages)", entry.Feature.Name, len(pages))
        issues, err := detectDriftForFeature(ctx, client, tools, entry, pages, pageReader, repoRoot)
        if err != nil {
            return nil, fmt.Errorf("DetectDrift %q: %w", entry.Feature.Name, err)
        }
        if len(issues) > 0 {
            findings = append(findings, DriftFinding{Feature: entry.Feature.Name, Issues: issues})
        }
    }

    return findings, nil
}

func detectDriftForFeature(
    ctx context.Context,
    client ToolLLMClient,
    tools []Tool,
    entry FeatureEntry,
    pages []string,
    pageReader func(url string) (string, error),
    repoRoot string,
) ([]DriftIssue, error) {
    // Build page summary lines for the initial prompt.
    var pageSummaries []string
    for _, url := range pages {
        pageSummaries = append(pageSummaries, fmt.Sprintf("- %s", url))
    }

    // PROMPT: Reviews documentation accuracy for one feature using tool calls to read source files and cached doc pages. Returns a JSON array of specific inaccuracies expressed as documentation feedback.
    systemPrompt := fmt.Sprintf(`You are reviewing documentation accuracy for a software feature.

Feature: %s
Code description: %s
Implemented in: %s
Symbols: %s

Documentation pages:
%s

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
Respond with only the JSON array. No markdown code fences. No prose.`,
        entry.Feature.Name,
        entry.Feature.Description,
        strings.Join(entry.Files, ", "),
        strings.Join(entry.Symbols, ", "),
        strings.Join(pageSummaries, "\n"),
    )

    messages := []ChatMessage{{Role: "user", Content: systemPrompt}}

    for round := 0; round < driftMaxRounds; round++ {
        resp, err := client.CompleteWithTools(ctx, messages, tools)
        if err != nil {
            return nil, err
        }
        messages = append(messages, resp)

        if len(resp.ToolCalls) == 0 {
            // Final response — parse JSON.
            var issues []DriftIssue
            if err := json.Unmarshal([]byte(strings.TrimSpace(resp.Content)), &issues); err != nil {
                return nil, fmt.Errorf("invalid JSON drift response: %w (raw: %q)", err, resp.Content)
            }
            return issues, nil
        }

        // Execute each tool call and append results.
        for _, tc := range resp.ToolCalls {
            result := executeTool(tc, pageReader, repoRoot)
            messages = append(messages, ChatMessage{
                Role:       "tool",
                Content:    result,
                ToolCallID: tc.ID,
            })
        }
    }

    return nil, fmt.Errorf("drift agent loop exceeded %d rounds without a final response", driftMaxRounds)
}

// executeTool runs one tool call and returns the result string to feed back to the LLM.
func executeTool(tc ToolCall, pageReader func(url string) (string, error), repoRoot string) string {
    switch tc.Name {
    case "read_file":
        return executeReadFile(tc.Arguments, repoRoot)
    case "read_page":
        return executeReadPage(tc.Arguments, pageReader)
    default:
        return fmt.Sprintf("unknown tool: %q", tc.Name)
    }
}

func executeReadFile(rawArgs, repoRoot string) string {
    var args struct {
        Path string `json:"path"`
    }
    if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
        return fmt.Sprintf("error parsing arguments: %v", err)
    }
    // Resolve to absolute path and verify it stays within repoRoot.
    abs := filepath.Join(repoRoot, args.Path)
    rel, err := filepath.Rel(repoRoot, abs)
    if err != nil || strings.HasPrefix(rel, "..") {
        return "access denied: path is outside the repository root"
    }
    content, err := os.ReadFile(abs)
    if err != nil {
        return fmt.Sprintf("error reading file: %v", err)
    }
    return string(content)
}

func executeReadPage(rawArgs string, pageReader func(url string) (string, error)) string {
    var args struct {
        URL string `json:"url"`
    }
    if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
        return fmt.Sprintf("error parsing arguments: %v", err)
    }
    content, err := pageReader(args.URL)
    if err != nil {
        return fmt.Sprintf("page not available: %v", err)
    }
    return content
}

// driftTools returns the two tool definitions available during drift detection.
func driftTools() []Tool {
    return []Tool{
        {
            Name:        "read_file",
            Description: "Read the full source content of a file in the repository. Use this to inspect implementation details before assessing documentation accuracy.",
            Parameters: map[string]any{
                "type": "object",
                "properties": map[string]any{
                    "path": map[string]any{
                        "type":        "string",
                        "description": "Repository-relative file path, e.g. internal/auth/login.go",
                    },
                },
                "required": []string{"path"},
            },
        },
        {
            Name:        "read_page",
            Description: "Read the full cached content of a documentation page. Use this to inspect what the docs currently say before comparing against the code.",
            Parameters: map[string]any{
                "type": "object",
                "properties": map[string]any{
                    "url": map[string]any{
                        "type":        "string",
                        "description": "The full URL of the documentation page.",
                    },
                },
                "required": []string{"url"},
            },
        },
    }
}
```

**Step 4: Run tests**

```bash
go test ./internal/analyzer/... -run "TestDetectDrift" -v
```

Expected: all PASS.

**Step 5: Run full analyzer suite**

```bash
go test ./internal/analyzer/... -v 2>&1 | tail -20
```

**Step 6: Commit**

```bash
git add internal/analyzer/drift.go internal/analyzer/drift_test.go
git commit -m "feat(analyzer): add DetectDrift with tool-use agent loop"
```

---

### Task 5: Wire `DetectDrift` into `analyze.go`

**Files:**
- Modify: `internal/cli/analyze.go`
- Modify: `internal/cli/analyze_test.go`
- Modify: `internal/cli/analyze_parallel_test.go`

**Step 1: Write failing test**

In `analyze_test.go`, find the test that exercises `runBothMaps` or the main `RunE` path. Add a test or assertion that `DetectDrift` is called when the LLM client satisfies `ToolLLMClient`. For now, a compilation-level test is sufficient — update the `llmClient` stub in the tests to also implement `CompleteWithTools` so the code compiles:

```go
// In the existing stub or fake LLMClient used in tests, add:
func (s *stubLLMClient) CompleteWithTools(ctx context.Context, messages []analyzer.ChatMessage, tools []analyzer.Tool) (analyzer.ChatMessage, error) {
    return analyzer.ChatMessage{Role: "assistant", Content: "[]"}, nil
}
```

Find the stub type name by reading `analyze_test.go` first.

**Step 2: Run to confirm RED (compile failure)**

```bash
go build ./internal/cli/...
```

Expected: compile error if `llmClient` in tests doesn't implement the new interface.

**Step 3: Update `analyze.go`**

After the mapping pass (around where `log.Debug("feature mapping complete"...` is), add:

```go
// Detect documentation drift for all documented features.
log.Infof("detecting documentation drift...")
pageReader := func(url string) (string, error) {
    path, ok := idx.FilePath(url)
    if !ok {
        return "", fmt.Errorf("page not in cache: %s", url)
    }
    data, err := os.ReadFile(path)
    return string(data), err
}
driftFindings, err := analyzer.DetectDrift(ctx, llmClient, featureMap, docsFeatureMap, pageReader, repoPath)
if err != nil {
    return fmt.Errorf("detect drift: %w", err)
}
```

Pass `driftFindings` to `reporter.WriteGaps` (signature update in Task 6).

Note: `idx` is the `*spider.Index` already in scope; `repoPath` is the `--repo` flag value. Check the exact variable names in `analyze.go` before editing.

**Step 4: Build**

```bash
go build ./...
```

**Step 5: Commit**

```bash
git add internal/cli/analyze.go internal/cli/analyze_test.go internal/cli/analyze_parallel_test.go
git commit -m "feat(cli): wire DetectDrift into analyze pipeline"
```

---

### Task 6: Add "Stale Documentation" section to `WriteGaps`

**Files:**
- Modify: `internal/reporter/reporter.go`
- Modify: `internal/reporter/reporter_test.go`

**Step 1: Write failing tests**

Add to `reporter_test.go`:

```go
func TestWriteGaps_StaleDocumentation_RendersFindings(t *testing.T) {
    dir := t.TempDir()
    mapping := analyzer.FeatureMap{
        {Feature: analyzer.CodeFeature{Name: "auth", UserFacing: true}, Files: []string{"auth.go"}},
    }
    drift := []analyzer.DriftFinding{
        {
            Feature: "auth",
            Issues: []analyzer.DriftIssue{
                {Page: "https://docs.example.com/auth", Issue: "The email field requirement is not documented."},
                {Page: "", Issue: "The error response format differs from what is described."},
            },
        },
    }
    if err := reporter.WriteGaps(dir, mapping, []string{"auth"}, drift); err != nil {
        t.Fatal(err)
    }
    data, err := os.ReadFile(filepath.Join(dir, "gaps.md"))
    if err != nil {
        t.Fatal(err)
    }
    content := string(data)

    if !strings.Contains(content, "## Stale Documentation") {
        t.Errorf("gaps.md must contain '## Stale Documentation' section, got:\n%s", content)
    }
    if !strings.Contains(content, "### auth") {
        t.Errorf("gaps.md must contain '### auth' under Stale Documentation, got:\n%s", content)
    }
    if !strings.Contains(content, "email field requirement") {
        t.Errorf("gaps.md must contain the drift issue text, got:\n%s", content)
    }
    if !strings.Contains(content, "https://docs.example.com/auth") {
        t.Errorf("gaps.md must cite the page URL for issues with a page, got:\n%s", content)
    }
}

func TestWriteGaps_StaleDocumentation_NoneFound(t *testing.T) {
    dir := t.TempDir()
    mapping := analyzer.FeatureMap{}
    if err := reporter.WriteGaps(dir, mapping, []string{}, []analyzer.DriftFinding{}); err != nil {
        t.Fatal(err)
    }
    data, _ := os.ReadFile(filepath.Join(dir, "gaps.md"))
    content := string(data)
    if !strings.Contains(content, "## Stale Documentation") {
        t.Errorf("section must always be present, got:\n%s", content)
    }
    if !strings.Contains(content, "_None found._") {
        t.Errorf("must show _None found._ when no drift, got:\n%s", content)
    }
}
```

**Step 2: Run to confirm RED**

```bash
go test ./internal/reporter/... -run "TestWriteGaps_StaleDocumentation" -v
```

Expected: FAIL — `WriteGaps` signature mismatch.

**Step 3: Update `reporter.go`**

Change `WriteGaps` signature:

```go
func WriteGaps(dir string, mapping analyzer.FeatureMap, allDocFeatures []string, drift []analyzer.DriftFinding) error {
```

Add the new section at the bottom, before the final `os.WriteFile`:

```go
// Stale documentation: inaccuracies found in pages that DO cover a feature.
sb.WriteString("\n## Stale Documentation\n\n")
if len(drift) == 0 {
    sb.WriteString("_None found._\n")
} else {
    for _, finding := range drift {
        fmt.Fprintf(&sb, "### %s\n\n", finding.Feature)
        for _, issue := range finding.Issues {
            if issue.Page != "" {
                fmt.Fprintf(&sb, "- **Page:** %s\n  %s\n\n", issue.Page, issue.Issue)
            } else {
                fmt.Fprintf(&sb, "- %s\n\n", issue.Issue)
            }
        }
    }
}
```

Fix the `WriteGaps` call site in `analyze.go` to pass `driftFindings`.
Fix any other call sites (check with `grep -rn "WriteGaps" .`).

**Step 4: Run full suite**

```bash
go test ./...
```

Expected: all packages pass.

**Step 5: Check coverage**

```bash
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | grep -E "analyzer|reporter|cli"
```

All modified packages must be ≥90%.

**Step 6: Lint**

```bash
golangci-lint run
```

**Step 7: Commit**

```bash
git add internal/reporter/reporter.go internal/reporter/reporter_test.go
git commit -m "feat(reporter): add Stale Documentation section to gaps.md"
```

---

### Task 7: Final verification

**Step 1: Full test suite**

```bash
go test ./...
```

All packages must pass.

**Step 2: Coverage**

```bash
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | grep total
```

Total must be ≥90%.

**Step 3: Build**

```bash
go build ./...
```

**Step 4: Lint**

```bash
golangci-lint run
```

**Step 5: Use finishing-a-development-branch skill to merge.**
