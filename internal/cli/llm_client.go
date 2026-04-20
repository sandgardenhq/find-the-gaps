package cli

import (
	"errors"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// LLMConfig holds provider selection for the analyze command.
type LLMConfig struct {
	Provider string // anthropic | openai | ollama | lmstudio | openai-compatible
	Model    string // empty = use provider default
	BaseURL  string // empty = use provider default; required for openai-compatible
}

// newLLMClient constructs the appropriate LLMClient for cfg.
// Fully implemented in Task 8; returns an error until then.
func newLLMClient(_ LLMConfig) (analyzer.LLMClient, error) {
	return nil, errors.New("LLM client not yet implemented — see Task 8")
}
