package cli

import (
	"testing"
)

func TestNewLLMClient_Ollama_DefaultsApplied(t *testing.T) {
	c, err := newLLMClient(LLMConfig{Provider: "ollama"})
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewLLMClient_Ollama_CustomBaseURL(t *testing.T) {
	c, err := newLLMClient(LLMConfig{Provider: "ollama", BaseURL: "http://localhost:9999", Model: "mistral"})
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewLLMClient_LMStudio_MissingModel_ReturnsError(t *testing.T) {
	_, err := newLLMClient(LLMConfig{Provider: "lmstudio"})
	if err == nil {
		t.Fatal("expected error when model not set for lmstudio")
	}
}

func TestNewLLMClient_OpenAICompatible_MissingBaseURL_ReturnsError(t *testing.T) {
	_, err := newLLMClient(LLMConfig{Provider: "openai-compatible", Model: "my-model"})
	if err == nil {
		t.Fatal("expected error when base URL not set")
	}
}

func TestNewLLMClient_UnknownProvider_ReturnsError(t *testing.T) {
	_, err := newLLMClient(LLMConfig{Provider: "grok"})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestNewLLMClient_OpenAI_NotYetImplemented_ReturnsError(t *testing.T) {
	_, err := newLLMClient(LLMConfig{Provider: "openai"})
	if err == nil {
		t.Fatal("expected error: bifrost provider not yet implemented")
	}
}

func TestNewLLMClient_Anthropic_NotYetImplemented_ReturnsError(t *testing.T) {
	_, err := newLLMClient(LLMConfig{Provider: "anthropic"})
	if err == nil {
		t.Fatal("expected error: bifrost provider not yet implemented")
	}
}

func TestNewLLMClient_DefaultProvider_NotYetImplemented_ReturnsError(t *testing.T) {
	_, err := newLLMClient(LLMConfig{Provider: ""})
	if err == nil {
		t.Fatal("expected error: bifrost provider not yet implemented")
	}
}

func TestNewLLMClient_OpenAICompatible_MissingModel_ReturnsError(t *testing.T) {
	_, err := newLLMClient(LLMConfig{Provider: "openai-compatible", BaseURL: "http://localhost:8080"})
	if err == nil {
		t.Fatal("expected error when model not set for openai-compatible")
	}
}

func TestNewLLMClient_LMStudio_CustomBaseURL(t *testing.T) {
	c, err := newLLMClient(LLMConfig{Provider: "lmstudio", BaseURL: "http://localhost:5678", Model: "phi3"})
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewLLMClient_OpenAICompatible_WithAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	c, err := newLLMClient(LLMConfig{Provider: "openai-compatible", BaseURL: "http://localhost:8080", Model: "my-model"})
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}
