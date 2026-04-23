package analyzer_test

import (
	"encoding/json"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodeFeature_JSONRoundtrip(t *testing.T) {
	f := analyzer.CodeFeature{
		Name:        "CLI command routing",
		Description: "Provides top-level command structure.",
		Layer:       "cli",
		UserFacing:  true,
	}
	data, err := json.Marshal(f)
	require.NoError(t, err)

	var got analyzer.CodeFeature
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, f, got)
}

func TestCodeFeature_UserFacingFalse_JSONRoundtrip(t *testing.T) {
	f := analyzer.CodeFeature{
		Name:        "token batching",
		Description: "Splits symbol indexes into token-budget-sized chunks.",
		Layer:       "analysis engine",
		UserFacing:  false,
	}
	data, err := json.Marshal(f)
	require.NoError(t, err)

	var got analyzer.CodeFeature
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, f, got)
}

func TestDriftFinding_JSONRoundtrip(t *testing.T) {
	f := analyzer.DriftFinding{
		Feature: "CLI command routing",
		Issues: []analyzer.DriftIssue{
			{Page: "https://docs.example.com/cli", Issue: "The --repo flag is not mentioned."},
		},
	}
	data, err := json.Marshal(f)
	require.NoError(t, err)
	var got analyzer.DriftFinding
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, f, got)
}

func TestToolCall_Fields(t *testing.T) {
	tc := analyzer.ToolCall{ID: "call_1", Name: "read_file", Arguments: `{"path":"foo.go"}`}
	assert.Equal(t, "call_1", tc.ID)
	assert.Equal(t, "read_file", tc.Name)
}

func TestChatMessage_Fields(t *testing.T) {
	msg := analyzer.ChatMessage{Role: "user", Content: "hello"}
	assert.Equal(t, "user", msg.Role)
}

func TestScreenshotGap_ZeroValue(t *testing.T) {
	var g analyzer.ScreenshotGap
	assert.Equal(t, "", g.PageURL)
	assert.Equal(t, "", g.PagePath)
	assert.Equal(t, "", g.QuotedPassage)
	assert.Equal(t, "", g.ShouldShow)
	assert.Equal(t, "", g.SuggestedAlt)
	assert.Equal(t, "", g.InsertionHint)
}

func TestScreenshotGap_JSONRoundTrip(t *testing.T) {
	in := analyzer.ScreenshotGap{
		PageURL:       "https://example.com/quickstart",
		PagePath:      "/cache/quickstart.md",
		QuotedPassage: "Click Save to continue.",
		ShouldShow:    "The Save button highlighted in the settings panel.",
		SuggestedAlt:  "Settings panel with Save button highlighted.",
		InsertionHint: "after the paragraph ending '...to continue.'",
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var out analyzer.ScreenshotGap
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, in, out)
}
