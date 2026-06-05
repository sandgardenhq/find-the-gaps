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

// TestBuildTierClient_Gemini_MissingKeyErrors pins that the Gemini tier fails
// cleanly with a named env-var error when GEMINI_API_KEY is unset, before any
// network attempt.
func TestBuildTierClient_Gemini_MissingKeyErrors(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	_, _, err := buildTierClient("gemini", "gemini-3.5-flash")
	if err == nil {
		t.Fatal("expected error when GEMINI_API_KEY is unset")
	}
	if !strings.Contains(err.Error(), "GEMINI_API_KEY not set") {
		t.Fatalf("error should name GEMINI_API_KEY, got %v", err)
	}
}

// TestBuildTierClient_Gemini_BuildsWithKey pins that a Gemini tier constructs a
// budget-gated client carrying the registry's MaxInputTokens (900k) when the
// key is present.
func TestBuildTierClient_Gemini_BuildsWithKey(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key-not-actually-used-for-build")
	client, counter, err := buildTierClient("gemini", "gemini-3.5-flash")
	if err != nil {
		t.Fatalf("buildTierClient: %v", err)
	}
	if counter == nil {
		t.Fatal("expected non-nil token counter")
	}
	if got := client.Capabilities().MaxInputTokens; got != 900000 {
		t.Fatalf("MaxInputTokens not propagated to client: %d", got)
	}
}

// TestIsMissingDefaultKeyErr_RecognizesGemini pins that the Gemini missing-key
// error routes to the friendly first-run setup hint instead of a raw error,
// matching the Anthropic/OpenAI behavior.
func TestIsMissingDefaultKeyErr_RecognizesGemini(t *testing.T) {
	assert.True(t, isMissingDefaultKeyErr(errors.New("GEMINI_API_KEY not set")))
}

// TestLLMKeySetupHint_NamesGemini pins that the setup hint lists Gemini as a
// default-eligible provider so a first-run user with only a Gemini key knows it
// works with no flags.
func TestLLMKeySetupHint_NamesGemini(t *testing.T) {
	assert.Contains(t, llmKeySetupHint, "GEMINI_API_KEY")
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
