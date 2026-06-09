# Drift Investigator Token-Overflow Fix

## Status

Fix #3 and Fix #2 IMPLEMENTED (2026-06-09). Fix #1 descoped by developer.
Commits on branch `dakar-v4`. Fix #3 = `233357e`. See PROGRESS.md for the
per-fix record. Remaining: real-API verification (Scenarios 9, 20) — not run
here (no live key in this session).

## The incident

```
detect drift: DetectDrift "Predictor primitives": bifrost tool completion:
Input tokens exceed the configured limit of 272000 tokens.
Your messages resulted in 317507 tokens. Please reduce the length of the messages.
```

### Verified facts

1. **The failing call is the drift *investigator*, not the judge.** The string
   `bifrost tool completion` is emitted only by `completeOneTurn`
   (`internal/analyzer/bifrost_client.go:414`), the single-turn function inside
   the investigator's multi-turn agent loop (`runAgentLoop` →
   `CompleteWithTools`). The judge stage uses `CompleteJSON` and a different
   wrap.

2. **The chunking/compaction we already shipped only covers the judge.**
   `chunkObservationsForJudge` / `judgeFeatureDrift` (`internal/analyzer/drift.go`)
   greedy-packs observations under `0.9 × MaxInputTokens` before each
   `CompleteJSON`. The investigator's agent loop has **no compaction** — it just
   appends every `read_file` / `read_page` tool result to the message history.
   Its only defense is the per-turn gate in `budgetedClient`.

3. **The gate was sized against a wrong number.** `internal/cli/capabilities.go`
   sets every Gemini model to `MaxInputTokens: 900000`, so the gate budget was
   `0.9 × 900000 = 810000`. The history (≈317507 by Gemini's count) passed under
   that, the gate stayed silent, and the request went out.

4. **272000 is NOT the model's real limit.** `gemini-3.5-flash` (the default
   typical tier, which runs the investigator — `tier_validate.go:28`) publishes a
   **1,048,576-token** context window, confirmed against OpenRouter, llm-stats,
   and ai.google.dev (June 2026). The error says "**configured** limit of
   272000" — Google-side wording for an account/project/tier-enforced cap.
   Bifrost's native Gemini provider (`core@v1.5.2/providers/gemini/`) contains no
   such string, so it is Google's error passed through verbatim.

5. **The rejection aborts the whole feature.** Because Google's overflow error is
   a raw provider error, not our typed `ErrTokenBudgetExceeded`, the
   investigator's recovery path (`investigateFeatureDrift`, drift.go:571–592 —
   "hand partial observations to the judge") never matched it. It fell through to
   `return nil, err`, which `DetectDrift` wrapped and propagated, killing the
   `"Predictor primitives"` feature and the run.

## The design tension to confirm before coding

**272000 looks account/tier-specific, not universal.** Hardcoding 272000 (or a
0.9× derivative) into the Gemini rows in `capabilities.go` would needlessly cap
users whose accounts honor the full 1M window. Leaving it at 900000 reproduces
this crash for accounts capped at 272K.

This is exactly why **fix #3 (recognize provider-side overflow) is the real
safety net** and #1 is only a guess at a moving target. Recommended posture:

- #1: lower the table value to a conservative, broadly-honored default and
  document it as a heuristic floor, not a measured cap.
- #3: make the system degrade gracefully *regardless* of table accuracy.
- #2: let long investigations compact and continue instead of truncating to
  partial observations.

**Open question for the developer:** do we know whether 272000 is a property of
this specific Gemini key/project (paid tier, preview cap, regional quota), or a
broader default? The answer decides the #1 table value. If unknown, I propose
`MaxInputTokens: 260000` for the Gemini rows (gate fires at 234000, safely under
the observed 272000) and rely on #3 for accounts that are even tighter. **Do not
start #1 until this value is confirmed.**

---

## Fix #1 — DESCOPED (developer decision, 2026-06-09)

The developer chose **not** to change the Gemini `MaxInputTokens` rows. The
table stays at 900000. Correctness instead rests entirely on Fix #3 (recognize
the provider-side overflow and recover) and Fix #2 (compact so the loop continues
before the provider ever rejects). The section below is retained for context
only; do not implement it.

### (not implemented) Correct the Gemini `MaxInputTokens`

**File:** `internal/cli/capabilities.go` (the eight `gemini` rows, lines 98–105)
and the rationale comment at 88–97.

**TDD:**

- RED: `internal/cli/capabilities_test.go` — add
  `TestGeminiMaxInputTokensConservative` asserting every `gemini` row's
  `MaxInputTokens` is `<= 260000` (or the confirmed value). Today it returns
  900000 → fails.
- GREEN: change the eight rows to the confirmed value. Rewrite the comment: the
  published window is 1,048,576 but the effective enforced ceiling observed in
  production was 272000, so the table sits conservatively below it; the real
  guard is the provider-overflow classifier (fix #3).
- Verify: `go test ./internal/cli/...`

**Caveat to surface in the comment:** this number is a heuristic floor, not a
measured per-account cap. The classifier in fix #3 is what makes correctness
independent of it.

---

## Fix #3 — Map provider-side overflow → `ErrTokenBudgetExceeded` (the safety net)

Do this **before** #2; it is what makes the run survive any future table drift,
and #2's value depends on the recovery path existing.

**Files:**
- `internal/analyzer/bifrost_client.go` — new classifier + wrap.
- `internal/analyzer/bifrost_client_test.go` — classifier unit tests.
- `internal/analyzer/drift.go` — confirm the investigator recovery already
  catches `ErrTokenBudgetExceeded` (it does, drift.go:576–592); add a test that
  a provider-overflow error surfaced from `CompleteWithTools` is handled as
  "hand partial observations to judge" when observations exist, and as a typed
  skip (no cache entry) when none exist.

**Design:**

1. Add `isContextOverflowBifrostError(e *schemas.BifrostError) bool` next to
   `isRateLimitBifrostError`. Match HTTP 400 **and** an overflow phrase. Phrase
   list (lowercased `Contains`): `"exceed the configured limit"`,
   `"exceeds the maximum number of tokens"`, `"reduce the length of the messages"`,
   `"maximum context length"`, `"input token count"`. Keep it a named slice
   mirroring `overloadPhrases` so it is greppable and testable.

2. In `wrapBifrostError`, after the rate-limit branch, add an overflow branch
   that returns `ErrTokenBudgetExceeded{Provider, Model, Counted, Budget, Where}`.
   `Counted`/`Budget` are unknown at this layer (the provider counted, not us) —
   parse them from the message when present, else leave 0. `Where` = the `stage`
   string. Wrap so `errors.Is(err, ErrTokenBudgetExceeded{})` matches.
   - Note: `ErrTokenBudgetExceeded` lives in `budgeted_client.go`, same package —
     constructible directly.

3. `completeOneTurn` already routes its error through `wrapBifrostError`
   (line 414), so the investigator loop will now see a typed error. No change to
   `runAgentLoop` needed: it returns the error up, and `investigateFeatureDrift`
   already branches on `errors.Is(err, ErrTokenBudgetExceeded{})`.

**TDD:**

- RED: `bifrost_client_test.go` —
  `TestIsContextOverflowBifrostError` (table: the real 272000 message → true;
  a 400 unrelated → false; a 429 rate-limit → false/handled by the other
  classifier) and `TestWrapBifrostError_Overflow` (asserts
  `errors.Is(wrapped, ErrTokenBudgetExceeded{})`). Both fail today (no
  classifier).
- GREEN: implement the classifier and the wrap branch.
- RED: `drift_test.go` —
  `TestInvestigateFeatureDrift_ProviderOverflowWithObservations` (stub
  `ToolLLMClient.CompleteWithTools` returns the typed overflow error after
  observations are recorded → returns partial observations, nil error) and
  `TestInvestigateFeatureDrift_ProviderOverflowNoObservations` (typed overflow,
  zero observations → returns the typed error so `DetectDrift` skips the cache
  write). Verify against drift.go:576–592 semantics.
- GREEN: should pass with no drift.go change if the existing branch is correct;
  if the existing `ErrTokenBudgetExceeded` branch needs the partial-observation
  path widened, adjust minimally.
- Verify: `go test ./internal/analyzer/...`

**Ordering precedence:** in `wrapBifrostError`, check rate-limit first, then
overflow, then default. A 400 is never a 429/503, so they don't collide, but
keep the explicit order for readability.

---

## Fix #2 — Compact the investigator's message history (largest, removes the cliff)

Today, when the gate fires the investigator *stops* and hands over whatever
observations it had. That degrades coverage on large features. Real compaction
lets the loop keep going.

**Insight that makes this cheap:** observations are captured out-of-band in the
`observations` slice via `note_observation`. Once an observation is recorded, the
verbatim `read_file` / `read_page` tool-result text that produced it is largely
redundant in the message history. So compaction = elide old tool-result *bodies*
while preserving assistant turns, tool-call structure, and the recorded
observations.

**Files:**
- `internal/analyzer/agent_loop.go` — add an optional pre-turn compaction hook.
- `internal/analyzer/budgeted_client.go` — wire compaction into
  `CompleteWithTools` alongside the existing gate.
- `internal/analyzer/agent_loop_test.go` / `budgeted_client_test.go` — tests.

**Design (proposed — confirm before building):**

- Add `AgentOption` `WithCompactor(fn func(messages []ChatMessage, budget int) []ChatMessage)`.
- In `runAgentLoop`, before the pre-turn hook, if a compactor is set and the
  estimated payload exceeds the budget, call it to produce a smaller history.
  Re-estimate; if still over, *then* the existing pre-turn gate fires (current
  behavior — graceful stop). So compaction is a strictly-additional layer; the
  ErrTokenBudgetExceeded path remains the backstop.
- `budgetedClient.CompleteWithTools` supplies a default compactor that walks the
  oldest `role:"tool"` messages and replaces `Content` with
  `"[earlier read elided to fit context — re-read if needed]"`, oldest-first,
  until the estimate fits `gated`. Never elides: the seeded system prompt
  (index 0), the most recent N tool results (keep recency for the active line of
  reasoning), or any assistant message (tool-call structure must stay intact for
  provider validation).
- Preserve `CacheBreakpoint` invariants: re-run `rotateCacheBreakpoint` shape
  after compaction so caching still works.

**TDD:**

- RED: `agent_loop_test.go` — `TestRunAgentLoop_CompactsWhenOverBudget`
  (compactor invoked, loop continues past the point a no-compactor loop would
  have hit the gate; observations preserved). `TestRunAgentLoop_CompactorPreservesStructure`
  (no assistant/tool-call message dropped; system prompt intact).
- RED: `budgeted_client_test.go` — `TestCompactorElidesOldestToolResults`
  (oldest tool bodies replaced first; recent ones kept; estimate drops under
  `gated`).
- GREEN: implement the option, the loop wiring, and the default compactor.
- Verify: `go test -race ./internal/analyzer/...`

**Risk to call out:** providers validate that every `tool_call` has a matching
`tool` result. Eliding a tool result's *body* (keeping the message + ToolCallID)
is safe; *dropping* the message is not. The compactor must only rewrite
`Content`, never remove tool messages.

---

## Build order

1. **Fix #3** (safety net) — makes the run survive overflow regardless of table.
2. **Fix #1** (table value) — once the developer confirms the number.
3. **Fix #2** (compaction) — removes the coverage cliff; largest surface area.

#3 first means even if #1's number is wrong for some account, the feature is
skipped-and-cached instead of crashing the run.

## Per-CLAUDE.md gates (every fix)

- Failing test first, watched fail for the right reason, then minimal code.
- `go test ./...` green, `go build ./...` clean, `golangci-lint run` clean,
  ≥90% statement coverage on touched packages.
- Commit after each RED-GREEN-REFACTOR cycle with the TDD-format message.
- Update `PROGRESS.md`.

## Verification (real, no mocks — per VERIFICATION_PLAN.md)

- **Scenario 9 (end-to-end real docs site)** and **Scenario 20 (Gemini provider
  end-to-end)** must both pass on the Gemini ladder. Add a targeted manual check:
  run `analyze` against a fixture large enough that the investigator's history
  for at least one feature exceeds 272000 Gemini tokens, on a Gemini key with the
  272000 cap, and confirm:
  - the run exits 0 (no abort),
  - the offending feature either completes via compaction (#2) or is
    skipped-and-cached with a `token budget` warning (#3 fallback),
  - a re-run resumes cleanly from the drift cache.
- Confirm Anthropic/OpenAI ladders are unaffected (their `MaxInputTokens`
  unchanged; classifier only adds a branch).
