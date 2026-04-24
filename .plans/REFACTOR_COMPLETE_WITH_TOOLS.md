# Refactor: Push the Agent Loop into `CompleteWithTools`

## Goal

Move tool-execution responsibility from the calling code (`detectDriftForFeature`)
into `CompleteWithTools` itself. After this change, callers describe *what* tools
exist and *how to run them*, then call a single method that runs the multi-turn
conversation to completion.

As a paired change, rewrite drift detection to use an **accumulator pattern**:
the LLM reports each finding incrementally via an `add_finding` tool call rather
than submitting everything at the end. This makes partial progress recoverable
on max-rounds exhaustion and eliminates the need for a "terminal tool" concept
inside `CompleteWithTools`.

## Design decisions (confirmed)

1. **Tool handlers attach to the `Tool` struct** via an `Execute` func field.
2. **Stop condition is natural text termination** — the loop ends when the LLM
   returns an assistant message with no tool calls. No sentinel error, no
   terminal-tool concept in the primitive.
3. **Max rounds returns a typed error** (`ErrMaxRounds`). Callers that care
   must check it and handle accumulated state themselves.
4. **Unknown tool calls** are fed back to the LLM as an error-string result,
   loop continues (preserves current behavior).
5. **No per-round callback** for now — add when a caller needs it.
6. **No single-turn primitive** — callers needing one-shot tool use set
   `WithMaxRounds(1)`.

## Shape

```go
// internal/analyzer/types.go
type Tool struct {
    Name        string
    Description string
    Parameters  map[string]any
    Execute     ToolHandler   // NEW
}

// ToolHandler runs one tool call. The returned string is sent back to the LLM
// as the tool result. A non-nil error aborts the agent loop and is propagated
// to the caller. Errors that should be reported TO the LLM (e.g. "file not
// found") must be returned as the result string, not as a Go error.
type ToolHandler func(ctx context.Context, args string) (string, error)

// internal/analyzer/client.go
type AgentResult struct {
    FinalMessage ChatMessage   // last assistant message (may be empty text)
    Rounds       int           // number of LLM calls made
}

var ErrMaxRounds = errors.New("agent loop exceeded max rounds")

type ToolLLMClient interface {
    LLMClient
    CompleteWithTools(
        ctx context.Context,
        messages []ChatMessage,
        tools []Tool,
        opts ...AgentOption,
    ) (AgentResult, error)
}

type AgentOption func(*agentConfig)
func WithMaxRounds(n int) AgentOption
```

Loop semantics inside `CompleteWithTools`:

```
for round := 0; round < maxRounds; round++ {
    resp = bifrost chat completion
    append resp to messages
    if len(resp.ToolCalls) == 0:
        return AgentResult{FinalMessage: resp, Rounds: round+1}, nil
    for each tc in resp.ToolCalls:
        handler = lookup tools[tc.Name]
        if handler == nil:
            toolResult = "unknown tool: <name>"
        else:
            toolResult, err = handler(ctx, tc.Arguments)
            if err != nil:
                return AgentResult{}, err
        append tool-role message with toolResult
}
return AgentResult{FinalMessage: lastAssistant, Rounds: maxRounds}, ErrMaxRounds
```

## Drift rewrite

`submit_findings` goes away. Replaced with `add_finding`:

```go
func detectDriftForFeature(...) ([]DriftIssue, error) {
    var findings []DriftIssue

    tools := []Tool{
        readFileTool(repoRoot),
        readPageTool(pageReader),
        {
            Name:        "add_finding",
            Description: "Record one documentation inaccuracy. Call once per issue. When done, respond in plain text.",
            Parameters: map[string]any{ /* page, issue schema */ },
            Execute: func(_ context.Context, args string) (string, error) {
                var f DriftIssue
                if err := json.Unmarshal([]byte(args), &f); err != nil {
                    return fmt.Sprintf("invalid arguments: %v", err), nil
                }
                findings = append(findings, f)
                return "recorded", nil
            },
        },
    }

    _, err := client.CompleteWithTools(ctx, messages, tools, WithMaxRounds(driftMaxRounds))
    if errors.Is(err, ErrMaxRounds) {
        log.Warnf("drift agent exceeded %d rounds for %q; returning %d accumulated findings",
            driftMaxRounds, entry.Feature.Name, len(findings))
        return findings, nil
    }
    if err != nil {
        return nil, err
    }
    return findings, nil
}
```

Prompt changes: replace "call submit_findings with the list" with "call
add_finding once per issue; when you have no more issues to report, reply with
plain text confirming you're done (e.g. 'done')".

## Test surface changes

Existing drift tests that need rework:

| Test | New behavior |
|------|-------|
| `TestDetectDrift_DocumentedFeature_ReturnsIssues` | LLM emits `add_finding` call then text "done" |
| `TestDetectDrift_LLMReturnsEmptyArray_FeatureDropped` | LLM emits text "done" with zero `add_finding` calls — feature produces zero findings and is dropped |
| `TestDetectDrift_ToolCall_ExecutedAndContinued` | No change in principle |
| `TestDetectDrift_TextResponseWithoutSubmitFindings_ReturnsError` | **Invert**: text response with no prior `add_finding` is the normal empty-findings case, no error. Delete the test. |
| `TestDetectDrift_SubmitFindingsBadJSON_ReturnsError` | **Replace** with `TestDetectDrift_AddFindingBadJSON_FedBackToLLM` (bad args returned as tool result, loop continues, no DetectDrift error) |
| `TestDetectDrift_MaxRoundsExceeded_DeclaresDoneAndContinues` | **Extend**: assert that findings accumulated via `add_finding` before exhaustion ARE returned (not nil) |
| `submitFindings` test helper | Delete; replace with `addFinding(issue)` helper that builds an `add_finding` tool call message |

New tests at the `CompleteWithTools` primitive level (against the fake Bifrost
requester):

1. LLM returns plain text on turn 1 → `AgentResult.Rounds == 1`, nil err.
2. LLM calls a registered tool → handler invoked with correct args → result
   fed back as tool-role message → turn 2 produces text → loop ends.
3. LLM calls a tool with no handler → `"unknown tool: X"` fed back → loop
   continues.
4. Handler returns non-nil error → propagated as `CompleteWithTools` error.
5. `WithMaxRounds(1)`: one tool call on turn 1 → loop exhausts → returns
   `ErrMaxRounds` with `Rounds == 1`.
6. Bifrost error on any turn → propagated.
7. Context cancelled mid-loop → propagated.

## Implementation order (TDD)

1. Add `ToolHandler`, `Tool.Execute`, `AgentResult`, `ErrMaxRounds`,
   `AgentOption`, `WithMaxRounds`.
2. Write primitive-level tests (1–7 above) red-first, then implement the loop
   inside `BifrostClient.CompleteWithTools`.
3. Extract the per-turn bifrost call into an unexported `nextTurn` helper so
   loop logic is isolated from schema translation.
4. Rewrite `detectDriftForFeature` to use `add_finding` accumulator + the new
   primitive. Delete `executeTool`, `executeReadFile`, `executeReadPage` free
   functions and fold them into `Execute` closures returned by small builder
   helpers (`readFileTool`, `readPageTool`, `addFindingTool`).
5. Update drift test suite per the table above.
6. Update all other test doubles (`stubToolClient`, `stubLLMClient`,
   `driftStubClient`, `driftStubClientWithErr`, `fakeNonToolClient`) to the
   new signature. They now need to model a *sequence* of responses where each
   may be tool-calls or terminal text.
7. Re-run the full suite. Coverage remains ≥ 90% per package.
8. Commit after each red-green-refactor cycle.

## Risks

- **Test-double complexity grows.** Previously the stubs returned a single
  response per call; now they must model a scripted sequence. Manageable, but
  touches ~5 files.
- **LLM behavior shift.** The new prompt asks for per-issue tool calls + a
  text ack instead of one batched submission. Some models (especially smaller
  ones) may be worse at this pattern. Verification plan Scenario 1–4 will
  surface this. If it regresses, we can add a `done` tool as a graceful
  fallback, but not pre-emptively.
- **`ErrMaxRounds` with real Bifrost errors.** Callers must use `errors.Is`
  rather than equality; document in godoc.
