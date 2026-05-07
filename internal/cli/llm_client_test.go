package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/stretchr/testify/assert"
)

// TestToAnalyzerCaps_PropagatesMaxInputTokens pins that the cli-to-analyzer
// capability conversion carries MaxInputTokens through. Without this, the
// budgeted client decorator constructed inside the analyzer package would
// read 0 (no gate) for every model and the budget gate would never fire.
func TestToAnalyzerCaps_PropagatesMaxInputTokens(t *testing.T) {
	in := ModelCapabilities{
		Provider:            "openai",
		Model:               "gpt-5.5",
		ToolUse:             true,
		Vision:              true,
		MaxCompletionTokens: 32000,
		MaxInputTokens:      260000,
	}
	out := toAnalyzerCaps(in)
	assert.Equal(t, "openai", out.Provider)
	assert.Equal(t, "gpt-5.5", out.Model)
	assert.True(t, out.ToolUse)
	assert.True(t, out.Vision)
	assert.Equal(t, 32000, out.MaxCompletionTokens)
	assert.Equal(t, 260000, out.MaxInputTokens)
}

// TestBuildTierClient_WrapsWithBudget pins that buildTierClient returns a
// budget-gated wrapper. Without the wrap, a huge prompt would proceed to
// the network (a real Anthropic call); with the wrap in place, the gate
// fires before any network attempt and we get ErrTokenBudgetExceeded.
//
// We use a tier whose model has a small-ish budget (haiku at 180k) and a
// prompt with enough tokens to exceed the gated 162k bar. The prompt
// content does not have to be exact — it just needs to be definitively
// over the gate.
func TestBuildTierClient_WrapsWithBudget(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key-not-actually-used-for-build")
	client, _, err := buildTierClient("anthropic", "claude-haiku-4-5")
	if err != nil {
		t.Fatalf("buildTierClient: %v", err)
	}
	// Sanity-check: capability propagation is in place from Task 2.
	if got := client.Capabilities().MaxInputTokens; got != 180000 {
		t.Fatalf("MaxInputTokens not on returned client: %d", got)
	}

	// Build a prompt larger than 0.9 × 180000 = 162000 tokens. cl100k_base
	// counts well-known English at ~9 tokens per "the quick brown fox jumps
	// over the lazy dog ". 25000 reps ≈ 225k tokens — comfortably over.
	huge := strings.Repeat("the quick brown fox jumps over the lazy dog ", 25000)

	_, err = client.CompleteJSON(context.Background(), huge, analyzer.JSONSchema{Name: "x", Doc: []byte(`{"type":"object","additionalProperties":false}`)})
	if !errors.Is(err, analyzer.ErrTokenBudgetExceeded{}) {
		t.Fatalf("expected ErrTokenBudgetExceeded from gate, got %v", err)
	}
}
