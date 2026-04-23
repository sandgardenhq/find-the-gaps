# Structured Outputs Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. TDD discipline per CLAUDE.md — every task is RED → GREEN → REFACTOR.

## Goal

Replace free-text "return JSON" prompts with provider-native structured outputs so the LLM produces parse-guaranteed JSON (or tool-call arguments) rather than fenced prose. Delete the `stripCodeFence` defense once every site is migrated.

## Why

The tiering refactor (#5) routed several analyzer call sites to the `Small` tier, where cheaper models routinely ignore "no markdown code fences" instructions and wrap output in ```` ```json ```` blocks. This broke `ftg analyze` in production:

```
WARN skipping https://doc.holiday/docs/: AnalyzePage ...: invalid JSON response: invalid character '`' looking for beginning of value
```

Commit `d3b7c91` added `stripCodeFence` as a defensive layer. That stops the bleed but leaves the underlying fragility: we're still asking the LLM to self-police output format. Structured outputs remove the class of bug.

**Prerequisite:** `stripCodeFence` must stay in place until the migration lands. It is belt AND suspenders — local models (Ollama / LM Studio) may never gain reliable structured-output support, so the strip is cheap insurance even after this plan ships.

## Scope

Seven JSON-parsing call sites across `internal/analyzer`:

| # | Site | Tier | Response shape |
|---|---|---|---|
| 1 | `AnalyzePage` (`analyze_page.go`) | Small | `{summary: string, features: string[]}` |
| 2 | `SynthesizeProduct` (`synthesize.go`) | Small | `{description: string, features: string[]}` |
| 3 | `mapPageToFeatures` (`docs_mapper.go`) | Small | `string[]` |
| 4 | `ExtractFeaturesFromCode` (`code_features.go`) | Typical | `CodeFeature[]` |
| 5 | `MapFeaturesToCode` (`mapper.go`) | Large | `mapEntry[]` (two prompt variants) |
| 6 | `classifyDriftPages` loop (`drift.go:160`) | Small | `driftIssue[]` |
| 7 | `DetectScreenshotGaps` (`screenshot_gaps.go`) | Typical | `screenshotGap[]` |

The agentic tool-use site `detectDriftForFeature` is **in scope as of 2026-04-23**: although it uses `CompleteWithTools` for investigation, its *final* message is free-text JSON, which is what produced the `<response>...</response>`-wrapper bug that kicked this work off. It will be migrated to emit the final answer via a `submit_findings` structured tool call whose arguments match the drift-issue schema.

Out of scope: anything else using `CompleteWithTools` (currently nothing — `detectDriftForFeature` is the only caller).

## Design

### Interface extension

Add a new method to `analyzer.LLMClient`:

```go
type LLMClient interface {
    Complete(ctx context.Context, prompt string) (string, error)
    CompleteJSON(ctx context.Context, prompt string, schema JSONSchema) (json.RawMessage, error)
}
```

`JSONSchema` is a small value type wrapping a JSON Schema document (object or array at the root) plus a name. Call sites define one per response shape, live alongside the `// PROMPT:` comment so schemas are discoverable by the same grep.

Call-site flow:

```go
// PROMPT: Summarizes a single documentation page...
// SCHEMA: {summary: string, features: string[]}
prompt := ...
raw, err := client.CompleteJSON(ctx, prompt, analyzePageSchema)
if err != nil { ... }
var resp analyzePageResponse
_ = json.Unmarshal(raw, &resp) // guaranteed to succeed when err == nil
```

### Per-provider implementations

`BifrostClient.CompleteJSON` dispatches on `c.provider`:

- **Anthropic** — forced tool use. Define a single tool `respond` with `input_schema = schema`, set `ToolChoice = {type: "tool", name: "respond"}`. Return `choice.Message.ToolCalls[0].Function.Arguments` as `json.RawMessage`. Anthropic does not support `response_format: json_schema`; this is the canonical pattern.
- **OpenAI** — `ChatParameters.ResponseFormat = {"type": "json_schema", "json_schema": {"name": schema.Name, "schema": schema.Doc, "strict": true}}`. Return `choice.Message.Content` as `json.RawMessage`.

`OpenAICompatibleClient.CompleteJSON` handles Ollama + LM Studio + generic:

- **Ollama** — set top-level `format: schema.Doc` on the request body (Ollama's native structured-outputs field; it honors a raw JSON Schema). Requires a recent Ollama (≥ 0.5.0).
- **LM Studio** — supports OpenAI-style `response_format: {"type": "json_schema", "json_schema": ...}`. Use the same payload as OpenAI.
- **Generic `openai-compatible`** — attempt `response_format: {"type": "json_object"}` (weaker guarantee, widely supported). Document this limitation; if the endpoint rejects it, surface a clear error and fall back to a `Complete` + `stripCodeFence` path behind a feature flag. **Open question (see below).**

Provider detection inside `OpenAICompatibleClient` is new — today the client doesn't know which flavor it's talking to. We have that information in `cli.buildTierClient`; pass it into the client constructor as a `flavor` string (`"ollama" | "lmstudio" | "openai-compatible"`).

### Schema definitions

One file per response shape, co-located with the call site. Use `encoding/json` `json.RawMessage` for the schema literal so we can keep it as a string and skip a second Marshal. Example:

```go
// internal/analyzer/analyze_page.go
var analyzePageSchema = JSONSchema{
    Name: "analyze_page_response",
    // PROMPT SCHEMA: output shape for AnalyzePage
    Doc: json.RawMessage(`{
      "type": "object",
      "properties": {
        "summary":  {"type": "string"},
        "features": {"type": "array", "items": {"type": "string"}}
      },
      "required": ["summary", "features"],
      "additionalProperties": false
    }`),
}
```

Comment convention: mark schemas with a `// PROMPT SCHEMA:` comment line so `grep -n 'PROMPT' internal/` still shows every LLM contract in one pass.

## Phased task breakdown

Every task is one RED-GREEN-REFACTOR cycle. Commit after each.

### Phase 1 — Foundation

1. **Add `JSONSchema` type** (`internal/analyzer/schema.go`) — `{Name string; Doc json.RawMessage}`, with a `Validate()` method that checks the Doc parses as a JSON object with a `type` field. Test the validator.
2. **Extend `LLMClient` interface** with `CompleteJSON`. Update the fake client in `testhelpers_test.go` to return canned `json.RawMessage` responses keyed by schema name. Every existing test continues to pass.

### Phase 2 — Provider implementations

3. **`BifrostClient.CompleteJSON` for Anthropic** — forced tool use. Integration-style test with a recorded Bifrost response (use `bifrostRequester` double).
4. **`BifrostClient.CompleteJSON` for OpenAI** — `response_format: json_schema` with strict mode. Same test pattern.
5. **`OpenAICompatibleClient.CompleteJSON` — Ollama flavor** — top-level `format` field. Test with `httptest` server asserting request body shape.
6. **`OpenAICompatibleClient.CompleteJSON` — LM Studio flavor** — OpenAI-style `response_format`. Same pattern.
7. **`OpenAICompatibleClient.CompleteJSON` — generic flavor** — `response_format: json_object` with a clear error on reject. Resolve the open question first.

### Phase 3 — Migrate call sites (one commit each)

Migrate in risk order (smallest blast radius first):

8. `mapPageToFeatures` — simplest shape (`string[]`).
9. `AnalyzePage` — the original victim.
10. `SynthesizeProduct`.
11. `classifyDriftPages`.
12. `DetectScreenshotGaps`.
13. `ExtractFeaturesFromCode`.
14. `MapFeaturesToCode` (both prompt variants).

Each migration: delete the "No markdown code fences. No prose." suffix from the prompt, define the schema, swap `Complete` for `CompleteJSON`, delete the `stripCodeFence` wrapper, update tests to assert the schema is passed through. All existing behavioral tests must continue to pass.

### Phase 3b — Migrate drift tool-use site

14b. **`detectDriftForFeature`** — add a `submit_findings` tool whose `input_schema` matches the drift-issue array. Update the prompt: tell the model to call `submit_findings` with its final findings instead of returning prose. In the agent loop, when the model calls `submit_findings`, treat it as the terminal condition: parse `tc.Arguments` via the schema, return the issues, and do not feed a tool result back. Remove the free-text JSON fallback path and the `extractJSONArray` call entirely.

### Phase 4 — Cleanup

15. **Delete `stripCodeFence`, `extractJSONArray`, `fence.go`, `fence_test.go`.** Confirm no call site still uses them (grep). Run full suite; coverage must stay ≥ 90%.
16. **Update CLAUDE.md's "LLM Prompt Conventions" section** — add a `// PROMPT SCHEMA:` rule next to the existing `// PROMPT:` rule.

### Phase 5 — Verification

17. **Add a testscript scenario** under `cmd/find-the-gaps/testdata/script/` that points `analyze` at an ollama-compatible httptest server and asserts the outgoing request body includes the `format` field (or `response_format`, depending on flavor). Covers the happy path end-to-end.
18. **Run all nine scenarios in `.plans/VERIFICATION_PLAN.md`** against real Bifrost + real `mdfetch` + the `doc.holiday` docs site that triggered the original bug. Scenario 1 must pass cleanly — no fence-related warnings.

## Open questions

1. **Generic `openai-compatible` endpoints.** Not every OpenAI-compatible server honors `response_format`. Options:
   - (a) Require structured outputs; fail fast at startup if the endpoint rejects a canary request. Clean contract but narrows what users can point at.
   - (b) Probe once at startup, cache capability, fall back to `Complete + stripCodeFence` when absent. Messier but forgiving.
   - **Recommendation:** (a). Users on exotic endpoints already sign up for "bring your own reliability"; the failure mode for (b) is what we just shipped a hotfix for.

2. **Anthropic strict schemas.** Anthropic's tool input_schema is advisory — the model usually obeys but isn't guaranteed to. In practice this is reliable with recent Sonnet/Haiku; do we want a JSON-Schema validator on the client side as a belt-and-suspenders check? **Recommendation:** Yes — validate `raw` against the schema in `CompleteJSON` before returning, and return a typed error on mismatch. Cheap, catches model regressions early. Use `github.com/santhosh-tekuri/jsonschema/v5` (already idiomatic in Go).

3. **Schema reuse across prompt variants.** `MapFeaturesToCode` has two prompt variants (`--no-symbols` vs. symbols+files) that share one response shape. One schema, two prompts. No action item — just worth noting.

4. **Token accounting.** Structured-output requests may count tokens slightly differently (tool-call arguments vs. content text). `Counter()` instrumentation should be re-checked post-migration. **Recommendation:** add an explicit task in Phase 4 if token counts drift > 5% on a representative run.

## Non-goals

- Migrating the tool-use sites (`detectDriftForFeature`). They already emit structured tool calls.
- Changing the tiering interface or the `--llm-small|typical|large` CLI surface.
- Adding a new provider.

## Success criteria

- Zero `stripCodeFence` references remain in `internal/`.
- Full test suite green; coverage ≥ 90% in every package.
- `ftg analyze --repo ./testdata/fixtures/known-good --docs-url https://doc.holiday/docs/` completes without any "invalid JSON response" warning across all pages.
- `grep -n 'PROMPT' internal/` still surfaces every LLM contract, now accompanied by a `PROMPT SCHEMA:` neighbor.
