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
			model = "llama3.1"
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
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY environment variable not set")
		}
		model := cfg.Model
		if model == "" {
			model = "gpt-5-mini"
		}
		return analyzer.NewBifrostClientWithProvider("openai", key, model)

	case "anthropic", "":
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable not set (or use --llm-provider ollama for a local model)")
		}
		model := cfg.Model
		if model == "" {
			model = "claude-sonnet-4-6"
		}
		return analyzer.NewBifrostClientWithProvider("anthropic", key, model)

	default:
		return nil, fmt.Errorf("unknown --llm-provider %q (supported: anthropic, openai, ollama, lmstudio, openai-compatible)", cfg.Provider)
	}
}
