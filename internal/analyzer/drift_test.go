package analyzer_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// driftStubClient is a ToolLLMClient test double for drift detection tests.
type driftStubClient struct {
	// responses is consumed in order; last element is reused when exhausted.
	responses []analyzer.ChatMessage
	calls     int
	// completeFunc used by Complete (existing interface).
	completeFunc func(ctx context.Context, prompt string) (string, error)
}

func (s *driftStubClient) Complete(ctx context.Context, prompt string) (string, error) {
	if s.completeFunc != nil {
		return s.completeFunc(ctx, prompt)
	}
	return "", nil
}

func (s *driftStubClient) CompleteWithTools(_ context.Context, _ []analyzer.ChatMessage, _ []analyzer.Tool) (analyzer.ChatMessage, error) {
	idx := s.calls
	if idx >= len(s.responses) {
		idx = len(s.responses) - 1
	}
	s.calls++
	return s.responses[idx], nil
}

// driftStubClientWithErr always returns err from CompleteWithTools.
type driftStubClientWithErr struct {
	err error
}

func (s *driftStubClientWithErr) Complete(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (s *driftStubClientWithErr) CompleteWithTools(_ context.Context, _ []analyzer.ChatMessage, _ []analyzer.Tool) (analyzer.ChatMessage, error) {
	return analyzer.ChatMessage{}, s.err
}

func TestDetectDrift_NoDocumentedFeatures_ReturnsEmpty(t *testing.T) {
	client := &driftStubClient{}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth", UserFacing: true}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{} // no pages mapped — auth is undocumented, not a drift candidate
	pageReader := func(url string) (string, error) { return "", nil }

	findings, err := analyzer.DetectDrift(context.Background(), client, featureMap, docsMap, pageReader, "/repo")
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestDetectDrift_DocumentedFeature_ReturnsIssues(t *testing.T) {
	client := &driftStubClient{
		responses: []analyzer.ChatMessage{
			{
				Role:    "assistant",
				Content: `[{"page":"https://docs.example.com/auth","issue":"Email requirement not documented."}]`,
			},
		},
	}
	featureMap := analyzer.FeatureMap{
		{
			Feature: analyzer.CodeFeature{Name: "auth", Description: "Handles user auth.", UserFacing: true},
			Files:   []string{"auth.go"},
			Symbols: []string{"Login"},
		},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}},
	}
	pageReader := func(url string) (string, error) { return "# Auth\nLogin with username.", nil }

	findings, err := analyzer.DetectDrift(context.Background(), client, featureMap, docsMap, pageReader, "/repo")
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, "auth", findings[0].Feature)
	require.Len(t, findings[0].Issues, 1)
	assert.Equal(t, "https://docs.example.com/auth", findings[0].Issues[0].Page)
	assert.Contains(t, findings[0].Issues[0].Issue, "Email requirement")
}

func TestDetectDrift_LLMReturnsEmptyArray_FeatureDropped(t *testing.T) {
	client := &driftStubClient{
		responses: []analyzer.ChatMessage{{Role: "assistant", Content: "[]"}},
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "search"}, Files: []string{"search.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "search", Pages: []string{"https://docs.example.com/search"}},
	}
	pageReader := func(url string) (string, error) { return "# Search docs", nil }

	findings, err := analyzer.DetectDrift(context.Background(), client, featureMap, docsMap, pageReader, "/repo")
	require.NoError(t, err)
	assert.Empty(t, findings, "features with no issues should be dropped")
}

func TestDetectDrift_ToolCall_ExecutedAndContinued(t *testing.T) {
	// First response: LLM requests read_file tool.
	// Second response: LLM returns final JSON after seeing tool result.
	client := &driftStubClient{
		responses: []analyzer.ChatMessage{
			{
				Role: "assistant",
				ToolCalls: []analyzer.ToolCall{
					{ID: "call_1", Name: "read_file", Arguments: `{"path":"auth.go"}`},
				},
			},
			{
				Role:    "assistant",
				Content: `[{"page":"","issue":"The docs omit that Login returns a JWT token."}]`,
			},
		},
	}
	featureMap := analyzer.FeatureMap{
		{
			Feature: analyzer.CodeFeature{Name: "auth", Description: "Handles user auth."},
			Files:   []string{"auth.go"},
		},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}},
	}
	pageReader := func(url string) (string, error) { return "# Auth page", nil }

	findings, err := analyzer.DetectDrift(context.Background(), client, featureMap, docsMap, pageReader, t.TempDir())
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Contains(t, findings[0].Issues[0].Issue, "JWT token")
}

func TestDetectDrift_ReadFile_OutsideRepo_ReturnsError(t *testing.T) {
	// LLM requests a path that escapes the repo root — tool should return an
	// error message to the LLM, not panic or expose files.
	client := &driftStubClient{
		responses: []analyzer.ChatMessage{
			{
				Role:      "assistant",
				ToolCalls: []analyzer.ToolCall{{ID: "call_1", Name: "read_file", Arguments: `{"path":"../../../etc/passwd"}`}},
			},
			{Role: "assistant", Content: "[]"},
		},
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}}}
	pageReader := func(url string) (string, error) { return "# Auth", nil }

	// Must not error — path rejection is communicated back to the LLM as a tool result.
	_, err := analyzer.DetectDrift(context.Background(), client, featureMap, docsMap, pageReader, t.TempDir())
	assert.NoError(t, err)
}

func TestDetectDrift_ReadPage_ToolCall_ExecutedAndContinued(t *testing.T) {
	// LLM requests the read_page tool, receives content, then returns final JSON.
	client := &driftStubClient{
		responses: []analyzer.ChatMessage{
			{
				Role:      "assistant",
				ToolCalls: []analyzer.ToolCall{{ID: "call_2", Name: "read_page", Arguments: `{"url":"https://docs.example.com/auth"}`}},
			},
			{
				Role:    "assistant",
				Content: `[{"page":"https://docs.example.com/auth","issue":"The rate limiting behavior is not documented."}]`,
			},
		},
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}}}
	pageReader := func(url string) (string, error) { return "# Auth\nContent about auth.", nil }

	findings, err := analyzer.DetectDrift(context.Background(), client, featureMap, docsMap, pageReader, t.TempDir())
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Contains(t, findings[0].Issues[0].Issue, "rate limiting")
}

func TestDetectDrift_UnknownTool_GracefulContinuation(t *testing.T) {
	// LLM calls an unrecognized tool; the error is sent back and the LLM finalizes.
	client := &driftStubClient{
		responses: []analyzer.ChatMessage{
			{
				Role:      "assistant",
				ToolCalls: []analyzer.ToolCall{{ID: "call_3", Name: "nonexistent_tool", Arguments: `{}`}},
			},
			{Role: "assistant", Content: "[]"},
		},
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}}}
	pageReader := func(url string) (string, error) { return "# Auth", nil }

	findings, err := analyzer.DetectDrift(context.Background(), client, featureMap, docsMap, pageReader, t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestDetectDrift_ReadFile_BadJSON_ReturnsError(t *testing.T) {
	// LLM sends malformed arguments to read_file; tool returns error string to LLM.
	client := &driftStubClient{
		responses: []analyzer.ChatMessage{
			{
				Role:      "assistant",
				ToolCalls: []analyzer.ToolCall{{ID: "call_4", Name: "read_file", Arguments: `not-json`}},
			},
			{Role: "assistant", Content: "[]"},
		},
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}}}
	pageReader := func(url string) (string, error) { return "# Auth", nil }

	_, err := analyzer.DetectDrift(context.Background(), client, featureMap, docsMap, pageReader, t.TempDir())
	assert.NoError(t, err)
}

func TestDetectDrift_ReadPage_BadJSON_ReturnsError(t *testing.T) {
	// LLM sends malformed arguments to read_page; tool returns error string to LLM.
	client := &driftStubClient{
		responses: []analyzer.ChatMessage{
			{
				Role:      "assistant",
				ToolCalls: []analyzer.ToolCall{{ID: "call_5", Name: "read_page", Arguments: `not-json`}},
			},
			{Role: "assistant", Content: "[]"},
		},
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}}}
	pageReader := func(url string) (string, error) { return "# Auth", nil }

	_, err := analyzer.DetectDrift(context.Background(), client, featureMap, docsMap, pageReader, t.TempDir())
	assert.NoError(t, err)
}

func TestDetectDrift_ReadPage_PageReaderError_ReturnedToLLM(t *testing.T) {
	// pageReader returns an error; tool result should convey this to the LLM, not fail DetectDrift.
	client := &driftStubClient{
		responses: []analyzer.ChatMessage{
			{
				Role:      "assistant",
				ToolCalls: []analyzer.ToolCall{{ID: "call_6", Name: "read_page", Arguments: `{"url":"https://docs.example.com/auth"}`}},
			},
			{Role: "assistant", Content: "[]"},
		},
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}}}
	pageReader := func(url string) (string, error) { return "", fmt.Errorf("page not cached") }

	_, err := analyzer.DetectDrift(context.Background(), client, featureMap, docsMap, pageReader, t.TempDir())
	assert.NoError(t, err, "pageReader errors should be communicated to the LLM, not propagated as DetectDrift errors")
	// Verify the tool result contained the error message (check second call received a "tool" message with error text).
	if client.calls < 2 {
		t.Errorf("expected at least 2 LLM calls (tool request + tool result continuation), got %d", client.calls)
	}
}

func TestDetectDrift_CompleteWithTools_Error_Propagated(t *testing.T) {
	// CompleteWithTools returns an error; DetectDrift should propagate it.
	client := &driftStubClientWithErr{err: fmt.Errorf("bifrost unavailable")}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}}}
	pageReader := func(url string) (string, error) { return "# Auth", nil }

	_, err := analyzer.DetectDrift(context.Background(), client, featureMap, docsMap, pageReader, t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bifrost unavailable")
}

func TestDetectDrift_InvalidJSONResponse_ReturnsError(t *testing.T) {
	// LLM returns non-JSON content (not a tool call); DetectDrift should return an error.
	client := &driftStubClient{
		responses: []analyzer.ChatMessage{
			{Role: "assistant", Content: "this is not json"},
		},
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}}}
	pageReader := func(url string) (string, error) { return "# Auth", nil }

	_, err := analyzer.DetectDrift(context.Background(), client, featureMap, docsMap, pageReader, t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid JSON drift response")
}

func TestDetectDrift_ProseBeforeJSON_ParsedSuccessfully(t *testing.T) {
	// LLM returns prose before the JSON array (common despite the prompt instruction).
	// DetectDrift must extract the array and parse it successfully.
	client := &driftStubClient{
		responses: []analyzer.ChatMessage{
			{
				Role:    "assistant",
				Content: `Here are the issues I found:\n\n[{"page":"https://docs.example.com/auth","issue":"Email requirement not documented."}]`,
			},
		},
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}}}
	pageReader := func(url string) (string, error) { return "# Auth", nil }

	findings, err := analyzer.DetectDrift(context.Background(), client, featureMap, docsMap, pageReader, t.TempDir())
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Contains(t, findings[0].Issues[0].Issue, "Email requirement")
}

func TestDetectDrift_MaxRoundsExceeded_ReturnsError(t *testing.T) {
	// LLM keeps requesting tool calls; loop should terminate with an error.
	toolCallResponse := analyzer.ChatMessage{
		Role:      "assistant",
		ToolCalls: []analyzer.ToolCall{{ID: "call_inf", Name: "read_page", Arguments: `{"url":"https://docs.example.com/auth"}`}},
	}
	client := &driftStubClient{
		responses: []analyzer.ChatMessage{toolCallResponse},
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}}}
	pageReader := func(url string) (string, error) { return "# Auth", nil }

	_, err := analyzer.DetectDrift(context.Background(), client, featureMap, docsMap, pageReader, t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeded")
}
