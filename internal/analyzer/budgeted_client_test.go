package analyzer

import (
	"errors"
	"strings"
	"testing"
)

// TestErrTokenBudgetExceeded_ImplementsError pins the error message format
// and the errors.Is contract. Callers detect the error via errors.Is against
// a zero-value sentinel; the message is rendered to the user when a single-
// shot caller refuses with a hint.
func TestErrTokenBudgetExceeded_ImplementsError(t *testing.T) {
	err := ErrTokenBudgetExceeded{
		Provider: "openai",
		Model:    "gpt-5.5",
		Counted:  294098,
		Budget:   234000,
		Where:    "drift-investigator",
	}
	msg := err.Error()
	for _, want := range []string{"openai", "gpt-5.5", "294098", "234000", "drift-investigator"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("Error() = %q, missing %q", msg, want)
		}
	}
	if !errors.Is(err, ErrTokenBudgetExceeded{}) {
		t.Fatalf("errors.Is should match against zero-value sentinel")
	}
}

// TestErrTokenBudgetExceeded_IsMatchesAcrossFieldDifferences pins that two
// instances with different field values still match via errors.Is, so
// callers can detect the budget condition without knowing exact counts.
func TestErrTokenBudgetExceeded_IsMatchesAcrossFieldDifferences(t *testing.T) {
	a := ErrTokenBudgetExceeded{Provider: "anthropic", Counted: 100}
	b := ErrTokenBudgetExceeded{Provider: "openai", Counted: 200}
	if !errors.Is(a, b) {
		t.Fatalf("errors.Is should match irrespective of field values")
	}
}

// TestCountPayloadTokens_ReturnsNonZeroForFlatPrompt pins the simplest case:
// a non-empty prompt produces a non-zero count.
func TestCountPayloadTokens_ReturnsNonZeroForFlatPrompt(t *testing.T) {
	if n := countPayloadTokens("hello world", nil, nil, JSONSchema{}); n <= 0 {
		t.Fatalf("expected >0 tokens, got %d", n)
	}
}

// TestCountPayloadTokens_SumsAllParts pins the additive contract: messages,
// tools, and the schema body each push the count up. Without this, the
// budgeted client would only gate against the prompt and miss large tool
// definitions or schemas that meaningfully consume the budget.
func TestCountPayloadTokens_SumsAllParts(t *testing.T) {
	prompt := "hello"
	msgs := []ChatMessage{{Role: "user", Content: "world"}}
	tools := []Tool{{Name: "read", Description: "reads", Parameters: map[string]any{"type": "object"}}}
	schema := JSONSchema{Name: "x", Doc: []byte(`{"type":"object","properties":{"y":{"type":"string"}}}`)}

	all := countPayloadTokens(prompt, msgs, tools, schema)
	just := countPayloadTokens(prompt, nil, nil, JSONSchema{})

	if all <= just {
		t.Fatalf("expected message+tool+schema tokens to add up, got all=%d just=%d", all, just)
	}
}

// TestCountPayloadTokens_CountsContentBlockText pins that multimodal
// messages (built around ContentBlocks) still contribute their text to the
// budget. Without this, the screenshot pass's image-bearing prompts would
// appear free even though their text describes the analysis task.
func TestCountPayloadTokens_CountsContentBlockText(t *testing.T) {
	withBlocks := []ChatMessage{{
		Role:          "user",
		ContentBlocks: []ContentBlock{{Type: ContentBlockText, Text: "lots of explanatory text here"}},
	}}
	withoutBlocks := []ChatMessage{{Role: "user", ContentBlocks: nil}}

	if countPayloadTokens("", withBlocks, nil, JSONSchema{}) <= countPayloadTokens("", withoutBlocks, nil, JSONSchema{}) {
		t.Fatalf("ContentBlock text should contribute to the count")
	}
}

// TestCountPayloadTokens_CountsToolCallArguments pins that an assistant
// message carrying ToolCall arguments contributes its argument JSON. The
// drift investigator's history is dominated by these once tool calls land.
func TestCountPayloadTokens_CountsToolCallArguments(t *testing.T) {
	withCall := []ChatMessage{{
		Role: "assistant",
		ToolCalls: []ToolCall{{
			ID: "1", Name: "read_file",
			Arguments: `{"path":"some/very/long/path/that/contributes/to/the/count"}`,
		}},
	}}
	withoutCall := []ChatMessage{{Role: "assistant"}}

	if countPayloadTokens("", withCall, nil, JSONSchema{}) <= countPayloadTokens("", withoutCall, nil, JSONSchema{}) {
		t.Fatalf("ToolCall arguments should contribute to the count")
	}
}
