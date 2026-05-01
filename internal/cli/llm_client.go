package cli

import (
	"fmt"
	"os"
	"sync/atomic"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// Compile-time assertion: *llmTiering must satisfy analyzer.LLMTiering.
var _ analyzer.LLMTiering = (*llmTiering)(nil)

// LLMCallCounts is a snapshot of how many LLM calls have flowed through each
// tier of an llmTiering. Reported at the end of an analyze run when --verbose
// is set.
type LLMCallCounts struct {
	Small, Typical, Large int64
}

// Total returns the sum across all tiers.
func (c LLMCallCounts) Total() int64 { return c.Small + c.Typical + c.Large }

// llmTiering holds one LLMClient + TokenCounter per tier. Implements analyzer.LLMTiering.
type llmTiering struct {
	small, typical, large                      analyzer.LLMClient
	smallCounter, typicalCounter, largeCounter analyzer.TokenCounter
	smallCalls, typicalCalls, largeCalls       atomic.Int64
}

// CallCounts snapshots the per-tier call counters.
func (t *llmTiering) CallCounts() LLMCallCounts {
	return LLMCallCounts{
		Small:   t.smallCalls.Load(),
		Typical: t.typicalCalls.Load(),
		Large:   t.largeCalls.Load(),
	}
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
	tg := &llmTiering{
		smallCounter: counters[0], typicalCounter: counters[1], largeCounter: counters[2],
	}
	tg.small = wrapWithCounter(built[0], &tg.smallCalls)
	tg.typical = wrapWithCounter(built[1], &tg.typicalCalls)
	tg.large = wrapWithCounter(built[2], &tg.largeCalls)
	return tg, nil
}

// buildTierClient constructs a single (LLMClient, TokenCounter) for one (provider, model).
//
// lmstudio collapses to Bifrost's schemas.OpenAI provider with a
// NetworkConfig.BaseURL override — Bifrost honors BaseURL for OpenAI
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
	case "groq":
		apiKey = os.Getenv("GROQ_API_KEY")
		if apiKey == "" {
			return nil, nil, fmt.Errorf("GROQ_API_KEY not set")
		}
		bifrostProvider = "groq"
		baseURL = "https://api.groq.com/openai/v1"
		counter = analyzer.NewTiktokenCounter()
	default:
		return nil, nil, fmt.Errorf("unknown provider %q", provider)
	}

	caps, _ := ResolveCapabilities(provider, model)
	analyzerCaps := analyzer.ModelCapabilities{
		Provider: caps.Provider,
		Model:    caps.Model,
		ToolUse:  caps.ToolUse,
		Vision:   caps.Vision,
	}
	client, err := analyzer.NewBifrostClientWithProvider(bifrostProvider, apiKey, model, baseURL, analyzerCaps)
	if err != nil {
		return nil, nil, err
	}
	return client, counter, nil
}
