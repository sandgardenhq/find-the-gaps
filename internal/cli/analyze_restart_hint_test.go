package cli

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/stretchr/testify/assert"
)

// TestIsResumableLLMError verifies the predicate that decides whether the
// restart hint fires. Both an exhausted retry budget and a rate-limit/overload
// that outlived Bifrost's retries leave the drift cache intact, so a re-run
// resumes — both must trigger the hint, even through fmt.Errorf %w wrapping.
func TestIsResumableLLMError(t *testing.T) {
	assert.True(t, isResumableLLMError(fmt.Errorf("detect drift: %w", analyzer.ErrLLMRetriesExhausted)),
		"exhausted retries must be resumable")
	assert.True(t, isResumableLLMError(fmt.Errorf("DetectDrift %q: %w", "Feat", analyzer.ErrRateLimited)),
		"rate-limit aborts must be resumable")
	assert.False(t, isResumableLLMError(errors.New("unexpected end of JSON input")),
		"ordinary errors are not resume-with-cache hints")
	assert.False(t, isResumableLLMError(nil), "nil is not an error")
}

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
