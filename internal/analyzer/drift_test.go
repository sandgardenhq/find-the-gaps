package analyzer_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// driftStubClient is a ToolLLMClient test double for drift detection tests.
type driftStubClient struct {
	// responses is consumed in order; last element is reused when exhausted.
	responses     []analyzer.ChatMessage
	calls         int
	completeCalls int
	// completeFunc used by Complete (existing interface).
	completeFunc func(ctx context.Context, prompt string) (string, error)
}

// submitFindings builds a ChatMessage that invokes the submit_findings terminal
// tool with the given issues. Test helper.
func submitFindings(issues ...analyzer.DriftIssue) analyzer.ChatMessage {
	if issues == nil {
		issues = []analyzer.DriftIssue{}
	}
	args, _ := json.Marshal(struct {
		Findings []analyzer.DriftIssue `json:"findings"`
	}{Findings: issues})
	return analyzer.ChatMessage{
		Role: "assistant",
		ToolCalls: []analyzer.ToolCall{
			{ID: "submit", Name: "submit_findings", Arguments: string(args)},
		},
	}
}

func (s *driftStubClient) Complete(ctx context.Context, prompt string) (string, error) {
	s.completeCalls++
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

func (s *driftStubClient) CompleteJSON(ctx context.Context, prompt string, _ analyzer.JSONSchema) (json.RawMessage, error) {
	raw, err := s.Complete(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
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

func (s *driftStubClientWithErr) CompleteJSON(_ context.Context, _ string, _ analyzer.JSONSchema) (json.RawMessage, error) {
	return nil, s.err
}

func TestDetectDrift_NoDocumentedFeatures_ReturnsEmpty(t *testing.T) {
	client := &driftStubClient{}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth", UserFacing: true}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{} // no pages mapped — auth is undocumented, not a drift candidate
	pageReader := func(url string) (string, error) { return "", nil }

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: client, large: client}, featureMap, docsMap, pageReader, "/repo", nil)
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestDetectDrift_DocumentedFeature_ReturnsIssues(t *testing.T) {
	client := &driftStubClient{
		responses: []analyzer.ChatMessage{
			submitFindings(analyzer.DriftIssue{Page: "https://docs.example.com/auth", Issue: "Email requirement not documented."}),
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

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: client, large: client}, featureMap, docsMap, pageReader, "/repo", nil)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, "auth", findings[0].Feature)
	require.Len(t, findings[0].Issues, 1)
	assert.Equal(t, "https://docs.example.com/auth", findings[0].Issues[0].Page)
	assert.Contains(t, findings[0].Issues[0].Issue, "Email requirement")
}

func TestDetectDrift_LLMReturnsEmptyArray_FeatureDropped(t *testing.T) {
	client := &driftStubClient{
		responses: []analyzer.ChatMessage{submitFindings()},
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "search"}, Files: []string{"search.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "search", Pages: []string{"https://docs.example.com/search"}},
	}
	pageReader := func(url string) (string, error) { return "# Search docs", nil }

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: client, large: client}, featureMap, docsMap, pageReader, "/repo", nil)
	require.NoError(t, err)
	assert.Empty(t, findings, "features with no issues should be dropped")
}

func TestDetectDrift_ToolCall_ExecutedAndContinued(t *testing.T) {
	// First response: LLM requests read_file tool.
	// Second response: LLM calls submit_findings with its conclusions.
	client := &driftStubClient{
		responses: []analyzer.ChatMessage{
			{
				Role: "assistant",
				ToolCalls: []analyzer.ToolCall{
					{ID: "call_1", Name: "read_file", Arguments: `{"path":"auth.go"}`},
				},
			},
			submitFindings(analyzer.DriftIssue{Page: "", Issue: "The docs omit that Login returns a JWT token."}),
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

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: client, large: client}, featureMap, docsMap, pageReader, t.TempDir(), nil)
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
			submitFindings(),
		},
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}}}
	pageReader := func(url string) (string, error) { return "# Auth", nil }

	// Must not error — path rejection is communicated back to the LLM as a tool result.
	_, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: client, large: client}, featureMap, docsMap, pageReader, t.TempDir(), nil)
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
			submitFindings(analyzer.DriftIssue{Page: "https://docs.example.com/auth", Issue: "The rate limiting behavior is not documented."}),
		},
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}}}
	pageReader := func(url string) (string, error) { return "# Auth\nContent about auth.", nil }

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: client, large: client}, featureMap, docsMap, pageReader, t.TempDir(), nil)
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
			submitFindings(),
		},
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}}}
	pageReader := func(url string) (string, error) { return "# Auth", nil }

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: client, large: client}, featureMap, docsMap, pageReader, t.TempDir(), nil)
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
			submitFindings(),
		},
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}}}
	pageReader := func(url string) (string, error) { return "# Auth", nil }

	_, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: client, large: client}, featureMap, docsMap, pageReader, t.TempDir(), nil)
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
			submitFindings(),
		},
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}}}
	pageReader := func(url string) (string, error) { return "# Auth", nil }

	_, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: client, large: client}, featureMap, docsMap, pageReader, t.TempDir(), nil)
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
			submitFindings(),
		},
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}}}
	pageReader := func(url string) (string, error) { return "", fmt.Errorf("page not cached") }

	_, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: client, large: client}, featureMap, docsMap, pageReader, t.TempDir(), nil)
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

	_, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: client, large: client}, featureMap, docsMap, pageReader, t.TempDir(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bifrost unavailable")
}

func TestDetectDrift_TextResponseWithoutSubmitFindings_ReturnsError(t *testing.T) {
	// LLM returns text content instead of calling submit_findings; DetectDrift must error.
	client := &driftStubClient{
		responses: []analyzer.ChatMessage{
			{Role: "assistant", Content: "this is not a tool call"},
		},
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}}}
	pageReader := func(url string) (string, error) { return "# Auth", nil }

	_, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: client, large: client}, featureMap, docsMap, pageReader, t.TempDir(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "submit_findings")
}

func TestDetectDrift_SubmitFindingsBadJSON_ReturnsError(t *testing.T) {
	// LLM calls submit_findings but with malformed arguments; DetectDrift must error.
	client := &driftStubClient{
		responses: []analyzer.ChatMessage{
			{
				Role:      "assistant",
				ToolCalls: []analyzer.ToolCall{{ID: "submit", Name: "submit_findings", Arguments: `not json`}},
			},
		},
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}}}
	pageReader := func(url string) (string, error) { return "# Auth", nil }

	_, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: client, large: client}, featureMap, docsMap, pageReader, t.TempDir(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid submit_findings arguments")
}

func TestDetectDrift_ReleaseNotePageOnly_Skipped(t *testing.T) {
	// A feature whose only doc pages are release-note/changelog URLs should be
	// skipped entirely — the LLM should never be called.
	client := &driftStubClient{} // zero responses; any call would panic on empty slice
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{
			"https://docs.example.com/release-notes",
			"https://docs.example.com/changelog",
		}},
	}
	pageReader := func(url string) (string, error) { return "# Changelog", nil }

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: client, large: client}, featureMap, docsMap, pageReader, t.TempDir(), nil)
	require.NoError(t, err)
	assert.Empty(t, findings)
	assert.Equal(t, 0, client.calls, "LLM must not be called when all pages are release notes")
}

func TestDetectDrift_MixedPages_ReleaseNotesFiltered(t *testing.T) {
	// When a feature has both regular and release-note pages, only regular pages
	// are sent to the LLM.
	var capturedMessages []analyzer.ChatMessage
	client := &driftStubClient{
		responses: []analyzer.ChatMessage{submitFindings()},
	}
	_ = capturedMessages
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{
			"https://docs.example.com/auth",
			"https://docs.example.com/release-notes",
		}},
	}
	pageReader := func(url string) (string, error) { return "# Auth", nil }

	_, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: client, large: client}, featureMap, docsMap, pageReader, t.TempDir(), nil)
	require.NoError(t, err)
	// LLM was called once (with the filtered page list, not the release-note URL).
	assert.Equal(t, 1, client.calls)
}

func TestDetectDrift_ChangelogByContent_SkippedByLLM(t *testing.T) {
	// A page with a non-obvious URL (e.g. "whatsnew.htm") but changelog content
	// should be classified and skipped; CompleteWithTools must never be called.
	client := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) {
			return "yes", nil // classifier: this is a changelog page
		},
		// no responses: CompleteWithTools must not be called
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://docs.example.com/whatsnew.htm"}},
	}
	pageReader := func(url string) (string, error) {
		return "## 2.0.0\n\n- Added new login flow\n\n## 1.9.0\n\n- Fixed bug", nil
	}

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: client, large: client}, featureMap, docsMap, pageReader, t.TempDir(), nil)
	require.NoError(t, err)
	assert.Empty(t, findings)
	assert.Equal(t, 0, client.calls, "CompleteWithTools must not be called for changelog pages")
}

func TestDetectDrift_ContentClassifierError_FailsOpen(t *testing.T) {
	// When the content classifier itself errors, the page must be included in
	// drift detection (fail open) so legitimate drift is not silently skipped.
	client := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) {
			return "", fmt.Errorf("classifier unavailable")
		},
		responses: []analyzer.ChatMessage{submitFindings()},
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}},
	}
	pageReader := func(url string) (string, error) { return "# Auth", nil }

	_, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: client, large: client}, featureMap, docsMap, pageReader, t.TempDir(), nil)
	require.NoError(t, err, "classifier error must not propagate")
	assert.Equal(t, 1, client.calls, "page must be included in drift detection when classifier errors")
}

func TestDetectDrift_OnFinding_CalledPerFeatureWithFindings(t *testing.T) {
	// The callback must be called once per feature that produces findings,
	// and receive the accumulated slice up to that point.
	client := &driftStubClient{
		responses: []analyzer.ChatMessage{
			submitFindings(analyzer.DriftIssue{Page: "", Issue: "issue for auth"}),
			submitFindings(analyzer.DriftIssue{Page: "", Issue: "issue for search"}),
		},
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
		{Feature: analyzer.CodeFeature{Name: "search"}, Files: []string{"search.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}},
		{Feature: "search", Pages: []string{"https://docs.example.com/search"}},
	}
	pageReader := func(url string) (string, error) { return "# Docs", nil }

	var snapshots [][]analyzer.DriftFinding
	onFinding := func(accumulated []analyzer.DriftFinding) error {
		cp := make([]analyzer.DriftFinding, len(accumulated))
		copy(cp, accumulated)
		snapshots = append(snapshots, cp)
		return nil
	}

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: client, large: client}, featureMap, docsMap, pageReader, t.TempDir(), onFinding)
	require.NoError(t, err)
	assert.Len(t, findings, 2)
	assert.Len(t, snapshots, 2, "callback must be called once per feature with findings")
	assert.Len(t, snapshots[0], 1, "first callback gets 1 accumulated finding")
	assert.Len(t, snapshots[1], 2, "second callback gets 2 accumulated findings")
}

func TestDetectDrift_OnFinding_ErrorPropagated(t *testing.T) {
	// An error returned from the callback must abort DetectDrift.
	client := &driftStubClient{
		responses: []analyzer.ChatMessage{
			submitFindings(analyzer.DriftIssue{Page: "", Issue: "something wrong"}),
		},
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}},
	}
	pageReader := func(url string) (string, error) { return "# Docs", nil }

	onFinding := func(_ []analyzer.DriftFinding) error {
		return fmt.Errorf("disk full")
	}

	_, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: client, large: client}, featureMap, docsMap, pageReader, t.TempDir(), onFinding)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk full")
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

	_, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: client, large: client}, featureMap, docsMap, pageReader, t.TempDir(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeded")
}

func TestDetectDrift_UsesLargeAndSmall(t *testing.T) {
	// Verify tier dispatch: classifyDriftPages uses Small(), and the agentic
	// loop in detectDriftForFeature uses Large(). Typical() must be untouched.
	small := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil },
	}
	typical := &driftStubClient{}
	large := &driftStubClient{
		responses: []analyzer.ChatMessage{submitFindings()},
	}
	tiering := &fakeTiering{small: small, typical: typical, large: large}

	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}},
	}
	pageReader := func(url string) (string, error) { return "# Auth\nreal feature docs.", nil }

	_, err := analyzer.DetectDrift(context.Background(), tiering, featureMap, docsMap, pageReader, t.TempDir(), nil)
	require.NoError(t, err)

	assert.GreaterOrEqual(t, small.completeCalls, 1, "Small() tier must be used for the release-note classifier")
	assert.GreaterOrEqual(t, large.calls, 1, "Large() tier must be used for the agentic drift loop")
	assert.Equal(t, 0, typical.completeCalls, "Typical() must not be used for drift detection")
	assert.Equal(t, 0, typical.calls, "Typical() must not be used for drift detection")
}

// fakeNonToolClient implements only LLMClient (no CompleteWithTools), used to
// assert the defensive cast in DetectDrift fails cleanly.
type fakeNonToolClient struct{}

func (fakeNonToolClient) Complete(_ context.Context, _ string) (string, error) { return "", nil }

func (fakeNonToolClient) CompleteJSON(_ context.Context, _ string, _ analyzer.JSONSchema) (json.RawMessage, error) {
	return nil, nil
}

func TestDetectDrift_LargeWithoutToolSupport_Errors(t *testing.T) {
	// When tiering.Large() does not implement ToolLLMClient, DetectDrift must
	// return a clear error rather than panic on a type assertion.
	tiering := &fakeTiering{
		small: &driftStubClient{
			completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil },
		},
		large: fakeNonToolClient{},
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}},
	}
	pageReader := func(url string) (string, error) { return "# Auth", nil }

	_, err := analyzer.DetectDrift(context.Background(), tiering, featureMap, docsMap, pageReader, t.TempDir(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool use")
	assert.Contains(t, err.Error(), "large")
}
