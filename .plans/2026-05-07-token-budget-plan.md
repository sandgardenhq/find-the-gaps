# Per-Model Token Budget Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Stop the drift investigator from crashing the run when its accumulated context exceeds the model's input cap. Add a per-model input-token budget, gate every LLM call against it, and degrade gracefully (early stop, judge chunking, page-skip) per call site.

**Architecture:** See `.plans/2026-05-07-token-budget-design.md` for the full design. Summary: a `budgetedClient` decorator wraps every BifrostClient at tier-construction time; the decorator counts tokens with the local cl100k_base estimator and refuses sends over `0.9 × MaxInputTokens`; multi-turn loops terminate cleanly on the budget error; the drift judge compacts via observation chunking; page/screenshot/mapper callers log + skip.

**Tech Stack:** Go 1.26+, `tiktoken-go/tokenizer` (already vendored), `testify`. TDD per `CLAUDE.md` — every production change starts with a failing test.

**Conventions:**
- Plans live in `.plans/` (override of skill default).
- Test file layout: `internal/foo/bar.go` → `internal/foo/bar_test.go`.
- Test command: `go test ./...`. Use `-run` and `-count=1` to target specific tests during inner loops.
- Lint: `golangci-lint run`. Format: `gofmt -w . && goimports -w .`.
- Commit per RED-GREEN-REFACTOR cycle. Conventional Commits (`fix:`, `feat:`, `refactor:`).

**Branch:** Already on `fix/drift-token-budget`. Do not switch branches.

**Total tasks:** 14. Each task is 2–10 minutes of focused work plus its commit.

---

## Task 1: Add `MaxInputTokens` to the cli capability table

**Files:**
- Modify: `internal/cli/capabilities.go`
- Test: `internal/cli/capabilities_test.go` (file may not exist yet — create if so)

**Why first:** Pure data-model change. Everything else reads from this table.

**Step 1: Write failing tests**

Add (or create) `internal/cli/capabilities_test.go` with:

```go
package cli

import "testing"

func TestResolveCapabilities_KnownModelsCarryMaxInputTokens(t *testing.T) {
	cases := []struct {
		provider, model string
		want            int
	}{
		{"anthropic", "claude-haiku-4-5", 180000},
		{"anthropic", "claude-sonnet-4-6", 180000},
		{"anthropic", "claude-opus-4-7", 180000},
		{"openai", "gpt-5.5", 260000},
		{"openai", "gpt-5.4", 260000},
		{"openai", "gpt-5.4-mini", 260000},
		{"openai", "gpt-5.4-nano", 260000},
		{"openai", "gpt-5", 260000},
		{"openai", "gpt-5-mini", 260000},
		{"openai", "gpt-4o", 115000},
		{"openai", "gpt-4o-mini", 115000},
		{"groq", "meta-llama/llama-4-scout-17b-16e-instruct", 120000},
	}
	for _, tc := range cases {
		t.Run(tc.provider+"/"+tc.model, func(t *testing.T) {
			caps, ok := ResolveCapabilities(tc.provider, tc.model)
			if !ok {
				t.Fatalf("ResolveCapabilities(%q,%q) returned ok=false", tc.provider, tc.model)
			}
			if caps.MaxInputTokens != tc.want {
				t.Fatalf("MaxInputTokens = %d, want %d", caps.MaxInputTokens, tc.want)
			}
		})
	}
}

func TestResolveCapabilities_UnknownModelOnKnownProviderUsesConservativeDefault(t *testing.T) {
	caps, ok := ResolveCapabilities("anthropic", "claude-future-99")
	if !ok {
		t.Fatalf("expected ok=true for known provider")
	}
	if caps.MaxInputTokens != 100000 {
		t.Fatalf("MaxInputTokens = %d, want 100000", caps.MaxInputTokens)
	}
}

func TestResolveCapabilities_SelfHostedWildcardHasNoBudget(t *testing.T) {
	for _, provider := range []string{"ollama", "lmstudio"} {
		caps, ok := ResolveCapabilities(provider, "anything-the-user-picked")
		if !ok {
			t.Fatalf("expected ok=true for %q", provider)
		}
		if caps.MaxInputTokens != 0 {
			t.Fatalf("%s MaxInputTokens = %d, want 0 (off)", provider, caps.MaxInputTokens)
		}
	}
}
```

**Step 2: Run tests, watch them fail**

```
go test ./internal/cli/ -run TestResolveCapabilities -count=1
```

Expected: compile error or all FAIL — `MaxInputTokens` doesn't exist on `ModelCapabilities`.

**Step 3: Implement minimally**

In `internal/cli/capabilities.go`:

1. Add to the `ModelCapabilities` struct (right after `MaxCompletionTokens`):
   ```go
   // MaxInputTokens is the per-model input cap including system prompt,
   // tool defs, and accumulated chat history. Zero means "no budget"
   // (used for ollama/lmstudio "*" rows where the user picks the model).
   // The budget gate sits at 0.9 × this value.
   MaxInputTokens int
   ```

2. Populate every row in `knownModels`:
   - `claude-haiku-4-5`, `claude-sonnet-4-6`, `claude-opus-4-7`: `MaxInputTokens: 180000`
   - `gpt-5.5`, `gpt-5.4`, `gpt-5.4-mini`, `gpt-5.4-nano`, `gpt-5`, `gpt-5-mini`: `MaxInputTokens: 260000`
   - `gpt-4o`, `gpt-4o-mini`: `MaxInputTokens: 115000`
   - `meta-llama/llama-4-scout-17b-16e-instruct`: `MaxInputTokens: 120000`
   - `ollama` / `lmstudio` `*`: leave as zero.

3. Update the unknown-model branch in `ResolveCapabilities` (currently returns `ModelCapabilities{Provider: provider, Model: model}`):
   ```go
   if providerKnown {
       return ModelCapabilities{Provider: provider, Model: model, MaxInputTokens: 100000}, true
   }
   ```

**Step 4: Run tests, confirm green**

```
go test ./internal/cli/ -run TestResolveCapabilities -count=1
```

Expected: PASS.

Also run the broader cli suite to confirm no incidental break:

```
go test ./internal/cli/ -count=1
```

**Step 5: Commit**

```bash
git add internal/cli/capabilities.go internal/cli/capabilities_test.go
git commit -m "$(cat <<'EOF'
feat(capabilities): add MaxInputTokens per-model budget field

- RED: TestResolveCapabilities_* covering known rows, unknown-model
  conservative default (100k), and self-hosted wildcard (no budget).
- GREEN: populate Anthropic 180k / OpenAI 5x family 260k / 4o family 115k /
  Groq llama-4-scout 120k; unknown model on a known provider returns 100k.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Mirror `MaxInputTokens` on the analyzer's `ModelCapabilities`

**Files:**
- Modify: `internal/analyzer/client.go` (add field)
- Modify: `internal/cli/llm_client.go` (`buildTierClient`, around line 184–191) — propagate the field into `analyzer.ModelCapabilities`.
- Test: `internal/analyzer/client_test.go` (extend existing)

**Why second:** The analyzer must see the value before any decorator can read it. This is the bridge across the cli ↔ analyzer boundary.

**Step 1: Write failing test**

Add to `internal/analyzer/client_test.go`:

```go
func TestModelCapabilities_HasMaxInputTokensField(t *testing.T) {
	c := ModelCapabilities{MaxInputTokens: 42}
	if c.MaxInputTokens != 42 {
		t.Fatalf("MaxInputTokens not stored: %d", c.MaxInputTokens)
	}
}
```

And in `internal/cli/llm_client_test.go` (create if absent), a focused test that propagation happens. Easier path: extract the conversion into a small helper and test the helper:

In `internal/cli/llm_client.go`, refactor the inline `analyzerCaps := analyzer.ModelCapabilities{...}` block into a function `toAnalyzerCaps(caps ModelCapabilities) analyzer.ModelCapabilities` and test that:

```go
func TestToAnalyzerCaps_PropagatesMaxInputTokens(t *testing.T) {
	in := ModelCapabilities{Provider: "openai", Model: "gpt-5.5", MaxInputTokens: 260000}
	out := toAnalyzerCaps(in)
	if out.MaxInputTokens != 260000 {
		t.Fatalf("MaxInputTokens not propagated: %d", out.MaxInputTokens)
	}
}
```

**Step 2: Run tests, watch them fail**

```
go test ./internal/analyzer/ -run TestModelCapabilities_HasMaxInputTokensField -count=1
go test ./internal/cli/ -run TestToAnalyzerCaps -count=1
```

Both should FAIL — field/function don't exist.

**Step 3: Implement minimally**

1. In `internal/analyzer/client.go`, add to `ModelCapabilities` (right after `MaxCompletionTokens`):
   ```go
   // MaxInputTokens is the per-model input cap. Mirrors cli.ModelCapabilities's
   // field of the same name; see that type for semantics.
   MaxInputTokens int
   ```

2. In `internal/cli/llm_client.go`, replace the inline conversion in `buildTierClient` with a call to `toAnalyzerCaps`. Define:
   ```go
   func toAnalyzerCaps(caps ModelCapabilities) analyzer.ModelCapabilities {
       return analyzer.ModelCapabilities{
           Provider:            caps.Provider,
           Model:               caps.Model,
           ToolUse:             caps.ToolUse,
           Vision:              caps.Vision,
           MaxCompletionTokens: caps.MaxCompletionTokens,
           MaxInputTokens:      caps.MaxInputTokens,
       }
   }
   ```
   Then call it: `analyzerCaps := toAnalyzerCaps(caps)`.

**Step 4: Run tests, confirm green**

```
go test ./internal/analyzer/ ./internal/cli/ -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/analyzer/client.go internal/cli/llm_client.go internal/analyzer/client_test.go internal/cli/llm_client_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): mirror MaxInputTokens on analyzer ModelCapabilities

- RED: tests for the field on analyzer.ModelCapabilities and for
  toAnalyzerCaps propagating MaxInputTokens.
- GREEN: add the field; extract toAnalyzerCaps and propagate.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Define `ErrTokenBudgetExceeded`

**Files:**
- Create: `internal/analyzer/budgeted_client.go`
- Create: `internal/analyzer/budgeted_client_test.go`

**Step 1: Write failing test**

```go
package analyzer

import (
	"errors"
	"strings"
	"testing"
)

func TestErrTokenBudgetExceeded_ImplementsError(t *testing.T) {
	err := ErrTokenBudgetExceeded{
		Provider: "openai", Model: "gpt-5.5",
		Counted: 294098, Budget: 234000,
		Where: "drift-investigator",
	}
	msg := err.Error()
	for _, want := range []string{"openai/gpt-5.5", "294098", "234000", "drift-investigator"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("Error() = %q, missing %q", msg, want)
		}
	}
	if !errors.Is(err, ErrTokenBudgetExceeded{}) {
		t.Fatalf("errors.Is should match the zero-value sentinel")
	}
}
```

**Step 2: Run test, watch it fail**

```
go test ./internal/analyzer/ -run TestErrTokenBudgetExceeded -count=1
```

Expected: compile error — type doesn't exist.

**Step 3: Implement minimally**

In `internal/analyzer/budgeted_client.go`:

```go
package analyzer

import "fmt"

// ErrTokenBudgetExceeded is returned by budgetedClient when a request's
// estimated input token count exceeds the gated budget. Callers handle this
// error by compacting (drift judge), refusing per-unit (page analyzer,
// screenshot pass), or terminating a multi-turn loop early (drift
// investigator). See .plans/2026-05-07-token-budget-design.md.
//
// Comparable via errors.Is to a zero-value ErrTokenBudgetExceeded sentinel.
type ErrTokenBudgetExceeded struct {
	Provider, Model string
	Counted, Budget int
	Where           string
}

func (e ErrTokenBudgetExceeded) Error() string {
	return fmt.Sprintf("token budget exceeded for %s/%s in %s: %d tokens > budget %d",
		e.Provider, e.Model, e.Where, e.Counted, e.Budget)
}

// Is allows errors.Is to match any ErrTokenBudgetExceeded against the
// zero-value sentinel without checking field equality.
func (e ErrTokenBudgetExceeded) Is(target error) bool {
	_, ok := target.(ErrTokenBudgetExceeded)
	return ok
}
```

**Step 4: Run test, confirm green**

```
go test ./internal/analyzer/ -run TestErrTokenBudgetExceeded -count=1
```

**Step 5: Commit**

```bash
git add internal/analyzer/budgeted_client.go internal/analyzer/budgeted_client_test.go
git commit -m "feat(analyzer): add ErrTokenBudgetExceeded sentinel error

- RED: error fields populated, errors.Is matches sentinel.
- GREEN: typed struct + Error/Is methods.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 4: Token-counting helper for chat messages

**Files:**
- Modify: `internal/analyzer/budgeted_client.go`
- Modify: `internal/analyzer/budgeted_client_test.go`

**Why:** The decorator needs to count `(systemPrompt, []ChatMessage, []Tool, JSONSchema)` payloads. tiktoken counts strings; messages are structured. Wrap the conversion.

**Step 1: Write failing tests**

Append to `budgeted_client_test.go`:

```go
func TestCountPayloadTokens_FlatPrompt(t *testing.T) {
	n := countPayloadTokens("hello world", nil, nil, JSONSchema{})
	if n <= 0 {
		t.Fatalf("expected non-zero count, got %d", n)
	}
}

func TestCountPayloadTokens_SumsAllParts(t *testing.T) {
	prompt := "hello"
	msgs := []ChatMessage{{Role: "user", Content: "world"}}
	tools := []Tool{{Name: "read", Description: "reads", Parameters: map[string]any{"type": "object"}}}
	schema := JSONSchema{Name: "x", Doc: []byte(`{"type":"object"}`)}

	all := countPayloadTokens(prompt, msgs, tools, schema)
	just := countPayloadTokens(prompt, nil, nil, JSONSchema{})

	if all <= just {
		t.Fatalf("expected message+tool+schema tokens to add up, got all=%d just=%d", all, just)
	}
}
```

Note: the test treats `countPayloadTokens` as an additive function; it does not pin an exact integer (which would tie us to tiktoken's exact bpe table).

**Step 2: Run, watch them fail**

```
go test ./internal/analyzer/ -run TestCountPayloadTokens -count=1
```

Expected: compile error.

**Step 3: Implement**

Append to `budgeted_client.go`:

```go
// countPayloadTokens returns an estimated token count for a complete
// outbound LLM payload. Sums the prompt, every chat message's text, every
// tool's name+description+parameters JSON, and the JSON schema body when
// present. Uses the local cl100k_base tiktoken encoder (countTokens) — a
// fast estimator that runs ~5–15% off true counts on non-OpenAI tokenizers.
// The decorator's gate at 0.9 × MaxInputTokens absorbs that drift.
func countPayloadTokens(prompt string, messages []ChatMessage, tools []Tool, schema JSONSchema) int {
	n := 0
	if prompt != "" {
		n += countTokens(prompt)
	}
	for _, m := range messages {
		n += countTokens(m.Content)
		// ContentBlocks (vision messages) carry text-only blocks too; count those.
		// Image blocks are NOT counted here — providers tokenize images out-of-band.
		for _, b := range m.ContentBlocks {
			if b.Text != "" {
				n += countTokens(b.Text)
			}
		}
		for _, tc := range m.ToolCalls {
			n += countTokens(tc.Name) + countTokens(tc.Arguments)
		}
	}
	for _, t := range tools {
		n += countTokens(t.Name) + countTokens(t.Description)
		if t.Parameters != nil {
			if buf, err := jsonMarshal(t.Parameters); err == nil {
				n += countTokens(string(buf))
			}
		}
	}
	if schema.Name != "" || len(schema.Doc) > 0 {
		n += countTokens(schema.Name) + countTokens(string(schema.Doc))
	}
	return n
}
```

Add `jsonMarshal` as a private helper:

```go
import "encoding/json"

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }
```

(`encoding/json` may already be imported — check first; collapse the wrapper if so.)

**Verify** that `ChatMessage` actually has the fields `Content`, `ContentBlocks` (with `.Text`), and `ToolCalls`. Read `internal/analyzer/types.go` to confirm before committing — adjust the helper to match the actual struct shape if any field name differs.

**Step 4: Run, confirm green**

```
go test ./internal/analyzer/ -run TestCountPayloadTokens -count=1
```

**Step 5: Commit**

```bash
git add internal/analyzer/budgeted_client.go internal/analyzer/budgeted_client_test.go
git commit -m "feat(analyzer): add countPayloadTokens helper for budget gating

- RED: additive sums across prompt + messages + tools + schema.
- GREEN: tiktoken-based counter wrapping the existing countTokens.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 5: `budgetedClient` decorator — single-shot path

**Files:**
- Modify: `internal/analyzer/budgeted_client.go`
- Modify: `internal/analyzer/budgeted_client_test.go`

**Step 1: Write failing tests**

```go
type fakeLLM struct {
	caps         ModelCapabilities
	completeFn   func(ctx context.Context, prompt string) (string, error)
	completeJSON func(ctx context.Context, prompt string, schema JSONSchema) (json.RawMessage, error)
	completeMM   func(ctx context.Context, msgs []ChatMessage, schema JSONSchema) (json.RawMessage, error)
	calls        int
}

func (f *fakeLLM) Complete(ctx context.Context, prompt string) (string, error) {
	f.calls++
	if f.completeFn != nil {
		return f.completeFn(ctx, prompt)
	}
	return "ok", nil
}
func (f *fakeLLM) CompleteJSON(ctx context.Context, prompt string, schema JSONSchema) (json.RawMessage, error) {
	f.calls++
	if f.completeJSON != nil {
		return f.completeJSON(ctx, prompt, schema)
	}
	return json.RawMessage(`{}`), nil
}
func (f *fakeLLM) CompleteJSONMultimodal(ctx context.Context, msgs []ChatMessage, schema JSONSchema) (json.RawMessage, error) {
	f.calls++
	if f.completeMM != nil {
		return f.completeMM(ctx, msgs, schema)
	}
	return json.RawMessage(`{}`), nil
}
func (f *fakeLLM) Capabilities() ModelCapabilities { return f.caps }

func TestBudgetedClient_PassthroughWhenUnderBudget(t *testing.T) {
	inner := &fakeLLM{caps: ModelCapabilities{Provider: "p", Model: "m", MaxInputTokens: 100000}}
	bc := newBudgetedClient(inner, "test")
	if _, err := bc.CompleteJSON(context.Background(), "tiny", JSONSchema{}); err != nil {
		t.Fatal(err)
	}
	if inner.calls != 1 {
		t.Fatalf("inner not called: calls=%d", inner.calls)
	}
}

func TestBudgetedClient_RefusesWhenOverBudget(t *testing.T) {
	inner := &fakeLLM{caps: ModelCapabilities{Provider: "p", Model: "m", MaxInputTokens: 10}}
	bc := newBudgetedClient(inner, "test")
	huge := strings.Repeat("token-ish-text ", 200)
	_, err := bc.CompleteJSON(context.Background(), huge, JSONSchema{})
	var bErr ErrTokenBudgetExceeded
	if !errors.As(err, &bErr) {
		t.Fatalf("want ErrTokenBudgetExceeded, got %v", err)
	}
	if bErr.Provider != "p" || bErr.Model != "m" || bErr.Where != "test" {
		t.Fatalf("error fields wrong: %+v", bErr)
	}
	if inner.calls != 0 {
		t.Fatalf("inner should NOT be called when over budget, calls=%d", inner.calls)
	}
}

func TestBudgetedClient_NoBudgetMeansNoGate(t *testing.T) {
	inner := &fakeLLM{caps: ModelCapabilities{Provider: "ollama", Model: "*", MaxInputTokens: 0}}
	bc := newBudgetedClient(inner, "test")
	huge := strings.Repeat("x ", 100000)
	if _, err := bc.CompleteJSON(context.Background(), huge, JSONSchema{}); err != nil {
		t.Fatal(err)
	}
	if inner.calls != 1 {
		t.Fatalf("inner not called when budget is 0: calls=%d", inner.calls)
	}
}
```

**Step 2: Run, watch them fail**

```
go test ./internal/analyzer/ -run TestBudgetedClient -count=1
```

Expected: compile error — `newBudgetedClient` doesn't exist.

**Step 3: Implement**

Append to `budgeted_client.go`:

```go
// budgetedClient wraps an LLMClient (and optionally a ToolLLMClient) and
// refuses requests whose estimated input token count exceeds 0.9 ×
// MaxInputTokens read from the inner client's Capabilities. A zero
// MaxInputTokens disables the gate entirely.
type budgetedClient struct {
	inner LLMClient
	tool  ToolLLMClient // nil if inner is non-tool
	where string        // free-form site label baked into ErrTokenBudgetExceeded.Where
}

// newBudgetedClient wraps inner. If inner also implements ToolLLMClient, the
// returned value also implements ToolLLMClient (callers can type-assert).
func newBudgetedClient(inner LLMClient, where string) *budgetedClient {
	bc := &budgetedClient{inner: inner, where: where}
	if t, ok := inner.(ToolLLMClient); ok {
		bc.tool = t
	}
	return bc
}

func (b *budgetedClient) Capabilities() ModelCapabilities { return b.inner.Capabilities() }

func (b *budgetedClient) gate(payload int) error {
	caps := b.inner.Capabilities()
	if caps.MaxInputTokens <= 0 {
		return nil
	}
	gated := int(0.9 * float64(caps.MaxInputTokens))
	if payload > gated {
		return ErrTokenBudgetExceeded{
			Provider: caps.Provider, Model: caps.Model,
			Counted: payload, Budget: gated,
			Where: b.where,
		}
	}
	return nil
}

func (b *budgetedClient) Complete(ctx context.Context, prompt string) (string, error) {
	if err := b.gate(countPayloadTokens(prompt, nil, nil, JSONSchema{})); err != nil {
		return "", err
	}
	return b.inner.Complete(ctx, prompt)
}

func (b *budgetedClient) CompleteJSON(ctx context.Context, prompt string, schema JSONSchema) (json.RawMessage, error) {
	if err := b.gate(countPayloadTokens(prompt, nil, nil, schema)); err != nil {
		return nil, err
	}
	return b.inner.CompleteJSON(ctx, prompt, schema)
}

func (b *budgetedClient) CompleteJSONMultimodal(ctx context.Context, msgs []ChatMessage, schema JSONSchema) (json.RawMessage, error) {
	if err := b.gate(countPayloadTokens("", msgs, nil, schema)); err != nil {
		return nil, err
	}
	return b.inner.CompleteJSONMultimodal(ctx, msgs, schema)
}
```

(Imports needed: `context`, `encoding/json`.)

**Step 4: Run, confirm green**

```
go test ./internal/analyzer/ -run TestBudgetedClient -count=1
```

**Step 5: Commit**

```bash
git add internal/analyzer/budgeted_client.go internal/analyzer/budgeted_client_test.go
git commit -m "feat(analyzer): budgetedClient decorator gates single-shot calls

- RED: passthrough under budget, refuse over budget, ignore when budget=0.
- GREEN: count payload, compare against 0.9 × MaxInputTokens, return
  ErrTokenBudgetExceeded with provider/model/counts populated.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 6: Wire the decorator into tier construction

**Files:**
- Modify: `internal/cli/llm_client.go` (`buildTierClient` near line 192)
- Modify: `internal/cli/llm_client_test.go`

**Why now:** Once tiers wrap their clients with `budgetedClient`, every existing analyzer call site already gets gated for single-shot calls — no other changes required for that path.

**Step 1: Write failing test**

Add to `internal/cli/llm_client_test.go`:

```go
// Build the tier client for a real-ish (provider, model) and assert the
// wrapper passes Capabilities through. The simplest signal that wrapping
// happened: Capabilities() round-trips MaxInputTokens.
func TestBuildTierClient_WrapsWithBudget(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key-not-used")
	client, _, err := buildTierClient("anthropic", "claude-haiku-4-5")
	if err != nil {
		t.Fatal(err)
	}
	if client.Capabilities().MaxInputTokens != 180000 {
		t.Fatalf("MaxInputTokens not propagated: %d", client.Capabilities().MaxInputTokens)
	}
	// The returned client must be the budgeted wrapper, not the bare
	// BifrostClient. Sentinel check: its concrete type name contains "budgeted".
	typeName := fmt.Sprintf("%T", client)
	if !strings.Contains(typeName, "budgetedClient") {
		t.Fatalf("expected budgetedClient wrapper, got %s", typeName)
	}
}
```

**Step 2: Run, watch it fail**

```
go test ./internal/cli/ -run TestBuildTierClient_WrapsWithBudget -count=1
```

Expected: FAIL — sentinel type check.

**Step 3: Implement**

In `internal/cli/llm_client.go`, at the end of `buildTierClient` (before the `return`):

```go
client, err := analyzer.NewBifrostClientWithProvider(bifrostProvider, apiKey, model, baseURL, analyzerCaps)
if err != nil {
    return nil, nil, err
}
// Wrap every constructed client in budgetedClient so all LLM calls
// (single-shot today; multi-turn after agent_loop wiring) are gated
// against the model's MaxInputTokens before reaching the provider.
return analyzer.NewBudgetedClient(client, fmt.Sprintf("%s/%s", provider, model)), counter, nil
```

…then export the constructor from the analyzer package: rename `newBudgetedClient` to `NewBudgetedClient` (capital N) in `internal/analyzer/budgeted_client.go` and update any test references. Keep its return type as `*budgetedClient` with an exported `Capabilities()` method — but the function signature now needs to return `LLMClient` (or `*budgetedClient`) to the caller.

Adjust the analyzer signature: `func NewBudgetedClient(inner LLMClient, where string) LLMClient`. If a tool-capable client is passed, the returned interface still needs to support `CompleteWithTools`. For Task 6 we only care about the LLM single-shot interface; the tool-use wiring lands in Task 7. But to avoid losing tool-use today, return `LLMClient` for now and accept that drift investigator briefly bypasses the wrapper. (The drift investigator path is restored in Task 7's `CompleteWithTools` implementation.)

Actually safer: return a richer type. Add a method on the budgetedClient that exposes the inner tool client unwrapped:

```go
// CompleteWithTools delegates to the inner ToolLLMClient unchanged. The
// budget gate is applied per-turn inside the agent loop in Task 7, not at
// this entry point.
func (b *budgetedClient) CompleteWithTools(ctx context.Context, msgs []ChatMessage, tools []Tool, opts ...AgentOption) (AgentResult, error) {
	if b.tool == nil {
		return AgentResult{}, fmt.Errorf("CompleteWithTools: inner client does not support tool use")
	}
	return b.tool.CompleteWithTools(ctx, msgs, tools, opts...)
}
```

This makes `*budgetedClient` satisfy `ToolLLMClient` whenever its inner does, and keeps Task 6 as a pure wrap that does not change behavior for the multi-turn path.

**Step 4: Run, confirm green**

```
go test ./internal/cli/ -run TestBuildTierClient_WrapsWithBudget -count=1
go test ./... -count=1
```

The full suite must still pass — wrapping should be invisible to existing callers because the gate is a no-op for in-budget payloads.

**Step 5: Commit**

```bash
git add internal/cli/llm_client.go internal/analyzer/budgeted_client.go internal/cli/llm_client_test.go
git commit -m "feat(cli): wrap every tier client in budgetedClient

- RED: tier client carries MaxInputTokens and is the budgeted wrapper.
- GREEN: NewBudgetedClient returns a ToolLLMClient passthrough; tier
  construction wraps every client. CompleteWithTools delegates unchanged
  for now (per-turn gating lands in the next task).

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 7: Agent loop — handle `ErrTokenBudgetExceeded` + per-tool-result clip

**Files:**
- Modify: `internal/analyzer/agent_loop.go`
- Modify: `internal/analyzer/agent_loop_test.go`
- Modify: `internal/analyzer/budgeted_client.go` (override `CompleteWithTools` with per-turn gating)
- Modify: `internal/analyzer/budgeted_client_test.go`

**Step 1: Write failing tests**

In `agent_loop_test.go`:

```go
func TestRunAgentLoop_StopsOnTokenBudgetError(t *testing.T) {
	calls := 0
	next := func(ctx context.Context, msgs []ChatMessage, tools []Tool) (ChatMessage, error) {
		calls++
		if calls == 2 {
			return ChatMessage{}, ErrTokenBudgetExceeded{Where: "test", Counted: 999, Budget: 100}
		}
		return ChatMessage{Role: "assistant", ToolCalls: []ToolCall{{ID: "1", Name: "noop", Arguments: "{}"}}}, nil
	}
	noop := Tool{Name: "noop", Execute: func(_ context.Context, _ string) (string, error) { return "ok", nil }}
	res, err := runAgentLoopExportedForTest(context.Background(), next,
		[]ChatMessage{{Role: "user", Content: "go"}}, []Tool{noop}, WithMaxRounds(10))
	if !errors.Is(err, ErrTokenBudgetExceeded{}) {
		t.Fatalf("expected ErrTokenBudgetExceeded, got %v", err)
	}
	if res.Rounds != 1 {
		t.Fatalf("expected Rounds=1 (one successful turn before budget hit), got %d", res.Rounds)
	}
}

func TestRunAgentLoop_ClipsLargeToolResults(t *testing.T) {
	huge := strings.Repeat("X", 200000) // ~tens of thousands of tokens
	noop := Tool{Name: "big", Execute: func(_ context.Context, _ string) (string, error) { return huge, nil }}

	var capturedToolMsg ChatMessage
	round := 0
	next := func(ctx context.Context, msgs []ChatMessage, tools []Tool) (ChatMessage, error) {
		round++
		if round == 1 {
			return ChatMessage{Role: "assistant", ToolCalls: []ToolCall{{ID: "1", Name: "big", Arguments: "{}"}}}, nil
		}
		// Round 2: capture the latest tool message (it should be clipped).
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Role == "tool" {
				capturedToolMsg = msgs[i]
				break
			}
		}
		return ChatMessage{Role: "assistant", Content: "done"}, nil
	}
	_, err := runAgentLoopExportedForTest(context.Background(), next,
		[]ChatMessage{{Role: "user", Content: "go"}}, []Tool{noop},
		WithMaxRounds(5), WithMaxToolResultTokens(1000))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(capturedToolMsg.Content, "[truncated") {
		t.Fatalf("tool message not clipped: len=%d, first 200 chars: %q",
			len(capturedToolMsg.Content), capturedToolMsg.Content[:min(200, len(capturedToolMsg.Content))])
	}
	if len(capturedToolMsg.Content) > 20000 { // generous upper bound (~5k tokens)
		t.Fatalf("tool message not clipped: len=%d", len(capturedToolMsg.Content))
	}
}
```

Add a small `min` helper if Go version doesn't have it (Go 1.21+ does).

In `agent_loop_export_test.go`, ensure `runAgentLoopExportedForTest` exists (the file already exists per the codebase listing — extend if needed).

In `budgeted_client_test.go`:

```go
type fakeToolLLM struct {
	*fakeLLM
	turns []ChatMessage
	err   error
}

func (f *fakeToolLLM) CompleteWithTools(ctx context.Context, msgs []ChatMessage, tools []Tool, opts ...AgentOption) (AgentResult, error) {
	// Treat the budgeted wrapper as having intercepted before us.
	return AgentResult{FinalMessage: ChatMessage{Role: "assistant", Content: "done"}, Rounds: 1}, nil
}

func TestBudgetedClient_GateAppliesPerTurn(t *testing.T) {
	// A budgeted ToolLLMClient with budget=10 and a 1000-char user message
	// must return ErrTokenBudgetExceeded on the first turn.
	inner := &fakeToolLLM{fakeLLM: &fakeLLM{caps: ModelCapabilities{Provider: "p", Model: "m", MaxInputTokens: 10}}}
	bc := newBudgetedClient(inner, "tool-test")
	huge := strings.Repeat("xx ", 1000)
	_, err := bc.CompleteWithTools(context.Background(),
		[]ChatMessage{{Role: "user", Content: huge}}, nil)
	if !errors.Is(err, ErrTokenBudgetExceeded{}) {
		t.Fatalf("expected budget error, got %v", err)
	}
}
```

**Step 2: Run, watch them fail**

```
go test ./internal/analyzer/ -run "TestRunAgentLoop_StopsOnTokenBudgetError|TestRunAgentLoop_ClipsLargeToolResults|TestBudgetedClient_GateAppliesPerTurn" -count=1
```

Expected: FAIL.

**Step 3: Implement**

In `internal/analyzer/agent_loop.go`:

1. Add `WithMaxToolResultTokens` option:
   ```go
   // WithMaxToolResultTokens caps the size of any single tool result before
   // it's appended to the message history. A result larger than n tokens is
   // truncated and a "[truncated: ~K tokens omitted]" marker appended. n <= 0
   // disables clipping. Set by budgetedClient when the inner client has a
   // non-zero MaxInputTokens.
   func WithMaxToolResultTokens(n int) AgentOption {
       return func(cfg *agentConfig) { cfg.maxToolResultTokens = n }
   }
   ```
   Add the field to `agentConfig`:
   ```go
   type agentConfig struct {
       maxRounds           int
       onTurn              func()
       maxToolResultTokens int
   }
   ```

2. In `runAgentLoop`, immediately after `next` returns, also handle `ErrTokenBudgetExceeded` like `ErrMaxRounds`:
   ```go
   resp, err := next(ctx, messages, tools)
   if err != nil {
       if errors.Is(err, ErrTokenBudgetExceeded{}) {
           return AgentResult{FinalMessage: lastAssistant, Rounds: round - 1}, err
       }
       return AgentResult{}, err
   }
   ```
   Imports: `errors`. Note: `round - 1` because the budget error came from the round we just attempted; only previous rounds completed.

3. After computing the tool result string and before appending the tool message, clip if oversized:
   ```go
   if cfg.maxToolResultTokens > 0 {
       result = clipToolResult(result, cfg.maxToolResultTokens)
   }
   messages = append(messages, ChatMessage{
       Role:       "tool",
       Content:    result,
       ToolCallID: tc.ID,
   })
   ```
   Implement `clipToolResult`:
   ```go
   // clipToolResult truncates result so its tiktoken count is <= max, then
   // appends a "[truncated: ~K tokens omitted]" marker. Returns result
   // unchanged when its count is already within max.
   func clipToolResult(result string, max int) string {
       n := countTokens(result)
       if n <= max {
           return result
       }
       // Heuristic: byte length per token averages ~4 across cl100k_base
       // English text. Slice to max*4 bytes, then re-count and bump down
       // if still over.
       cut := max * 4
       if cut > len(result) {
           cut = len(result)
       }
       trimmed := result[:cut]
       for countTokens(trimmed) > max && len(trimmed) > 0 {
           trimmed = trimmed[:len(trimmed)/2*2-1]  // halve and re-test (rare path)
           if len(trimmed) <= 0 { break }
       }
       omitted := n - countTokens(trimmed)
       return trimmed + fmt.Sprintf("\n\n[truncated: ~%d tokens omitted from this tool result]", omitted)
   }
   ```

In `internal/analyzer/budgeted_client.go`, replace the placeholder `CompleteWithTools` from Task 6 with a real per-turn gated version:

```go
func (b *budgetedClient) CompleteWithTools(ctx context.Context, msgs []ChatMessage, tools []Tool, opts ...AgentOption) (AgentResult, error) {
	if b.tool == nil {
		return AgentResult{}, fmt.Errorf("CompleteWithTools: inner client does not support tool use")
	}
	caps := b.inner.Capabilities()
	if caps.MaxInputTokens <= 0 {
		return b.tool.CompleteWithTools(ctx, msgs, tools, opts...)
	}

	// Budget hooks: pre-gate every turn AND clip large tool results.
	gatedBudget := int(0.9 * float64(caps.MaxInputTokens))
	clipMax := gatedBudget / 2 // Z: per-tool-result cap = 0.5 × gated budget
	opts = append(opts, WithMaxToolResultTokens(clipMax))

	// We have to gate *inside* the inner agent loop. The simplest hook today
	// is for the inner BifrostClient to expose a way to check the payload
	// size; in lieu of that, wrap the messages slice using a synthetic check
	// before each call. Since runAgentLoop is package-private, we delegate
	// to a helper that re-runs the loop with our gated turn function.
	return runGatedToolLoop(ctx, b.tool, msgs, tools, gatedBudget,
		ErrTokenBudgetExceeded{Provider: caps.Provider, Model: caps.Model, Where: b.where},
		opts...)
}
```

That helper requires access to the BifrostClient's per-turn function, which is currently encapsulated. Two approaches:

**Approach A (preferred):** Expose a `nextTurn` callable on `BifrostClient` (e.g. `NextTurn(ctx, msgs, tools) (ChatMessage, error)`) so the budgeted loop can drive `runAgentLoop` itself.

**Approach B:** Add an `AgentOption` (`WithPreTurnHook(func([]ChatMessage, []Tool) error)`) so the inner BifrostClient calls the hook before each round and short-circuits on a non-nil return.

Choose **Approach B** — it's a smaller surface and doesn't reorganize `BifrostClient`. Implement:

In `agent_loop.go`:
```go
// WithPreTurnHook registers a function called immediately before each LLM
// turn. If the hook returns a non-nil error, runAgentLoop terminates and
// returns that error. Used by budgetedClient to gate per-turn input size.
func WithPreTurnHook(fn func([]ChatMessage, []Tool) error) AgentOption {
    return func(cfg *agentConfig) { cfg.preTurnHook = fn }
}
```

Add `preTurnHook func([]ChatMessage, []Tool) error` to `agentConfig`.

Inside `runAgentLoop`, immediately before calling `next`:
```go
if cfg.preTurnHook != nil {
    if err := cfg.preTurnHook(messages, tools); err != nil {
        return AgentResult{FinalMessage: lastAssistant, Rounds: round - 1}, err
    }
}
```

Then `budgetedClient.CompleteWithTools` becomes:
```go
hook := func(msgs []ChatMessage, tools []Tool) error {
    if n := countPayloadTokens("", msgs, tools, JSONSchema{}); n > gatedBudget {
        return ErrTokenBudgetExceeded{Provider: caps.Provider, Model: caps.Model, Counted: n, Budget: gatedBudget, Where: b.where}
    }
    return nil
}
opts = append(opts, WithPreTurnHook(hook), WithMaxToolResultTokens(clipMax))
return b.tool.CompleteWithTools(ctx, msgs, tools, opts...)
```

**Step 4: Run, confirm green**

```
go test ./internal/analyzer/ -run "TestRunAgentLoop_StopsOnTokenBudgetError|TestRunAgentLoop_ClipsLargeToolResults|TestBudgetedClient_GateAppliesPerTurn" -count=1
go test ./... -count=1
```

**Step 5: Commit**

```bash
git add internal/analyzer/agent_loop.go internal/analyzer/agent_loop_test.go internal/analyzer/budgeted_client.go internal/analyzer/budgeted_client_test.go internal/analyzer/agent_loop_export_test.go
git commit -m "feat(agent_loop): pre-turn budget hook and per-tool-result clip

- RED: budget hook stops the loop with ErrTokenBudgetExceeded; oversized
  tool results are clipped with a marker.
- GREEN: WithPreTurnHook + WithMaxToolResultTokens; budgetedClient wires
  both into CompleteWithTools when the model has a non-zero MaxInputTokens.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 8: `investigateFeatureDrift` recovery + zero-observation typed error

**Files:**
- Modify: `internal/analyzer/drift.go`
- Modify: `internal/analyzer/drift_test.go`

**Step 1: Write failing tests**

In `drift_test.go`:

```go
func TestInvestigateFeatureDrift_BudgetHitWithObservations_HandsToJudge(t *testing.T) {
	// Stub a tool client that records two observations then returns
	// ErrTokenBudgetExceeded on the next round (simulated by the loop's
	// pre-turn hook). Use the test helper that exposes runAgentLoop with
	// custom turn functions.
	// ... (mirror the existing tests' setup; record observations[] via
	// note_observation, then have the next turn return budget error)
	obs, err := investigateFeatureDriftForTest(t, ...) // existing test seam or build one
	if err != nil {
		t.Fatalf("investigator should swallow budget error when obs > 0; got %v", err)
	}
	if len(obs) == 0 {
		t.Fatalf("expected non-zero observations on partial-handoff path")
	}
}

func TestInvestigateFeatureDrift_BudgetHitWithZeroObservations_ReturnsTypedError(t *testing.T) {
	_, err := investigateFeatureDriftForTest(t, withZeroObservations(...))
	if !errors.Is(err, ErrTokenBudgetExceeded{}) {
		t.Fatalf("expected ErrTokenBudgetExceeded when zero observations, got %v", err)
	}
}
```

Match the existing test seams in `drift_test.go` for stubbing the investigator's tool client. If no clean seam exists, extract one as a tiny helper now (returns the `ToolLLMClient` used by `investigateFeatureDrift`).

**Step 2: Run, watch them fail**

```
go test ./internal/analyzer/ -run TestInvestigateFeatureDrift_BudgetHit -count=1
```

**Step 3: Implement**

In `internal/analyzer/drift.go`, locate the existing `ErrMaxRounds` recovery branch in `investigateFeatureDrift` and extend it:

```go
_, err := client.CompleteWithTools(ctx, messages, tools, WithMaxRounds(budget))
if errors.Is(err, ErrMaxRounds) || errors.Is(err, ErrTokenBudgetExceeded{}) {
    if errors.Is(err, ErrTokenBudgetExceeded{}) && len(observations) == 0 {
        // Typed return so DetectDrift can skip persisting an empty cache
        // entry. A re-run with --llm-typical=<bigger-model> retries fresh.
        return nil, fmt.Errorf("investigateFeatureDrift %q: %w",
            entry.Feature.Name, err)
    }
    log.Warnf("drift investigator hit budget for feature %q (%d files, %d pages); handing %d observations to judge",
        entry.Feature.Name, len(entry.Files), len(pages), len(observations))
    return observations, nil
}
```

**Step 4: Run, confirm green**

```
go test ./internal/analyzer/ -run TestInvestigateFeatureDrift_BudgetHit -count=1
```

**Step 5: Commit**

```bash
git add internal/analyzer/drift.go internal/analyzer/drift_test.go
git commit -m "feat(drift): recover from token budget like ErrMaxRounds

- RED: with non-zero observations, swallow the error and hand to judge;
  with zero observations, return a typed error to the caller.
- GREEN: extend the existing ErrMaxRounds branch in
  investigateFeatureDrift with the zero-observation guard.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 9: `DetectDrift` skips cache writes for un-investigated features

**Files:**
- Modify: `internal/analyzer/drift.go`
- Modify: `internal/analyzer/drift_test.go`

**Step 1: Write failing test**

```go
func TestDetectDrift_SkipsCacheWriteOnZeroObservationBudgetError(t *testing.T) {
	// Construct a fake investigator that returns ErrTokenBudgetExceeded
	// with zero observations for a single feature. Capture onFeatureDone
	// invocations.
	var doneCalls int
	onDone := func(_ string, _, _, _ []string, _ []DriftIssue) error {
		doneCalls++
		return nil
	}
	_, err := DetectDrift(... featureMap with one feature ..., onDone)
	if err != nil {
		// We expect the run NOT to abort on this error: the feature is
		// logged + skipped, other features continue.
		// (If we DO want it to abort, flip this assertion.)
		t.Fatalf("DetectDrift should not abort on a single-feature budget error: %v", err)
	}
	if doneCalls != 0 {
		t.Fatalf("onFeatureDone called %d times for un-investigated feature; want 0", doneCalls)
	}
}
```

Decision encoded: a single feature's budget error does NOT abort the whole run. Other features still get a chance.

**Step 2: Run, watch fail**

**Step 3: Implement**

Inside `DetectDrift`, the worker function calls `investigateFeatureDrift` and then `judgeFeatureDrift`. Wrap the investigator call with a typed-error skip:

```go
observations, err := investigateFeatureDrift(ctx, investigator, entry, pages, pageReader, repoRoot)
if err != nil {
    if errors.Is(err, ErrTokenBudgetExceeded{}) {
        log.Warnf("drift investigator could not start for feature %q: %v; skipping (no cache entry written)", entry.Feature.Name, err)
        return nil
    }
    return fmt.Errorf("DetectDrift %q: %w", entry.Feature.Name, err)
}
```

`return nil` from the worker means "this feature contributed nothing this run" — no findings, no cache entry, the run continues.

**Step 4: Run, confirm green**

**Step 5: Commit**

```bash
git add internal/analyzer/drift.go internal/analyzer/drift_test.go
git commit -m "feat(drift): skip cache write when investigator can't start

- RED: a feature whose first turn already overruns the budget must not
  produce an onFeatureDone call (no false 'no drift' cache entry).
- GREEN: detect ErrTokenBudgetExceeded from investigateFeatureDrift,
  log + skip the feature, run continues.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 10: Drift judge — chunked-judging compaction

**Files:**
- Modify: `internal/analyzer/drift.go`
- Modify: `internal/analyzer/drift_test.go`

**Step 1: Write failing tests**

```go
func TestJudgeFeatureDrift_ChunksWhenOverBudget(t *testing.T) {
	// Build N=20 observations, each with quotes long enough that the
	// concatenated prompt exceeds a stub LLMClient's MaxInputTokens.
	// First call returns ErrTokenBudgetExceeded; chunked retries succeed.
	stub := newChunkAwareStub(t, /* budget */ 5000) // refuses any prompt > 5000 tokens
	feat := CodeFeature{Name: "F", Description: "x"}
	obs := makeBigObservations(20, 800)            // ~3200 chars each ≈ 800 tokens

	issues, err := judgeFeatureDrift(context.Background(), stub, feat, obs)
	if err != nil { t.Fatal(err) }
	if len(issues) == 0 { t.Fatalf("expected issues from chunked judging") }
	if stub.calls < 2 { t.Fatalf("expected multiple chunked calls, got %d", stub.calls) }
}

func TestJudgeFeatureDrift_ClipsOversizedSingleObservation(t *testing.T) {
	// One observation whose quotes alone exceed the per-chunk budget.
	// The judge must still produce an issue, and the prompt sent to the
	// stub must contain the "[…]" marker on the clipped quotes.
	// ...
}
```

Build `chunkAwareStub` as a test-local fake that returns `ErrTokenBudgetExceeded` if `countPayloadTokens(prompt, ...)` exceeds its budget, otherwise returns a canned response.

**Step 2: Run, watch them fail**

**Step 3: Implement**

In `internal/analyzer/drift.go`, refactor `judgeFeatureDrift`:

```go
func judgeFeatureDrift(ctx context.Context, client LLMClient, feature CodeFeature, observations []driftObservation) ([]DriftIssue, error) {
    if len(observations) == 0 { return nil, nil }

    issues, err := judgeOneShot(ctx, client, feature, observations)
    if !errors.Is(err, ErrTokenBudgetExceeded{}) {
        return issues, err
    }
    // Compaction path: chunk observations to fit, judge each, concatenate.
    chunks := chunkObservationsToFit(client, feature, observations)
    return judgeChunked(ctx, client, feature, chunks)
}
```

`judgeOneShot` is the existing body of `judgeFeatureDrift` (the for-loop with retries) extracted as-is. `chunkObservationsToFit` greedily packs observations such that the rendered prompt for each chunk stays under `0.9 × MaxInputTokens`:

```go
// chunkObservationsToFit splits observations into the smallest number of
// groups whose rendered judge prompts each fit within the model's budget.
// If a single observation's quotes alone exceed the per-chunk budget, both
// quotes are clipped to clipQuoteMaxChars characters with a "[…]" marker.
func chunkObservationsToFit(client LLMClient, feature CodeFeature, obs []driftObservation) [][]driftObservation {
    caps := client.Capabilities()
    if caps.MaxInputTokens <= 0 {
        return [][]driftObservation{obs}
    }
    budget := int(0.9 * float64(caps.MaxInputTokens))

    // Pre-clip quotes that are individually larger than half the budget.
    clipped := make([]driftObservation, len(obs))
    for i, o := range obs {
        clipped[i] = clipObservationQuotes(o, clipQuoteMaxChars)
    }

    // Greedy pack: start a new chunk whenever rendering the running prompt
    // for the next observation would push past the budget.
    var chunks [][]driftObservation
    var cur []driftObservation
    for _, o := range clipped {
        candidate := append(cur, o)
        if countTokens(renderJudgePrompt(feature, candidate)) > budget && len(cur) > 0 {
            chunks = append(chunks, cur)
            cur = []driftObservation{o}
            continue
        }
        cur = candidate
    }
    if len(cur) > 0 { chunks = append(chunks, cur) }
    return chunks
}

const clipQuoteMaxChars = 1500

func clipObservationQuotes(o driftObservation, max int) driftObservation {
    if len(o.DocQuote) > max { o.DocQuote = o.DocQuote[:max] + " […]" }
    if len(o.CodeQuote) > max { o.CodeQuote = o.CodeQuote[:max] + " […]" }
    return o
}

func renderJudgePrompt(feature CodeFeature, obs []driftObservation) string {
    // Same body as the existing prompt construction inside judgeOneShot;
    // extracted so chunkObservationsToFit can re-use it.
    ...
}
```

`judgeChunked` runs `judgeOneShot` once per chunk and concatenates issues. No cross-chunk dedupe in v1 (acceptable per design).

**Step 4: Run, confirm green**

**Step 5: Commit**

```bash
git add internal/analyzer/drift.go internal/analyzer/drift_test.go
git commit -m "feat(drift): chunked-judging compaction on token-budget errors

- RED: large observation lists previously failed; quote-bombs in a single
  observation must be clipped with a marker.
- GREEN: chunkObservationsToFit greedily packs observations; oversized
  quotes pre-clipped to 1500 chars with '[…]'; judgeChunked merges the
  per-chunk issue lists.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 11: Page analyzer — log + skip on budget error

**Files:**
- Modify: `internal/analyzer/analyze_page.go`
- Modify: `internal/analyzer/analyze_page_test.go`

**Step 1: Write failing test**

```go
func TestAnalyzePage_SkipsOnTokenBudgetError(t *testing.T) {
	stub := &fakeLLM{
		caps: ModelCapabilities{Provider: "p", Model: "m", MaxInputTokens: 100},
		completeJSON: func(_ context.Context, _ string, _ JSONSchema) (json.RawMessage, error) {
			return nil, ErrTokenBudgetExceeded{Provider: "p", Model: "m", Counted: 999, Budget: 90, Where: "test"}
		},
	}
	tiering := newSingleTier(stub)
	res, err := AnalyzePage(context.Background(), tiering, "https://x", strings.Repeat("y", 10000))
	if err != nil {
		t.Fatalf("expected nil error (skip), got %v", err)
	}
	if res.URL != "" { t.Fatalf("expected empty result on skip, got %+v", res) }
}
```

`newSingleTier` is a tiny test helper (build it next to the test) that returns an `LLMTiering` whose all three tiers are the same stub.

**Step 2: Run, watch fail**

**Step 3: Implement**

In `internal/analyzer/analyze_page.go`, around the `client.CompleteJSON` error branch (line ~70):

```go
raw, err := client.CompleteJSON(ctx, prompt, analyzePageSchema)
if err != nil {
    if errors.Is(err, ErrTokenBudgetExceeded{}) {
        log.Warnf("AnalyzePage: skipping %s (%v)", pageURL, err)
        return PageAnalysis{}, nil
    }
    return PageAnalysis{}, fmt.Errorf("AnalyzePage %s: %w", pageURL, err)
}
```

(Add the `log` import if absent: `"github.com/charmbracelet/log"`.)

**Step 4: Run, confirm green**

**Step 5: Commit**

```bash
git add internal/analyzer/analyze_page.go internal/analyzer/analyze_page_test.go
git commit -m "feat(analyze_page): skip oversize pages with a logged warning

- RED: ErrTokenBudgetExceeded must yield a zero PageAnalysis with nil
  error so the run continues.
- GREEN: detect the typed error, log, and return.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 12: Screenshot pass — log + skip on budget error

**Files:**
- Modify: `internal/analyzer/screenshot_gaps.go`
- Modify: `internal/analyzer/screenshot_gaps_test.go`

Mirror Task 11 at the two call sites in `screenshot_gaps.go` (around lines 1017 `CompleteJSONMultimodal` and 1123 `CompleteJSON`).

For each: catch `ErrTokenBudgetExceeded`, log via `log.Warnf("screenshot pass: skipping %s (%v)", url, err)`, return `nil` for that page's contribution.

Add tests that mirror Task 11's pattern.

Commit: `feat(screenshot_gaps): skip oversize pages on budget error`.

---

## Task 13: Mapper — fatal-for-batch on budget error

**Files:**
- Modify: `internal/analyzer/mapper.go` (around line 187, the `client.CompleteJSON` error branch)
- Modify: `internal/analyzer/mapper_test.go`

**Step 1: Write failing test**

```go
func TestMapFeaturesToCode_BudgetErrorSkipsBatchAndContinues(t *testing.T) {
	// Two batches: first succeeds, second hits the budget. The result must
	// include features mapped from the first batch, not abort the run.
	// ... build a stub that returns ok then ErrTokenBudgetExceeded ...
}
```

**Step 2: Run, watch fail**

**Step 3: Implement**

```go
raw, err := client.CompleteJSON(ctx, promptText, mapSchema)
if err != nil {
    if errors.Is(err, ErrTokenBudgetExceeded{}) {
        log.Warnf("MapFeaturesToCode: skipping batch %d/%d (%v) — batcher should have prevented this; investigate", i+1, len(queue), err)
        continue
    }
    return nil, fmt.Errorf("MapFeaturesToCode: %w", err)
}
```

Note: the batcher's pre-send token check (line 164) uses `counter.CountTokens` which is provider-exact for Anthropic. The decorator's tiktoken estimate is what fires inside the BifrostClient call. If they disagree, the decorator wins; this code path is a safety net.

**Step 4: Run, confirm green**

**Step 5: Commit**

```bash
git add internal/analyzer/mapper.go internal/analyzer/mapper_test.go
git commit -m "feat(mapper): skip batch on budget error, continue with the rest

- RED: a budget-busting batch must not abort the whole mapping run.
- GREEN: log + continue; the message names the batcher as the upstream
  to investigate.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Task 14: End-to-end verification on the failing fixture

**Files:** none (verification only)

**Step 1: Reproduce the original failure on `main`**

```bash
git stash --include-untracked
git checkout main
go build -o /tmp/ftg-main ./cmd/ftg
# Re-run the exact command that crashed the user
/tmp/ftg-main analyze --repo <fixture> --docs-url <docs-url>
# Confirm the "Input tokens exceed the configured limit of 272000" error.
git checkout -
git stash pop || true
```

If the user does not have the fixture handy, skip this step but flag in the PR description that a manual repro on the original failing case is pending.

**Step 2: Build the fixed binary**

```bash
go build -o /tmp/ftg-fix ./cmd/ftg
```

**Step 3: Run the fixed binary on the same input**

```bash
/tmp/ftg-fix analyze --repo <fixture> --docs-url <docs-url> -v
```

Expected:
- Run exits 0 (or with the existing non-budget exit code).
- Stderr contains a `drift investigator hit budget for feature "Standard library grammar configuration"` log line OR the run completes without hitting the budget at all (cache hits, smaller prompts).
- `gaps.md` exists and contains entries.
- No `Input tokens exceed the configured limit` error anywhere in the output.

**Step 4: Run the broader test suite + lint**

```bash
go test ./... -count=1
golangci-lint run
```

Both must pass clean.

**Step 5: Update PROGRESS.md and commit**

Add a final section to `PROGRESS.md`:

```markdown
## Task: Per-Model Token Budget — COMPLETE
- Started: 2026-05-07
- Tests: all passing
- Build: ✅ Successful
- Linting: ✅ Clean
- Verification: rerun of original failing fixture no longer throws "Input
  tokens exceed the configured limit"; budget-hit warnings logged for
  features that previously crashed.
- Completed: 2026-05-07
- Notes: see .plans/2026-05-07-token-budget-design.md and the
  per-task commits on fix/drift-token-budget for the full RED/GREEN trail.
```

```bash
git add PROGRESS.md
git commit -m "docs(progress): record token-budget completion"
```

**Step 6: Open the PR**

```bash
gh pr create --base main --title "fix(drift): per-model token budget prevents 272k overrun" --body "$(cat <<'EOF'
## Summary
- Adds per-model `MaxInputTokens` to the capability table (Anthropic 180k,
  OpenAI 5x family 260k, GPT-4o 115k, Groq llama-4-scout 120k; conservative
  100k default for unknown models on a known provider).
- Introduces `budgetedClient` decorator that wraps every BifrostClient at
  tier construction; gates sends at 0.9 × `MaxInputTokens`.
- Multi-turn drift investigator now stops cleanly on budget error and
  hands partial observations to the judge — same shape as `ErrMaxRounds`.
- Drift judge gains a chunked-judging compaction path: when its prompt
  exceeds budget, observations are split into chunks that fit, judged
  separately, and merged. Lossless at the observation level.
- Page analyzer / screenshot pass / mapper batch log + skip on budget
  error rather than aborting the run.
- Per-tool-result hard cap (0.5 × gated budget) prevents one giant file
  read from single-handedly busting the next turn.

Design: `.plans/2026-05-07-token-budget-design.md`.
Plan:   `.plans/2026-05-07-token-budget-plan.md`.

## Test plan
- [ ] `go test ./... -count=1` passes
- [ ] `golangci-lint run` clean
- [ ] Original failing fixture no longer errors with "Input tokens exceed
      the configured limit"; instead emits a budget-hit warning and
      continues.
- [ ] At least one drift finding still produced for the previously
      crashing feature, OR a clear log line indicating partial-observation
      handoff.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Done

After Task 14, the branch is ready to merge. CLAUDE.md says PRs use a merge commit (no squash); coordinate with the user before merging.

**Checklist for marking the work complete:**
- [ ] All 14 tasks committed.
- [ ] `go test ./... -count=1` green.
- [ ] `golangci-lint run` clean.
- [ ] PROGRESS.md updated.
- [ ] PR open against `main`.
- [ ] Manual reproduction of the original failing fixture confirms the fix.
