# Anthropic Prompt Caching — Design

**Status**: Design **revised 2026-04-27** after Task 0 spike findings. See *Bifrost API Findings (Verified)* below — original assumptions about tool-level caching were wrong; user-message-content-block caching is the supported path.

## Goal

Cut Anthropic API spend by enabling prompt caching on every Anthropic call site that has a stable prefix large enough to cache. Provider-gated: OpenAI and Ollama paths are untouched.

## Motivation

Find the Gaps makes a lot of Anthropic calls per analysis run:

- **Drift investigator** (`internal/analyzer/drift.go:296`) — multi-turn agent loop, replays the entire transcript every turn. The same three tool definitions (`read_file`, `read_page`, `note_observation`) and the per-feature investigator prompt are re-billed at full input rate on every round.
- **Drift judge** (`drift.go:judgeFeatureDrift`) — single `CompleteJSON` call per feature; all calls share the same `judgeSchema` JSON-schema.
- **Feature mapping** (`mapper.go`), **page summaries** (`analyze_page.go`), **doc-feature mapping** (`docs_mapper.go`), **screenshot gaps** (`screenshot_gaps.go`), **synthesis** (`synthesize.go`), **code-feature extraction** (`code_features.go`), **drift page classifier** (`drift.go:classifyDriftPages`) — `CompleteJSON` calls that reuse the same schema across many invocations in one run.

None of these set `cache_control` today. Cache reads cost ~10% of base input price; cache writes cost ~125% (5-minute TTL). Break-even is two requests inside the TTL — every multi-turn loop and every batched `CompleteJSON` clears that on the second call.

## Non-Goals

- Caching for OpenAI or Ollama paths. (OpenAI has automatic caching, no API surface to manage; Ollama has no caching.)
- 1-hour TTL. The 5-minute default is sufficient for a single analysis run; the 1-hour write premium (2×) costs more than the gain at our request frequency.
- Changing call ordering or batching strategy.
- A user-facing kill switch. Caching is purely additive; if it breaks something we revert the code, not toggle a flag.

**Explicitly in-goal:** caching is applied **everywhere the Anthropic API allows it**, including single-turn `Complete` and `CompleteJSON` calls whose prompts may never repeat byte-for-byte in a run. The user's directive is "always cache." See *Cost Analysis Per Call Site* below for what this costs in the worst case.

## Bifrost API Findings (Verified by Task 0 Spike)

The following were verified empirically against `github.com/maximhq/bifrost/core@v1.5.2` and the Anthropic API on 2026-04-27. Treat these as project-specific gotchas — they are not all in the Bifrost docs and the docs that exist are partially out of date.

| Finding | Evidence | Implication for design |
|---|---|---|
| **User-message content-block caching works.** Set `cache_control` on a `schemas.ChatContentBlock` inside `ChatMessageContent.ContentBlocks`. After warmup, repeated identical requests read from cache. | Direct SDK spike: 4 calls all read 4802 tokens. Bifrost user-block spike (warmed): both calls read 4804 tokens. | This is the primary cacheable path we will use. |
| **Tool-level caching in Bifrost appears non-deterministic.** Setting `cache_control` on `schemas.ChatTool.CacheControl` produces a different `cached_write_tokens` count between two identical calls (5229 vs 5556 — a 327-token delta that cannot be explained by tail content or propagation lag). | Original Task 0 spike. | **Do not rely on tool-level caching in Bifrost.** Drop that breakpoint from the placement plan. |
| **Anthropic has a propagation lag for fresh cache entries.** Two calls fired within ~1 second of each other on a freshly-written prefix both report `cache_creation_input_tokens > 0` and `cache_read_input_tokens = 0` — neither sees the other's write. After several seconds the entry becomes globally readable. | Direct SDK spike: first run cw=4802, cw=4802 (no read). Second run with delays: cr=4802 on every call. | **Production cadence is fine** (drift rounds are seconds apart, well past the lag). Spike tests must include a delay between calls or accept warm-cache reads. |
| **`schemas.CacheControl.Type` is the typed constant `CacheControlTypeEphemeral`, not `*string`.** The Opus 4.7 chat transcript referenced `Type: schemas.Ptr("ephemeral")` — that does not match v1.5.2. | `go doc github.com/maximhq/bifrost/core/schemas CacheControl`. | Use `&schemas.CacheControl{Type: schemas.CacheControlTypeEphemeral}` exactly. |
| **There is no `SystemMessages` field on `BifrostChatRequest` (v1.5.2 or v1.5.5).** The Opus 4.7 transcript described one. | Confirmed by `go doc` against both versions. | System content goes in the `Input` slice as `Role: schemas.ChatMessageRoleSystem`. |
| **Cache token usage lives on `Usage.PromptTokensDetails`** as `CachedReadTokens` / `CachedWriteTokens`, not on top-level `CacheReadInputTokens` / `CacheCreationInputTokens` as the original plan assumed. | `go doc github.com/maximhq/bifrost/core/schemas ChatPromptTokensDetails`. | Verbose-logging task surfaces these field names. |

## Mechanism

Cache breakpoints attach to **content blocks within messages**. Bifrost's Anthropic provider passes `cache_control` through to the wire (`providers/anthropic/chat.go:144` for the tool path, content-block conversions elsewhere). The supported placement for our use is on `ChatContentBlock` instances inside `ChatMessageContent.ContentBlocks` of a user message.

The current `BifrostClient.completeOneTurn` uses the simpler `ContentStr` form for every message. To attach a cache marker, the message has to be rendered as a one-block `ContentBlocks` slice instead. The implementation plan does this conversion in one place (gated on `c.provider == schemas.Anthropic` and a per-message `CacheBreakpoint` flag); callers continue to use `ChatMessage{Content: string}` unchanged.

**What we deliberately do NOT do:** set `cache_control` on `schemas.ChatTool.CacheControl`. That path is broken in v1.5.2 (see findings table). If a future Bifrost release fixes it we can revisit, but the design here does not depend on it.

## Placement Strategy

Anthropic allows at most **4 `cache_control` breakpoints per request**, walked in order `tools` → `system` → `messages`. We use only the message-content-block path (the only verified-working path in Bifrost). Tool-definition breakpoints are explicitly excluded — see *Bifrost API Findings*.

### `CompleteWithTools` (drift investigator)

Two breakpoints per request, applied inside `completeOneTurn` when `c.provider == schemas.Anthropic`:

1. **First user message** (the investigator system prompt) — per-feature, byte-identical across every round of the same feature's investigation. Marker stays put for the lifetime of the loop; rounds 2..N read it from cache.
2. **Most-recently-appended message — rotating per turn.** On each turn, mark the latest appended message; on the next turn, that message becomes part of the cached prefix and the marker moves to the new tail. Breakpoint #1 remains a valid read point, so each round reads the entire prior transcript at ~10% of base input rate.

Per-turn savings on a feature with B rounds:

- Round 1: cache miss, write the investigator prompt (1.25× write premium on those bytes only).
- Round 2: read the prompt + round-1 turn from cache; pay full rate only on tool-result content appended this turn; write the new tail.
- Rounds 3..B: read everything except the newest tail; same pattern.

**Cross-feature savings are gone** in this revised design (used to come from caching the tool definitions, which Bifrost can't reliably do). The investigator prompt cache is per-feature only.

Mechanically: the rotating marker requires unflagging the previous tail when a new message is appended. Implemented in `runAgentLoop` (where messages are appended) so callers don't have to think about it.

Anthropic's 4-breakpoint budget: seeded user message (1) + rotating tail (1) = 2, leaves 2 spare.

### `CompleteJSON` (Anthropic forced-tool-use branch)

One breakpoint per request:

1. **The user prompt** — sent as a content block carrying `cache_control`. If the same prompt is re-sent (retry, deterministic batching), the second call reads it from cache. If not, the marker writes (1.25× premium) but never reads — accepted per the always-cache directive; absolute dollar impact is small.

The original design also cached the `respond` tool definition (which carries the schema). **This is dropped** because Bifrost's tool-level cache_control is non-deterministic. Cross-call schema reuse via the cache is no longer available.

### `Complete` (plain one-shot)

One breakpoint:

1. **The user prompt** — single message, marked as a cache breakpoint.

Production caller: the drift page classifier (`drift.go:459`). Each call passes a different page body, so within a single run the cache never reads. We mark it anyway per the always-cache directive.

### `CompleteJSON` (OpenAI/Ollama branches)

Untouched. No caching API surface.

## Cost Analysis Per Call Site (Revised)

| Call site | Best case | Worst case | Net |
|---|---|---|---|
| `CompleteWithTools` (drift investigator, B rounds × N features) | Round 2..B reads investigator prompt + transcript from cache. | Every feature has B=1 → no rounds to amortize the write premium across. | **Strongly positive whenever B ≥ 2** (the typical case — drift investigations almost always run multiple rounds). |
| `CompleteJSON` Anthropic, varying user prompt | Same prompt re-sent in 5 min (retry). | Every prompt unique → write premium, no read. | **Small loss** in the common case. Was a *win* in the original design via tool/schema caching; that win is now gone. |
| `Complete` (drift page classifier) | Same page re-classified within 5 min. | Every page unique within a run (the common case). | **Small loss** on the cached portion of the prompt. Classifier prompt is bounded; absolute dollar impact is small. |
| Below cache minimum (Sonnet <2048 tokens, Opus <4096 tokens) | n/a | n/a | **Zero** — Anthropic silently no-ops the marker, no write, no read, no cost. |

**Net across a typical run is still positive** because the drift investigator dominates spend by a large margin and that win is intact. The losses on `CompleteJSON` and `Complete` in the worst-case-unique-prompt scenarios are bounded by their ~25% write premium on bounded-size prompts — measurable but small relative to the investigator savings.

**Estimated overall savings vs the original design:** smaller. The original assumed wins from cross-feature tool-definition reuse (gone) and cross-call schema reuse in `CompleteJSON` (gone). What remains is the per-feature drift investigator transcript cache, which is the largest single line item — so most of the dollar value is preserved.

If post-implementation measurement shows the lost `CompleteJSON` schema-cache win materially hurts (or `Complete` write premium becomes meaningful), follow-ups in priority order:

1. **Restructure prompts** to put a stable instruction prefix first and varying content last, then cache the prefix as a user-message content block. (Works in Bifrost today.)
2. **File a Bifrost upstream issue / PR** for the tool-level cache_control non-determinism. If fixed, restore tool-definition breakpoints in a follow-up.
3. **Add a thin `anthropic-sdk-go` adapter** for the Anthropic-bound calls only, bypassing Bifrost. Largest architectural change; only if (1) and (2) prove insufficient.

## Silent Invalidators — Audit Before Shipping

A single byte change anywhere in the cached prefix invalidates it. The Go code that builds prompts must be checked for these patterns. A non-deterministic prefix means **every request misses the cache** while still paying the write premium — strictly worse than no caching.

| Pattern | Where to check | Fix |
|---|---|---|
| `time.Now()` interpolated into a prompt | `grep -rn "time.Now\|time\.Now" internal/analyzer/` | Move out of the prompt or render before the breakpoint with a fixed value (e.g. date only). |
| `for k, v := range map[...]` building prompt strings or tool slices | every `// PROMPT:` site | Sort keys with `sort.Strings` before iterating. Go map order is randomized per run. |
| `json.Marshal(map[...])` for tool input schemas | `bifrost_client.go:307` (`json.Unmarshal(schema.Doc, &params)` round-trip) | Verify `schema.Doc` is byte-stable across calls. If `Doc` is built dynamically, switch to a sorted-key encoder. |
| Per-call IDs (commit SHA, run UUID, request ID) in the system prompt or any tool description | grep for `uuid`, `sha`, `runID` near prompt construction | Move to a non-cacheable position (after the last breakpoint). |
| Tool slice order varying between calls | `drift.go:246` (tools assembled from a slice literal — fine) and any caller that builds tools from a map | Always slice literals in declaration order. |

The audit is mechanical and short — there's only ~10 prompt sites total in `internal/analyzer/`.

## Verification

The Bifrost response surfaces cache token counts on `resp.Usage.PromptTokensDetails` as `CachedReadTokens` and `CachedWriteTokens` (NOT the top-level `CacheReadInputTokens` / `CacheCreationInputTokens` fields the original plan assumed — those are on the Anthropic SDK's `Usage`, not Bifrost's). The plan wires these into the existing verbose-logging path (`.plans/VERBOSE_LOGGING.md`) so a single `find-the-gaps analyze --verbose` run shows per-call cache activity.

**Acceptance signal:** on a multi-feature drift run, `CachedReadTokens` is non-zero on round 2+ of every feature investigation. (Cross-feature reuse is no longer expected — see *Placement Strategy* — so we do not assert reads on round 1 of feature 2..N.)

**Important — propagation lag:** Anthropic's cache requires several seconds to become globally readable after a fresh write. Two calls fired milliseconds apart will both write and neither read; this is normal and not a bug. Spike tests must include a deliberate delay or accept that calls 1+2 of a cold run may both miss while calls 3+ read.

If `CachedReadTokens == 0` across an entire production run with multiple drift rounds spaced seconds apart, then a silent invalidator is present and Task 1's audit missed something. Diff the rendered prompt bytes between two requests to find it.

## Risks and Mitigations

| Risk | Mitigation |
|---|---|
| Bifrost's user-message content-block path silently drops `cache_control` for the chat-completion endpoint. | **Already disproved.** Task 0 spike (warmed) showed both calls reading 4804 tokens through the Bifrost user-block path. |
| Prefix below the cache minimum (2048 for Sonnet, 4096 for Opus). | Anthropic silently no-ops on too-small prefixes — no error, just zero cache writes. Acceptable cost (zero); we don't gate on size. |
| Switching the analyzer model invalidates the cache. | Caches are model-scoped — this is correct behavior. No mitigation needed. |
| Bifrost's tool-level `cache_control` non-determinism is actually a freshness-lag artifact and not a real bug. | Possible but unlikely. The 327-token cache_write delta on the original tool spike cannot be explained by the ~13-token tail or by the propagation lag (which produces zero reads, not larger writes). The revised design dropping tool caching is conservative; if the upstream is fixed or proven correct, we can add tool breakpoints in a follow-up at low cost. |
| A future Bifrost upgrade changes the chat-completion serialization in a way that breaks the user-block path too. | Verbose-logging task surfaces cache hit/miss every run; a regression would show up immediately as zero `CachedReadTokens`. Manual end-to-end verification (Task 8) gates the PR. |

## Open Questions

- **Should we file a Bifrost upstream bug for the tool-level cache_control non-determinism?** Yes, eventually. Out of scope for this plan; tracked as a follow-up.
- **Does the propagation lag exceed any production cadence we care about?** No — the LLM call itself takes seconds, well past the lag window. Confirmed by direct-SDK spike with mixed delays.
