package cli

import (
	"slices"
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

func TestResolveCapabilities_Gateway_TrustsUser(t *testing.T) {
	// The gateway provider exposes opaque aliases; find-the-gaps trusts the
	// user that whichever model the gateway resolves the alias to is
	// vision-capable and tool-use-capable. Capabilities are static for any
	// alias name (wildcard row).
	caps, ok := ResolveCapabilities("gateway", "cheap-tier")
	if !ok {
		t.Fatal("gateway provider must be recognized")
	}
	if !caps.Vision {
		t.Error("gateway aliases must report Vision=true")
	}
	if !caps.ToolUse {
		t.Error("gateway aliases must report ToolUse=true")
	}
	if caps.MaxCompletionTokens != 0 {
		t.Errorf("gateway aliases must use the BifrostClient default cap (0); got %d", caps.MaxCompletionTokens)
	}
}

func TestResolveCapabilities_Gateway_AnyAliasName(t *testing.T) {
	// Wildcard match: every alias the user invents flows through.
	for _, alias := range []string{"cheap-tier", "balanced", "team-a/best", "with.dots"} {
		caps, ok := ResolveCapabilities("gateway", alias)
		if !ok {
			t.Errorf("alias %q must resolve via wildcard", alias)
		}
		if !caps.Vision || !caps.ToolUse {
			t.Errorf("alias %q must inherit static caps", alias)
		}
	}
}

func TestKnownProviders_IncludesGateway(t *testing.T) {
	// validateTierConfigs's "valid: ..." error message reads from knownProviders().
	// Gateway must appear so users see it as a valid choice when they typo a tier.
	got := knownProviders()
	if !slices.Contains(got, "gateway") {
		t.Fatalf("gateway must be in knownProviders(); got %v", got)
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
