package cli

import (
	"fmt"
	"os"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// LLMConfig holds provider selection for the analyze command.
type LLMConfig struct {
	Provider string // anthropic | openai | ollama | lmstudio | openai-compatible
	Model    string // empty = use provider default; resolved in-place by newLLMClient
	BaseURL  string // empty = use provider default; required for openai-compatible
}

// newLLMClient constructs the appropriate LLMClient for cfg.
// It writes the resolved model name back to cfg.Model so callers can use the
// same value (e.g. for token counting) without duplicating provider defaults.
func newLLMClient(cfg *LLMConfig) (analyzer.LLMClient, error) {
	switch cfg.Provider {
	case "ollama":
		if cfg.BaseURL == "" {
			cfg.BaseURL = "http://localhost:11434"
		}
		if cfg.Model == "" {
			cfg.Model = "llama3.1"
		}
		return analyzer.NewOpenAICompatibleClient(cfg.BaseURL, cfg.Model, ""), nil

	case "lmstudio":
		if cfg.BaseURL == "" {
			cfg.BaseURL = "http://localhost:1234"
		}
		if cfg.Model == "" {
			return nil, fmt.Errorf("--llm-model is required for lmstudio (check the Local Server tab in LM Studio for the loaded model name)")
		}
		return analyzer.NewOpenAICompatibleClient(cfg.BaseURL, cfg.Model, ""), nil

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
		if cfg.Model == "" {
			cfg.Model = "gpt-5-mini"
		}
		return analyzer.NewBifrostClientWithProvider("openai", key, cfg.Model)

	case "anthropic", "":
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable not set (or use --llm-provider ollama for a local model)")
		}
		if cfg.Model == "" {
			cfg.Model = "claude-sonnet-4-6"
		}
		return analyzer.NewBifrostClientWithProvider("anthropic", key, cfg.Model)

	default:
		return nil, fmt.Errorf("unknown --llm-provider %q (supported: anthropic, openai, ollama, lmstudio, openai-compatible)", cfg.Provider)
	}
}

// Compile-time assertion: *llmTiering must satisfy analyzer.LLMTiering.
var _ analyzer.LLMTiering = (*llmTiering)(nil)

// llmTiering holds one LLMClient + TokenCounter per tier. Implements analyzer.LLMTiering.
type llmTiering struct {
	small, typical, large                      analyzer.LLMClient
	smallCounter, typicalCounter, largeCounter analyzer.TokenCounter
}

func (t *llmTiering) Small() analyzer.LLMClient             { return t.small }
func (t *llmTiering) Typical() analyzer.LLMClient           { return t.typical }
func (t *llmTiering) Large() analyzer.LLMClient             { return t.large }
func (t *llmTiering) SmallCounter() analyzer.TokenCounter   { return t.smallCounter }
func (t *llmTiering) TypicalCounter() analyzer.TokenCounter { return t.typicalCounter }
func (t *llmTiering) LargeCounter() analyzer.TokenCounter   { return t.largeCounter }

// newLLMTiering parses and validates the three tier strings, then eagerly
// constructs all three clients. Empty strings fall back to built-in defaults
// (anthropic/claude-haiku-4-5, -sonnet-4-6, -opus-4-7). Missing API keys or
// unsupported providers fail here, before any analyze work begins.
func newLLMTiering(small, typical, large string) (*llmTiering, error) {
	if err := validateTierConfigs(small, typical, large); err != nil {
		return nil, err
	}

	tiers := []struct {
		name, raw, fallback string
	}{
		{"small", small, defaultSmallTier},
		{"typical", typical, defaultTypicalTier},
		{"large", large, defaultLargeTier},
	}
	built := make([]analyzer.LLMClient, len(tiers))
	counters := make([]analyzer.TokenCounter, len(tiers))
	for i, tc := range tiers {
		raw := tc.raw
		if raw == "" {
			raw = tc.fallback
		}
		provider, model, _ := parseTierString(raw) // already validated
		client, counter, err := buildTierClient(provider, model)
		if err != nil {
			return nil, fmt.Errorf("tier %q: %w", tc.name, err)
		}
		built[i] = client
		counters[i] = counter
	}
	return &llmTiering{
		small: built[0], typical: built[1], large: built[2],
		smallCounter: counters[0], typicalCounter: counters[1], largeCounter: counters[2],
	}, nil
}

// buildTierClient constructs a single (LLMClient, TokenCounter) for one (provider, model).
func buildTierClient(provider, model string) (analyzer.LLMClient, analyzer.TokenCounter, error) {
	switch provider {
	case "anthropic":
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			return nil, nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
		}
		client, err := analyzer.NewBifrostClientWithProvider("anthropic", key, model)
		if err != nil {
			return nil, nil, err
		}
		counter := analyzer.NewAnthropicCounter(key, model, os.Getenv("ANTHROPIC_BASE_URL"))
		return client, counter, nil
	case "openai":
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			return nil, nil, fmt.Errorf("OPENAI_API_KEY not set")
		}
		client, err := analyzer.NewBifrostClientWithProvider("openai", key, model)
		if err != nil {
			return nil, nil, err
		}
		// OpenAI uses local tiktoken counter.
		counter := analyzer.NewTiktokenCounter()
		return client, counter, nil
	case "ollama":
		baseURL := os.Getenv("OLLAMA_BASE_URL")
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
		return analyzer.NewOpenAICompatibleClient(baseURL, model, ""), analyzer.NewTiktokenCounter(), nil
	case "lmstudio":
		baseURL := os.Getenv("LMSTUDIO_BASE_URL")
		if baseURL == "" {
			baseURL = "http://localhost:1234"
		}
		return analyzer.NewOpenAICompatibleClient(baseURL, model, ""), analyzer.NewTiktokenCounter(), nil
	case "openai-compatible":
		baseURL := os.Getenv("OPENAI_COMPATIBLE_BASE_URL")
		if baseURL == "" {
			return nil, nil, fmt.Errorf("OPENAI_COMPATIBLE_BASE_URL env var required for openai-compatible")
		}
		return analyzer.NewOpenAICompatibleClient(baseURL, model, os.Getenv("OPENAI_API_KEY")), analyzer.NewTiktokenCounter(), nil
	default:
		return nil, nil, fmt.Errorf("unknown provider %q", provider)
	}
}
