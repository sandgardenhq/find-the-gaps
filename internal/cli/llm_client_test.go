package cli

import (
	"testing"
)

func TestNewLLMClient_Anthropic_EmptyModel_WritesDefaultToConfig(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "fake-key")
	cfg := &LLMConfig{Provider: "anthropic"}
	_, err := newLLMClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "claude-sonnet-4-6" {
		t.Errorf("expected Model=%q, got %q", "claude-sonnet-4-6", cfg.Model)
	}
}

func TestNewLLMClient_Ollama_EmptyModel_WritesDefaultToConfig(t *testing.T) {
	cfg := &LLMConfig{Provider: "ollama"}
	_, err := newLLMClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "llama3.1" {
		t.Errorf("expected Model=%q, got %q", "llama3.1", cfg.Model)
	}
}

func TestNewLLMClient_OpenAI_EmptyModel_WritesDefaultToConfig(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "fake-key")
	cfg := &LLMConfig{Provider: "openai"}
	_, err := newLLMClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "gpt-5-mini" {
		t.Errorf("expected Model=%q, got %q", "gpt-5-mini", cfg.Model)
	}
}

func TestNewLLMClient_Ollama_DefaultsApplied(t *testing.T) {
	c, err := newLLMClient(&LLMConfig{Provider: "ollama"})
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewLLMClient_Ollama_CustomBaseURL(t *testing.T) {
	c, err := newLLMClient(&LLMConfig{Provider: "ollama", BaseURL: "http://localhost:9999", Model: "mistral"})
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewLLMClient_LMStudio_MissingModel_ReturnsError(t *testing.T) {
	_, err := newLLMClient(&LLMConfig{Provider: "lmstudio"})
	if err == nil {
		t.Fatal("expected error when model not set for lmstudio")
	}
}

func TestNewLLMClient_OpenAICompatible_MissingBaseURL_ReturnsError(t *testing.T) {
	_, err := newLLMClient(&LLMConfig{Provider: "openai-compatible", Model: "my-model"})
	if err == nil {
		t.Fatal("expected error when base URL not set")
	}
}

func TestNewLLMClient_UnknownProvider_ReturnsError(t *testing.T) {
	_, err := newLLMClient(&LLMConfig{Provider: "grok"})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestNewLLMClient_OpenAI_MissingKey_ReturnsError(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	_, err := newLLMClient(&LLMConfig{Provider: "openai"})
	if err == nil {
		t.Fatal("expected error when OPENAI_API_KEY is not set")
	}
}

func TestNewLLMClient_Anthropic_MissingKey_ReturnsError(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	_, err := newLLMClient(&LLMConfig{Provider: "anthropic"})
	if err == nil {
		t.Fatal("expected error when ANTHROPIC_API_KEY is not set")
	}
}

func TestNewLLMClient_DefaultProvider_MissingKey_ReturnsError(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	_, err := newLLMClient(&LLMConfig{Provider: ""})
	if err == nil {
		t.Fatal("expected error when ANTHROPIC_API_KEY is not set for default provider")
	}
}

func TestNewLLMClient_OpenAICompatible_MissingModel_ReturnsError(t *testing.T) {
	_, err := newLLMClient(&LLMConfig{Provider: "openai-compatible", BaseURL: "http://localhost:8080"})
	if err == nil {
		t.Fatal("expected error when model not set for openai-compatible")
	}
}

func TestNewLLMClient_LMStudio_CustomBaseURL(t *testing.T) {
	c, err := newLLMClient(&LLMConfig{Provider: "lmstudio", BaseURL: "http://localhost:5678", Model: "phi3"})
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewLLMClient_OpenAICompatible_WithAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	c, err := newLLMClient(&LLMConfig{Provider: "openai-compatible", BaseURL: "http://localhost:8080", Model: "my-model"})
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewLLMClient_OpenAI_WithKey_DefaultModel_ReturnsClient(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "fake-key")
	c, err := newLLMClient(&LLMConfig{Provider: "openai"})
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewLLMClient_OpenAI_WithKey_CustomModel_ReturnsClient(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "fake-key")
	c, err := newLLMClient(&LLMConfig{Provider: "openai", Model: "gpt-4-turbo"})
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewLLMClient_Anthropic_WithKey_DefaultModel_ReturnsClient(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "fake-key")
	c, err := newLLMClient(&LLMConfig{Provider: "anthropic"})
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewLLMClient_Anthropic_WithKey_CustomModel_ReturnsClient(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "fake-key")
	c, err := newLLMClient(&LLMConfig{Provider: "anthropic", Model: "claude-3-haiku-20240307"})
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewLLMClient_DefaultProvider_WithKey_ReturnsClient(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "fake-key")
	c, err := newLLMClient(&LLMConfig{Provider: ""})
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}
