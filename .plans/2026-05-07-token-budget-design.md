# Per-Model Token Budget — Design

## Problem

`find-the-gaps analyze` crashed mid-run on a real fixture:

```
Error: detect drift: DetectDrift "Standard library grammar configuration":
  bifrost tool completion: Input tokens exceed the configured limit of
  272000 tokens. Your messages resulted in 294098 tokens.
```

The drift investigator is a multi-turn agent loop. Every turn appends the
assistant message and the full tool result (entire file or doc page). After
enough rounds the cumulative input exceeds the model's hard input cap and the
provider rejects the request, killing the run.

Single-shot calls (page analyzer, drift judge, mapper batch, screenshot pass,
classifier) can hit the same cap when their inputs are pathologically large —
most plausibly the drift judge, which receives every observation the
investigator surfaced in one prompt.

## Goal

Add a **per-model input-token budget** that gates every LLM call. When a call
would exceed the budget, take the action that preserves correctness for that
specific call shape: stop a multi-turn loop early, refuse a single-shot, or
compact a single-shot that has a known semantic structure.

Never silently truncate where the LLM would emit confidently-wrong output on
incomplete input.

## Out of scope

- Provider-side rate limiting and 429 backoff (existing retry layer).
- Output-token budgets (separate field `MaxCompletionTokens`, already wired).
- Live token-count APIs (Anthropic `/count_tokens`); we use a local tiktoken
  estimator with a safety margin.
- Auto-selecting a larger model on overflow.
- Compaction of page-analyzer / screenshot-pass prompts. Refuse with a hint;
  layer head+tail trimming later if a user actually has multi-megabyte pages.

## Architecture

### 1. `MaxInputTokens` on the capability table

Both `internal/cli/capabilities.go:ModelCapabilities` and the analyzer mirror
in `internal/analyzer/client.go` get one new field:

```go
// MaxInputTokens is the per-model input cap, including system + tools +
// accumulated history. The decorator gates sends at 0.9 × this value.
// Zero means "no budget" — used for self-hosted ollama/lmstudio "*" rows
// where the user picks the model and the harness can't know the limit.
MaxInputTokens int
```

`knownModels` rows updated:

| Provider  | Model                                                    | MaxInputTokens |
|-----------|----------------------------------------------------------|----------------|
| anthropic | claude-haiku-4-5                                         | 180000         |
| anthropic | claude-sonnet-4-6                                        | 180000         |
| anthropic | claude-opus-4-7                                          | 180000         |
| openai    | gpt-5.5 / gpt-5.4 / gpt-5.4-mini / gpt-5.4-nano          | 260000         |
| openai    | gpt-5 / gpt-5-mini                                       | 260000         |
| openai    | gpt-4o / gpt-4o-mini                                     | 115000         |
| groq      | meta-llama/llama-4-scout-17b-16e-instruct                | 120000         |
| ollama    | `*`                                                      | 0 (off)        |
| lmstudio  | `*`                                                      | 0 (off)        |

Each value is ~10% under the provider's published context window so output
tokens and per-provider serialization overhead don't push the request over.

`ResolveCapabilities`'s "known provider, unknown model" branch changes from
returning a zero `ModelCapabilities` to returning `MaxInputTokens: 100000`.
That number is below GPT-4o's 128k floor and any modern hosted production
model, so a user adding a brand-new GPT-5.6 row gets a conservative budget
until the table catches up — and an ancient or weird model can't reproduce
the 294k incident.

### 2. Decorator: `budgetedClient`

A new file `internal/analyzer/budgeted_client.go` introduces:

```go
type budgetedClient struct {
    inner  ToolLLMClient // non-nil iff wrapping a tool client
    text   LLMClient     // == inner if tool client; otherwise the wrapped non-tool client
    budget int           // raw MaxInputTokens; 0 disables the check
}

type ErrTokenBudgetExceeded struct {
    Provider, Model string
    Counted, Budget int   // Budget is post-margin (0.9 × MaxInputTokens)
    Where           string // "judge", "drift-investigator", "page-analyzer", …
}
func (e ErrTokenBudgetExceeded) Error() string { /* user-facing one-liner with hint */ }
```

The decorator implements `LLMClient` and (when the inner is tool-capable)
`ToolLLMClient`. It is constructed once per tier in `internal/cli/tier.go`
right after `NewBifrostClient`, so every analyzer call site is unchanged.

#### Counting

All counting uses the local cl100k_base estimator (`countTokens` in
`internal/analyzer/tokens.go`). The decorator counts the entire payload it
would send: system prompt + chat history + tool definitions + JSON schema (if
any). Numbers approximate for non-OpenAI models but the 10% margin in (1)
plus the additional 0.9 gate (below) is more than enough to absorb tokenizer
skew.

#### Gating

A request is permitted iff:

```
estimated <= int(0.9 * float64(MaxInputTokens))
```

When `MaxInputTokens == 0`, the gate is skipped entirely.

#### Single-shot path

`Complete`, `CompleteJSON`, `CompleteJSONMultimodal` all share the same shape:

```
1. count(payload)
2. if count > 0.9 * budget:  return ErrTokenBudgetExceeded{...}
3. else:                     return inner.<call>(...)
```

The decorator never edits the payload. Compaction is the caller's job.

#### Multi-turn path (`CompleteWithTools`)

The decorator wraps the per-turn callable inside `BifrostClient`:

```go
gated := func(ctx context.Context, msgs []ChatMessage, tools []Tool) (ChatMessage, error) {
    if c.budget > 0 {
        n := tiktokenForMessages(msgs, tools)
        if n > int(0.9 * float64(c.budget)) {
            return ChatMessage{}, ErrTokenBudgetExceeded{Where: "drift-investigator", ...}
        }
    }
    return realTurn(ctx, msgs, tools)
}
return runAgentLoop(ctx, gated, msgs, tools, opts...)
```

`runAgentLoop` gets one new branch: when `next` returns
`ErrTokenBudgetExceeded`, terminate cleanly with the rounds completed so far
— the same shape as `ErrMaxRounds`. Partial accumulator state in tool
handlers (e.g. `note_observation` appending into `observations`) survives,
because the loop just returns rather than discarding.

### 3. Per-tool-result hard cap (preventative)

Inside `runAgentLoop`, where each tool result is appended to the message
history, results larger than `0.5 × 0.9 × MaxInputTokens` are clipped:

```
if budget > 0 && tiktoken(result) > 0.45 * budget:
    result = clip(result, 0.45 * budget) +
             "\n\n[truncated: ~N tokens omitted from this tool result]"
```

The marker is plain text the LLM sees, so it can choose to call again with a
narrower argument or move on. This is *not* a failure path — it's an
input-reshaping step that prevents one giant file from single-handedly
busting the next turn's budget.

The cap is plumbed into the loop via a new `AgentOption`
(`WithMaxToolResultTokens(int)`) that the BifrostClient sets on the loop when
the model has a budget. Clients with `MaxInputTokens == 0` get no cap.

## Failure modes per call site

### Drift investigator (multi-turn)

```go
_, err := client.CompleteWithTools(ctx, messages, tools, WithMaxRounds(budget))
if errors.Is(err, ErrMaxRounds) || asTokenBudget(err) {
    if len(observations) == 0 {
        // First turn was already over budget. We have nothing to judge.
        // Return a typed error so the caller does NOT write a drift-cache
        // entry — a re-run with a bigger model retries cleanly.
        return nil, fmt.Errorf("investigateFeatureDrift %q: %w",
            entry.Feature.Name, ErrTokenBudgetExceeded{...})
    }
    log.Warnf("drift investigator hit token budget for %q (%d files, %d pages); handing %d observations to judge",
        entry.Feature.Name, len(entry.Files), len(pages), len(observations))
    return observations, nil
}
```

`DetectDrift` then has to handle the typed error from
`investigateFeatureDrift`: log the feature as un-investigated and **do not**
call `onFeatureDone` for it. The cache stays clean; a re-run with `--llm-typical=<bigger-model>` picks it up fresh.

### Drift judge (single-shot, compaction-eligible)

The judge prompt is `feature header + N observations + page-role hints +
rubric`. When the observations are what blew the budget — the most common
pathological case — split:

```go
issues, err := client.CompleteJSON(ctx, prompt, judgeSchema)
if asTokenBudget(err) {
    chunks := chunkObservationsToFit(observations, header, rubric, budget)
    issues, err = judgeChunked(ctx, client, feature, chunks)
}
```

`chunkObservationsToFit` packs observations greedily so each chunk's prompt
fits under `0.9 × budget`. `judgeChunked` runs the judge once per chunk and
concatenates issues; a final dedupe pass is optional (we accept minor
duplication rather than risk a second over-budget call to merge).

If a single observation's quotes alone exceed the per-chunk budget, both
quotes are clipped to a 1500-char max with a `[…]` marker before chunking.
Rare in practice.

This compaction is **lossless at the observation level**: every observation
the investigator surfaced is still seen by the judge. The only quality cost
is that an issue spanning two chunks won't be merged.

### Page analyzer (single-shot, refuse)

```go
result, err := client.CompleteJSON(ctx, prompt, schema)
if asTokenBudget(err) {
    log.Warnf("skipping page analysis for %s: %v", url, err)
    return nil, nil // skip this page; run continues
}
```

No compaction. Pages this big are rare and usually indicate a flattened API
reference. Surfacing the refusal in the log is the right signal.

### Screenshot pass (single-shot, refuse)

Same as page analyzer.

### Mapper batch (single-shot, fatal-for-batch)

`batcher.go` sizes batches before sending. If the budget gate fires on a
mapper batch, that's a regression in the batcher, not a compaction
opportunity. Log a clear error pointing at the batcher and skip the batch;
the run continues with the remaining batches.

### Classifier (`isReleaseNotePage`)

Already passes a 1000-char preview. The gate cannot fire.

## Testing

All behavior tested against the fake LLM client (no network):

1. **Capability table**
   - `MaxInputTokens` populated on every known model.
   - `ResolveCapabilities` returns `100000` for unknown model on known provider.
   - `ResolveCapabilities` returns `0` (and no fallback) for unknown provider.

2. **Decorator** (single-shot)
   - Under-budget: passthrough, inner sees the call.
   - Over-budget: returns `ErrTokenBudgetExceeded` with populated fields;
     inner is *not* called.
   - `MaxInputTokens == 0`: passthrough regardless of size.

3. **Decorator** (multi-turn)
   - Loop terminates with `ErrTokenBudgetExceeded` on the round that would
     exceed budget; partial state in tool handlers preserved.
   - Per-tool-result cap clips the result and appends the truncation marker.

4. **Drift investigator**
   - On `ErrTokenBudgetExceeded` with non-zero observations: logs warning,
     hands observations to judge, persists feature.
   - On `ErrTokenBudgetExceeded` with zero observations: returns typed error,
     `DetectDrift` does NOT write a drift-cache entry for the feature.

5. **Drift judge compaction**
   - With N observations whose combined size fits: single call, baseline
     issues.
   - With N observations whose combined size does NOT fit: chunked into M
     calls, every observation appears in some chunk's prompt, issues
     concatenated.
   - With one observation whose quotes alone exceed per-chunk budget: quotes
     clipped to 1500 chars with marker, judge sees the clipped form.

6. **Page analyzer / screenshot pass / mapper**
   - On `ErrTokenBudgetExceeded`: logs the skip, run continues, exit code is
     unaffected.

7. **Verification**
   - Re-run the original failing fixture (Standard library grammar
     configuration). Run completes without the "Input tokens exceed the
     configured limit" error. Drift findings present for that feature OR a
     log line indicating partial-observation handoff.

## Files touched

- `internal/cli/capabilities.go` — add `MaxInputTokens`, populate rows,
  conservative default.
- `internal/analyzer/client.go` — mirror the field on `ModelCapabilities`.
- `internal/analyzer/budgeted_client.go` *(new)* — decorator,
  `ErrTokenBudgetExceeded`, message-counting helpers.
- `internal/analyzer/budgeted_client_test.go` *(new)*.
- `internal/analyzer/agent_loop.go` — handle `ErrTokenBudgetExceeded` like
  `ErrMaxRounds`; add `WithMaxToolResultTokens` and the per-result clip.
- `internal/analyzer/agent_loop_test.go` — extend.
- `internal/analyzer/drift.go`
  - `investigateFeatureDrift` recognises the new error and the
    "zero observations" guard.
  - `judgeFeatureDrift` adds the chunked-judging compaction path.
  - `DetectDrift` does not call `onFeatureDone` when investigation
    returned the un-investigated typed error.
- `internal/analyzer/drift_test.go` — extend.
- `internal/analyzer/analyze_page.go`, `screenshot_gaps.go`, `mapper.go` —
  catch and log the budget error per call site.
- `internal/cli/tier.go` (or wherever `NewBifrostClient` is wired) — wrap
  every constructed client in `budgetedClient`.

## TDD order

1. Capability table changes + tests.
2. `ErrTokenBudgetExceeded` + decorator (single-shot path) + tests.
3. Decorator multi-turn path + agent-loop integration + tests.
4. Per-tool-result cap + tests.
5. `judgeFeatureDrift` chunking + tests.
6. `investigateFeatureDrift` + `DetectDrift` recovery branches + tests.
7. Page analyzer / screenshot / mapper call-site refusals + tests.
8. Tier wiring (constructs `budgetedClient` per tier).
9. End-to-end fixture verification.

Each step ships its own commit per the project's TDD rules.
