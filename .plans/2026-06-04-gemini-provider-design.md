# Gemini Provider Support — Design

**Date:** 2026-06-04
**Status:** Approved, ready for implementation

## Goal

Add Google Gemini as a **default-eligible** LLM provider, on par with Anthropic
and OpenAI. A user with only `GEMINI_API_KEY` set should get a working
three-tier default ladder with no extra flags; a user can also select Gemini
explicitly per tier via `--llm-small=gemini/...` etc.

## Why this is small

The provider abstraction already isolates everything provider-specific to a few
seams, and Bifrost v1.5.2 (our pinned version) supports Gemini natively:

- **Key handling needs no new code.** The Gemini provider reads
  `key.Value.GetValue()` and sends it as the `x-goog-api-key` header
  (`providers/gemini/gemini.go:119`). It needs no special key config like
  Ollama's `OllamaKeyConfig` or Vertex's `VertexKeyConfig`. Our existing
  `bifrostAccount.GetKeysForProvider` path works unchanged.
- **Structured output reuses the OpenAI path.** Bifrost's Gemini provider
  (`providers/gemini/utils.go:1063`) reads `params.ResponseFormat`, casts it to a
  map, matches `"json_schema"`, and converts the schema into Gemini's
  `responseJsonSchema`. That is exactly the map `completeJSONOpenAIMessages`
  already builds; the `strict` field is ignored. So Gemini routes through the
  existing OpenAI structured-output branch.
- **Tool use is already provider-agnostic.** `CompleteWithTools` →
  `completeOneTurn` works for Gemini with no change.

## Decisions

### Tier ladder (default)

Mirrors the haiku/sonnet/opus and nano/mini/5.5 ladders, all GA models:

| Tier | Model |
|---|---|
| small | `gemini-3.1-flash-lite` |
| typical | `gemini-3.5-flash` |
| large | `gemini-3.1-pro-preview` |

`gemini-3.5-flash` for the typical tier matters: the typical tier runs the drift
investigator's tool-use loop, so it must be tool-use-capable — 3.5 Flash is and
has the strongest agentic performance in the lineup.

> **Resolved at implementation time (2026-06-05).** There is no stable
> `gemini-3.1-pro`; the flagship Pro tier is still preview-only, exposed as
> `gemini-3.1-pro-preview` (the older `gemini-3-pro-preview` ID now aliases to
> it). The user approved shipping the preview ID as the default large tier
> (option 2 in the brainstorm) over falling back to the stable `gemini-2.5-pro`.
> `MaxInputTokens` stays at the 900k baseline for all three rows.

### Auth

`GEMINI_API_KEY` only. One provider → one obvious env var, matching
`ANTHROPIC_API_KEY` / `OPENAI_API_KEY` / `GROQ_API_KEY`. No `GOOGLE_API_KEY`
fallback.

### Default precedence

First key present wins, in order **Anthropic → OpenAI → Gemini**. Existing
behavior is byte-identical; the Gemini ladder engages only when `GEMINI_API_KEY`
is the sole key set.

### Capabilities (`knownModels` rows)

```go
{Provider: "gemini", Model: "gemini-3.1-flash-lite",  ToolUse: true, Vision: true, MaxInputTokens: 900000},
{Provider: "gemini", Model: "gemini-3.5-flash",       ToolUse: true, Vision: true, MaxInputTokens: 900000},
{Provider: "gemini", Model: "gemini-3.1-pro-preview", ToolUse: true, Vision: true, MaxInputTokens: 900000},
```

- `MaxInputTokens: 900000` mirrors the ~10%-under-published-cap convention for
  1M-window models (Sonnet, GPT-5.5).
- **No `MaxCompletionTokens` override.** Gemini's 65,536 output max clears our
  default 32k send. (Contrast Groq's 8,192 cap.)
- Unknown Gemini model IDs fall through `ResolveCapabilities`'s "known provider,
  unknown model → 100k floor" path, so new IDs work before the table catches up.

### Token counting

`NewTiktokenCounter()` — the same heuristic used for openai/groq/ollama.
cl100k undercounts Gemini tokens by a similar ~5–15%, absorbed by the existing
1.2× screenshot-prompt margin and the 0.9× budget gate.

## Touch points

| File | Change |
|---|---|
| `internal/analyzer/bifrost_client.go` | `NewBifrostClientWithProvider`: add `case "gemini": provider = schemas.Gemini` (no baseURL required). `completeJSONMessages`: add `schemas.Gemini` to the existing `case schemas.OpenAI, schemas.Ollama` branch. |
| `internal/cli/llm_client.go` | `buildTierClient`: add `case "gemini"` reading `GEMINI_API_KEY`, `bifrostProvider = "gemini"`, tiktoken counter. Update `llmKeySetupHint` and `isMissingDefaultKeyErr` to name Gemini. |
| `internal/cli/capabilities.go` | Add the three `knownModels` rows. |
| `internal/cli/tier_validate.go` | Add `defaultSmall/Typical/LargeTierGemini` consts; extend `tierFallbacks` with the Gemini branch. |
| README | Add `GEMINI_API_KEY` + `gemini/...` tier syntax to the LLM-provider/config section. |
| `.plans/VERIFICATION_PLAN.md` | Note that the analyze pipeline runs end-to-end on the Gemini ladder. |

## Testing (TDD)

- `capabilities_test.go` — Gemini rows resolve vision+tooluse; unknown Gemini
  model → 100k floor; `knownProviders()` includes `gemini`.
- `tier_validate_test.go` — `tierFallbacks` returns the Gemini ladder when only
  `GEMINI_API_KEY` is set; Anthropic still wins when both present; the typical
  tier passes the tool-use gate.
- `tier_parse_test.go` — `gemini/gemini-3.1-pro` parses.
- `llm_client_test.go` — `buildTierClient("gemini", ...)` errors cleanly with no
  key; `isMissingDefaultKeyErr` recognizes the Gemini message; the setup-hint
  path fires when Gemini is the defaulted-but-keyless provider.
- `doctor_test.go` — doctor reports a `gemini/*` resolved tier.
- **Integration** (`bifrost_client_integration_test.go` pattern, gated on
  `GEMINI_API_KEY` like the Groq test) — real `CompleteJSON`, `Complete`, and a
  tool-use round against `gemini-3.5-flash`. No mocks (Verification Rules).

## Out of scope (YAGNI)

- Vertex AI auth (separate service-account/`VertexKeyConfig` model).
- A dedicated Gemini `countTokens`-API token counter.
- Explicit Gemini prompt-cache wiring (server-side implicit caching still
  applies at no cost).
- `GEMINI_BASE_URL` override.
- `GOOGLE_API_KEY` fallback.
