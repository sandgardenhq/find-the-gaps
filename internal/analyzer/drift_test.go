package analyzer_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

// addFinding builds a ChatMessage that invokes the add_finding tool with one
// drift issue. Test helper retained because the legacy add_finding tool is
// still defined on drift.go (Task 5 deletes it). New tests use noteObservation.
//
//nolint:unused // paired with addFindingTool; deleted in the next refactor task
func addFinding(issue analyzer.DriftIssue) analyzer.ChatMessage {
	args, _ := json.Marshal(issue)
	return analyzer.ChatMessage{
		Role: "assistant",
		ToolCalls: []analyzer.ToolCall{
			{ID: "add_" + issue.Issue, Name: "add_finding", Arguments: string(args)},
		},
	}
}

// noteObservation builds a ChatMessage that invokes note_observation with one
// observation. Test helper.
func noteObservation(page, docQuote, codeQuote, concern string) analyzer.ChatMessage {
	args, _ := json.Marshal(map[string]string{
		"page":       page,
		"doc_quote":  docQuote,
		"code_quote": codeQuote,
		"concern":    concern,
	})
	return analyzer.ChatMessage{
		Role: "assistant",
		ToolCalls: []analyzer.ToolCall{
			{ID: "obs_" + concern, Name: "note_observation", Arguments: string(args)},
		},
	}
}

// driftDone builds a plain-text assistant message that terminates the agent
// loop (no tool calls). Test helper.
func driftDone() analyzer.ChatMessage {
	return analyzer.ChatMessage{Role: "assistant", Content: "done"}
}

// judgeJSON builds a completeFunc that returns one DriftIssue with the given
// page and issue text wrapped in the judge's JSON envelope. Test helper for
// scripting a judge stub on the Large tier.
func judgeJSON(page, issue string) func(context.Context, string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"issues": []map[string]string{{"page": page, "issue": issue}},
	})
	return func(_ context.Context, _ string) (string, error) {
		return string(body), nil
	}
}

func (s *driftStubClient) Complete(ctx context.Context, prompt string) (string, error) {
	s.completeCalls++
	if s.completeFunc != nil {
		return s.completeFunc(ctx, prompt)
	}
	return "", nil
}

func (s *driftStubClient) CompleteWithTools(ctx context.Context, msgs []analyzer.ChatMessage, tools []analyzer.Tool, opts ...analyzer.AgentOption) (analyzer.AgentResult, error) {
	next := func(_ context.Context, _ []analyzer.ChatMessage, _ []analyzer.Tool) (analyzer.ChatMessage, error) {
		idx := s.calls
		if idx >= len(s.responses) {
			idx = len(s.responses) - 1
		}
		s.calls++
		return s.responses[idx], nil
	}
	return analyzer.RunAgentLoop(ctx, next, msgs, tools, opts...)
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

func (s *driftStubClientWithErr) CompleteWithTools(_ context.Context, _ []analyzer.ChatMessage, _ []analyzer.Tool, _ ...analyzer.AgentOption) (analyzer.AgentResult, error) {
	return analyzer.AgentResult{}, s.err
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

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: client, typical: client, large: client}, featureMap, docsMap, pageReader, "/repo", nil)
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestDetectDrift_DocumentedFeature_ReturnsIssues(t *testing.T) {
	typical := &driftStubClient{
		responses: []analyzer.ChatMessage{
			noteObservation("https://docs.example.com/auth", "Login with username", "Login(email, password) error", "docs omit email"),
			driftDone(),
		},
	}
	large := &driftStubClient{
		completeFunc: judgeJSON("https://docs.example.com/auth", "Email requirement not documented."),
	}
	small := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil },
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

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, "/repo", nil)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, "auth", findings[0].Feature)
	require.Len(t, findings[0].Issues, 1)
	assert.Equal(t, "https://docs.example.com/auth", findings[0].Issues[0].Page)
	assert.Contains(t, findings[0].Issues[0].Issue, "Email requirement")
}

func TestDetectDrift_LLMReturnsEmptyArray_FeatureDropped(t *testing.T) {
	// Investigator emits zero observations — the feature produces zero findings,
	// the judge must NOT be invoked, and the feature is dropped.
	typical := &driftStubClient{
		responses: []analyzer.ChatMessage{driftDone()},
	}
	large := &driftStubClient{} // any call would increment completeCalls
	small := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil },
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "search"}, Files: []string{"search.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "search", Pages: []string{"https://docs.example.com/search"}},
	}
	pageReader := func(url string) (string, error) { return "# Search docs", nil }

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, "/repo", nil)
	require.NoError(t, err)
	assert.Empty(t, findings, "features with no issues should be dropped")
	assert.Equal(t, 0, large.completeCalls, "judge must be skipped when investigator emits zero observations")
}

func TestDetectDrift_ToolCall_ExecutedAndContinued(t *testing.T) {
	// First response: investigator requests read_file. Second: note_observation.
	// Third: driftDone. Then judge runs once on the Large tier.
	typical := &driftStubClient{
		responses: []analyzer.ChatMessage{
			{
				Role: "assistant",
				ToolCalls: []analyzer.ToolCall{
					{ID: "call_1", Name: "read_file", Arguments: `{"path":"auth.go"}`},
				},
			},
			noteObservation("", "docs say nothing about JWT", "func Login(...) (jwt string, error)", "docs omit JWT return"),
			driftDone(),
		},
	}
	large := &driftStubClient{
		completeFunc: judgeJSON("", "The docs omit that Login returns a JWT token."),
	}
	small := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil },
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

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), nil)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Contains(t, findings[0].Issues[0].Issue, "JWT token")
}

func TestDetectDrift_ReadFile_OutsideRepo_ReturnsError(t *testing.T) {
	// LLM requests a path that escapes the repo root — tool should return an
	// error message to the LLM, not panic or expose files.
	typical := &driftStubClient{
		responses: []analyzer.ChatMessage{
			{
				Role:      "assistant",
				ToolCalls: []analyzer.ToolCall{{ID: "call_1", Name: "read_file", Arguments: `{"path":"../../../etc/passwd"}`}},
			},
			driftDone(),
		},
	}
	large := &driftStubClient{}
	small := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil },
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}}}
	pageReader := func(url string) (string, error) { return "# Auth", nil }

	// Must not error — path rejection is communicated back to the LLM as a tool result.
	_, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), nil)
	assert.NoError(t, err)
}

func TestDetectDrift_ReadPage_ToolCall_ExecutedAndContinued(t *testing.T) {
	// LLM requests the read_page tool, receives content, records an observation,
	// then ends. Judge produces one issue.
	typical := &driftStubClient{
		responses: []analyzer.ChatMessage{
			{
				Role:      "assistant",
				ToolCalls: []analyzer.ToolCall{{ID: "call_2", Name: "read_page", Arguments: `{"url":"https://docs.example.com/auth"}`}},
			},
			noteObservation("https://docs.example.com/auth", "no rate limit info", "code enforces 100/min", "rate limit undocumented"),
			driftDone(),
		},
	}
	large := &driftStubClient{
		completeFunc: judgeJSON("https://docs.example.com/auth", "The rate limiting behavior is not documented."),
	}
	small := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil },
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}}}
	pageReader := func(url string) (string, error) { return "# Auth\nContent about auth.", nil }

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), nil)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Contains(t, findings[0].Issues[0].Issue, "rate limiting")
}

func TestDetectDrift_UnknownTool_GracefulContinuation(t *testing.T) {
	// LLM calls an unrecognized tool; the error is sent back and the LLM finalizes.
	typical := &driftStubClient{
		responses: []analyzer.ChatMessage{
			{
				Role:      "assistant",
				ToolCalls: []analyzer.ToolCall{{ID: "call_3", Name: "nonexistent_tool", Arguments: `{}`}},
			},
			driftDone(),
		},
	}
	large := &driftStubClient{}
	small := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil },
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}}}
	pageReader := func(url string) (string, error) { return "# Auth", nil }

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), nil)
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestDetectDrift_ReadFile_BadJSON_ReturnsError(t *testing.T) {
	// LLM sends malformed arguments to read_file; tool returns error string to LLM.
	typical := &driftStubClient{
		responses: []analyzer.ChatMessage{
			{
				Role:      "assistant",
				ToolCalls: []analyzer.ToolCall{{ID: "call_4", Name: "read_file", Arguments: `not-json`}},
			},
			driftDone(),
		},
	}
	large := &driftStubClient{}
	small := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil },
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}}}
	pageReader := func(url string) (string, error) { return "# Auth", nil }

	_, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), nil)
	assert.NoError(t, err)
}

func TestDetectDrift_ReadPage_BadJSON_ReturnsError(t *testing.T) {
	// LLM sends malformed arguments to read_page; tool returns error string to LLM.
	typical := &driftStubClient{
		responses: []analyzer.ChatMessage{
			{
				Role:      "assistant",
				ToolCalls: []analyzer.ToolCall{{ID: "call_5", Name: "read_page", Arguments: `not-json`}},
			},
			driftDone(),
		},
	}
	large := &driftStubClient{}
	small := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil },
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}}}
	pageReader := func(url string) (string, error) { return "# Auth", nil }

	_, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), nil)
	assert.NoError(t, err)
}

func TestDetectDrift_ReadPage_PageReaderError_ReturnedToLLM(t *testing.T) {
	// pageReader returns an error; tool result should convey this to the LLM, not fail DetectDrift.
	typical := &driftStubClient{
		responses: []analyzer.ChatMessage{
			{
				Role:      "assistant",
				ToolCalls: []analyzer.ToolCall{{ID: "call_6", Name: "read_page", Arguments: `{"url":"https://docs.example.com/auth"}`}},
			},
			driftDone(),
		},
	}
	large := &driftStubClient{}
	small := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil },
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}}}
	pageReader := func(url string) (string, error) {
		if url == "https://docs.example.com/auth" {
			return "", fmt.Errorf("page not cached")
		}
		// Allow the classifier to read the page (separate call path).
		return "# Auth", nil
	}

	_, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), nil)
	assert.NoError(t, err, "pageReader errors should be communicated to the LLM, not propagated as DetectDrift errors")
	// Verify the investigator was called multiple rounds (tool request + tool result continuation).
	if typical.calls < 2 {
		t.Errorf("expected at least 2 investigator LLM calls (tool request + tool result continuation), got %d", typical.calls)
	}
}

func TestDetectDrift_CompleteWithTools_Error_Propagated(t *testing.T) {
	// CompleteWithTools returns an error; DetectDrift should propagate it.
	client := &driftStubClientWithErr{err: fmt.Errorf("bifrost unavailable")}
	small := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil },
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}}}
	pageReader := func(url string) (string, error) { return "# Auth", nil }

	_, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: client, large: client}, featureMap, docsMap, pageReader, t.TempDir(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bifrost unavailable")
}

func TestDetectDrift_NoteObservationBadJSON_FedBackToLLM(t *testing.T) {
	// Investigator calls note_observation with malformed arguments. The tool
	// must report the parse error back to the LLM as a tool result string and
	// the loop must continue — DetectDrift must NOT error, no observation is
	// recorded, the judge is skipped, and findings stay empty.
	typical := &driftStubClient{
		responses: []analyzer.ChatMessage{
			{
				Role:      "assistant",
				ToolCalls: []analyzer.ToolCall{{ID: "obs_bad", Name: "note_observation", Arguments: `not json`}},
			},
			driftDone(),
		},
	}
	large := &driftStubClient{}
	small := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil },
	}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}}}
	pageReader := func(url string) (string, error) { return "# Auth", nil }

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), nil)
	require.NoError(t, err, "bad note_observation args should be communicated to the LLM, not propagated as DetectDrift errors")
	assert.Empty(t, findings, "malformed note_observation payload must not be recorded")
	assert.GreaterOrEqual(t, typical.calls, 2, "loop must continue after bad-args feedback")
	assert.Equal(t, 0, large.completeCalls, "judge must not be called when no observations are recorded")
}

func TestDetectDrift_ReleaseNotePageOnly_Skipped(t *testing.T) {
	// A feature whose only doc pages are release-note/changelog URLs should be
	// skipped entirely — neither investigator nor judge is called.
	typical := &driftStubClient{} // zero responses; any call would panic on empty slice
	large := &driftStubClient{}
	small := &driftStubClient{}
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

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), nil)
	require.NoError(t, err)
	assert.Empty(t, findings)
	assert.Equal(t, 0, typical.calls, "investigator must not be called when all pages are release notes")
	assert.Equal(t, 0, large.completeCalls, "judge must not be called when all pages are release notes")
}

func TestDetectDrift_MixedPages_ReleaseNotesFiltered(t *testing.T) {
	// When a feature has both regular and release-note pages, only regular pages
	// reach the investigator.
	typical := &driftStubClient{
		responses: []analyzer.ChatMessage{driftDone()},
	}
	large := &driftStubClient{}
	small := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil },
	}
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

	_, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), nil)
	require.NoError(t, err)
	// Investigator was called once (with the filtered page list, not the release-note URL).
	assert.Equal(t, 1, typical.calls)
}

func TestDetectDrift_ChangelogByContent_SkippedByLLM(t *testing.T) {
	// A page with a non-obvious URL (e.g. "whatsnew.htm") but changelog content
	// should be classified and skipped; investigator and judge must never be called.
	small := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) {
			return "yes", nil // classifier: this is a changelog page
		},
	}
	typical := &driftStubClient{} // no responses: investigator must not be called
	large := &driftStubClient{}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://docs.example.com/whatsnew.htm"}},
	}
	pageReader := func(url string) (string, error) {
		return "## 2.0.0\n\n- Added new login flow\n\n## 1.9.0\n\n- Fixed bug", nil
	}

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), nil)
	require.NoError(t, err)
	assert.Empty(t, findings)
	assert.Equal(t, 0, typical.calls, "investigator must not be called for changelog pages")
	assert.Equal(t, 0, large.completeCalls, "judge must not be called for changelog pages")
}

func TestDetectDrift_ContentClassifierError_FailsOpen(t *testing.T) {
	// When the content classifier itself errors, the page must be included in
	// drift detection (fail open) so legitimate drift is not silently skipped.
	small := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) {
			return "", fmt.Errorf("classifier unavailable")
		},
	}
	typical := &driftStubClient{
		responses: []analyzer.ChatMessage{driftDone()},
	}
	large := &driftStubClient{}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}},
	}
	pageReader := func(url string) (string, error) { return "# Auth", nil }

	_, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), nil)
	require.NoError(t, err, "classifier error must not propagate")
	assert.Equal(t, 1, typical.calls, "page must be included in drift detection when classifier errors")
}

func TestDetectDrift_OnFinding_CalledPerFeatureWithFindings(t *testing.T) {
	// The callback must be called once per feature that produces findings,
	// and receive the accumulated slice up to that point.
	typical := &driftStubClient{
		responses: []analyzer.ChatMessage{
			noteObservation("", "doc A", "code A", "issue for auth"),
			driftDone(),
			noteObservation("", "doc B", "code B", "issue for search"),
			driftDone(),
		},
	}
	// Judge is called per feature; alternate canned responses by tracking which
	// feature is being adjudicated via the prompt.
	large := &driftStubClient{
		completeFunc: func(_ context.Context, prompt string) (string, error) {
			if strings.Contains(prompt, "auth") && !strings.Contains(prompt, "search") {
				body, _ := json.Marshal(map[string]any{
					"issues": []map[string]string{{"page": "", "issue": "issue for auth"}},
				})
				return string(body), nil
			}
			body, _ := json.Marshal(map[string]any{
				"issues": []map[string]string{{"page": "", "issue": "issue for search"}},
			})
			return string(body), nil
		},
	}
	small := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil },
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

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), onFinding)
	require.NoError(t, err)
	assert.Len(t, findings, 2)
	assert.Len(t, snapshots, 2, "callback must be called once per feature with findings")
	assert.Len(t, snapshots[0], 1, "first callback gets 1 accumulated finding")
	assert.Len(t, snapshots[1], 2, "second callback gets 2 accumulated findings")
}

func TestDetectDrift_OnFinding_ErrorPropagated(t *testing.T) {
	// An error returned from the callback must abort DetectDrift.
	typical := &driftStubClient{
		responses: []analyzer.ChatMessage{
			noteObservation("", "doc", "code", "something wrong"),
			driftDone(),
		},
	}
	large := &driftStubClient{
		completeFunc: judgeJSON("", "something wrong"),
	}
	small := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil },
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

	_, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), onFinding)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk full")
}

func TestDetectDrift_MaxRoundsExceeded_PartialFindingsReturnedAndContinues(t *testing.T) {
	// Feature "auth": investigator records one observation, then loops forever
	// with read_page tool calls until max rounds is reached.
	// Feature "search": investigator records one observation and replies with text.
	// DetectDrift must NOT error when feature "auth" hits max rounds — and
	// CRUCIALLY, the partial observation accumulated before exhaustion must be
	// handed to the judge, not discarded. Then "search" must still be processed.
	toolCallResponse := analyzer.ChatMessage{
		Role:      "assistant",
		ToolCalls: []analyzer.ToolCall{{ID: "call_inf", Name: "read_page", Arguments: `{"url":"https://docs.example.com/auth"}`}},
	}
	// Feature "auth": 1 note_observation (round 1) + 29 tool calls (rounds 2..30),
	// exhausting the budget after round 30 with one accumulated observation.
	// Feature "search": 1 note_observation + 1 driftDone (loop exits cleanly).
	responses := make([]analyzer.ChatMessage, 0, 32)
	responses = append(responses, noteObservation("", "doc says auth", "code does auth", "partial auth issue"))
	for i := 0; i < 29; i++ {
		responses = append(responses, toolCallResponse)
	}
	responses = append(responses, noteObservation("", "doc says search", "code does search", "issue for search"))
	responses = append(responses, driftDone())
	typical := &driftStubClient{responses: responses}
	large := &driftStubClient{
		completeFunc: func(_ context.Context, prompt string) (string, error) {
			if strings.Contains(prompt, "auth") && !strings.Contains(prompt, "search") {
				return string(mustJSON(map[string]any{
					"issues": []map[string]string{{"page": "", "issue": "partial auth issue"}},
				})), nil
			}
			return string(mustJSON(map[string]any{
				"issues": []map[string]string{{"page": "", "issue": "issue for search"}},
			})), nil
		},
	}
	small := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil },
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

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), nil)
	require.NoError(t, err, "max-rounds exhaustion must not terminate DetectDrift")
	require.Len(t, findings, 2, "partially-accumulated observations from the timed-out feature must still be judged, and the next feature must still be processed")
	assert.Equal(t, "auth", findings[0].Feature)
	require.Len(t, findings[0].Issues, 1)
	assert.Contains(t, findings[0].Issues[0].Issue, "partial auth issue")
	assert.Equal(t, "search", findings[1].Feature)
	assert.Contains(t, findings[1].Issues[0].Issue, "issue for search")
}

// mustJSON marshals v to JSON, panicking on error. Test helper for inline
// canned responses that are guaranteed to be valid.
func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func TestDetectDrift_UsesSmallTypicalLarge(t *testing.T) {
	// Verify tier dispatch:
	//   classifyDriftPages    -> Small
	//   investigator agent    -> Typical
	//   judge CompleteJSON    -> Large
	small := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil },
	}
	typical := &driftStubClient{
		responses: []analyzer.ChatMessage{
			noteObservation("https://docs/x", "doc", "code", "mismatch"),
			driftDone(),
		},
	}
	large := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) {
			return `{"issues":[{"page":"https://docs/x","issue":"docs are stale"}]}`, nil
		},
	}
	tiering := &fakeTiering{small: small, typical: typical, large: large}

	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://docs/x"}},
	}
	pageReader := func(url string) (string, error) { return "# Auth\nreal docs.", nil }

	findings, err := analyzer.DetectDrift(context.Background(), tiering, featureMap, docsMap, pageReader, t.TempDir(), nil)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Len(t, findings[0].Issues, 1)
	assert.Equal(t, "docs are stale", findings[0].Issues[0].Issue)

	assert.GreaterOrEqual(t, small.completeCalls, 1, "Small must classify pages")
	assert.GreaterOrEqual(t, typical.calls, 1, "Typical must run the investigator agent loop")
	assert.GreaterOrEqual(t, large.completeCalls, 1, "Large must run the judge CompleteJSON call")
}

func TestDetectDrift_NoObservations_SkipsJudge(t *testing.T) {
	small := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil },
	}
	typical := &driftStubClient{
		responses: []analyzer.ChatMessage{driftDone()}, // investigator records nothing
	}
	large := &driftStubClient{} // any call would increment completeCalls
	tiering := &fakeTiering{small: small, typical: typical, large: large}

	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://docs/x"}},
	}
	pageReader := func(url string) (string, error) { return "# Auth", nil }

	findings, err := analyzer.DetectDrift(context.Background(), tiering, featureMap, docsMap, pageReader, t.TempDir(), nil)
	require.NoError(t, err)
	assert.Empty(t, findings)
	assert.Equal(t, 0, large.completeCalls, "Judge must not be called when investigator emits zero observations")
}

// fakeNonToolClient implements only LLMClient (no CompleteWithTools), used to
// assert the defensive cast in DetectDrift fails cleanly.
type fakeNonToolClient struct{}

func (fakeNonToolClient) Complete(_ context.Context, _ string) (string, error) { return "", nil }

func (fakeNonToolClient) CompleteJSON(_ context.Context, _ string, _ analyzer.JSONSchema) (json.RawMessage, error) {
	return nil, nil
}

func TestDetectDrift_TypicalWithoutToolSupport_Errors(t *testing.T) {
	// When tiering.Typical() does not implement ToolLLMClient, DetectDrift must
	// return a clear error rather than panic on a type assertion. The Large
	// tier is no longer required to support tool use.
	tiering := &fakeTiering{
		small: &driftStubClient{
			completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil },
		},
		typical: fakeNonToolClient{},
		large:   &driftStubClient{},
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
	assert.Contains(t, err.Error(), "typical")
}

func TestInvestigateFeatureDrift_RecordsObservations(t *testing.T) {
	client := &driftStubClient{
		responses: []analyzer.ChatMessage{
			noteObservation("https://docs/x", "doc says A", "code says B", "mismatch on A vs B"),
			driftDone(),
		},
	}
	entry := analyzer.FeatureEntry{
		Feature: analyzer.CodeFeature{Name: "auth", Description: "login"},
		Files:   []string{"auth.go"},
		Symbols: []string{"Login"},
	}
	pageReader := func(url string) (string, error) { return "# Docs", nil }

	obs, err := analyzer.InvestigateFeatureDrift(context.Background(), client, entry, []string{"https://docs/x"}, pageReader, t.TempDir())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Equal(t, "https://docs/x", obs[0].Page)
	assert.Equal(t, "doc says A", obs[0].DocQuote)
	assert.Equal(t, "code says B", obs[0].CodeQuote)
	assert.Equal(t, "mismatch on A vs B", obs[0].Concern)
}

func TestJudgeFeatureDrift_NoObservations_SkipsLLM(t *testing.T) {
	client := &driftStubClient{} // any call increments completeCalls
	feature := analyzer.CodeFeature{Name: "auth", Description: "login"}

	issues, err := analyzer.JudgeFeatureDrift(context.Background(), client, feature, nil)
	require.NoError(t, err)
	assert.Nil(t, issues)
	assert.Equal(t, 0, client.completeCalls, "Judge must not call the LLM with zero observations")
}

func TestJudgeFeatureDrift_ProducesIssues(t *testing.T) {
	// driftStubClient.CompleteJSON dispatches to Complete; set a completeFunc
	// that returns canned JSON.
	client := &driftStubClient{
		completeFunc: func(_ context.Context, prompt string) (string, error) {
			// Sanity: dossier must mention the feature name and an observation quote.
			if !strings.Contains(prompt, "auth") || !strings.Contains(prompt, "doc says X") {
				return "", fmt.Errorf("prompt missing expected fields: %s", prompt)
			}
			return `{"issues":[{"page":"https://docs/x","issue":"docs claim X but code does Y"}]}`, nil
		},
	}
	feature := analyzer.CodeFeature{Name: "auth", Description: "login"}
	obs := []analyzer.DriftObservation{
		{Page: "https://docs/x", DocQuote: "doc says X", CodeQuote: "code does Y", Concern: "mismatch"},
	}

	issues, err := analyzer.JudgeFeatureDrift(context.Background(), client, feature, obs)
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, "https://docs/x", issues[0].Page)
	assert.Contains(t, issues[0].Issue, "docs claim X but code does Y")
}

func TestInvestigateFeatureDrift_MaxRoundsHit_ReturnsAccumulated(t *testing.T) {
	// Two observations recorded, then loop exhausts without "done". The
	// stub reuses the last element when responses runs out, so we script
	// enough observations to deterministically exceed the round cap.
	client := &driftStubClient{
		responses: []analyzer.ChatMessage{
			noteObservation("p1", "d1", "c1", "concern 1"),
			noteObservation("p2", "d2", "c2", "concern 2"),
		},
	}
	for i := 0; i < 60; i++ {
		client.responses = append(client.responses,
			noteObservation(fmt.Sprintf("p%d", i+3), "d", "c", "concern"))
	}

	entry := analyzer.FeatureEntry{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"a.go"}}
	obs, err := analyzer.InvestigateFeatureDrift(context.Background(), client, entry, []string{"https://x"}, func(string) (string, error) { return "", nil }, t.TempDir())
	require.NoError(t, err, "round-cap exhaustion must not be a hard error")
	assert.GreaterOrEqual(t, len(obs), 2, "all observations recorded before cap must be returned")
}
