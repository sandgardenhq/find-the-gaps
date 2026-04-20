package cli

import (
	"fmt"
	"os"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// LLMConfig holds provider selection for the analyze command.
type LLMConfig struct {
	Provider string // anthropic | openai | ollama | lmstudio | openai-compatible
	Model    string // empty = use provider default
	BaseURL  string // empty = use provider default; required for openai-compatible
}

// newLLMClient constructs the appropriate LLMClient for cfg.
func newLLMClient(cfg LLMConfig) (analyzer.LLMClient, error) {
	switch cfg.Provider {
	case "ollama":
		baseURL := cfg.BaseURL
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
		model := cfg.Model
		if model == "" {
			model = "llama3"
		}
		return analyzer.NewOpenAICompatibleClient(baseURL, model, ""), nil

	case "lmstudio":
		baseURL := cfg.BaseURL
		if baseURL == "" {
			baseURL = "http://localhost:1234"
		}
		if cfg.Model == "" {
			return nil, fmt.Errorf("--llm-model is required for lmstudio (check the Local Server tab in LM Studio for the loaded model name)")
		}
		return analyzer.NewOpenAICompatibleClient(baseURL, cfg.Model, ""), nil

	case "openai-compatible":
		if cfg.BaseURL == "" {
			return nil, fmt.Errorf("--llm-base-url is required for openai-compatible provider")
		}
		if cfg.Model == "" {
			return nil, fmt.Errorf("--llm-model is required for openai-compatible provider")
		}
		return analyzer.NewOpenAICompatibleClient(cfg.BaseURL, cfg.Model, os.Getenv("OPENAI_API_KEY")), nil

	case "openai":
		// BifrostClient not yet implemented (Task 9).
		return nil, fmt.Errorf("bifrost provider not yet implemented")

	case "anthropic", "":
		// BifrostClient not yet implemented (Task 9).
		return nil, fmt.Errorf("bifrost provider not yet implemented")

	default:
		return nil, fmt.Errorf("unknown --llm-provider %q (supported: anthropic, openai, ollama, lmstudio, openai-compatible)", cfg.Provider)
	}
}
