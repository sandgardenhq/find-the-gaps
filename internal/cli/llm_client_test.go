package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestToAnalyzerCaps_PropagatesMaxInputTokens pins that the cli-to-analyzer
// capability conversion carries MaxInputTokens through. Without this, the
// budgeted client decorator constructed inside the analyzer package would
// read 0 (no gate) for every model and the budget gate would never fire.
func TestToAnalyzerCaps_PropagatesMaxInputTokens(t *testing.T) {
	in := ModelCapabilities{
		Provider:            "openai",
		Model:               "gpt-5.5",
		ToolUse:             true,
		Vision:              true,
		MaxCompletionTokens: 32000,
		MaxInputTokens:      260000,
	}
	out := toAnalyzerCaps(in)
	assert.Equal(t, "openai", out.Provider)
	assert.Equal(t, "gpt-5.5", out.Model)
	assert.True(t, out.ToolUse)
	assert.True(t, out.Vision)
	assert.Equal(t, 32000, out.MaxCompletionTokens)
	assert.Equal(t, 260000, out.MaxInputTokens)
}
