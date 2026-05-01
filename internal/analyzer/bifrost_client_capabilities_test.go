package analyzer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBifrostClient_CapabilitiesAreSetAtConstruction(t *testing.T) {
	caps := ModelCapabilities{Provider: "anthropic", Model: "claude-haiku-4-5", ToolUse: true, Vision: true}
	c, err := NewBifrostClientWithProvider("anthropic", "test-key", "claude-haiku-4-5", "", caps)
	assert.NoError(t, err)
	assert.Equal(t, caps, c.Capabilities())
}
