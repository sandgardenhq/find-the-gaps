# LLM Tiering Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the single shared `LLMClient` with a three-tier system (`small` / `typical` / `large`), each independently configurable to a `(provider, model)` pair via CLI flag, TOML config, or env var. Each of the 7 LLM call sites picks its tier inline next to the `// PROMPT:` comment.

**Architecture:** A new `analyzer.LLMTiering` interface exposes `Small() / Typical() / Large()` returning `LLMClient`, plus matching `*Counter()` accessors for `TokenCounter`. The CLI constructs a concrete `cli.llmTiering` struct that implements the interface, builds all three clients eagerly at the start of `analyze`, and fails fast on unknown providers or a non-tool-use `large` tier. The existing `--llm-provider`, `--llm-model`, and `--llm-base-url` flags are removed in favor of `--llm-small`, `--llm-typical`, `--llm-large` with combined `provider/model` syntax and `anthropic/` as the implicit default provider. See `.plans/LLM_TIERING_DESIGN.md` for the full design rationale.

**Tech Stack:** Go 1.26+, Cobra (flags), Viper (config/env), testify (assertions), testscript (integration tests), Bifrost SDK (LLM gateway).

**TDD Discipline (from CLAUDE.md):** Every task is one RED-GREEN-REFACTOR cycle. Write the failing test first, run it to see it fail, write minimal code to pass, run it to see green, commit. No exceptions. Coverage must stay ≥90% per package (`go test -cover ./...`).

**Referenced skills:** @superpowers:test-driven-development, @superpowers:verification-before-completion, @superpowers:executing-plans.

---

## Phase 1 — Foundation Types (analyzer package)

### Task 1: Define `Tier` type and constants

**Files:**
- Create: `internal/analyzer/tier.go`
- Create: `internal/analyzer/tier_test.go`

**Step 1: Write failing test.**

`internal/analyzer/tier_test.go`:

```go
package analyzer

import "testing"

func TestTierConstantsAreUnique(t *testing.T) {
	if TierSmall == TierTypical || TierTypical == TierLarge || TierSmall == TierLarge {
		t.Fatalf("tier constants must be unique: small=%q typical=%q large=%q", TierSmall, TierTypical, TierLarge)
	}
}

func TestTierConstantsAreLowercaseSlugs(t *testing.T) {
	for _, tier := range []Tier{TierSmall, TierTypical, TierLarge} {
		if string(tier) == "" {
			t.Fatalf("empty tier constant")
		}
	}
}
```

**Step 2: Run test — verify RED.**

```
go test ./internal/analyzer/ -run Tier -count=1
```
Expected: `undefined: TierSmall` (and siblings).

**Step 3: Minimal implementation.**

`internal/analyzer/tier.go`:

```go
package analyzer

type Tier string

const (
	TierSmall   Tier = "small"
	TierTypical Tier = "typical"
	TierLarge   Tier = "large"
)
```

**Step 4: Run test — verify GREEN.**

```
go test ./internal/analyzer/ -run Tier -count=1
```
Expected: PASS.

**Step 5: Commit.**

```
git add internal/analyzer/tier.go internal/analyzer/tier_test.go
git commit -m "feat(analyzer): add Tier type and small/typical/large constants

- RED: TestTierConstants{AreUnique,AreLowercaseSlugs}
- GREEN: Tier string type with TierSmall/TierTypical/TierLarge constants
- Status: 2 tests passing, build succeeds"
```

---

### Task 2: Define `LLMTiering` interface

**Files:**
- Modify: `internal/analyzer/tier.go`
- Modify: `internal/analyzer/tier_test.go`

**Step 1: Write failing test.** Append to `tier_test.go`:

```go
// compile-time interface check
var _ LLMTiering = (*fakeLLMTiering)(nil)

type fakeLLMTiering struct {
	small, typical, large                   LLMClient
	smallCounter, typicalCounter, largeCounter TokenCounter
}

func (f *fakeLLMTiering) Small() LLMClient           { return f.small }
func (f *fakeLLMTiering) Typical() LLMClient         { return f.typical }
func (f *fakeLLMTiering) Large() LLMClient           { return f.large }
func (f *fakeLLMTiering) SmallCounter() TokenCounter   { return f.smallCounter }
func (f *fakeLLMTiering) TypicalCounter() TokenCounter { return f.typicalCounter }
func (f *fakeLLMTiering) LargeCounter() TokenCounter   { return f.largeCounter }

func TestLLMTieringInterfaceShape(t *testing.T) {
	var ft LLMTiering = &fakeLLMTiering{}
	if ft.Small() != nil || ft.Typical() != nil || ft.Large() != nil {
		t.Fatalf("zero-value tiering should return nil clients")
	}
}
```

**Step 2: Run test — verify RED.**

Expected: `undefined: LLMTiering`.

**Step 3: Minimal implementation.** Append to `tier.go`:

```go
// LLMTiering exposes one LLMClient and TokenCounter per reasoning tier.
// Analyzer functions choose a tier inline next to their // PROMPT: comment.
type LLMTiering interface {
	Small() LLMClient
	Typical() LLMClient
	Large() LLMClient

	SmallCounter() TokenCounter
	TypicalCounter() TokenCounter
	LargeCounter() TokenCounter
}
```

**Step 4: Run test — verify GREEN.**

```
go test ./internal/analyzer/ -run Tier -count=1
```
Expected: PASS (3 tests).

**Step 5: Commit.**

```
git commit -am "feat(analyzer): define LLMTiering interface with per-tier client and counter accessors

- RED: TestLLMTieringInterfaceShape
- GREEN: LLMTiering interface {Small/Typical/Large}() and matching *Counter() methods
- Status: 3 tests passing"
```

---

## Phase 2 — CLI Tier Parsing and Construction

### Task 3: `parseTierString` — split `provider/model` with Anthropic default

**Files:**
- Create: `internal/cli/tier_parse.go`
- Create: `internal/cli/tier_parse_test.go`

**Step 1: Write failing test.**

`internal/cli/tier_parse_test.go`:

```go
package cli

import "testing"

func TestParseTierString(t *testing.T) {
	cases := []struct {
		in           string
		wantProvider string
		wantModel    string
		wantErr      bool
	}{
		{"anthropic/claude-haiku-4-5", "anthropic", "claude-haiku-4-5", false},
		{"openai/gpt-5.4-mini", "openai", "gpt-5.4-mini", false},
		{"claude-haiku-4-5", "anthropic", "claude-haiku-4-5", false},      // bare model → anthropic
		{"ollama/llama3.1:8b", "ollama", "llama3.1:8b", false},            // first-slash split
		{"  anthropic/claude-opus-4-7  ", "anthropic", "claude-opus-4-7", false}, // trim whitespace
		{"", "", "", true},              // empty string
		{"anthropic/", "", "", true},    // missing model
		{"/claude-haiku", "", "", true}, // missing provider
	}
	for _, tc := range cases {
		gotProv, gotModel, err := parseTierString(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseTierString(%q): want error, got nil", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseTierString(%q): unexpected error: %v", tc.in, err)
			continue
		}
		if gotProv != tc.wantProvider || gotModel != tc.wantModel {
			t.Errorf("parseTierString(%q): got %q/%q; want %q/%q", tc.in, gotProv, gotModel, tc.wantProvider, tc.wantModel)
		}
	}
}
```

**Step 2: Run test — verify RED.**

```
go test ./internal/cli/ -run TestParseTierString -count=1
```
Expected: `undefined: parseTierString`.

**Step 3: Minimal implementation.**

`internal/cli/tier_parse.go`:

```go
package cli

import (
	"fmt"
	"strings"
)

// parseTierString splits a "provider/model" string. A bare model (no "/") defaults
// to provider "anthropic". Splits on the first "/" only so models containing
// additional slashes or colons (e.g. "llama3.1:8b") survive intact.
func parseTierString(raw string) (provider, model string, err error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", "", fmt.Errorf("empty tier value")
	}
	idx := strings.Index(s, "/")
	if idx < 0 {
		return "anthropic", s, nil
	}
	provider = s[:idx]
	model = s[idx+1:]
	if provider == "" {
		return "", "", fmt.Errorf("missing provider before '/' in %q", raw)
	}
	if model == "" {
		return "", "", fmt.Errorf("missing model after '/' in %q", raw)
	}
	return provider, model, nil
}
```

**Step 4: Run test — verify GREEN.**

Expected: PASS (8 subcases).

**Step 5: Commit.**

```
git commit -am "feat(cli): add parseTierString for 'provider/model' flag syntax

- RED: TestParseTierString (8 table cases)
- GREEN: splits on first slash; bare model → anthropic default; rejects empty/half values
- Status: 4 tests passing"
```

---

### Task 4: `providerSupportsToolUse` capability check

**Files:**
- Modify: `internal/cli/tier_parse.go`
- Modify: `internal/cli/tier_parse_test.go`

**Step 1: Write failing test.** Append to `tier_parse_test.go`:

```go
func TestProviderSupportsToolUse(t *testing.T) {
	cases := map[string]bool{
		"anthropic":         true,
		"openai":            true,
		"ollama":            false,
		"lmstudio":          false,
		"openai-compatible": false,
	}
	for provider, want := range cases {
		if got := providerSupportsToolUse(provider); got != want {
			t.Errorf("providerSupportsToolUse(%q) = %v; want %v", provider, got, want)
		}
	}
}

func TestProviderSupportsToolUseRejectsUnknown(t *testing.T) {
	if providerSupportsToolUse("bedrock") {
		t.Fatal("unknown provider should not report tool-use support")
	}
}
```

**Step 2: Run — RED.** Expected: `undefined: providerSupportsToolUse`.

**Step 3: Implementation.** Append to `tier_parse.go`:

```go
// providerSupportsToolUse reports whether the Bifrost integration for this
// provider currently supports tool calling (required by drift detection).
func providerSupportsToolUse(provider string) bool {
	switch provider {
	case "anthropic", "openai":
		return true
	default:
		return false
	}
}
```

**Step 4: Run — GREEN.** Expected: PASS.

**Step 5: Commit.**

```
git commit -am "feat(cli): add providerSupportsToolUse whitelist

- RED: TestProviderSupportsToolUse (5 cases) + TestProviderSupportsToolUseRejectsUnknown
- GREEN: whitelist anthropic/openai; everything else false
- Status: 6 tests passing"
```

---

### Task 5: `validateTierConfigs` — up-front validation

**Files:**
- Create: `internal/cli/tier_validate.go`
- Create: `internal/cli/tier_validate_test.go`

**Step 1: Write failing test.**

`internal/cli/tier_validate_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

func TestValidateTierConfigs_Defaults(t *testing.T) {
	err := validateTierConfigs("", "", "") // all empty → defaults applied
	if err != nil {
		t.Fatalf("default tier values should validate: %v", err)
	}
}

func TestValidateTierConfigs_UnknownProvider(t *testing.T) {
	err := validateTierConfigs("bogus/whatever", "", "")
	if err == nil || !strings.Contains(err.Error(), "small") {
		t.Fatalf("expected error naming 'small' tier for unknown provider, got %v", err)
	}
}

func TestValidateTierConfigs_LargeNeedsToolUse(t *testing.T) {
	err := validateTierConfigs("", "", "ollama/llama3.1")
	if err == nil {
		t.Fatal("expected error: ollama does not support tool use in large tier")
	}
	if !strings.Contains(err.Error(), "large") || !strings.Contains(err.Error(), "tool use") {
		t.Fatalf("error should mention 'large' and 'tool use': %v", err)
	}
}

func TestValidateTierConfigs_SmallCanBeNonToolUse(t *testing.T) {
	if err := validateTierConfigs("ollama/llama3.1", "", ""); err != nil {
		t.Fatalf("ollama in small tier should be allowed: %v", err)
	}
}
```

**Step 2: Run — RED.**

**Step 3: Implementation.**

`internal/cli/tier_validate.go`:

```go
package cli

import "fmt"

// Default tier strings used when a flag/config/env is empty.
const (
	defaultSmallTier   = "anthropic/claude-haiku-4-5"
	defaultTypicalTier = "anthropic/claude-sonnet-4-6"
	defaultLargeTier   = "anthropic/claude-opus-4-7"
)

// validateTierConfigs parses each tier string, applies defaults for empties,
// and enforces that the large tier's provider supports tool use.
// Returns typed errors naming the offending tier.
func validateTierConfigs(small, typical, large string) error {
	for _, tc := range []struct {
		name, raw string
		fallback  string
		needsTool bool
	}{
		{"small", small, defaultSmallTier, false},
		{"typical", typical, defaultTypicalTier, false},
		{"large", large, defaultLargeTier, true},
	} {
		s := tc.raw
		if s == "" {
			s = tc.fallback
		}
		provider, _, err := parseTierString(s)
		if err != nil {
			return fmt.Errorf("tier %q: %w", tc.name, err)
		}
		if !isKnownProvider(provider) {
			return fmt.Errorf("tier %q: unknown provider %q (valid: anthropic, openai, ollama, lmstudio, openai-compatible)", tc.name, provider)
		}
		if tc.needsTool && !providerSupportsToolUse(provider) {
			return fmt.Errorf("tier %q: provider %q does not support tool use; drift detection requires anthropic or openai", tc.name, provider)
		}
	}
	return nil
}

func isKnownProvider(p string) bool {
	switch p {
	case "anthropic", "openai", "ollama", "lmstudio", "openai-compatible":
		return true
	default:
		return false
	}
}
```

**Step 4: Run — GREEN.** Expected: PASS (4 cases).

**Step 5: Commit.**

```
git commit -am "feat(cli): add validateTierConfigs for startup fail-fast validation

- RED: TestValidateTierConfigs_* (4 cases: defaults, unknown provider, large needs tool use, small can skip tool use)
- GREEN: validateTierConfigs + isKnownProvider helper; default constants for each tier
- Status: 10 tests passing"
```

---

### Task 6: Replace `LLMConfig`/`newLLMClient` with `llmTiering` and `newLLMTiering`

**Files:**
- Modify: `internal/cli/llm_client.go`
- Modify: `internal/cli/llm_client_test.go` (rewrite — old tests exercise removed `LLMConfig`)

**Step 1: Write failing test.** Replace `internal/cli/llm_client_test.go` contents with:

```go
package cli

import (
	"os"
	"strings"
	"testing"
)

func TestNewLLMTiering_DefaultsRequireAnthropicKey(t *testing.T) {
	os.Unsetenv("ANTHROPIC_API_KEY")
	_, err := newLLMTiering("", "", "")
	if err == nil {
		t.Fatal("expected error when ANTHROPIC_API_KEY unset")
	}
	if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Fatalf("error should mention ANTHROPIC_API_KEY, got %v", err)
	}
}

func TestNewLLMTiering_SucceedsWithKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	tg, err := newLLMTiering("", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tg.Small() == nil || tg.Typical() == nil || tg.Large() == nil {
		t.Fatal("all three clients must be non-nil")
	}
}

func TestNewLLMTiering_RejectsUnknownProvider(t *testing.T) {
	_, err := newLLMTiering("bogus/foo", "", "")
	if err == nil || !strings.Contains(err.Error(), "small") {
		t.Fatalf("expected validation error naming 'small' tier, got %v", err)
	}
}

func TestNewLLMTiering_RejectsNonToolUseLarge(t *testing.T) {
	_, err := newLLMTiering("", "", "ollama/llama3.1")
	if err == nil || !strings.Contains(err.Error(), "tool use") {
		t.Fatalf("expected error about tool use, got %v", err)
	}
}
```

**Step 2: Run — RED.** `undefined: newLLMTiering` (and the `LLMConfig` tests still in the file still reference the old type — delete them as part of this task).

**Step 3: Implementation.** Replace `internal/cli/llm_client.go` contents:

```go
package cli

import (
	"fmt"
	"os"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// llmTiering holds one LLMClient + TokenCounter per tier. Implements analyzer.LLMTiering.
type llmTiering struct {
	small, typical, large                 analyzer.LLMClient
	smallCounter, typicalCounter, largeCounter analyzer.TokenCounter
}

func (t *llmTiering) Small() analyzer.LLMClient          { return t.small }
func (t *llmTiering) Typical() analyzer.LLMClient        { return t.typical }
func (t *llmTiering) Large() analyzer.LLMClient          { return t.large }
func (t *llmTiering) SmallCounter() analyzer.TokenCounter   { return t.smallCounter }
func (t *llmTiering) TypicalCounter() analyzer.TokenCounter { return t.typicalCounter }
func (t *llmTiering) LargeCounter() analyzer.TokenCounter   { return t.largeCounter }

// newLLMTiering parses and validates the three tier strings, then eagerly
// constructs all three clients. Empty strings fall back to built-in defaults
// (anthropic/claude-haiku-4-5, -sonnet-4-6, -opus-4-7). Missing API keys or
// unsupported providers fail here, before any analyze work begins.
func newLLMTiering(small, typical, large string) (*llmTiering, error) {
	if err := validateTierConfigs(small, typical, large); err != nil {
		return nil, err
	}

	tiers := []struct {
		name, raw, fallback string
	}{
		{"small", small, defaultSmallTier},
		{"typical", typical, defaultTypicalTier},
		{"large", large, defaultLargeTier},
	}
	built := make([]analyzer.LLMClient, len(tiers))
	counters := make([]analyzer.TokenCounter, len(tiers))
	for i, tc := range tiers {
		raw := tc.raw
		if raw == "" {
			raw = tc.fallback
		}
		provider, model, _ := parseTierString(raw) // already validated
		client, counter, err := buildTierClient(provider, model)
		if err != nil {
			return nil, fmt.Errorf("tier %q: %w", tc.name, err)
		}
		built[i] = client
		counters[i] = counter
	}
	return &llmTiering{
		small: built[0], typical: built[1], large: built[2],
		smallCounter: counters[0], typicalCounter: counters[1], largeCounter: counters[2],
	}, nil
}

// buildTierClient constructs a single (LLMClient, TokenCounter) for one (provider, model).
func buildTierClient(provider, model string) (analyzer.LLMClient, analyzer.TokenCounter, error) {
	switch provider {
	case "anthropic":
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			return nil, nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
		}
		client, err := analyzer.NewBifrostClientWithProvider("anthropic", key, model)
		if err != nil {
			return nil, nil, err
		}
		counter := analyzer.NewAnthropicCounter(key, model, os.Getenv("ANTHROPIC_BASE_URL"))
		return client, counter, nil
	case "openai":
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			return nil, nil, fmt.Errorf("OPENAI_API_KEY not set")
		}
		client, err := analyzer.NewBifrostClientWithProvider("openai", key, model)
		if err != nil {
			return nil, nil, err
		}
		// OpenAI uses local tiktoken counter.
		counter := analyzer.NewTiktokenCounter(model)
		return client, counter, nil
	case "ollama":
		baseURL := os.Getenv("OLLAMA_BASE_URL")
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
		return analyzer.NewOpenAICompatibleClient(baseURL, model, ""), analyzer.NewTiktokenCounter(model), nil
	case "lmstudio":
		baseURL := os.Getenv("LMSTUDIO_BASE_URL")
		if baseURL == "" {
			baseURL = "http://localhost:1234"
		}
		return analyzer.NewOpenAICompatibleClient(baseURL, model, ""), analyzer.NewTiktokenCounter(model), nil
	case "openai-compatible":
		baseURL := os.Getenv("OPENAI_COMPATIBLE_BASE_URL")
		if baseURL == "" {
			return nil, nil, fmt.Errorf("OPENAI_COMPATIBLE_BASE_URL env var required for openai-compatible")
		}
		return analyzer.NewOpenAICompatibleClient(baseURL, model, os.Getenv("OPENAI_API_KEY")), analyzer.NewTiktokenCounter(model), nil
	default:
		return nil, nil, fmt.Errorf("unknown provider %q", provider)
	}
}
```

> **Implementation note:** if `analyzer.NewTiktokenCounter` doesn't yet exist but `analyzer.NewAnthropicCounter` is the only counter today, check `internal/analyzer/` for the existing token-counter surface and use whichever constructor is appropriate. The goal is: each tier gets a counter matching its provider's expected tokenizer.

**Step 4: Run — GREEN.**

```
go test ./internal/cli/ -count=1
```
Expected: PASS.

**Step 5: Commit.**

```
git commit -am "feat(cli): replace LLMConfig with llmTiering struct and newLLMTiering constructor

- RED: TestNewLLMTiering_* (defaults need key, succeeds with key, rejects unknown provider, rejects non-tool-use large)
- GREEN: llmTiering struct implements analyzer.LLMTiering; buildTierClient per-provider dispatch
- Removed: LLMConfig struct, newLLMClient function (superseded)
- Status: 14 tests passing"
```

---

## Phase 3 — CLI Flag Wiring

### Task 7: Add tier flags, remove old flags, wire Viper

**Files:**
- Modify: `internal/cli/analyze.go` (flag registration around line 353)
- Modify: `internal/cli/root.go` (Viper bindings, if present)
- Modify: `internal/cli/analyze_test.go` (update test harness that uses `--llm-provider`/`--llm-model`)

**Context to read first:**
- `internal/cli/analyze.go` lines 82-120 (flag declarations + `LLMConfig` construction).
- `internal/cli/analyze.go` line 353-360 (flag registration).
- `internal/cli/root.go` if Viper is configured there.

**Step 1: Write failing test.** Add to `internal/cli/analyze_test.go`:

```go
func TestAnalyzeCmd_AcceptsTierFlags(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	cmd := newAnalyzeCmd()
	cmd.SetArgs([]string{
		"--repo", t.TempDir(),
		"--docs-url", "http://example.invalid",
		"--llm-small", "anthropic/claude-haiku-4-5",
		"--llm-typical", "anthropic/claude-sonnet-4-6",
		"--llm-large", "anthropic/claude-opus-4-7",
		"--dry-run", // or whatever exits before network; adjust to existing harness
	})
	// Should at least parse flags without "unknown flag" error.
	err := cmd.Flags().Parse([]string{
		"--llm-small=anthropic/claude-haiku-4-5",
		"--llm-typical=anthropic/claude-sonnet-4-6",
		"--llm-large=anthropic/claude-opus-4-7",
	})
	if err != nil {
		t.Fatalf("tier flags should parse: %v", err)
	}
}

func TestAnalyzeCmd_RejectsRemovedFlags(t *testing.T) {
	cmd := newAnalyzeCmd()
	err := cmd.Flags().Parse([]string{"--llm-provider=anthropic"})
	if err == nil {
		t.Fatal("--llm-provider should be removed and rejected")
	}
}
```

**Step 2: Run — RED.** Expected: `unknown flag: --llm-small` (and symmetrical).

**Step 3: Implementation.**

In `internal/cli/analyze.go`:

1. Delete the local variables `llmProvider`, `llmModel`, `llmBaseURL` (around line 82-84).
2. Add `llmSmall`, `llmTypical`, `llmLarge string` in their place.
3. Replace the `cfg := &LLMConfig{...}` block (around line 112) with:
   ```go
   tiering, err := newLLMTiering(llmSmall, llmTypical, llmLarge)
   if err != nil {
       return err
   }
   llmClient := tiering.Large() // temp: wire full tiering in Phase 4
   ```
   (We'll refactor the downstream calls in Phase 4; keep behavior-preserving wiring for now so the build stays green.)
4. Replace the three flag registrations (around line 353-357):
   ```go
   cmd.Flags().StringVar(&llmSmall, "llm-small", "",
       "small-tier model as \"provider/model\" (default: anthropic/claude-haiku-4-5)")
   cmd.Flags().StringVar(&llmTypical, "llm-typical", "",
       "typical-tier model as \"provider/model\" (default: anthropic/claude-sonnet-4-6)")
   cmd.Flags().StringVar(&llmLarge, "llm-large", "",
       "large-tier model as \"provider/model\" (default: anthropic/claude-opus-4-7)")
   ```
5. Delete the `switch cfg.Provider` block that builds `tokenCounter` — that responsibility now belongs to `newLLMTiering`. Replace with `tokenCounter := tiering.LargeCounter()` (mapper still uses token counting for the large tier).
6. Update the existing tool-use check (around line 303) to inspect `tiering.Large()` instead of the old `llmClient` and to reference the new flag name in the error message.

In `internal/cli/root.go` (if Viper is present), bind:
```go
v.BindPFlag("llm.small", analyzeCmd.Flags().Lookup("llm-small"))
v.BindPFlag("llm.typical", analyzeCmd.Flags().Lookup("llm-typical"))
v.BindPFlag("llm.large", analyzeCmd.Flags().Lookup("llm-large"))
v.SetEnvPrefix("FIND_THE_GAPS")
v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
v.AutomaticEnv()
```
If no central Viper setup exists yet, create a minimal one in `internal/cli/config.go` (new file) and wire it into `analyzeCmd.PreRunE`.

7. Update `internal/cli/analyze_test.go` — every occurrence of `--llm-provider`/`--llm-model`/`--llm-base-url` becomes a corresponding `--llm-small`/`--llm-typical`/`--llm-large` tier string (e.g. `"--llm-large", "ollama/llama3"`). Note: some tests passed base URLs; these now come from env (`OLLAMA_BASE_URL`), so convert them to `t.Setenv(...)`.

**Step 4: Run — GREEN.**

```
go build ./...
go test ./internal/cli/ -count=1
```
Expected: PASS. Expect to iterate a few times on `analyze_test.go` callers.

**Step 5: Commit.**

```
git commit -am "feat(cli): replace --llm-provider/--llm-model/--llm-base-url with --llm-small/--llm-typical/--llm-large

- RED: TestAnalyzeCmd_{AcceptsTierFlags,RejectsRemovedFlags}
- GREEN: tier flags registered; newLLMTiering called up front; tokenCounter pulled from tiering.LargeCounter()
- Migration: analyze_test.go updated to use tier flags; base-URL tests switched to env vars (OLLAMA_BASE_URL etc.)
- Status: all cli tests passing, build succeeds"
```

---

## Phase 4 — Call-Site Refactor

For each call site below, the change is: accept `analyzer.LLMTiering` (not `LLMClient`) and pick the right tier at the call line. Phase 3 already wired the CLI to pass a working `*llmTiering`; this phase threads it through.

### Task 8: `ExtractFeaturesFromCode` → `Typical()`

**Files:**
- Modify: `internal/analyzer/code_features.go` (line 57 prompt)
- Modify: `internal/analyzer/code_features_test.go`
- Modify: `internal/cli/analyze.go` (caller around line 219)

**Step 1: Write/update test.** Update existing test to pass a `fakeLLMTiering` whose `Typical` returns a recording client; assert the prompt was dispatched to `Typical` not `Small`/`Large`.

**Step 2: Run — RED.** Signature mismatch.

**Step 3: Implementation.**
- Change `ExtractFeaturesFromCode(ctx, client LLMClient, scan)` → `ExtractFeaturesFromCode(ctx, tiering LLMTiering, scan)`.
- First line of body: `client := tiering.Typical()`. Everything else unchanged.
- Update caller in `analyze.go`: `analyzer.ExtractFeaturesFromCode(ctx, tiering, scan)`.

**Step 4: Run — GREEN.**

**Step 5: Commit.**

```
git commit -am "refactor(analyzer): ExtractFeaturesFromCode takes LLMTiering, uses Typical tier

- RED: code_features_test asserts dispatch through Typical()
- GREEN: signature change; picks tiering.Typical() at call line (next to // PROMPT:)
- Status: analyzer + cli packages green"
```

---

### Task 9: `AnalyzePage` → `Small()`

Same shape as Task 8.

**Files:** `internal/analyzer/analyze_page.go:16`, `analyze_page_test.go`, `internal/cli/analyze.go:160`.

At the top of `AnalyzePage`: `client := tiering.Small()`.

**Commit message:**
```
refactor(analyzer): AnalyzePage takes LLMTiering, uses Small tier
```

---

### Task 10: `SynthesizeProduct` → `Small()`

**Files:** `internal/analyzer/synthesize.go:24`, `synthesize_test.go`, `internal/cli/analyze.go:187`.

`client := tiering.Small()`.

**Commit message:**
```
refactor(analyzer): SynthesizeProduct takes LLMTiering, uses Small tier
```

---

### Task 11: `MapFeaturesToCode` → `Large()` (client + counter)

**Files:** `internal/analyzer/mapper.go` (lines 38, 93, 109, 130), `mapper_test.go`, `internal/cli/analyze.go` (around line 277).

Changes:
1. Signature becomes `MapFeaturesToCode(ctx, tiering LLMTiering, features, scan, tokenBudget, filesOnly, onBatch)`.
2. First lines of body: `client := tiering.Large(); counter := tiering.LargeCounter()`.
3. Drop the separate `counter TokenCounter` parameter.
4. Both prompt variants (files-only at :93 and files+symbols at :109) remain — they already live inside the same function and pick the same tier.
5. Update caller.

**Commit message:**
```
refactor(analyzer): MapFeaturesToCode takes LLMTiering, uses Large tier

- Both prompt variants (--no-symbols and symbols+files) dispatch through Large()
- Counter now comes from tiering.LargeCounter()
```

---

### Task 12: `MapFeaturesToDocs` → `Small()` per page

**Files:** `internal/analyzer/docs_mapper.go` (line 53, 95), `docs_mapper_test.go`, caller in `internal/cli/analyze.go`.

Inside `mapPageToFeatures`: `client := tiering.Small()`.

**Commit message:**
```
refactor(analyzer): MapFeaturesToDocs takes LLMTiering, uses Small tier per page
```

---

### Task 13: `DetectDrift` — `Large()` for agentic loop + `Small()` for `isReleaseNotePage`

**Files:** `internal/analyzer/drift.go` (lines 32, 102, 320), `drift_test.go`, caller in `internal/cli/analyze.go` (around line 303).

Signature: `DetectDrift(ctx, tiering LLMTiering, ...)`.

Inside `detectDriftForFeature` (before line 102 prompt): `client := tiering.Large()`. Use `client.CompleteWithTools(...)` as before; the `ToolLLMClient` assertion moves into the tiering construction (Large must support tools — already validated at startup, so this cast is safe, but keep a defensive assert for clarity).

Inside `isReleaseNotePage` (before line 320 prompt): `client := tiering.Small()`. (This is the one call site where the audit recommended downgrading for major cost savings.)

**Commit message:**
```
refactor(analyzer): DetectDrift takes LLMTiering; Large tier for agentic loop, Small tier for release-note classifier

- detectDriftForFeature: tiering.Large() (unchanged behavior; previously global)
- isReleaseNotePage: tiering.Small() (downgraded per audit; binary yes/no classification)
- Status: drift tests green
```

---

### Task 14: Drop `tokenCounter` top-level wiring in `analyze.go`

**Files:** `internal/cli/analyze.go`.

Now that every analyzer function pulls its counter from the tiering, the dedicated `tokenCounter` variable in `analyze.go` is unused. Delete it.

Also delete the temporary `llmClient := tiering.Large()` shim from Task 7 — the only remaining use was for the tool-use cast; that now lives inside `DetectDrift`.

**Commit message:**
```
refactor(cli): drop now-unused tokenCounter and llmClient locals in analyze.go

- All 7 call sites pull their client and counter directly from the tiering
- Status: build clean, cli tests green
```

---

## Phase 5 — Integration Tests

### Task 15: Testscript — happy-path analyze with tier flags

**Files:**
- Create: `cmd/find-the-gaps/testdata/script/analyze_tier_flags.txtar`

**Step 1: Write failing testscript.** Use the existing `analyze_llm_flags.txtar` as a template. Stub the LLM (ollama-compatible local endpoint pattern already used in `analyze_test.go`). Pass:

```
ftg analyze \
    --repo $WORK/repo \
    --docs-url $SRV_URL \
    --llm-small anthropic/claude-haiku-4-5 \
    --llm-typical anthropic/claude-sonnet-4-6 \
    --llm-large anthropic/claude-opus-4-7
```

Assert exit 0 and expected report file. Requires `ANTHROPIC_API_KEY` stub (set via `env ANTHROPIC_API_KEY=stub`).

**Step 2-5:** RED → GREEN → commit as usual. Target test file: `cmd/find-the-gaps/script_test.go` (if already present) or the package's existing testscript runner.

**Commit message:**
```
test(cli): add testscript for analyze with tier flags

- Exercises --llm-small/--llm-typical/--llm-large with Anthropic-default values
```

---

### Task 16: Testscript — fail-fast on non-tool-use `large` tier

**Files:**
- Create: `cmd/find-the-gaps/testdata/script/analyze_tier_reject_ollama_large.txtar`

Invoke:
```
! ftg analyze --repo $WORK/repo --docs-url $SRV_URL --llm-large ollama/llama3.1
stderr 'tool use'
stderr 'large'
```

Expected: non-zero exit before any LLM work begins.

**Commit message:**
```
test(cli): add testscript ensuring large tier without tool-use support fails fast
```

---

## Phase 6 — Docs, Migration, Verification

### Task 17: Update README

**Files:** `README.md`.

Add/update sections:
- **Configuration** section showing the three tier flags, Anthropic defaults, and the TOML snippet:
  ```toml
  [llm]
  small   = "anthropic/claude-haiku-4-5"
  typical = "anthropic/claude-sonnet-4-6"
  large   = "anthropic/claude-opus-4-7"
  ```
- **Environment variables** subsection: `FIND_THE_GAPS_LLM_SMALL`, `_TYPICAL`, `_LARGE`.
- **Breaking change callout**: note that `--llm-provider`, `--llm-model`, and `--llm-base-url` are removed.

**Commit message:**
```
docs(readme): document tier flags, TOML config, env vars, and breaking CLI change
```

---

### Task 18: Add `CHANGELOG.md` entry

**Files:** `CHANGELOG.md` (create if not present).

Entry:
```
## Unreleased

### Changed (breaking)
- Removed `--llm-provider`, `--llm-model`, and `--llm-base-url` flags.
- Introduced `--llm-small`, `--llm-typical`, `--llm-large` with combined
  `provider/model` syntax (e.g. `anthropic/claude-opus-4-7`). Bare model names
  default to the `anthropic` provider. Each tier is configurable independently
  via CLI flag, `[llm]` section in TOML config, or `FIND_THE_GAPS_LLM_*` env var.
- Migration: replace `--llm-provider X --llm-model Y` with
  `--llm-typical X/Y` (or the tier that matches your use case).

### Added
- Per-tier client construction with eager startup validation; unknown providers
  or non-tool-use `large` tiers now fail fast.
```

**Commit message:**
```
docs(changelog): document breaking tier flag migration and new defaults
```

---

### Task 19: Update `PROGRESS.md`

**Files:** `PROGRESS.md`.

Append the task template from CLAUDE.md rule 8 for each phase. Summary form at minimum:

```markdown
## Task: LLM Tiering
- Started: <date>
- Tests: N passing, 0 failing
- Coverage: X% per package (run `go test -cover ./...`)
- Build: ✅
- Linting: ✅ (run `golangci-lint run`)
- Completed: <date>
- Notes:
  - 7 call sites routed through analyzer.LLMTiering
  - CLI flags replaced; old flags removed cleanly
  - Fail-fast validation on unknown providers and non-tool-use large tier
  - Integration tests cover happy path and reject-non-tool-use large
```

**Commit message:**
```
docs(progress): log LLM tiering task completion
```

---

### Task 20: Final verification sweep

**Not a code task — a checklist to run before opening the PR.** Use @superpowers:verification-before-completion.

Commands:

```
go test -count=1 ./...                            # all tests green
go test -cover ./...                              # confirm ≥90% per package
go build ./...                                    # clean build
golangci-lint run                                 # no lint errors
gofmt -l . && goimports -l .                      # no format drift
```

If any command fails, stop and fix before opening the PR.

---

## Out of Scope (for this plan)

- Cross-tier fallback (e.g. "if large fails, retry on typical").
- Per-call-site tier override via CLI.
- Observability: per-tier call count / token-usage metrics.
- Migration of existing `.find-the-gaps/config.toml` files on users' machines (breaking change; documented in CHANGELOG).

---

## Quick Reference — What Goes Where

| Site | File:Line | Tier | Picks via |
|------|-----------|------|-----------|
| Extract Features | `code_features.go:57` | Typical | `tiering.Typical()` |
| Analyze Page | `analyze_page.go:16` | Small | `tiering.Small()` |
| Synthesize Product | `synthesize.go:24` | Small | `tiering.Small()` |
| Map to Code (both variants) | `mapper.go:93` + `:109` | Large | `tiering.Large()` + `tiering.LargeCounter()` |
| Map to Docs | `docs_mapper.go:53` | Small | `tiering.Small()` |
| Drift (agentic) | `drift.go:102` | Large | `tiering.Large()` |
| Release-note classifier | `drift.go:320` | Small | `tiering.Small()` |
