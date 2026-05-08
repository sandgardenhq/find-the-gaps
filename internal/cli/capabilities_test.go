package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveCapabilities_ExactMatchWins(t *testing.T) {
	caps, ok := ResolveCapabilities("anthropic", "claude-haiku-4-5")
	assert.True(t, ok)
	assert.True(t, caps.ToolUse)
	assert.True(t, caps.Vision)
}

func TestResolveCapabilities_WildcardForSelfHosted(t *testing.T) {
	caps, ok := ResolveCapabilities("ollama", "anything-goes")
	assert.True(t, ok)
	assert.False(t, caps.ToolUse)
	assert.False(t, caps.Vision)
}

func TestResolveCapabilities_UnknownProviderReturnsFalse(t *testing.T) {
	_, ok := ResolveCapabilities("not-a-provider", "anything")
	assert.False(t, ok)
}

func TestResolveCapabilities_UnknownModelOnKnownProviderReturnsZero(t *testing.T) {
	caps, ok := ResolveCapabilities("anthropic", "claude-future-9-9")
	assert.True(t, ok)
	assert.False(t, caps.ToolUse)
	assert.False(t, caps.Vision)
}

func TestResolveCapabilities_GroqVisionModel(t *testing.T) {
	caps, ok := ResolveCapabilities("groq", "meta-llama/llama-4-scout-17b-16e-instruct")
	assert.True(t, ok)
	assert.True(t, caps.ToolUse)
	assert.True(t, caps.Vision)
}

// TestResolveCapabilities_OpenAI2026Lineup pins the GPT-5.4 / GPT-5.5
// generation that became OpenAI's API-default lineup in March/April 2026.
// All three models support tool use AND vision — they are the OpenAI
// counterparts of the Anthropic haiku/sonnet/opus defaults.
func TestResolveCapabilities_OpenAI2026Lineup(t *testing.T) {
	for _, model := range []string{
		"gpt-5.4-nano",
		"gpt-5.4-mini",
		"gpt-5.4",
		"gpt-5.5",
	} {
		caps, ok := ResolveCapabilities("openai", model)
		assert.True(t, ok, "openai/%s must resolve", model)
		assert.True(t, caps.ToolUse, "openai/%s must support tool use", model)
		assert.True(t, caps.Vision, "openai/%s must support vision", model)
	}
}

// TestOpenAIDefaults_AreVisionAndToolCapable pins the contract that the
// OpenAI tier defaults flipped to by tierFallbacks() resolve to models with
// both ToolUse and Vision in the registry. Without this, an OpenAI-only
// user's `ftg analyze` would either fail tier validation (typical needs
// tool use) or silently skip the vision-aware screenshot pass.
func TestOpenAIDefaults_AreVisionAndToolCapable(t *testing.T) {
	for _, tc := range []struct {
		name string
		tier string
	}{
		{"small", defaultSmallTierOpenAI},
		{"typical", defaultTypicalTierOpenAI},
		{"large", defaultLargeTierOpenAI},
	} {
		provider, model, err := parseTierString(tc.tier)
		assert.NoError(t, err, "%s default %q must parse", tc.name, tc.tier)
		caps, ok := ResolveCapabilities(provider, model)
		assert.True(t, ok, "%s default %q must resolve", tc.name, tc.tier)
		assert.True(t, caps.ToolUse, "%s default %q must support tool use", tc.name, tc.tier)
		assert.True(t, caps.Vision, "%s default %q must support vision", tc.name, tc.tier)
	}
}

// TestResolveCapabilities_KnownModelsCarryMaxInputTokens pins the per-model
// input budget for every hosted model in knownModels. Values are ~10% under
// the provider's published context window so output tokens and per-provider
// serialization overhead do not push a request over the wire-level cap.
func TestResolveCapabilities_KnownModelsCarryMaxInputTokens(t *testing.T) {
	cases := []struct {
		provider, model string
		want            int
	}{
		{"anthropic", "claude-haiku-4-5", 180000},
		{"anthropic", "claude-sonnet-4-6", 900000},
		{"anthropic", "claude-opus-4-7", 900000},
		{"openai", "gpt-5.5", 900000},
		{"openai", "gpt-5.4", 260000},
		{"openai", "gpt-5.4-mini", 360000},
		{"openai", "gpt-5.4-nano", 360000},
		{"openai", "gpt-5", 260000},
		{"openai", "gpt-5-mini", 260000},
		{"openai", "gpt-4o", 115000},
		{"openai", "gpt-4o-mini", 115000},
		{"groq", "meta-llama/llama-4-scout-17b-16e-instruct", 120000},
	}
	for _, tc := range cases {
		t.Run(tc.provider+"/"+tc.model, func(t *testing.T) {
			caps, ok := ResolveCapabilities(tc.provider, tc.model)
			assert.True(t, ok)
			assert.Equal(t, tc.want, caps.MaxInputTokens)
		})
	}
}

// TestResolveCapabilities_UnknownModelOnKnownProviderUsesConservativeDefault
// pins the fallback behavior: a future model on a known provider gets a
// 100k budget rather than zero. This is below GPT-4o's 128k floor and any
// modern hosted production model, so the gate fires safely on a brand-new
// row before the table catches up — and an obscure model can't reproduce
// the 294k incident this work fixes.
func TestResolveCapabilities_UnknownModelOnKnownProviderUsesConservativeDefault(t *testing.T) {
	caps, ok := ResolveCapabilities("anthropic", "claude-future-99")
	assert.True(t, ok)
	assert.Equal(t, 100000, caps.MaxInputTokens)
}

// TestResolveCapabilities_SelfHostedWildcardHasNoBudget pins the contract
// that ollama/lmstudio's wildcard rows carry a zero MaxInputTokens, meaning
// the budget gate is disabled. The user picks the model and the harness
// can't know its limit; failing closed would surprise users running tiny
// local models that fit comfortably.
func TestResolveCapabilities_SelfHostedWildcardHasNoBudget(t *testing.T) {
	for _, provider := range []string{"ollama", "lmstudio"} {
		caps, ok := ResolveCapabilities(provider, "anything-the-user-picked")
		assert.True(t, ok, provider)
		assert.Equal(t, 0, caps.MaxInputTokens, provider)
	}
}
