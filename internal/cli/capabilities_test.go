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

// TestResolveCapabilities_Gemini2026Lineup pins the Gemini default ladder
// (small gemini-3.1-flash-lite · typical gemini-3.5-flash · large
// gemini-3.1-pro-preview). All three support tool use AND vision — the typical
// tier runs the drift investigator's tool-use loop and the screenshot pass
// needs vision.
func TestResolveCapabilities_Gemini2026Lineup(t *testing.T) {
	for _, model := range []string{
		"gemini-3.1-flash-lite",
		"gemini-3.5-flash",
		"gemini-3.1-pro-preview",
	} {
		caps, ok := ResolveCapabilities("gemini", model)
		assert.True(t, ok, "gemini/%s must resolve", model)
		assert.True(t, caps.ToolUse, "gemini/%s must support tool use", model)
		assert.True(t, caps.Vision, "gemini/%s must support vision", model)
	}
}

// TestResolveCapabilities_AnthropicLineup pins the full Claude 4.x lineup that
// is callable through the API as of June 2026: the current flagships, the
// still-available legacy rows, and the deprecated-but-not-yet-retired models.
// Every Claude model supports text + image input and tool use.
func TestResolveCapabilities_AnthropicLineup(t *testing.T) {
	for _, model := range []string{
		"claude-opus-4-8",
		"claude-sonnet-4-6",
		"claude-haiku-4-5",
		"claude-opus-4-7",
		"claude-opus-4-6",
		"claude-sonnet-4-5",
		"claude-opus-4-5",
		"claude-opus-4-1",
		"claude-sonnet-4",
		"claude-opus-4",
	} {
		caps, ok := ResolveCapabilities("anthropic", model)
		assert.True(t, ok, "anthropic/%s must resolve", model)
		assert.True(t, caps.ToolUse, "anthropic/%s must support tool use", model)
		assert.True(t, caps.Vision, "anthropic/%s must support vision", model)
	}
}

// TestResolveCapabilities_OpenAIFullLineup pins the OpenAI API models beyond the
// tier-default set: the pro reasoning variants, gpt-5.2, and the gpt-5-nano
// sibling. All support tool use and vision.
func TestResolveCapabilities_OpenAIFullLineup(t *testing.T) {
	for _, model := range []string{
		"gpt-5.5",
		"gpt-5.5-pro",
		"gpt-5.4",
		"gpt-5.4-pro",
		"gpt-5.4-mini",
		"gpt-5.4-nano",
		"gpt-5.2",
		"gpt-5",
		"gpt-5-mini",
		"gpt-5-nano",
		"gpt-4o",
		"gpt-4o-mini",
	} {
		caps, ok := ResolveCapabilities("openai", model)
		assert.True(t, ok, "openai/%s must resolve", model)
		assert.True(t, caps.ToolUse, "openai/%s must support tool use", model)
		assert.True(t, caps.Vision, "openai/%s must support vision", model)
	}
}

// TestResolveCapabilities_GeminiFullLineup pins the Gemini API models beyond the
// tier-default ladder: the 3.x preview Pro/Flash siblings and the GA 2.5
// family. Every Gemini model accepts image input and supports function calling.
func TestResolveCapabilities_GeminiFullLineup(t *testing.T) {
	for _, model := range []string{
		"gemini-3.1-pro-preview",
		"gemini-3-pro-preview",
		"gemini-3.5-flash",
		"gemini-3-flash-preview",
		"gemini-3.1-flash-lite",
		"gemini-2.5-pro",
		"gemini-2.5-flash",
		"gemini-2.5-flash-lite",
	} {
		caps, ok := ResolveCapabilities("gemini", model)
		assert.True(t, ok, "gemini/%s must resolve", model)
		assert.True(t, caps.ToolUse, "gemini/%s must support tool use", model)
		assert.True(t, caps.Vision, "gemini/%s must support vision", model)
	}
}

// TestResolveCapabilities_GeminiUnknownModelGetsConservativeDefault pins that a
// future Gemini model ID (gemini is a known provider) falls through to the
// 100k floor rather than failing the run — so new Gemini releases work before
// the table catches up.
func TestResolveCapabilities_GeminiUnknownModelGetsConservativeDefault(t *testing.T) {
	caps, ok := ResolveCapabilities("gemini", "gemini-99-ultra")
	assert.True(t, ok)
	assert.Equal(t, 100000, caps.MaxInputTokens)
}

// TestKnownProviders_IncludesGemini pins that gemini appears in the
// deduplicated provider list that drives the "valid: ..." error message.
func TestKnownProviders_IncludesGemini(t *testing.T) {
	assert.Contains(t, knownProviders(), "gemini")
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
		// Anthropic — current 4.x flagships and the still-available legacy
		// / deprecated rows. 1M-window models get 900k; 200k-window models
		// get 180k (~10% under their published cap).
		{"anthropic", "claude-opus-4-8", 900000},
		{"anthropic", "claude-sonnet-4-6", 900000},
		{"anthropic", "claude-haiku-4-5", 180000},
		{"anthropic", "claude-opus-4-7", 900000},
		{"anthropic", "claude-opus-4-6", 900000},
		{"anthropic", "claude-sonnet-4-5", 180000},
		{"anthropic", "claude-opus-4-5", 180000},
		{"anthropic", "claude-opus-4-1", 180000},
		{"anthropic", "claude-sonnet-4", 180000},
		{"anthropic", "claude-opus-4", 180000},
		// OpenAI — gpt-5.5 and gpt-5.4 both publish a 1.05M context window
		// (gpt-5.4 grew from the old 272k standard tier), so both sit at 900k.
		// The 400k-window 5.4-mini/nano/5.2 sit at 360k; the gpt-5 generation
		// enforces a 272k input cap, so it stays at 260k.
		{"openai", "gpt-5.5", 900000},
		{"openai", "gpt-5.5-pro", 900000},
		{"openai", "gpt-5.4", 900000},
		{"openai", "gpt-5.4-pro", 900000},
		{"openai", "gpt-5.4-mini", 360000},
		{"openai", "gpt-5.4-nano", 360000},
		{"openai", "gpt-5.2", 360000},
		{"openai", "gpt-5", 260000},
		{"openai", "gpt-5-mini", 260000},
		{"openai", "gpt-5-nano", 260000},
		{"openai", "gpt-4o", 115000},
		{"openai", "gpt-4o-mini", 115000},
		{"groq", "meta-llama/llama-4-scout-17b-16e-instruct", 120000},
		// Gemini — every 2.5/3.x model publishes a 1,048,576-token input
		// window, so all sit at 900k.
		{"gemini", "gemini-3.1-pro-preview", 900000},
		{"gemini", "gemini-3-pro-preview", 900000},
		{"gemini", "gemini-3.5-flash", 900000},
		{"gemini", "gemini-3-flash-preview", 900000},
		{"gemini", "gemini-3.1-flash-lite", 900000},
		{"gemini", "gemini-2.5-pro", 900000},
		{"gemini", "gemini-2.5-flash", 900000},
		{"gemini", "gemini-2.5-flash-lite", 900000},
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
