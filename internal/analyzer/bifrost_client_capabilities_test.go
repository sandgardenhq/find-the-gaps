package analyzer

import (
	"context"
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
	c, err := NewBifrostClientWithProvider("groq", "gsk_test", "meta-llama/llama-4-scout-17b-16e-instruct", "https://api.groq.com/openai", caps)
	require.NoError(t, err)
	assert.Equal(t, schemas.OpenAI, c.provider)
	assert.True(t, c.Capabilities().Vision)
}

func TestNewBifrostClientWithProvider_GroqRequiresBaseURL(t *testing.T) {
	_, err := NewBifrostClientWithProvider("groq", "gsk_test", "x", "", ModelCapabilities{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "baseURL")
}

// Groq's meta-llama/llama-4-scout-17b-16e-instruct rejects max_completion_tokens > 8192
// with: "must be less than or equal to 8192, the maximum value for max_completion_tokens
// is less than the context_window for this model". When ModelCapabilities carries an
// explicit per-model cap, the BifrostClient must honor it instead of the 32k default.
func TestBifrostClient_HonorsCapsMaxCompletionTokens(t *testing.T) {
	text := "ok"
	fake := &fakeBifrostRequester{
		resp: &schemas.BifrostChatResponse{
			Choices: []schemas.BifrostResponseChoice{
				makeChoice(&schemas.ChatMessageContent{ContentStr: &text}),
			},
		},
	}
	caps := ModelCapabilities{
		Provider:            "groq",
		Model:               "meta-llama/llama-4-scout-17b-16e-instruct",
		MaxCompletionTokens: 8192,
	}
	c := &BifrostClient{client: fake, provider: schemas.OpenAI, model: caps.Model, caps: caps}
	_, err := c.Complete(context.Background(), "ping")
	require.NoError(t, err)
	require.NotNil(t, fake.lastRequest)
	require.NotNil(t, fake.lastRequest.Params)
	require.NotNil(t, fake.lastRequest.Params.MaxCompletionTokens)
	assert.Equal(t, 8192, *fake.lastRequest.Params.MaxCompletionTokens)
}

// When caps.MaxCompletionTokens is zero, the client falls back to its built-in
// default so existing Anthropic/OpenAI flows that depend on a generous output
// window keep working unchanged.
func TestBifrostClient_FallsBackToDefaultWhenCapsZero(t *testing.T) {
	text := "ok"
	fake := &fakeBifrostRequester{
		resp: &schemas.BifrostChatResponse{
			Choices: []schemas.BifrostResponseChoice{
				makeChoice(&schemas.ChatMessageContent{ContentStr: &text}),
			},
		},
	}
	c := &BifrostClient{
		client:   fake,
		provider: schemas.Anthropic,
		model:    "claude-opus-4-7",
		caps:     ModelCapabilities{Provider: "anthropic", Model: "claude-opus-4-7"},
	}
	_, err := c.Complete(context.Background(), "ping")
	require.NoError(t, err)
	require.NotNil(t, fake.lastRequest.Params.MaxCompletionTokens)
	assert.GreaterOrEqual(t, *fake.lastRequest.Params.MaxCompletionTokens, 16_000)
}
