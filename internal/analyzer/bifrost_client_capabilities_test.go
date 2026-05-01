package analyzer

import (
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBifrostClient_CapabilitiesAreSetAtConstruction(t *testing.T) {
	caps := ModelCapabilities{Provider: "anthropic", Model: "claude-haiku-4-5", ToolUse: true, Vision: true}
	c, err := NewBifrostClientWithProvider("anthropic", "test-key", "claude-haiku-4-5", "", caps)
	assert.NoError(t, err)
	assert.Equal(t, caps, c.Capabilities())
}

func TestNewBifrostClientWithProvider_GroqUsesOpenAIWithCustomBase(t *testing.T) {
	caps := ModelCapabilities{Provider: "groq", Model: "meta-llama/llama-4-scout-17b-16e-instruct", ToolUse: true, Vision: true}
	c, err := NewBifrostClientWithProvider("groq", "gsk_test", "meta-llama/llama-4-scout-17b-16e-instruct", "https://api.groq.com/openai/v1", caps)
	require.NoError(t, err)
	assert.Equal(t, schemas.OpenAI, c.provider)
	assert.True(t, c.Capabilities().Vision)
}

func TestNewBifrostClientWithProvider_GroqRequiresBaseURL(t *testing.T) {
	_, err := NewBifrostClientWithProvider("groq", "gsk_test", "x", "", ModelCapabilities{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "baseURL")
}
