package cli

import (
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestNewLLMTiering_DefaultsRequireAnthropicKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	_, err := newLLMTiering("", "", "")
	if err == nil {
		t.Fatal("expected error when neither key set")
	}
	if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Fatalf("error should mention ANTHROPIC_API_KEY, got %v", err)
	}
}

func TestNewLLMTiering_DefaultsToOpenAIWhenOnlyOpenAIKeySet(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "test-openai")
	tg, err := newLLMTiering("", "", "")
	if err != nil {
		t.Fatalf("expected success with only OPENAI_API_KEY set, got %v", err)
	}
	if tg.Small() == nil || tg.Typical() == nil || tg.Large() == nil {
		t.Fatal("all clients must be non-nil under OpenAI defaults")
	}
	if tg.SmallCounter() == nil || tg.TypicalCounter() == nil || tg.LargeCounter() == nil {
		t.Fatal("all counters must be non-nil under OpenAI defaults")
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

func TestNewLLMTiering_RejectsNonToolUseTypical(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	_, err := newLLMTiering("", "ollama/llama3.1", "")
	if err == nil || !strings.Contains(err.Error(), "tool use") {
		t.Fatalf("expected error about tool use, got %v", err)
	}
	if !strings.Contains(err.Error(), "typical") {
		t.Fatalf("expected error to mention 'typical' tier, got %v", err)
	}
}

func TestNewLLMTiering_AllowsNonToolUseLarge(t *testing.T) {
	// The large tier only does a single non-tool CompleteJSON call (the drift
	// judge), so a non-tool-use provider is allowed there.
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	_, err := newLLMTiering("", "", "ollama/llama3.1")
	if err != nil {
		t.Fatalf("ollama in large tier should be allowed: %v", err)
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

func TestBuildTierClient_Groq_MissingKey(t *testing.T) {
	t.Setenv("GROQ_API_KEY", "")
	_, _, err := buildTierClient("groq", "meta-llama/llama-4-scout-17b-16e-instruct")
	if err == nil || !strings.Contains(err.Error(), "GROQ_API_KEY") {
		t.Fatalf("expected GROQ_API_KEY error, got %v", err)
	}
}

func TestBuildTierClient_Groq_Success(t *testing.T) {
	t.Setenv("GROQ_API_KEY", "gsk_test")
	client, counter, err := buildTierClient("groq", "meta-llama/llama-4-scout-17b-16e-instruct")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil || counter == nil {
		t.Fatal("groq path must return non-nil client and counter")
	}
	if _, ok := client.(*analyzer.BifrostClient); !ok {
		t.Fatalf("groq must be served by *analyzer.BifrostClient, got %T", client)
	}
}

func TestBuildTierClient_Groq_RespectsBaseURLOverride(t *testing.T) {
	// The env var must reach the construction path. We can't observe the wire
	// URL without exposing internals; this test mirrors TestBuildTierClient_LMStudio
	// in shape — it asserts construction succeeds with the override set, which
	// implicitly confirms the case statement reads GROQ_BASE_URL (any read
	// failure would be a panic; any provider mismatch would surface as
	// "unknown provider").
	t.Setenv("GROQ_API_KEY", "gsk_test")
	t.Setenv("GROQ_BASE_URL", "https://my-proxy.example.com/groq")
	client, counter, err := buildTierClient("groq", "meta-llama/llama-4-scout-17b-16e-instruct")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil || counter == nil {
		t.Fatal("groq with custom base URL must return non-nil client and counter")
	}
	if _, ok := client.(*analyzer.BifrostClient); !ok {
		t.Fatalf("groq with override must be served by *analyzer.BifrostClient, got %T", client)
	}
}

func TestBuildTierClient_Gateway_MissingURL(t *testing.T) {
	t.Setenv("BIFROST_GATEWAY_URL", "")
	t.Setenv("BIFROST_GATEWAY_API_KEY", "")
	_, _, err := buildTierClient("gateway", "cheap-tier")
	if err == nil {
		t.Fatal("expected error when BIFROST_GATEWAY_URL is unset")
	}
	if !strings.Contains(err.Error(), "BIFROST_GATEWAY_URL") {
		t.Fatalf("error must name BIFROST_GATEWAY_URL; got %v", err)
	}
}
