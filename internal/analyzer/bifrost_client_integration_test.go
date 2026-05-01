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
