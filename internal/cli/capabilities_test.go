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
