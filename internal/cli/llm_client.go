package cli

import (
	"fmt"
	"os"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

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
