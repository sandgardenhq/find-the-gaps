# LLM Tiering Design

## Context

Today, every LLM call in Find the Gaps routes through one `LLMClient` configured with a single `(provider, model)` pair. All seven call sites share it. The audit in `.plans/LLM_CALL_AUDIT.md` showed that the workload is heterogeneous: most calls are high-volume, low-reasoning tasks (page summaries, feature matching, yes/no classification), while a few are agentic, tool-use-heavy, judgment-intensive tasks (feature↔code mapping, drift detection). Running everything on one mid-tier model either overpays for the simple calls or underpowers the complex ones.

This design introduces three configurable tiers — `small`, `typical`, `large` — one per call site. Tier strings are chosen by the operator via CLI, config file, or env var; each tier resolves to an independent `(provider, model)` pair.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Tier dispatch | Tag at the call site | Tier lives next to the `// PROMPT:` comment; visible during review |
| Per-tier provider | Each tier independent | Lets operators mix providers (e.g. Anthropic `small`, OpenAI `large`) |
| Flag syntax | Combined `provider/model` | One flag per tier; `provider/` is optional, defaulting to `anthropic` |
| Existing flags | Remove `--llm-provider`, `--llm-model` | Clean break; no deprecation dance for an early-stage tool |
| Validation | Fail fast at startup | No warning-and-continue paths |

## Tier Taxonomy and Defaults

| Tier | Role | Default |
|------|------|---------|
| `small` | High-volume, simple classification / summarization / matching | `anthropic/claude-haiku-4-5` |
| `typical` | Medium-complexity structured extraction | `anthropic/claude-sonnet-4-6` |
| `large` | Multi-turn reasoning, tool use, agentic loops, high-stakes mapping | `anthropic/claude-opus-4-7` |

Anthropic is the default provider whenever a tier string omits the `provider/` prefix. Model names above reflect Anthropic's flagship IDs as of April 2026.

## Call-Site Tier Assignments

Hard-coded in Go next to each prompt. These reflect the audit's recommendations.

| # | Prompt | Location | Tier |
|---|--------|----------|------|
| 1 | Extract Features from Code | `internal/analyzer/code_features.go:57` | `typical` |
| 2 | Analyze Single Doc Page | `internal/analyzer/analyze_page.go:16` | `small` |
| 3 | Synthesize Product Summary | `internal/analyzer/synthesize.go:24` | `small` |
| 4 | Map Features to Code | `internal/analyzer/mapper.go:93` (files-only, `--no-symbols`) / `mapper.go:109` (files+symbols) | `large` |
| 5 | Map Features to Docs | `internal/analyzer/docs_mapper.go:53` | `small` |
| 6 | Detect Drift (agentic) | `internal/analyzer/drift.go:102` | `large` |
| 7 | Classify Release-Note Page | `internal/analyzer/drift.go:320` | `small` |

Cold-run distribution on a typical repo: ~60-80 `small` calls, ~1-2 `typical` calls, ~15-25 `large` calls. Caching collapses most `small` and `typical` traffic on repeat runs.

## Config Surface

### CLI Flags

```
--llm-small   string   e.g. "anthropic/claude-haiku-4-5" or "claude-haiku-4-5"
--llm-typical string   e.g. "openai/gpt-5.4-mini"
--llm-large   string   e.g. "anthropic/claude-opus-4-7"
```

Values without a `/` are treated as a bare model name; the provider defaults to `anthropic`. Values split on the **first** `/` only, so `ollama/llama3.1:8b` parses as provider `ollama`, model `llama3.1:8b`. Recognized providers: `anthropic`, `openai`, `ollama`, `lmstudio`, `openai-compatible`.

### Config File (TOML, loaded by Viper)

Search order: `$XDG_CONFIG_HOME/find-the-gaps/config.toml` → `~/.find-the-gaps/config.toml` → project-local `.find-the-gaps/config.toml`.

```toml
[llm]
small   = "anthropic/claude-haiku-4-5"
typical = "anthropic/claude-sonnet-4-6"
large   = "anthropic/claude-opus-4-7"
```

### Environment Variables

```
FIND_THE_GAPS_LLM_SMALL=anthropic/claude-haiku-4-5
FIND_THE_GAPS_LLM_TYPICAL=openai/gpt-5.4-mini
FIND_THE_GAPS_LLM_LARGE=anthropic/claude-opus-4-7
```

### Precedence

Standard Viper order: CLI flag > env var > config file > built-in default.

### Per-Tier Independence

Any tier can be overridden on its own. Unset tiers fall through to defaults — there is **no** automatic cascade from one tier to another. If a user sets only `--llm-large openai/gpt-5.4`, `small` and `typical` stay on Anthropic defaults.

## Internal Architecture

Replace the single `LLMClient` returned by `newLLMClient()` with an `LLMTiering` struct that holds three independent clients.

```go
// internal/cli/llm_client.go

type Tier string

const (
    TierSmall   Tier = "small"
    TierTypical Tier = "typical"
    TierLarge   Tier = "large"
)

type LLMTiering struct {
    small   LLMClient
    typical LLMClient
    large   LLMClient
}

func (t *LLMTiering) Small()   LLMClient { return t.small }
func (t *LLMTiering) Typical() LLMClient { return t.typical }
func (t *LLMTiering) Large()   LLMClient { return t.large }
```

### Construction: Eager and Validated

- **At CLI config resolution (before any subcommand runs):**
  - Parse all three `provider/model` strings.
  - Reject unknown providers with a hard error naming the offending tier flag.
  - Reject any `large` tier whose provider does not support tool use (currently anything other than `anthropic` or `openai`). Error message points at `--llm-large`.
- **At the start of `analyze` (the only subcommand that needs LLM access):**
  - Eagerly construct all three Bifrost clients. Missing API keys, invalid models, or connectivity failures surface immediately.
- **`doctor` and other LLM-free subcommands:** skip client construction entirely. Parse-and-validate still runs so bad config is caught everywhere.

No warning-and-continue paths. Every validation failure is an error.

### Token Counting

`LLMClient.CountTokens` stays on the existing interface. Each tier carries its own counter: Anthropic models use the `count_tokens` API; OpenAI and local providers use tiktoken. Call sites that batch by token budget (prompts 1 and 4) call `client.CountTokens(...)` on their tier's client — each batch gets an accurate count for the provider actually being used.

### Call-Site Refactor

Analyzer entry points accept `*LLMTiering` instead of `LLMClient`. Each call site picks its tier:

```go
// internal/analyzer/mapper.go (prompt 4)
client := tiering.Large()
client.Complete(ctx, prompt)

// internal/analyzer/analyze_page.go (prompt 2)
client := tiering.Small()
client.Complete(ctx, prompt)
```

The `LLMClient` interface (`Complete`, `CompleteWithTools`, `CountTokens`) is unchanged.

## Error Handling

All validation failures return typed errors containing the offending tier name so the user knows which flag or config key to fix. Examples:

- `tier "large": unknown provider "bedrock" — valid providers: anthropic, openai, ollama, lmstudio, openai-compatible`
- `tier "large": provider "ollama" does not support tool use; drift detection requires anthropic or openai`
- `tier "typical": failed to construct client: ANTHROPIC_API_KEY not set`

## Testing Plan

- **Unit: tier-string parser.** Table-driven test covering slash-splitting, bare model names defaulting to `anthropic`, whitespace handling, empty values, models containing colons (`llama3.1:8b`).
- **Unit: provider capability check.** Exhaustive test that every known provider reports the right `supportsToolUse()` value.
- **Unit: config precedence.** Table-driven test that CLI > env > file > default works for every tier.
- **Unit: validation errors.** Each failure mode (unknown provider, non-tool-use `large`, missing API key) produces the expected typed error.
- **Integration (`testscript`):** `analyze` with a non-tool-use provider on `large` exits non-zero with a readable error before any LLM work begins.
- **Analyzer tests:** fakes switch from a single `LLMClient` to a `*LLMTiering` that returns a recording client per tier. Each analyzer test asserts its prompt was dispatched through the expected tier (e.g. prompt 6 through `Large()`, prompt 2 through `Small()`).

## Migration

This is a breaking CLI change. `--llm-provider` and `--llm-model` are removed. Mitigation:

- Clear `CHANGELOG.md` entry naming the removed flags and the new tier flags.
- `README.md` update with the new flags, a TOML config snippet, and a short "what changed" callout.
- If either removed flag is passed, Cobra's standard "unknown flag" error fires — acceptable for a clean break.

## Files Touched

- `internal/cli/llm_client.go` — refactor center; adds `Tier`, `LLMTiering`, parser, validation, eager construction.
- `internal/cli/root.go` (or wherever flags are registered) — register new flags, bind via Viper, remove old flags.
- `internal/analyzer/code_features.go`, `analyze_page.go`, `synthesize.go`, `mapper.go`, `docs_mapper.go`, `drift.go` — 7 call sites swap `LLMClient` for `*LLMTiering` and pick their tier.
- `cmd/find-the-gaps/testdata/*.txtar` — scripts covering new-flag happy path and tool-use validation failure.
- `CHANGELOG.md`, `README.md` — migration callout.

## Out of Scope

- Cross-tier fallback (e.g. "if `large` fails, retry on `typical`"). Not needed; fail fast is clearer.
- Per-call-site tier override via CLI (e.g. "force drift onto `typical`"). Not a current need; add later if operators ask.
- Observability counters per tier. Useful but independent; separate design.
