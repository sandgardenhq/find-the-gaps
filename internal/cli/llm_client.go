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
//
// lmstudio and openai-compatible collapse to Bifrost's schemas.OpenAI provider
// with a NetworkConfig.BaseURL override — Bifrost honors BaseURL for OpenAI
// (see bifrost core schemas/provider.go:54). ollama uses the dedicated Bifrost
// Ollama provider, which threads the server URL through Key.OllamaKeyConfig.
func buildTierClient(provider, model string) (analyzer.LLMClient, analyzer.TokenCounter, error) {
	var (
		bifrostProvider string
		apiKey, baseURL string
		counter         analyzer.TokenCounter
	)
	switch provider {
	case "anthropic":
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return nil, nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
		}
		bifrostProvider = "anthropic"
		counter = analyzer.NewAnthropicCounter(apiKey, model, os.Getenv("ANTHROPIC_BASE_URL"))
	case "openai":
		apiKey = os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			return nil, nil, fmt.Errorf("OPENAI_API_KEY not set")
		}
		bifrostProvider = "openai"
		counter = analyzer.NewTiktokenCounter()
	case "ollama":
		baseURL = os.Getenv("OLLAMA_BASE_URL")
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
		bifrostProvider = "ollama"
		counter = analyzer.NewTiktokenCounter()
	case "lmstudio":
		baseURL = os.Getenv("LMSTUDIO_BASE_URL")
		if baseURL == "" {
			baseURL = "http://localhost:1234"
		}
		bifrostProvider = "openai"
		counter = analyzer.NewTiktokenCounter()
	case "openai-compatible":
		baseURL = os.Getenv("OPENAI_COMPATIBLE_BASE_URL")
		if baseURL == "" {
			return nil, nil, fmt.Errorf("OPENAI_COMPATIBLE_BASE_URL env var required for openai-compatible")
		}
		apiKey = os.Getenv("OPENAI_API_KEY")
		bifrostProvider = "openai"
		counter = analyzer.NewTiktokenCounter()
	default:
		return nil, nil, fmt.Errorf("unknown provider %q", provider)
	}

	client, err := analyzer.NewBifrostClientWithProvider(bifrostProvider, apiKey, model, baseURL)
	if err != nil {
		return nil, nil, err
	}
	return client, counter, nil
}
