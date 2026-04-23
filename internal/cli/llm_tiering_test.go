package cli

import (
	"os"
	"strings"
	"testing"
)

func TestNewLLMTiering_DefaultsRequireAnthropicKey(t *testing.T) {
	os.Unsetenv("ANTHROPIC_API_KEY")
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
