package cli

import (
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestNewLLMTiering_DefaultsRequireAnthropicKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
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

func TestNewLLMTiering_ExposesCounters(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	tg, err := newLLMTiering("", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tg.SmallCounter() == nil || tg.TypicalCounter() == nil || tg.LargeCounter() == nil {
		t.Fatal("all three counters must be non-nil")
	}
}

func TestBuildTierClient_OpenAI_MissingKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	_, _, err := buildTierClient("openai", "gpt-4o")
	if err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatalf("expected OPENAI_API_KEY error, got %v", err)
	}
}

func TestBuildTierClient_OpenAI_Success(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	client, counter, err := buildTierClient("openai", "gpt-4o")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil || counter == nil {
		t.Fatal("openai path must return non-nil client and counter")
	}
}

func TestBuildTierClient_Ollama_DefaultBaseURL(t *testing.T) {
	t.Setenv("OLLAMA_BASE_URL", "")
	client, counter, err := buildTierClient("ollama", "llama3.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil || counter == nil {
		t.Fatal("ollama path must return non-nil client and counter")
	}
	if _, ok := client.(*analyzer.BifrostClient); !ok {
		t.Fatalf("ollama must be served by *analyzer.BifrostClient, got %T", client)
	}
}

func TestBuildTierClient_Ollama_CustomBaseURL(t *testing.T) {
	t.Setenv("OLLAMA_BASE_URL", "http://ollama.local:11434")
	client, _, err := buildTierClient("ollama", "llama3.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if _, ok := client.(*analyzer.BifrostClient); !ok {
		t.Fatalf("ollama must be served by *analyzer.BifrostClient, got %T", client)
	}
}

func TestBuildTierClient_LMStudio(t *testing.T) {
	t.Setenv("LMSTUDIO_BASE_URL", "")
	client, counter, err := buildTierClient("lmstudio", "local-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil || counter == nil {
		t.Fatal("lmstudio path must return non-nil client and counter")
	}
	if _, ok := client.(*analyzer.BifrostClient); !ok {
		t.Fatalf("lmstudio must be served by *analyzer.BifrostClient, got %T", client)
	}
}

func TestBuildTierClient_OpenAICompatible_Removed(t *testing.T) {
	// openai-compatible was removed in favor of lmstudio for the local-server use case.
	t.Setenv("OPENAI_COMPATIBLE_BASE_URL", "http://example.local")
	_, _, err := buildTierClient("openai-compatible", "local-model")
	if err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("expected unknown provider error, got %v", err)
	}
}

func TestBuildTierClient_UnknownProvider(t *testing.T) {
	_, _, err := buildTierClient("bogus", "whatever")
	if err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("expected unknown provider error, got %v", err)
	}
}
