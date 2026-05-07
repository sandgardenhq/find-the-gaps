package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPrintRestartHint verifies the user-visible warning emitted when
// analyze stops after the LLM-call retry budget is exhausted. The text
// must name the restart command and explain that completed features are
// cached so the user knows the run is resumable.
func TestPrintRestartHint(t *testing.T) {
	var buf bytes.Buffer
	printRestartHint(&buf)

	out := buf.String()
	assert.True(t, strings.Contains(out, "WARNING"), "must label itself as a warning so users notice it: %q", out)
	assert.True(t, strings.Contains(out, "ftg analyze"), "must name the command to re-run: %q", out)
	assert.True(t, strings.Contains(out, "cached"), "must communicate that progress is preserved: %q", out)
}
