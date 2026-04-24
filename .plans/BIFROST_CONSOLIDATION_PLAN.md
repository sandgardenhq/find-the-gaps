# Bifrost Consolidation Plan

## Goal

Remove `internal/analyzer/openai_compatible_client.go` and route every LLM
provider (`anthropic`, `openai`, `ollama`, `lmstudio`, `openai-compatible`)
through `analyzer.BifrostClient`.

## Why

Bifrost is already a required dependency and natively supports Ollama plus an
`OpenAI` provider with a configurable base URL (`NetworkConfig.BaseURL`). The
hand-rolled `OpenAICompatibleClient` duplicates that plumbing, has stubbed tool
handling (the `tools` argument is ignored), and forces us to maintain a second
HTTP client for the same shape. Consolidating lets us:

- delete ~210 LOC of production code and ~340 LOC of tests,
- reuse Bifrost's retry / timeout / structured-output handling,
- gain tool use on local models for free when the endpoint supports it.

## Provider → Bifrost mapping

| CLI provider name   | Bifrost `schemas.ModelProvider` | URL configured via                              |
|---------------------|---------------------------------|-------------------------------------------------|
| `anthropic`         | `schemas.Anthropic`             | (default)                                       |
| `openai`            | `schemas.OpenAI`                | (default; or `NetworkConfig.BaseURL` if passed) |
| `ollama`            | `schemas.Ollama`                | `Key.OllamaKeyConfig.URL`                       |
| `lmstudio`          | `schemas.OpenAI`                | `NetworkConfig.BaseURL`                         |
| `openai-compatible` | `schemas.OpenAI`                | `NetworkConfig.BaseURL`                         |

CLI env-var sources and defaults stay unchanged (`OLLAMA_BASE_URL`,
`LMSTUDIO_BASE_URL`, `OPENAI_COMPATIBLE_BASE_URL`).

## Scope

Analyzer layer recognises three Bifrost-level provider names:
`"anthropic" | "openai" | "ollama"`. The CLI layer collapses `lmstudio` and
`openai-compatible` into `"openai"` + a base URL when it calls
`NewBifrostClientWithProvider`. This keeps the analyzer's vocabulary aligned
with Bifrost's real providers and avoids leaking CLI-specific naming into the
analyzer.

### Signature change

```go
// Before
func NewBifrostClientWithProvider(providerName, apiKey, model string) (*BifrostClient, error)

// After
func NewBifrostClientWithProvider(providerName, apiKey, model, baseURL string) (*BifrostClient, error)
```

`baseURL` is empty for the default endpoint of a given provider. Required for
`"ollama"`.

### Account-level plumbing

`bifrostAccount` gains a `baseURL` field. `GetKeysForProvider` populates
`Key.OllamaKeyConfig{URL: baseURL}` when the provider is Ollama. Otherwise the
base URL lands on `ProviderConfig.NetworkConfig.BaseURL`, which Bifrost honours
for OpenAI / Anthropic / Cohere / Mistral / Ollama per `schemas/provider.go:54`.

## TDD order

1. **RED**: add tests exercising the new signature and Ollama/custom-base-URL
   paths in `bifrost_client_test.go`. Update existing call sites for the new
   signature.
2. **GREEN**: extend `bifrost_client.go` with the `baseURL` parameter and the
   `"ollama"` provider branch.
3. **RED**: strengthen `llm_tiering_test.go` — the existing
   `TestBuildTierClient_Ollama_*`, `TestBuildTierClient_LMStudio`, and
   `TestBuildTierClient_OpenAICompatible_Success` tests will be updated to
   assert the returned client is `*analyzer.BifrostClient`.
4. **GREEN**: rewrite the three cases in `buildTierClient` to call
   `NewBifrostClientWithProvider`.
5. Delete `openai_compatible_client.go` and `openai_compatible_client_test.go`.
6. `go build ./...`, `go test ./...`, `golangci-lint run`.
7. Update `PROGRESS.md`, commit.

## Out of scope

- Integration tests against a real Ollama/LM Studio server. The verification
  plan already covers that (`LLM_ANALYSIS_VERIFICATION_PLAN.md` scenarios 9/10)
  and we don't have access to those servers in this workspace.
- Changing CLI provider names or env var contracts.
- Supporting new Bifrost providers that weren't reachable before (vLLM, SGL,
  Groq, etc.). Easy follow-up, not this task.
