package analyzer

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// fakeInnerLLM is a minimal in-package LLMClient used by budgetedClient
// tests. (The cross-package fakeClient in analyzer_test cannot be reached
// from package analyzer.) Captures call counts so tests can assert the
// gate either passed through (calls == 1) or refused (calls == 0).
type fakeInnerLLM struct {
	caps  ModelCapabilities
	calls int
}

func (f *fakeInnerLLM) Complete(_ context.Context, _ string) (string, error) {
	f.calls++
	return "ok", nil
}
func (f *fakeInnerLLM) CompleteJSON(_ context.Context, _ string, _ JSONSchema) (json.RawMessage, error) {
	f.calls++
	return json.RawMessage(`{}`), nil
}
func (f *fakeInnerLLM) CompleteJSONMultimodal(_ context.Context, _ []ChatMessage, _ JSONSchema) (json.RawMessage, error) {
	f.calls++
	return json.RawMessage(`{}`), nil
}
func (f *fakeInnerLLM) Capabilities() ModelCapabilities { return f.caps }

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

// TestBudgetedClient_PassthroughWhenUnderBudget pins that requests well
// under the gate hit the inner client unchanged.
func TestBudgetedClient_PassthroughWhenUnderBudget(t *testing.T) {
	inner := &fakeInnerLLM{caps: ModelCapabilities{Provider: "p", Model: "m", MaxInputTokens: 100000}}
	bc := newBudgetedClient(inner, "test")
	if _, err := bc.CompleteJSON(context.Background(), "tiny", JSONSchema{}); err != nil {
		t.Fatal(err)
	}
	if inner.calls != 1 {
		t.Fatalf("inner not called: calls=%d", inner.calls)
	}
}

// TestBudgetedClient_RefusesWhenOverBudget pins the core gate behavior:
// a payload bigger than 0.9 × MaxInputTokens returns ErrTokenBudgetExceeded
// without reaching the inner client. The original 294k-token incident is a
// concrete instance of this path.
func TestBudgetedClient_RefusesWhenOverBudget(t *testing.T) {
	inner := &fakeInnerLLM{caps: ModelCapabilities{Provider: "p", Model: "m", MaxInputTokens: 10}}
	bc := newBudgetedClient(inner, "test-site")
	huge := strings.Repeat("token-ish-text ", 200)

	_, err := bc.CompleteJSON(context.Background(), huge, JSONSchema{})

	var bErr ErrTokenBudgetExceeded
	if !errors.As(err, &bErr) {
		t.Fatalf("want ErrTokenBudgetExceeded, got %v", err)
	}
	if bErr.Provider != "p" || bErr.Model != "m" || bErr.Where != "test-site" {
		t.Fatalf("error fields wrong: %+v", bErr)
	}
	if bErr.Counted <= bErr.Budget {
		t.Fatalf("Counted (%d) should exceed Budget (%d)", bErr.Counted, bErr.Budget)
	}
	if inner.calls != 0 {
		t.Fatalf("inner should NOT be called when over budget, calls=%d", inner.calls)
	}
}

// TestBudgetedClient_NoBudgetMeansNoGate pins the self-hosted contract:
// MaxInputTokens=0 disables the gate so ollama/lmstudio users with
// large-context models aren't artificially throttled.
func TestBudgetedClient_NoBudgetMeansNoGate(t *testing.T) {
	inner := &fakeInnerLLM{caps: ModelCapabilities{Provider: "ollama", Model: "*", MaxInputTokens: 0}}
	bc := newBudgetedClient(inner, "test")
	huge := strings.Repeat("x ", 100000)
	if _, err := bc.CompleteJSON(context.Background(), huge, JSONSchema{}); err != nil {
		t.Fatal(err)
	}
	if inner.calls != 1 {
		t.Fatalf("inner not called when budget is 0: calls=%d", inner.calls)
	}
}

// fakeInnerToolLLM extends fakeInnerLLM with a CompleteWithTools that
// drives an in-process agent loop using a caller-supplied turn function.
// Used to exercise budgetedClient.CompleteWithTools end-to-end without a
// real BifrostClient. The agent loop runs inside the fake so opts (incl.
// WithPreTurnHook and WithMaxToolResultTokens) reach a real runAgentLoop.
type fakeInnerToolLLM struct {
	*fakeInnerLLM
	turn turnFunc
}

func (f *fakeInnerToolLLM) CompleteWithTools(ctx context.Context, msgs []ChatMessage, tools []Tool, opts ...AgentOption) (AgentResult, error) {
	return runAgentLoop(ctx, f.turn, msgs, tools, opts...)
}

// TestBudgetedClient_CompleteWithToolsGatesEachTurn pins the multi-turn
// fix for the original 294k-token incident. After two successful tool
// roundtrips the accumulated history would push past the budget; the
// pre-turn hook detects this on round 3 and the loop terminates cleanly
// with ErrTokenBudgetExceeded — partial state captured by tool handlers
// is preserved (the existing ErrMaxRounds shape).
func TestBudgetedClient_CompleteWithToolsGatesEachTurn(t *testing.T) {
	round := 0
	turn := func(_ context.Context, _ []ChatMessage, _ []Tool) (ChatMessage, error) {
		round++
		// Every turn requests one tool call so messages keep growing.
		return ChatMessage{
			Role:      "assistant",
			ToolCalls: []ToolCall{{ID: "1", Name: "noop", Arguments: "{}"}},
		}, nil
	}
	noop := Tool{
		Name: "noop",
		Execute: func(_ context.Context, _ string) (string, error) {
			// Return a chunk of "filler" text that will balloon the
			// accumulated history past a tiny budget after a couple
			// rounds.
			return strings.Repeat("filler text ", 500), nil
		},
	}
	inner := &fakeInnerToolLLM{
		fakeInnerLLM: &fakeInnerLLM{caps: ModelCapabilities{Provider: "p", Model: "m", MaxInputTokens: 5000}},
		turn:         turn,
	}
	bc := newBudgetedClient(inner, "drift-investigator")

	res, err := bc.CompleteWithTools(context.Background(),
		[]ChatMessage{{Role: "user", Content: "go"}},
		[]Tool{noop}, WithMaxRounds(20))

	if !errors.Is(err, ErrTokenBudgetExceeded{}) {
		t.Fatalf("expected ErrTokenBudgetExceeded after history grows, got %v", err)
	}
	if res.Rounds < 1 {
		t.Fatalf("expected at least one successful turn before the gate fired; got Rounds=%d", res.Rounds)
	}
	if res.Rounds >= 20 {
		t.Fatalf("expected gate to fire before 20 rounds; got Rounds=%d", res.Rounds)
	}
}

// TestBudgetedClient_NoBudgetMeansNoToolGate pins that ollama/lmstudio
// (MaxInputTokens=0) bypass per-turn gating just as they bypass single-shot.
func TestBudgetedClient_NoBudgetMeansNoToolGate(t *testing.T) {
	turn := func(_ context.Context, _ []ChatMessage, _ []Tool) (ChatMessage, error) {
		return ChatMessage{Role: "assistant", Content: "done"}, nil
	}
	inner := &fakeInnerToolLLM{
		fakeInnerLLM: &fakeInnerLLM{caps: ModelCapabilities{Provider: "ollama", Model: "*", MaxInputTokens: 0}},
		turn:         turn,
	}
	bc := newBudgetedClient(inner, "x")
	huge := []ChatMessage{{Role: "user", Content: strings.Repeat("xx ", 100000)}}
	res, err := bc.CompleteWithTools(context.Background(), huge, nil)
	if err != nil {
		t.Fatalf("expected no error when budget=0, got %v", err)
	}
	if res.Rounds != 1 {
		t.Fatalf("expected one turn, got %d", res.Rounds)
	}
}

// TestBudgetedClient_GatesAllSingleShotMethods pins that Complete,
// CompleteJSON, and CompleteJSONMultimodal each go through the gate.
func TestBudgetedClient_GatesAllSingleShotMethods(t *testing.T) {
	huge := strings.Repeat("xx ", 1000)

	t.Run("Complete", func(t *testing.T) {
		inner := &fakeInnerLLM{caps: ModelCapabilities{Provider: "p", Model: "m", MaxInputTokens: 10}}
		bc := newBudgetedClient(inner, "t")
		if _, err := bc.Complete(context.Background(), huge); !errors.Is(err, ErrTokenBudgetExceeded{}) {
			t.Fatalf("expected budget error, got %v", err)
		}
		if inner.calls != 0 {
			t.Fatalf("inner should not be called: %d", inner.calls)
		}
	})

	t.Run("CompleteJSONMultimodal", func(t *testing.T) {
		inner := &fakeInnerLLM{caps: ModelCapabilities{Provider: "p", Model: "m", MaxInputTokens: 10}}
		bc := newBudgetedClient(inner, "t")
		msgs := []ChatMessage{{Role: "user", Content: huge}}
		if _, err := bc.CompleteJSONMultimodal(context.Background(), msgs, JSONSchema{}); !errors.Is(err, ErrTokenBudgetExceeded{}) {
			t.Fatalf("expected budget error, got %v", err)
		}
		if inner.calls != 0 {
			t.Fatalf("inner should not be called: %d", inner.calls)
		}
	})
}
