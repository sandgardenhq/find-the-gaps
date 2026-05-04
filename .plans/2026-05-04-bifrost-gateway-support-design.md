# Bifrost Gateway Support — Design

**Date:** 2026-05-04
**Status:** Proposed
**Author:** Britt Crawford (with Claude)

## Problem

find-the-gaps embeds the Bifrost Go SDK and talks directly to providers (Anthropic, OpenAI, Ollama, Groq). Some users — including the author — already run a self-hosted Bifrost gateway that centralizes routing, keys, observability, and caching. Today there is no way to point find-the-gaps at that gateway. Every install duplicates the per-provider key configuration the gateway already owns.

Goal: let a user say "send my LLM calls through my Bifrost gateway" without find-the-gaps caring which underlying provider the gateway resolves to.

## Non-goals

- Replacing the existing direct-provider lanes. Anthropic, OpenAI, Ollama, Groq, and lmstudio stay exactly as they are.
- Server-side alias capability probing.
- A config file mapping aliases to families or capabilities.
- An umbrella `--llm-mode=gateway-only` flag.
- Per-tier independent gateway URLs.
- Recovering Anthropic prompt-caching wins when an alias resolves to Claude (gateway-side caching is the user's responsibility).

## Approach

The Bifrost gateway exposes OpenAI-compatible endpoints (`{gateway}/openai/v1/chat/completions`). Routing find-the-gaps through them is mechanically the same trick already used for Groq: a new pseudo-provider that uses the OpenAI lane with a custom `BaseURL` and API key.

The gateway resolves a user-defined alias (e.g. `cheap-tier`, `balanced`, `best`) to a real provider+model server-side. find-the-gaps sends the alias as the `model` field and stays out of that decision.

### Wire transport

In `internal/analyzer/bifrost_client.go`, `NewBifrostClientWithProvider` gains a new case:

```go
case "gateway":
    provider = schemas.OpenAI
    if baseURL == "" {
        return nil, fmt.Errorf("gateway provider requires a baseURL")
    }
```

Implications, all of which fall out of routing through `schemas.OpenAI`:

- **No `cache_control` blocks.** `cacheable` in `renderBifrostMessages` is already `false` when `provider != Anthropic`. We get this for free.
- **Structured outputs use `response_format=json_schema` with `strict=true`** via `completeJSONOpenAIMessages`. The gateway translates to the underlying provider's structured-output convention (e.g. Anthropic forced-tool-use) under the hood.
- **Vision** sends `ContentBlockImageURL` blocks unconditionally. The user is responsible for configuring a vision-capable model behind any alias used for the small tier.

### CLI surface

In `internal/cli/llm_client.go`, `buildTierClient` gains a `gateway` case mirroring `groq`:

```go
case "gateway":
    apiKey = os.Getenv("BIFROST_GATEWAY_API_KEY") // optional
    baseURL = os.Getenv("BIFROST_GATEWAY_URL")
    if baseURL == "" {
        return nil, nil, fmt.Errorf("BIFROST_GATEWAY_URL not set")
    }
    bifrostProvider = "gateway"
    counter = analyzer.NewTiktokenCounter()
```

Tier flag syntax is unchanged — `provider/model` with the gateway alias as the model name:

```
--llm-small=gateway/cheap-tier
--llm-typical=gateway/balanced
--llm-large=gateway/best
```

Tiers are resolved independently, so users can mix lanes (e.g. `--llm-small=anthropic/claude-haiku-4-5 --llm-large=gateway/best`). This is a free property of the existing tier system.

### Environment variables

- `BIFROST_GATEWAY_URL` — required when any tier uses `gateway/*`. Must NOT include `/v1`; Bifrost's OpenAI handler appends `/v1/chat/completions` itself.
- `BIFROST_GATEWAY_API_KEY` — optional. When empty, the existing `localServerPlaceholderKey` trick (used for keyless OpenAI-compatible servers) applies.

### Capabilities

In `internal/cli/capabilities.go`, `ResolveCapabilities` gains a `gateway` branch that returns a static struct:

- `Vision = true`
- `ToolUse = true`
- `PromptCache = false`
- `MaxCompletionTokens = 0` (falls back to the 32k default)

No model-name lookup. We trust the user's gateway configuration.

### `ftg doctor`

Adds a "gateway" line. Prints `BIFROST_GATEWAY_URL` when set and pings a health endpoint on the gateway for green/red. Specific endpoint TBD against the running gateway version.

## Testing

Per the project's no-mocks verification rule, real integration tests run against the user's actual gateway and live in the verification plan, not the test suite.

### Unit (`internal/analyzer/bifrost_client_test.go`)

Inject a fake `bifrostRequester` and assert wire shape:

- `TestNewBifrostClientWithProvider_Gateway_RequiresBaseURL`
- `TestGatewayClient_NoCacheControl` — locks in OpenAI-lane behavior for the gateway path.
- `TestGatewayClient_StructuredOutputs_UsesResponseFormat` — `response_format=json_schema, strict=true`, NOT a forced "respond" tool.
- `TestGatewayClient_Vision_PassesImageBlocks`

### CLI (`internal/cli/llm_tiering_test.go`, `analyze_test.go`)

- `TestBuildTierClient_Gateway_RequiresURL`
- `TestBuildTierClient_Gateway_AllowsEmptyAPIKey`
- `TestResolveCapabilities_Gateway_TrustsUser` — returns the static struct above regardless of model name.
- `TestTiering_MixedLanes` — `--llm-small=anthropic/...`, `--llm-large=gateway/best` builds.

### testscript (`cmd/find-the-gaps/testdata/`)

CLI smoke test: tier flag parsing, missing-env-var error path, `doctor` output formatting. No real gateway calls.

### Verification (`.plans/VERIFICATION_PLAN.md`)

New **Scenario 14 — Bifrost Gateway**, three sub-cases, all against a real running gateway:

- **(a)** Gateway alias resolving to a vision-capable Claude model. Run analyze on a known-good fixture; assert `screenshots.md` populates and `gaps.md` matches the direct-Anthropic baseline.
- **(b)** Gateway alias resolving to a non-vision model. Assert vision-off behavior on per-page errors, no run failure.
- **(c)** `ftg doctor` against a reachable gateway (exit 0, prints URL); against a bogus URL (non-zero, clear error).

## TDD task order

Each step is one commit. Test goes first, fails for the expected reason, then minimal code to pass.

1. **RED**: `TestNewBifrostClientWithProvider_Gateway_RequiresBaseURL`. **GREEN**: add `case "gateway"` in `NewBifrostClientWithProvider`.
2. **RED**: `TestGatewayClient_StructuredOutputs_UsesResponseFormat`. **GREEN**: passes via reuse of `completeJSONOpenAIMessages`. Test locks the behavior.
3. **RED**: `TestGatewayClient_NoCacheControl`, `TestGatewayClient_Vision_PassesImageBlocks`. **GREEN**: should pass via reuse; tests lock the contract.
4. **RED**: `TestBuildTierClient_Gateway_RequiresURL`, `_AllowsEmptyAPIKey`. **GREEN**: add `case "gateway"` in `buildTierClient`.
5. **RED**: `TestResolveCapabilities_Gateway_TrustsUser`. **GREEN**: add gateway branch in `ResolveCapabilities`.
6. **RED**: `TestTiering_MixedLanes`. **GREEN**: usually passes after 1–5; locks the mixing property.
7. **RED**: testscript scenario for CLI parsing + missing-env-var error. **GREEN**: wiring fixes.
8. **RED**: `ftg doctor` test for the gateway line. **GREEN**: extend `doctor`.
9. Add Scenario 14 to `.plans/VERIFICATION_PLAN.md` (no code change).
10. README update: env vars and tier flag examples. No new external runtime dependency, so the "What this installs" section is unchanged.

## Risks and known limits

- **Tokenization is approximate** on the gateway path (tiktoken `cl100k_base` undercounts non-OpenAI families by ~5–15%). Same caveat as Groq and Ollama today. Acceptable.
- **Loss of Anthropic prompt-caching wins** when the alias resolves to Claude. Mitigation: gateway-side caching, if enabled. Revisit only if measurable cost regression appears.
- **Mis-configured vision alias** surfaces as a per-page error in the screenshot relevance pass. The existing per-page error path handles this gracefully — not a run-killer.
- **No regressions on direct-Anthropic path.** The new case does not touch the existing Anthropic lane.
