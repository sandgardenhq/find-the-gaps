//go:build integration

package analyzer_test

import (
	"context"
	"os"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestBifrostClient_Anthropic_RealCompletion(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	client, err := analyzer.NewBifrostClientWithProvider("anthropic", key, "claude-3-5-sonnet-20241022", "", analyzer.ModelCapabilities{})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.Complete(context.Background(), "Reply with the single word: pong")
	if err != nil {
		t.Fatal(err)
	}
	if resp == "" {
		t.Error("expected non-empty response")
	}
	t.Logf("Response: %s", resp)
}

func TestBifrostClient_OpenAI_RealCompletion(t *testing.T) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	client, err := analyzer.NewBifrostClientWithProvider("openai", key, "gpt-4o-mini", "", analyzer.ModelCapabilities{})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.Complete(context.Background(), "Reply with the single word: pong")
	if err != nil {
		t.Fatal(err)
	}
	if resp == "" {
		t.Error("expected non-empty response")
	}
	t.Logf("Response: %s", resp)
}

// TestBifrostClient_Gemini_RealCompletion exercises the real Gemini provider
// end-to-end: a plain Complete plus a CompleteJSON structured-output round
// against the typical-tier default (gemini-3.5-flash). Gated on GEMINI_API_KEY
// like the Groq/OpenAI integration tests — no mocks (Verification Rules).
func TestBifrostClient_Gemini_RealCompletion(t *testing.T) {
	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		t.Skip("GEMINI_API_KEY not set")
	}

	caps := analyzer.ModelCapabilities{Provider: "gemini", Model: "gemini-3.5-flash", ToolUse: true, Vision: true, MaxInputTokens: 900000}
	client, err := analyzer.NewBifrostClientWithProvider("gemini", key, "gemini-3.5-flash", "", caps)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.Complete(context.Background(), "Reply with the single word: pong")
	if err != nil {
		t.Fatal(err)
	}
	if resp == "" {
		t.Error("expected non-empty response")
	}
	t.Logf("Complete response: %s", resp)

	// Structured output must round-trip through Gemini's responseJsonSchema
	// path (the OpenAI-compat json_schema response_format).
	schema := analyzer.JSONSchema{
		Name: "ping",
		Doc:  []byte(`{"type":"object","additionalProperties":false,"required":["word"],"properties":{"word":{"type":"string"}}}`),
	}
	// PROMPT: Forces a single-key JSON object so the structured-output path is exercised.
	raw, err := client.CompleteJSON(context.Background(), `Respond with a JSON object {"word":"pong"}.`, schema)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Error("expected non-empty JSON response")
	}
	t.Logf("CompleteJSON response: %s", string(raw))
}
