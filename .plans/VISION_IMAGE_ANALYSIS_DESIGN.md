# Vision-Aware Screenshot Analysis — Design

**Status:** Brainstormed, ready for implementation plan
**Date:** 2026-05-01
**Branch:** `vision-image-analysis`

## Summary

Add vision capability to Find the Gaps' screenshot-detection pass. When the configured `--llm-small` model accepts image input, the analyzer:

1. Inspects each `<img>` already on a docs page and decides whether the image actually depicts what the surrounding prose claims.
2. Suppresses missing-screenshot suggestions when an existing image already covers the moment.

Adds **Groq** as a new provider to enable a non-Anthropic, non-OpenAI vision option. Anthropic and OpenAI gain vision wiring on their existing models. Cerebras is out of scope (no vision-capable models on their inference API as of April 2026).

## Use Case

Find the Gaps already detects "missing screenshot" gaps from prose alone. Today the detector ignores what existing images on the page actually contain — so it can flag a moment as needing a screenshot even when one is right there. It can also miss findings where docs prose claims one thing but the embedded image shows another.

Vision unlocks both:

- **Image relevance** — surface mismatched, swapped, or contextually-wrong images.
- **Smarter missing-screenshot detection** — drop false positives when an existing image already shows the moment.

## Scope

**In:**
- New provider `groq` with `meta-llama/llama-4-scout-17b-16e-instruct` registered.
- Per-model capability registry (replacing the flat provider whitelist).
- Vision-aware screenshot pipeline using the small tier's client.
- New `## Image Issues` section in `screenshots.md`.
- `ftg doctor` reports resolved capabilities per tier.

**Out:**
- Cerebras (no vision models hosted; defer entirely).
- Image-anchored doc generation, alt-text generation, screenshot-vs-UI drift.
- Authenticated image fetching for private docs sites.
- New CLI flags (vision auto-engages on capable tiers).

## Provider Research

| Provider | Vision-capable model | Notes |
|---|---|---|
| Anthropic | claude-haiku-4-5, claude-sonnet-4-6, claude-opus-4-7 | All recent Claude models accept images. Source URL or base64. |
| OpenAI | gpt-5, gpt-5-mini, gpt-4o, gpt-4o-mini | Multimodal across the modern lineup. |
| Groq | meta-llama/llama-4-scout-17b-16e-instruct | Preview, 128K context, tool use, **5-image cap per request**, 20MB URL / 4MB base64. |
| Cerebras | (none) | Lineup is text-only: Llama 3.1 8B, GPT-OSS-120B, Qwen 3 235B, GLM 4.7. |

Sources: [Groq Vision](https://console.groq.com/docs/vision), [Cerebras Models](https://inference-docs.cerebras.ai/models/overview).

## Capability Registry

Replace `internal/cli/tier_validate.go`'s `isKnownProvider()` + `providerSupportsToolUse()` with a single per-model table:

```go
type ModelCapabilities struct {
    Provider string
    Model    string
    ToolUse  bool
    Vision   bool
}

var knownModels = []ModelCapabilities{
    {"anthropic", "claude-haiku-4-5",   true, true},
    {"anthropic", "claude-sonnet-4-6",  true, true},
    {"anthropic", "claude-opus-4-7",    true, true},
    {"openai",    "gpt-5",              true, true},
    {"openai",    "gpt-5-mini",         true, true},
    {"openai",    "gpt-4o",             true, true},
    {"openai",    "gpt-4o-mini",        true, true},
    {"groq",      "meta-llama/llama-4-scout-17b-16e-instruct", true, true},
    {"ollama",    "*", false, false}, // wildcard — capabilities unknown
    {"lmstudio",  "*", false, false},
}
```

**Lookup rules.**
- Exact `provider/model` match wins.
- Wildcard `*` covers self-hosted providers; capabilities default to off.
- Unknown `provider/model` on a known provider: warn but allow, treat as no capabilities. New models can be used before the registry knows about them.

**Validation rules.**
- Typical tier: must resolve to a model with `ToolUse=true` (today's rule, restated through the new registry).
- Small tier: no required capability. If `Vision=true`, the screenshot pipeline auto-engages the vision path; otherwise it runs as today.

## Pipeline

When `client.Capabilities().Vision` is true on the small tier, `DetectScreenshotGaps` switches from one text-only call per page to **two call types per page**:

### 1. Relevance pass (vision, batched)

Group every image on the page into batches of ≤5 images. Each call carries:

- the full page prose (capped by `ScreenshotPromptBudget`)
- image bytes via URL content blocks for that batch's images, indexed `[img-1]…[img-N]`
- alt/heading metadata for those same indices

Output (structured):

```json
{
  "image_issues": [
    { "index": "img-2", "src": "...", "reason": "...", "suggested_action": "..." }
  ]
}
```

A 12-image page → three calls (5 + 5 + 2). Image issues merge by index.

### 2. Detection pass (text only, verdict-enriched)

One call per page. Carries the full prose plus a list of every image on the page with its alt/heading metadata **and the verdict from step 1** (e.g. `[img-3] alt="Settings page" — verdict: matches prose / does not match`). Output: `missing_screenshots`.

The model is instructed to suppress a finding when any image with `verdict: matches` already covers the moment. This gives the detection pass joint context across all of the page's images even when relevance had to be split into batches.

### Image data path

Pass image URLs directly to the provider. Anthropic uses `{type:"image", source:{type:"url", url}}`. OpenAI and Groq use `{type:"image_url", image_url:{url}}`. No upfront prefetch, no base64.

### Failure handling

- Image URL unreachable → drop just that image, keep the rest of the batch.
- Vision call returns invalid output → fall back to today's text-only screenshot pass for that page; log a warning.
- Image src is `data:`, relative-without-base, or otherwise unusable → skip.

### Token budget

`ScreenshotPromptBudget` (150K) caps prose. Images are extra; a 5-image batch on Anthropic is ~8K extra tokens, well within the 200K window.

## Output

`screenshots.md` gains a second top-level section:

```markdown
# Screenshots

## Missing Screenshots

(unchanged shape)

- **Page:** https://example.com/docs/setup
  **Section:** Configuring credentials
  **Suggestion:** A screenshot of the credentials form with field labels visible.
  **Excerpt:** "Open the Credentials tab and fill out..."

## Image Issues

(only present when vision ran)

- **Page:** https://example.com/docs/dashboard
  **Image:** ![Dashboard overview](https://.../dash.png)
  **Issue:** Mismatch — surrounding prose describes the Settings page; image shows the Dashboard.
  **Suggested action:** Replace with a screenshot of the Settings page.
```

When vision ran but found no issues, the section header still appears with `_No image issues detected._` so users can see vision actually ran. When vision did not run, the section is omitted.

The stdout `reports:` block lists `screenshots.md` as one entry — no new file. The Hugo site renders it unchanged; no theme work.

### Audit log

One line per page:

```
page=<url> vision=on relevance_batches=3 images_seen=12 image_issues=2 missing_screenshots=4 missing_suppressed=1
```

`missing_suppressed` is the count of moments the detection pass would have flagged but didn't because relevance found a covering image. Watch this metric to know whether vision is actually preventing false positives in the wild.

## Implementation Surface

```
internal/cli/tier_validate.go
  Replace isKnownProvider() + providerSupportsToolUse() with
  ModelCapabilities table + resolveCapabilities() / validateTier().

internal/cli/llm_client.go
  buildTierClient: add `groq` case. Reads GROQ_API_KEY. Bifrost
  provider name + base URL per Groq's OpenAI-compat endpoint.

internal/analyzer/types.go
  ChatMessage gains optional ContentBlocks []ContentBlock alongside
  the existing Content string. Existing string-Content paths keep
  working unchanged.

internal/analyzer/bifrost_client.go
  CompleteJSON / CompleteWithTools learn to send mixed content
  blocks. Provider-specific marshaling for image_url.

internal/analyzer/client.go
  LLMClient gains Capabilities() ModelCapabilities so the analyzer
  can branch without knowing provider names.

internal/analyzer/screenshot_gaps.go
  When Capabilities().Vision is true:
    - splitImageBatches() groups image refs into ≤5-image batches
    - relevancePass() makes N vision calls, returns image issues
    - detectionPass() makes one verdict-enriched text call
  Returns ScreenshotResult{ MissingGaps, ImageIssues, AuditStats }.

internal/reporter/screenshots.go
  WriteScreenshots accepts the new ImageIssues slice. Renders both
  sections per the output spec above.

cmd/find-the-gaps/testdata/*.txtar
  New scripted scenario covering the vision path against a fake
  vision-capable LLM server.

internal/analyzer/screenshot_gaps_test.go
  Unit tests for batching math, verdict merging, audit stats.
```

Two new `// PROMPT:`-tagged templates land in `screenshot_gaps.go`: one for the relevance pass, one for the verdict-enriched detection pass.

## Testing

### Unit (TDD, per CLAUDE.md)

- `tier_validate_test.go` — exact match wins, wildcard fallback, unknown model warns, typical-tier rejects non-tool-use.
- `screenshot_gaps_test.go` — batching math (1, 5, 6, 12, 0 images); verdict merge across batches by stable index; audit-stats counts.
- `bifrost_client_test.go` — image content-block marshaling for Anthropic vs OpenAI/Groq shape.
- `client_test.go` — `Capabilities()` plumbing.

### Integration (`testscript`)

- New `.txtar`: fixture page with a known-mismatched image, fake LLM server set to a vision-capable model, assert `## Image Issues` populates.
- New `.txtar`: page with 12 images, assert relevance pass fires three batches.
- All existing non-vision scenarios stay green.

### Manual verification

Add **Scenario 13: Vision-aware screenshot analysis** to `.plans/VERIFICATION_PLAN.md`:

- (a) `--llm-small=anthropic/claude-haiku-4-5` → vision engages, `## Image Issues` populated.
- (b) `--llm-small=ollama/...` → vision skipped, output unchanged from today, audit log shows `vision=off`.
- (c) `--llm-small=groq/meta-llama/llama-4-scout-17b-16e-instruct` → vision engages, batching observable on a >5-image page.

No mocks, real `GROQ_API_KEY`, real docs URLs.

## Rollout

- No flag to flip — vision auto-engages on capable tiers. First user impact is the next release after merge.
- CHANGELOG entry covers: capability registry, Groq provider, `## Image Issues` output, `ftg doctor` capability lines.
- README "What this installs" / "Configuration" updated for `GROQ_API_KEY`.
