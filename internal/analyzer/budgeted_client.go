package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
)

// countPayloadTokens estimates the total input token count for one outbound
// LLM request: prompt + every chat message's text + tool definitions + the
// JSON schema body when present.
//
// Uses the local cl100k_base tiktoken encoder (countTokens). Counts run
// approximate for non-OpenAI tokenizers (~5–15% off for Anthropic, Llama,
// Qwen). The budgeted client absorbs that drift via the 0.9 × MaxInputTokens
// gate plus the ~10% headroom baked into each model's MaxInputTokens.
//
// Image content blocks are NOT counted here — providers tokenize images
// out-of-band and the byte-level URL is uninformative for budget sizing.
func countPayloadTokens(prompt string, messages []ChatMessage, tools []Tool, schema JSONSchema) int {
	n := 0
	if prompt != "" {
		n += countTokens(prompt)
	}
	for _, m := range messages {
		if m.Content != "" {
			n += countTokens(m.Content)
		}
		for _, b := range m.ContentBlocks {
			if b.Type == ContentBlockText && b.Text != "" {
				n += countTokens(b.Text)
			}
		}
		for _, tc := range m.ToolCalls {
			n += countTokens(tc.Name)
			n += countTokens(tc.Arguments)
		}
	}
	for _, t := range tools {
		n += countTokens(t.Name)
		n += countTokens(t.Description)
		if t.Parameters != nil {
			if buf, err := json.Marshal(t.Parameters); err == nil {
				n += countTokens(string(buf))
			}
		}
	}
	if schema.Name != "" {
		n += countTokens(schema.Name)
	}
	if len(schema.Doc) > 0 {
		n += countTokens(string(schema.Doc))
	}
	return n
}

// budgetedClient wraps an LLMClient (and, when the inner also satisfies
// ToolLLMClient, exposes the multi-turn shape too) and refuses sends whose
// estimated input token count exceeds 0.9 × the inner client's
// Capabilities().MaxInputTokens. A zero MaxInputTokens disables the gate
// entirely (used for ollama/lmstudio "*" rows).
//
// The decorator never edits the payload. Compaction (e.g. judge chunking)
// lives in the caller, which knows the prompt's semantic structure.
//
// Constructed once per tier in cli/llm_client.go via NewBudgetedClient.
type budgetedClient struct {
	inner LLMClient
	tool  ToolLLMClient // non-nil iff inner satisfies ToolLLMClient
	where string        // free-form site label baked into ErrTokenBudgetExceeded.Where
}

// newBudgetedClient wraps inner. When inner also implements ToolLLMClient,
// the returned value satisfies ToolLLMClient too.
func newBudgetedClient(inner LLMClient, where string) *budgetedClient {
	bc := &budgetedClient{inner: inner, where: where}
	if t, ok := inner.(ToolLLMClient); ok {
		bc.tool = t
	}
	return bc
}

// NewBudgetedClient is the exported constructor used by tier wiring.
// Returns LLMClient; callers that need ToolLLMClient type-assert.
func NewBudgetedClient(inner LLMClient, where string) LLMClient {
	return newBudgetedClient(inner, where)
}

// Capabilities returns the inner client's capabilities verbatim. The
// decorator does not alter what the rest of the analyzer sees about the
// model — it only adds a pre-send gate.
func (b *budgetedClient) Capabilities() ModelCapabilities { return b.inner.Capabilities() }

// gate returns ErrTokenBudgetExceeded when payload exceeds the inner
// client's gated budget (0.9 × MaxInputTokens). MaxInputTokens <= 0
// disables the gate.
func (b *budgetedClient) gate(payload int) error {
	caps := b.inner.Capabilities()
	if caps.MaxInputTokens <= 0 {
		return nil
	}
	gated := int(0.9 * float64(caps.MaxInputTokens))
	if payload > gated {
		return ErrTokenBudgetExceeded{
			Provider: caps.Provider,
			Model:    caps.Model,
			Counted:  payload,
			Budget:   gated,
			Where:    b.where,
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

// CompleteWithTools delegates to the inner ToolLLMClient. The per-turn
// budget hook and per-tool-result clip land in the agent-loop integration
// task; this method exists today so *budgetedClient satisfies
// ToolLLMClient and tier wiring can land before the loop hook is in.
func (b *budgetedClient) CompleteWithTools(ctx context.Context, msgs []ChatMessage, tools []Tool, opts ...AgentOption) (AgentResult, error) {
	if b.tool == nil {
		return AgentResult{}, fmt.Errorf("CompleteWithTools: inner client does not support tool use")
	}
	return b.tool.CompleteWithTools(ctx, msgs, tools, opts...)
}

// ErrTokenBudgetExceeded is returned by budgetedClient when a request's
// estimated input token count exceeds the gated per-model budget. Callers
// handle this error per call site:
//
//   - drift investigator: stop the loop, hand partial observations to the
//     judge (mirrors ErrMaxRounds semantics);
//   - drift judge:       fall back to a chunked-judging compaction path;
//   - page analyzer / screenshot pass / mapper batch: log + skip that unit,
//     run continues.
//
// Detect via errors.Is against a zero-value sentinel:
//
//	if errors.Is(err, ErrTokenBudgetExceeded{}) { ... }
//
// See .plans/2026-05-07-token-budget-design.md for the full per-call-site
// failure-mode matrix.
type ErrTokenBudgetExceeded struct {
	// Provider and Model identify the LLM that refused the payload. Used in
	// the user-facing message so a hint like "rerun with --llm-large=…" can
	// be rendered.
	Provider, Model string
	// Counted is the estimator's payload size in tokens.
	Counted int
	// Budget is the post-margin cap (already 0.9 × MaxInputTokens).
	Budget int
	// Where is a free-form site label ("drift-investigator", "judge",
	// "page-analyzer", …). Travels into log lines and the error message so
	// users can tell which unit of work refused.
	Where string
}

// Error renders a single-line user-facing message naming the provider/model,
// the offending count, the budget, and the call site.
func (e ErrTokenBudgetExceeded) Error() string {
	return fmt.Sprintf("token budget exceeded for %s/%s in %s: %d tokens > budget %d",
		e.Provider, e.Model, e.Where, e.Counted, e.Budget)
}

// Is allows errors.Is to match any ErrTokenBudgetExceeded against a
// zero-value sentinel without comparing field values, so callers can ask
// "is this the budget error?" without reproducing the exact counts.
func (e ErrTokenBudgetExceeded) Is(target error) bool {
	_, ok := target.(ErrTokenBudgetExceeded)
	return ok
}
