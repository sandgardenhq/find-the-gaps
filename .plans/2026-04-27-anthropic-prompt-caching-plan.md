# Anthropic Prompt Caching Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Enable Anthropic prompt caching on user-message content blocks across `BifrostClient.CompleteWithTools`, the Anthropic branch of `BifrostClient.CompleteJSON`, and `BifrostClient.Complete`. Provider-gated on `c.provider == schemas.Anthropic`. OpenAI and Ollama paths untouched.

**Architecture:** Bifrost's user-message content-block path passes `cache_control` through to Anthropic correctly (verified by spike). Cacheable messages are rendered as one-element `ContentBlocks` slices instead of `ContentStr`. One conversion site inside `bifrost_client.go`; callers continue to use `ChatMessage{Content: string}` plus a new `CacheBreakpoint bool` flag. Verification via `resp.Usage.PromptTokensDetails.CachedReadTokens` surfaced in verbose logging.

**What this plan does NOT do:** set `cache_control` on `schemas.ChatTool.CacheControl`. The original design used this; the Task 0 spike showed Bifrost v1.5.2's tool-cache path is non-deterministic (different `cached_write_tokens` between identical calls). See the design doc's *Bifrost API Findings* table.

**Tech Stack:** Go 1.26+, `github.com/maximhq/bifrost/core` v1.5.2, Bifrost Anthropic provider, testify, testscript. Spike-only dep on `github.com/anthropics/anthropic-sdk-go` (already in `go.mod`).

**Reference:** See `.plans/2026-04-27-anthropic-prompt-caching-design.md` for the design and rationale.

---

## Task 0: Confirm Bifrost user-block cache path works (revised)

**Why first:** The original Task 0 spike asserted tool-level caching, which turned out to be non-deterministic in Bifrost v1.5.2. The revised spike validates the path we will actually use: `cache_control` on a user-message **content block**. Findings from the original investigation are recorded in the design doc's *Bifrost API Findings* table — do not re-derive them.

**Pre-existing files (uncommitted, leftover from the original spike — clean these up before writing new ones):**
- `internal/analyzer/bifrost_cache_spike_test.go` — original tool-cache spike, FAILED
- `internal/analyzer/anthropic_direct_spike_test.go` — direct-SDK spike, PASSED (proved Anthropic API works)

Delete both, then create the revised single spike below.

**Files:**
- Create: `internal/analyzer/bifrost_cache_spike_test.go` (build tag `cachespike`)

**Step 1: Clean up the leftover spike files**

```bash
rm /Users/brittcrawford/conductor/workspaces/find-the-gaps/seoul-v1/internal/analyzer/bifrost_cache_spike_test.go
rm /Users/brittcrawford/conductor/workspaces/find-the-gaps/seoul-v1/internal/analyzer/anthropic_direct_spike_test.go
```

**Step 2: Write the revised spike**

```go
//go:build cachespike

package analyzer

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	bifrost "github.com/maximhq/bifrost/core"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/stretchr/testify/require"
)

// TestBifrostUserBlockCacheControlEndToEnd verifies the only Bifrost
// caching path this project will use: cache_control on a user-message
// content block. After warming the cache and observing Anthropic's
// freshness-lag window, every subsequent identical request must read
// from cache.
//
// IMPORTANT — propagation lag:
//   Anthropic's cache requires several seconds for a fresh write to
//   become globally readable. Two calls in milliseconds may BOTH report
//   cache_write > 0 with cache_read = 0 — that is normal, not a bug.
//   This spike intentionally inserts a 10s delay after the first call
//   to clear the lag window.
//
// Run with: go test -tags=cachespike -run TestBifrostUserBlockCacheControlEndToEnd -v ./internal/analyzer/
func TestBifrostUserBlockCacheControlEndToEnd(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	account := &bifrostAccount{provider: schemas.Anthropic, apiKey: apiKey}
	client, err := bifrost.Init(context.Background(), schemas.BifrostConfig{Account: account})
	require.NoError(t, err)

	preamble := strings.Repeat("This is documentation that should be cached. ", 600)
	tail := "Now answer briefly: what is 2 + 2?"

	send := func(label string) *schemas.BifrostChatResponse {
		req := &schemas.BifrostChatRequest{
			Provider: schemas.Anthropic,
			Model:    "claude-sonnet-4-6",
			Input: []schemas.ChatMessage{{
				Role: schemas.ChatMessageRoleUser,
				Content: &schemas.ChatMessageContent{
					ContentBlocks: []schemas.ChatContentBlock{
						{
							Type:         schemas.ChatContentBlockTypeText,
							Text:         schemas.Ptr(preamble),
							CacheControl: &schemas.CacheControl{Type: schemas.CacheControlTypeEphemeral},
						},
						{
							Type: schemas.ChatContentBlockTypeText,
							Text: schemas.Ptr(tail),
						},
					},
				},
			}},
			Params: &schemas.ChatParameters{
				MaxCompletionTokens: schemas.Ptr(64),
			},
		}
		ctx := schemas.NewBifrostContext(context.Background(), schemas.NoDeadline)
		resp, bErr := client.ChatCompletionRequest(ctx, req)
		require.Nil(t, bErr, "%s: bifrost error: %+v", label, bErr)
		require.NotNil(t, resp.Usage)
		require.NotNil(t, resp.Usage.PromptTokensDetails)
		t.Logf("%s usage: cache_write=%d cache_read=%d",
			label,
			resp.Usage.PromptTokensDetails.CachedWriteTokens,
			resp.Usage.PromptTokensDetails.CachedReadTokens,
		)
		return resp
	}

	resp1 := send("call 1 (warmup)")
	// Either an immediate write (cold cache) or an immediate read (warm
	// from a prior run within TTL) is acceptable proof the path is wired.
	require.True(t,
		resp1.Usage.PromptTokensDetails.CachedWriteTokens > 0 ||
			resp1.Usage.PromptTokensDetails.CachedReadTokens > 0,
		"expected EITHER cache write or read on call 1; got %+v",
		resp1.Usage.PromptTokensDetails)

	// Wait long enough for Anthropic's freshness-lag window to close.
	t.Log("waiting 10s for cache propagation...")
	time.Sleep(10 * time.Second)

	resp2 := send("call 2 (after 10s)")
	require.Greater(t, resp2.Usage.PromptTokensDetails.CachedReadTokens, 0,
		"call 2 must read from cache after 10s settle; got %+v",
		resp2.Usage.PromptTokensDetails)

	resp3 := send("call 3 (immediate)")
	require.Greater(t, resp3.Usage.PromptTokensDetails.CachedReadTokens, 0,
		"call 3 must also read from cache; got %+v",
		resp3.Usage.PromptTokensDetails)
}
```

**Step 3: Run the spike**

```sh
set -a && source .env.local && set +a && go test -tags=cachespike -run TestBifrostUserBlockCacheControlEndToEnd -v ./internal/analyzer/ 2>&1 | tee /tmp/spike-output.log
```

Expected: PASS within ~30s. Logs should show non-zero `cache_read` on call 2 and call 3. Call 1 may be either a write (cold) or a read (warm).

**Step 4: If FAIL, stop and decide**

A fail at this point — after the design has been revised based on the original spike's findings — means a deeper problem (e.g. a regression in Bifrost or a change in Anthropic's API behavior). Stop. Capture the test output verbatim. Do not commit. Do not proceed.

**Step 5: Commit (on PASS)**

```bash
git add internal/analyzer/bifrost_cache_spike_test.go
git commit -m "$(cat <<'EOF'
test(analyzer): spike — verify Bifrost user-block cache_control end-to-end

- Replaces a failed earlier spike that tested tool-level caching (Bifrost v1.5.2 is non-deterministic on that path; see design doc's Bifrost API Findings).
- Asserts cache_read > 0 on calls 2 and 3 with cache_control on a user-message content block, after a 10s delay to clear Anthropic's propagation lag window.
- Status: passing under -tags=cachespike; gated on ANTHROPIC_API_KEY; build-tagged so it does not run in normal CI.
EOF
)"
```

---

## Task 1: Audit prompts for silent invalidators

**Why:** A non-deterministic byte in the cached prefix means every request misses the cache while still paying the 1.25× write premium — strictly worse than no caching.

**Files:**
- Modify (if anything is found): the offending file in `internal/analyzer/`
- No new test file at this stage; the audit produces fixes, each with its own test.

**Step 1: Run the four audit greps**

```bash
grep -rn "time\.Now\|time\.Date" internal/analyzer/ | grep -v _test.go
grep -rnE "for [a-zA-Z_]+, [a-zA-Z_]+ := range " internal/analyzer/ | grep -v _test.go
grep -rni "uuid\|run.?id\|commit.?sha" internal/analyzer/ | grep -v _test.go
grep -rn "json.Marshal" internal/analyzer/ | grep -v _test.go
```

**Step 2: Classify each hit**

For each match, decide:
- **Inside a `// PROMPT:` site or feeding tool definitions?** → potential invalidator, fix required.
- **Outside prompt construction (logging, file IO, etc.)?** → ignore.
- **Map iteration that feeds prompt bytes?** → must add `sort.Strings(keys)` before iteration.

**Step 3: For each invalidator, write a failing test then fix**

Example pattern (only run if an invalidator is found — substitute the actual function name):

```go
func TestBuildXPrompt_IsByteStableAcrossCalls(t *testing.T) {
	in := /* representative input that exercises the invalidator */

	first := buildXPrompt(in)
	for i := 0; i < 5; i++ {
		got := buildXPrompt(in)
		require.Equal(t, first, got, "buildXPrompt must be byte-stable; iteration %d differs", i)
	}
}
```

Run: `go test -run TestBuildXPrompt_IsByteStableAcrossCalls -count=10 ./internal/analyzer/`
Expected: FAIL on at least one iteration if a map-iteration order or random ID is in play.

Fix the source: sort keys, freeze IDs, or move the dynamic content out of the cached region.

Re-run: PASS.

**Step 4: Commit each fix individually**

```bash
git add internal/analyzer/<file>.go internal/analyzer/<file>_test.go
git commit -m "fix(analyzer): make <prompt name> byte-stable for prompt caching

- RED: TestBuildXPrompt_IsByteStableAcrossCalls fails under -count=10 because of map iteration order
- GREEN: sort.Strings(keys) before iterating
- Status: existing analyzer tests still pass; new byte-stability test passing"
```

**Step 5: If audit finds nothing**

Commit a marker note in the plan only — no code change. Skip to Task 2.

```bash
git commit --allow-empty -m "chore(analyzer): prompt-caching audit found no silent invalidators"
```

---

## Task 2: Add a per-message `CacheControl` to `ChatMessage`

**Why:** The internal `ChatMessage` type has no field for cache control. Callers shouldn't have to think about Bifrost internals; the field lives on the message and is materialized into a content-block during the Bifrost conversion.

**Files:**
- Modify: `internal/analyzer/agent_loop.go` (or wherever `ChatMessage` is defined — verify with `grep -n "type ChatMessage" internal/analyzer/`)
- Test: existing `agent_loop_test.go` — extend, don't add a new file.

**Step 1: Locate `ChatMessage` definition**

```bash
grep -rn "type ChatMessage struct" internal/analyzer/
```

**Step 2: Write the failing test**

Add to `internal/analyzer/agent_loop_test.go`:

```go
func TestChatMessage_CacheBreakpointFieldExists(t *testing.T) {
	// Compile-time assertion: ChatMessage must carry a CacheBreakpoint flag
	// for callers to opt a message into prompt caching.
	m := ChatMessage{Role: "user", Content: "x", CacheBreakpoint: true}
	require.True(t, m.CacheBreakpoint)
}
```

**Step 3: Run — expect compile failure**

```bash
go test -run TestChatMessage_CacheBreakpointFieldExists ./internal/analyzer/
```

Expected: build error `unknown field CacheBreakpoint`.

**Step 4: Add the field**

In the file containing `type ChatMessage struct`:

```go
type ChatMessage struct {
	// ... existing fields
	CacheBreakpoint bool // when true, marks this message as the last block of a cacheable prefix on the Anthropic provider; ignored by other providers
}
```

**Step 5: Run — expect PASS**

```bash
go test -run TestChatMessage_CacheBreakpointFieldExists ./internal/analyzer/
```

Expected: PASS.

**Step 6: Run the whole package — expect PASS**

```bash
go test ./internal/analyzer/
```

Expected: PASS (additive field, nothing else changes).

**Step 7: Commit**

```bash
git add internal/analyzer/
git commit -m "feat(analyzer): add ChatMessage.CacheBreakpoint flag for prompt caching

- RED: TestChatMessage_CacheBreakpointFieldExists asserts the field
- GREEN: add CacheBreakpoint bool to ChatMessage
- Status: all analyzer tests passing"
```

---

## Task 3: Convert flagged `CacheBreakpoint` messages into content-blocks with `CacheControl`

**Why:** Bifrost's `CacheControl` lives on a content block, not on `ContentStr`. The conversion has to happen once, in `completeOneTurn`, and only when the provider is Anthropic.

**Files:**
- Modify: `internal/analyzer/bifrost_client.go` — the `for _, m := range messages` block in `completeOneTurn` (~line 129).
- Test: `internal/analyzer/bifrost_client_test.go`

**Step 1: Inspect Bifrost's content-block shape**

```bash
go doc github.com/maximhq/bifrost/core/schemas ChatMessageContent
```

Identify the field name for the content-blocks slice (likely `ContentBlocks []ChatContentBlock` or similar). Read the struct definition in `~/go/pkg/mod/github.com/maximhq/bifrost/core@v1.5.2/schemas/chatcompletions.go` around the `CacheControl` references at lines 928 and 326 to confirm the exact field name and content-block type.

**Step 2: Write the failing test**

Add to `internal/analyzer/bifrost_client_test.go`:

```go
func TestCompleteOneTurn_CacheBreakpoint_RendersContentBlockWithCacheControl(t *testing.T) {
	var captured *schemas.BifrostChatRequest
	fake := &fakeBifrostRequester{
		fn: func(ctx *schemas.BifrostContext, req *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
			captured = req
			return &schemas.BifrostChatResponse{
				Choices: []schemas.BifrostChatResponseChoice{{
					Message: &schemas.ChatMessage{
						Role:    schemas.ChatMessageRoleAssistant,
						Content: &schemas.ChatMessageContent{ContentStr: schemas.Ptr("ok")},
					},
				}},
			}, nil
		},
	}
	c := &BifrostClient{client: fake, provider: schemas.Anthropic, model: "claude-sonnet-4-6"}

	_, err := c.completeOneTurn(context.Background(),
		[]ChatMessage{
			{Role: "user", Content: "uncached"},
			{Role: "user", Content: "cached prefix end", CacheBreakpoint: true},
		},
		nil,
	)
	require.NoError(t, err)
	require.NotNil(t, captured)
	require.Len(t, captured.Input, 2)

	// First message: ContentStr form, no cache_control.
	require.NotNil(t, captured.Input[0].Content)
	require.NotNil(t, captured.Input[0].Content.ContentStr)
	require.Equal(t, "uncached", *captured.Input[0].Content.ContentStr)

	// Second message: rendered as a content block carrying CacheControl.
	require.NotNil(t, captured.Input[1].Content)
	// EXACT ASSERTION DEPENDS ON BIFROST FIELD NAMES — adjust after Step 1.
	// Pattern: assert ContentBlocks has one block, block has CacheControl != nil and Type == ephemeral.
}

func TestCompleteOneTurn_CacheBreakpoint_NotAnthropic_NoOp(t *testing.T) {
	var captured *schemas.BifrostChatRequest
	fake := &fakeBifrostRequester{
		fn: func(ctx *schemas.BifrostContext, req *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
			captured = req
			return &schemas.BifrostChatResponse{
				Choices: []schemas.BifrostChatResponseChoice{{
					Message: &schemas.ChatMessage{Role: schemas.ChatMessageRoleAssistant, Content: &schemas.ChatMessageContent{ContentStr: schemas.Ptr("ok")}},
				}},
			}, nil
		},
	}
	c := &BifrostClient{client: fake, provider: schemas.OpenAI, model: "gpt-4o"}

	_, err := c.completeOneTurn(context.Background(),
		[]ChatMessage{{Role: "user", Content: "x", CacheBreakpoint: true}},
		nil,
	)
	require.NoError(t, err)
	// OpenAI path: still ContentStr, never content-blocks.
	require.NotNil(t, captured.Input[0].Content.ContentStr)
}
```

If `fakeBifrostRequester` does not exist, define it next to the test:

```go
type fakeBifrostRequester struct {
	fn func(*schemas.BifrostContext, *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError)
}

func (f *fakeBifrostRequester) ChatCompletionRequest(ctx *schemas.BifrostContext, req *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
	return f.fn(ctx, req)
}
```

**Step 3: Run — expect FAIL**

```bash
go test -run TestCompleteOneTurn_CacheBreakpoint -v ./internal/analyzer/
```

Expected: FAIL — flag is currently ignored.

**Step 4: Implement**

In `bifrost_client.go`, modify the message-conversion loop. For each message, if `c.provider == schemas.Anthropic && m.CacheBreakpoint`, render `bm.Content` as a one-element content-blocks slice carrying `CacheControl: &schemas.CacheControl{Type: schemas.CacheControlTypeEphemeral}`. Otherwise keep the existing `ContentStr` path.

The exact field names for content-blocks come from Step 1.

**Step 5: Run — expect PASS**

```bash
go test -run TestCompleteOneTurn_CacheBreakpoint -v ./internal/analyzer/
```

**Step 6: Run the whole package**

```bash
go test ./internal/analyzer/
```

Expected: all PASS — non-cached path is unchanged.

**Step 7: Commit**

```bash
git add internal/analyzer/bifrost_client.go internal/analyzer/bifrost_client_test.go
git commit -m "feat(analyzer): render CacheBreakpoint messages as Anthropic content-blocks with cache_control

- RED: TestCompleteOneTurn_CacheBreakpoint_RendersContentBlockWithCacheControl + non-Anthropic no-op test
- GREEN: in completeOneTurn, when provider==Anthropic and m.CacheBreakpoint, build a one-block ContentBlocks slice carrying ephemeral CacheControl; otherwise unchanged
- Status: all analyzer tests passing"
```

---

## Task 4: ~~Cache the last Tool definition~~ — REMOVED

**Why removed:** The Task 0 spike found Bifrost v1.5.2's tool-level `cache_control` path is non-deterministic — two byte-identical requests produced different `cached_write_tokens` counts (327-token delta), so the cache never reads. See the design doc's *Bifrost API Findings* table.

This task was the only place in the plan that touched `schemas.ChatTool.CacheControl`. With it removed, the implementation never sets that field. If a future Bifrost release fixes the non-determinism, restoring this task is straightforward — the original test code is in this file's git history.

No work to do here. Proceed to Task 5.

---

## Task 5: Mark the drift investigator's first user message as a cache breakpoint

**Why:** The investigator system prompt is byte-identical across every round of one feature's investigation. Marking it lets rounds 2..B read it from cache.

**Files:**
- Modify: `internal/analyzer/drift.go` — `investigateFeatureDrift` (~line 291).
- Test: `internal/analyzer/drift_test.go`

**Step 1: Write the failing test**

```go
func TestInvestigateFeatureDrift_MarksFirstMessageAsCacheBreakpoint(t *testing.T) {
	var capturedMsgs [][]ChatMessage
	fake := &fakeToolClient{
		complete: func(ctx context.Context, msgs []ChatMessage, tools []Tool, opts ...AgentOption) (AgentResult, error) {
			snapshot := make([]ChatMessage, len(msgs))
			copy(snapshot, msgs)
			capturedMsgs = append(capturedMsgs, snapshot)
			return AgentResult{FinalMessage: ChatMessage{Role: "assistant", Content: "done"}, Rounds: 1}, nil
		},
	}

	entry := FeatureMapEntry{ /* minimal viable entry */ }
	pages := []string{"https://example.com/page"}
	pageReader := func(string) (string, error) { return "stub", nil }

	_, err := investigateFeatureDrift(context.Background(), fake, entry, pages, pageReader, "/repo")
	require.NoError(t, err)
	require.Len(t, capturedMsgs, 1)
	require.NotEmpty(t, capturedMsgs[0])
	require.True(t, capturedMsgs[0][0].CacheBreakpoint, "investigator's first user message must be marked as cache breakpoint")
}
```

If `fakeToolClient` does not exist, define alongside (mirroring the requester fake in `bifrost_client_test.go`).

**Step 2: Run — expect FAIL**

```bash
go test -run TestInvestigateFeatureDrift_MarksFirstMessage -v ./internal/analyzer/
```

**Step 3: Implement**

In `drift.go` line 291:

```go
messages := []ChatMessage{{Role: "user", Content: systemPrompt, CacheBreakpoint: true}}
```

**Step 4: Run — expect PASS**

```bash
go test ./internal/analyzer/
```

**Step 5: Commit**

```bash
git add internal/analyzer/drift.go internal/analyzer/drift_test.go
git commit -m "feat(analyzer): mark drift investigator prompt as cache breakpoint

- RED: TestInvestigateFeatureDrift_MarksFirstMessageAsCacheBreakpoint
- GREEN: set CacheBreakpoint=true on the first user message; reads from cache on rounds 2..B for the same feature
- Status: all analyzer tests passing"
```

---

## Task 5b: Rotating per-turn cache breakpoint in `runAgentLoop`

**Why:** Beyond Task 5, the growing transcript itself is reusable across rounds. After round 1 the assistant turn + tool results are part of the prefix; on round 2 we want them read from cache, with the marker now on the new tail. This needs `runAgentLoop` to manage the rotation — callers must not have to think about it.

The 4-breakpoint Anthropic budget after this task: tools (1) + first user message (1) + most-recent appended message (1) = 3, leaves 1 spare.

**Files:**
- Modify: `internal/analyzer/agent_loop.go` — `runAgentLoop` (~lines 76 and 95).
- Test: `internal/analyzer/agent_loop_test.go`

**Step 1: Write the failing tests**

```go
// On round 2, the previously-marked message must be unflagged and the
// newly-appended tool result must carry the marker.
func TestRunAgentLoop_RotatesCacheBreakpointToLatestMessage(t *testing.T) {
	var observed [][]ChatMessage
	scripted := []ChatMessage{
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Name: "echo", Arguments: `{}`}}},
		{Role: "assistant", Content: "ok"},
	}
	tools := []Tool{{
		Name: "echo", Description: "x",
		Execute: func(_ context.Context, _ string) (string, error) { return "result", nil },
	}}
	initial := []ChatMessage{{Role: "user", Content: "go", CacheBreakpoint: true}}

	_, err := runAgentLoop(context.Background(), scriptedTurns(scripted, &observed), initial, tools)
	require.NoError(t, err)
	require.Len(t, observed, 2)

	// Round 1: only the seeded user message has the flag.
	require.Len(t, observed[0], 1)
	require.True(t, observed[0][0].CacheBreakpoint)

	// Round 2: messages = [seeded user, assistant tool_use, tool result].
	// The seeded user message must KEEP its flag (durable breakpoint #2);
	// the latest message (tool result) must carry the rotating breakpoint;
	// the assistant message in between must NOT have it.
	require.Len(t, observed[1], 3)
	require.True(t, observed[1][0].CacheBreakpoint, "investigator prompt keeps its breakpoint")
	require.False(t, observed[1][1].CacheBreakpoint, "intermediate assistant message must not be flagged")
	require.True(t, observed[1][2].CacheBreakpoint, "latest tool result must carry rotating breakpoint")
}

// When there is NO seeded breakpoint, the rotating marker still applies
// to the latest appended message starting from round 2.
func TestRunAgentLoop_RotatingBreakpoint_NoSeeded(t *testing.T) {
	var observed [][]ChatMessage
	scripted := []ChatMessage{
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Name: "echo", Arguments: `{}`}}},
		{Role: "assistant", Content: "ok"},
	}
	tools := []Tool{{
		Name: "echo", Description: "x",
		Execute: func(_ context.Context, _ string) (string, error) { return "result", nil },
	}}
	initial := []ChatMessage{{Role: "user", Content: "go"}}

	_, err := runAgentLoop(context.Background(), scriptedTurns(scripted, &observed), initial, tools)
	require.NoError(t, err)
	require.Len(t, observed[1], 3)
	require.False(t, observed[1][0].CacheBreakpoint)
	require.False(t, observed[1][1].CacheBreakpoint)
	require.True(t, observed[1][2].CacheBreakpoint)
}
```

**Step 2: Run — expect FAIL**

```bash
go test -run TestRunAgentLoop_RotatingBreakpoint -v ./internal/analyzer/
go test -run TestRunAgentLoop_RotatesCacheBreakpoint -v ./internal/analyzer/
```

**Step 3: Implement**

In `runAgentLoop` (`agent_loop.go`), after every `messages = append(...)` that adds the *final* message of a turn (the assistant message at line 76 if it has tool calls, or each tool result at lines 95-99 — only the last one of a batch), apply a rotating-breakpoint helper:

```go
// rotateCacheBreakpoint clears the rotating breakpoint on every message
// EXCEPT the seeded one (index 0 if it was originally flagged), then sets
// the rotating breakpoint on messages[len(messages)-1]. The seeded flag
// at index 0 is preserved as a durable breakpoint.
func rotateCacheBreakpoint(messages []ChatMessage) {
	seededFlag := len(messages) > 0 && messages[0].CacheBreakpoint
	for i := range messages {
		messages[i].CacheBreakpoint = false
	}
	if seededFlag {
		messages[0].CacheBreakpoint = true
	}
	if len(messages) > 0 {
		messages[len(messages)-1].CacheBreakpoint = true
	}
}
```

Call `rotateCacheBreakpoint(messages)` after each `messages = append(...)` site that completes a turn append:
- After appending the assistant `resp` (line 76).
- After the tool-result append loop (after line 100).

**Note on round 1:** The first call to `next(ctx, messages, tools)` happens *before* any rotation, so the seeded user message (if flagged) is the only flagged message on round 1. That's correct — there's no transcript to cache yet.

**Step 4: Run — expect PASS**

```bash
go test ./internal/analyzer/
```

All PASS, including the existing agent-loop tests.

**Step 5: Commit**

```bash
git add internal/analyzer/agent_loop.go internal/analyzer/agent_loop_test.go
git commit -m "feat(analyzer): rotating cache breakpoint on the latest appended message in runAgentLoop

- RED: TestRunAgentLoop_RotatesCacheBreakpointToLatestMessage + no-seeded variant
- GREEN: rotateCacheBreakpoint preserves the seeded index-0 flag and rotates the marker to len-1 on every turn append
- Status: all analyzer tests passing
- Notes: stays within Anthropic's 4-breakpoint budget (tools + seeded + rotating = 3)"
```

---

## Task 6: Cache the user prompt in the Anthropic `CompleteJSON` branch

**Why:** Caches the user prompt so any retry or deterministic-batched re-send reads from cache. Note: the original plan also cached the `respond` tool definition (which carries the schema). **That part is dropped** — Bifrost v1.5.2 tool-level cache_control is broken (see Task 4 removal note).

**Files:**
- Modify: `internal/analyzer/bifrost_client.go` — `completeJSONAnthropic` (~line 303).
- Test: `internal/analyzer/bifrost_client_test.go`

**Step 1: Write the failing test**

```go
func TestCompleteJSONAnthropic_UserPromptHasCacheControl(t *testing.T) {
	var captured *schemas.BifrostChatRequest
	fake := &fakeBifrostRequester{
		fn: func(ctx *schemas.BifrostContext, req *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
			captured = req
			return &schemas.BifrostChatResponse{
				Choices: []schemas.BifrostChatResponseChoice{{
					Message: &schemas.ChatMessage{
						Role: schemas.ChatMessageRoleAssistant,
						ChatAssistantMessage: &schemas.ChatAssistantMessage{
							ToolCalls: []schemas.ChatAssistantMessageToolCall{{
								ID:       schemas.Ptr("c1"),
								Function: schemas.ChatAssistantMessageToolCallFunction{Name: schemas.Ptr("respond"), Arguments: `{"ok":true}`},
							}},
						},
					},
				}},
			}, nil
		},
	}
	c := &BifrostClient{client: fake, provider: schemas.Anthropic, model: "claude-sonnet-4-6"}

	schema := JSONSchema{
		Name: "test",
		Doc:  json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"]}`),
	}
	_, err := c.CompleteJSON(context.Background(), "go", schema)
	require.NoError(t, err)
	require.NotNil(t, captured)

	// (1) The respond tool MUST NOT carry cache_control. Bifrost's
	// tool-level cache_control is non-deterministic (see design doc).
	require.Len(t, captured.Params.Tools, 1)
	require.Nil(t, captured.Params.Tools[0].CacheControl,
		"tool-level cache_control is broken in Bifrost v1.5.2; this must remain nil")

	// (2) The user prompt is rendered as a content block carrying cache_control.
	require.Len(t, captured.Input, 1)
	// EXACT ASSERTION DEPENDS ON BIFROST FIELD NAMES — adjust to match
	// the same content-block path used in Task 3.
}

// Ollama path goes through completeJSONOpenAI which is unchanged in this task.
// OpenAI: also unchanged. Both inherit from existing tests.
```

**Step 2: Run — expect FAIL**

```bash
go test -run TestCompleteJSONAnthropic_UserPromptHasCacheControl -v ./internal/analyzer/
```

**Step 3: Implement**

In `completeJSONAnthropic`, render the user prompt as a content block with `CacheControl` instead of `ContentStr`. Reuse the helper introduced in Task 3 — extract it if needed so both `completeOneTurn` and `completeJSONAnthropic` share it.

**Do NOT** set `tool.CacheControl`. The non-Anthropic-tool-cache assertion above is a guardrail against re-introducing the broken path.

**Step 4: Run — expect PASS**

```bash
go test ./internal/analyzer/
```

**Step 5: Commit**

```bash
git add internal/analyzer/bifrost_client.go internal/analyzer/bifrost_client_test.go
git commit -m "feat(analyzer): cache the user prompt in CompleteJSON Anthropic branch

- RED: TestCompleteJSONAnthropic_UserPromptHasCacheControl (asserts tool-cache stays nil and user prompt carries cache_control)
- GREEN: render user prompt as a content block carrying cache_control via the Task 3 helper
- Status: all analyzer tests passing
- Notes: tool-level cache_control deliberately not set — Bifrost v1.5.2 is non-deterministic on that path"
```

---

## Task 6b: Cache the user prompt in `Complete` (Anthropic only)

**Why:** Per the design's *always cache* directive. Production caller is the drift page classifier (`drift.go:459`); same-page re-classification within 5 minutes hits cache. Different-page calls write but never read — net cost is small (1.25× write premium on the cached fraction of the prompt) and accepted.

**Files:**
- Modify: `internal/analyzer/bifrost_client.go` — `Complete` (~line 372).
- Test: `internal/analyzer/bifrost_client_test.go`

**Step 1: Write the failing test**

```go
func TestComplete_Anthropic_UserPromptHasCacheControl(t *testing.T) {
	var captured *schemas.BifrostChatRequest
	fake := &fakeBifrostRequester{
		fn: func(ctx *schemas.BifrostContext, req *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
			captured = req
			return &schemas.BifrostChatResponse{
				Choices: []schemas.BifrostChatResponseChoice{{
					Message: &schemas.ChatMessage{Role: schemas.ChatMessageRoleAssistant, Content: &schemas.ChatMessageContent{ContentStr: schemas.Ptr("ok")}},
				}},
			}, nil
		},
	}
	c := &BifrostClient{client: fake, provider: schemas.Anthropic, model: "claude-sonnet-4-6"}

	_, err := c.Complete(context.Background(), "classify this page")
	require.NoError(t, err)
	require.NotNil(t, captured)
	require.Len(t, captured.Input, 1)
	// Same content-block + CacheControl assertion shape as Task 3 / Task 6.
}

func TestComplete_OpenAI_UserPromptIsContentStr(t *testing.T) {
	var captured *schemas.BifrostChatRequest
	fake := &fakeBifrostRequester{
		fn: func(ctx *schemas.BifrostContext, req *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError) {
			captured = req
			return &schemas.BifrostChatResponse{
				Choices: []schemas.BifrostChatResponseChoice{{
					Message: &schemas.ChatMessage{Role: schemas.ChatMessageRoleAssistant, Content: &schemas.ChatMessageContent{ContentStr: schemas.Ptr("ok")}},
				}},
			}, nil
		},
	}
	c := &BifrostClient{client: fake, provider: schemas.OpenAI, model: "gpt-4o"}

	_, err := c.Complete(context.Background(), "classify this page")
	require.NoError(t, err)
	require.NotNil(t, captured.Input[0].Content.ContentStr)
}
```

**Step 2: Run — expect FAIL**

**Step 3: Implement**

In `Complete`, when `c.provider == schemas.Anthropic`, render the user message as a content block carrying `CacheControl` (reuse the Task 3 helper). Otherwise keep `ContentStr`.

**Step 4: Run — expect PASS**

```bash
go test ./internal/analyzer/
```

**Step 5: Commit**

```bash
git add internal/analyzer/bifrost_client.go internal/analyzer/bifrost_client_test.go
git commit -m "feat(analyzer): cache the user prompt on Anthropic Complete

- RED: TestComplete_Anthropic_UserPromptHasCacheControl + OpenAI no-op
- GREEN: render the user message as a content block carrying cache_control on the Anthropic path
- Status: all analyzer tests passing"
```

---

## Task 7: Surface cache usage in verbose logging

**Why:** Acceptance criterion. Without this we can't tell whether caching is actually working in production.

**Files:**
- Modify: wherever the existing verbose-logging hooks live for Bifrost responses. Find with:

```bash
grep -rn "Usage\|usage" internal/analyzer/bifrost_client.go
grep -rn "log\.\|charmbracelet/log" internal/analyzer/ | head -20
```

If `Usage` is not currently logged anywhere, add it. The existing verbose-logging plan (`.plans/VERBOSE_LOGGING.md`) is the reference for where these logs should go.

**Step 1: Write the failing test**

This is harder to TDD because it's a logging side effect. Use one of:

- **(a) Capture logs:** point `charmbracelet/log` at a `bytes.Buffer` and assert the strings appear.
- **(b) Refactor:** extract the usage-logging into a small helper that takes a `*schemas.BifrostLLMUsage` (or whatever the actual Bifrost-v1.5.2 type name is — verify via `go doc`) and an `io.Writer`, test the helper directly, call it from the request paths.

Prefer (b). Test:

```go
func TestLogUsage_IncludesCacheTokens(t *testing.T) {
	var buf bytes.Buffer
	logUsage(&buf, &schemas.BifrostLLMUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		PromptTokensDetails: &schemas.ChatPromptTokensDetails{
			CachedWriteTokens: 1024,
			CachedReadTokens:  2048,
		},
	})
	out := buf.String()
	require.Contains(t, out, "cache_write=1024")
	require.Contains(t, out, "cache_read=2048")
}

// nil PromptTokensDetails is the common case for non-Anthropic providers.
func TestLogUsage_NilDetails_ReportsZeroes(t *testing.T) {
	var buf bytes.Buffer
	logUsage(&buf, &schemas.BifrostLLMUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
	})
	out := buf.String()
	require.Contains(t, out, "cache_write=0")
	require.Contains(t, out, "cache_read=0")
}
```

> **Note:** the original plan referenced `Usage.CacheCreationInputTokens` / `CacheReadInputTokens`. Those don't exist on Bifrost's usage type — they live on `Usage.PromptTokensDetails` as `CachedWriteTokens` / `CachedReadTokens`. Confirmed via `go doc github.com/maximhq/bifrost/core/schemas ChatPromptTokensDetails`. (The Anthropic Go SDK does have the `Cache*InputTokens` fields, but we're not using it directly.)

**Step 2: Run — expect FAIL** (function doesn't exist)

**Step 3: Implement `logUsage`**

```go
func logUsage(w io.Writer, u *schemas.BifrostLLMUsage) {
	if u == nil {
		return
	}
	var cw, cr int
	if u.PromptTokensDetails != nil {
		cw = u.PromptTokensDetails.CachedWriteTokens
		cr = u.PromptTokensDetails.CachedReadTokens
	}
	fmt.Fprintf(w, "usage: prompt=%d completion=%d cache_write=%d cache_read=%d\n",
		u.PromptTokens, u.CompletionTokens, cw, cr)
}
```

Verify the exact type name (`BifrostLLMUsage`, `BifrostUsage`, etc.) with `go doc github.com/maximhq/bifrost/core/schemas` before writing — different Bifrost versions have used different names.

**Step 4: Run — expect PASS**

**Step 5: Wire `logUsage` into the three call sites**

After each successful `ChatCompletionRequest` in `completeOneTurn`, `completeJSONAnthropic`, `completeJSONOpenAI`, and `Complete`, call `logUsage(os.Stderr, resp.Usage)` gated on a verbose flag (use whatever existing flag the project has — if none, just log at debug level via `log.Debugf`).

Verify: `go test ./internal/analyzer/` PASSES.

**Step 6: Commit**

```bash
git add internal/analyzer/bifrost_client.go internal/analyzer/bifrost_client_test.go
git commit -m "feat(analyzer): log Bifrost cache usage on every LLM call

- RED: TestLogUsage_IncludesCacheTokens
- GREEN: extract logUsage helper; wire into completeOneTurn / completeJSON* / Complete
- Status: all analyzer tests passing"
```

---

## Task 8: End-to-end verification on a real run

**Why:** The unit tests prove markers are set; only a real Anthropic round-trip proves the cache is actually hit.

**Files:** none (manual verification)

**Step 1: Build and run with verbose logging**

```bash
go build ./cmd/find-the-gaps
ANTHROPIC_API_KEY=... ./find-the-gaps analyze \
    --repo ./testdata/fixtures/known-good \
    --docs-url https://<known-docs> \
    --llm-typical anthropic:claude-sonnet-4-6 \
    --llm-large anthropic:claude-sonnet-4-6 \
    --llm-small anthropic:claude-sonnet-4-6 \
    --verbose 2>&1 | tee /tmp/ftg-cache-run.log
```

(Use whatever the actual flag and tier names are; check `.plans/LLM_TIERING_PLAN.md` and `internal/cli/llm_client.go` for the exact CLI shape.)

**Step 2: Inspect the log**

```bash
grep "cache_" /tmp/ftg-cache-run.log
```

Expected (per the revised design — see *Cost Analysis Per Call Site*):

- **Drift investigation rounds 2..B for any feature** show `cache_read > 0` covering the investigator prompt + the prior turns of the transcript (rotating breakpoint from Task 5b). This is the dominant win.
- **Drift investigation round 1** of any feature shows `cache_write > 0`, `cache_read = 0`. No cross-feature reuse is expected — tool definitions are not cached in this revised plan.
- **`CompleteJSON` calls** generally show `cache_write > 0`, `cache_read = 0` within a single run — schema-cache reuse was lost when Task 4 was dropped. Reads only happen if the same user prompt is re-sent (retry).
- **`Complete` calls (page classifier)** show `cache_write > 0`, `cache_read = 0` within a single run. Expected and accepted; absolute dollar impact is small.
- **No call** should show `cache_write` on a tool slice. A non-zero tool-cache_write is a regression — Bifrost's tool-cache path is non-deterministic; the implementation never sets it.

**Step 3: If `cache_read = 0` everywhere**

Stop. A silent invalidator is present that the Task 1 audit missed. Diff the rendered request bytes between two consecutive calls:

```bash
# add a temporary debug print of the JSON-marshaled BifrostChatRequest
# in completeOneTurn, run, diff calls 1 and 2
```

Identify the differing byte, fix it, re-run. Do not declare the feature done until cache reads are observed.

**Step 4: Document the result in PROGRESS.md**

Per `CLAUDE.md` rule 8.

**Step 5: Commit (PROGRESS.md only)**

```bash
git add PROGRESS.md
git commit -m "docs(progress): anthropic prompt caching verified end-to-end

- cache_write observed on first call of every batch
- cache_read observed on second+ call of every batch
- cache_read observed on round 2+ of every drift investigation"
```

---

## Task 9: Open the PR

**Files:** none (PR mechanics)

**Step 1: Push**

```bash
git push -u origin anthropic-prompt-caching
```

**Step 2: Open PR**

```bash
gh pr create --base main \
  --title "feat(analyzer): enable Anthropic prompt caching on cacheable call sites" \
  --body "$(cat <<'EOF'
## Summary
- Cache the drift investigator's first user message AND apply a rotating per-turn cache breakpoint to the latest appended message in `runAgentLoop` — within one feature investigation, every round after round 1 reads the entire prior transcript from cache.
- Cache the user prompt on the Anthropic branches of `CompleteJSON` and `Complete`.
- Add `ChatMessage.CacheBreakpoint` so callers can opt a message into caching without touching Bifrost internals.
- Surface `cache_write` and `cache_read` token counts on every LLM call for verification.
- Provider-gated: OpenAI and Ollama paths untouched.

**Not done in this PR (originally planned, removed after Task 0 spike):**
- Tool-definition caching in `CompleteWithTools` and the `respond` tool in `CompleteJSON` Anthropic branch — Bifrost v1.5.2's tool-level `cache_control` is non-deterministic. See design doc *Bifrost API Findings*.

## Design
See `.plans/2026-04-27-anthropic-prompt-caching-design.md`.

## Test plan
- [ ] Spike test passes (Task 0): real Anthropic call shows cache_write on call 1, cache_read on call 2.
- [ ] All `go test ./internal/analyzer/` passes.
- [ ] End-to-end run (Task 8) shows non-zero cache_read on round 2+ of every drift investigation and on call 2+ of batched CompleteJSON.
- [ ] Coverage stays >= 90% on `internal/analyzer/`.
EOF
)"
```

---

## Final Acceptance Criteria

- [ ] `go test ./...` passes.
- [ ] `golangci-lint run` is clean.
- [ ] Coverage on `internal/analyzer/` ≥ 90%.
- [ ] End-to-end run logs show `cache_read > 0` on round 2+ of drift investigations and on call 2+ of batched `CompleteJSON`.
- [ ] `PROGRESS.md` updated.
- [ ] PR open against `main`.
