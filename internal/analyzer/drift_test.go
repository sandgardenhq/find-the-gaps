package analyzer_test

import (
	"context"
	"encoding/json"
	"errors"
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
// scripting a judge stub on the Large tier. Priority defaults to "medium" and
// priority_reason to "test stub" so the judge validator accepts the response.
func judgeJSON(page, issue string) func(context.Context, string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"issues": []map[string]string{{
			"page":            page,
			"issue":           issue,
			"priority":        "medium",
			"priority_reason": "test stub",
		}},
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

func (s *driftStubClient) CompleteJSONMultimodal(_ context.Context, _ []analyzer.ChatMessage, _ analyzer.JSONSchema) (json.RawMessage, error) {
	return nil, nil
}

func (s *driftStubClient) Capabilities() analyzer.ModelCapabilities {
	return analyzer.ModelCapabilities{}
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

func (s *driftStubClientWithErr) CompleteJSONMultimodal(_ context.Context, _ []analyzer.ChatMessage, _ analyzer.JSONSchema) (json.RawMessage, error) {
	return nil, s.err
}

func (s *driftStubClientWithErr) Capabilities() analyzer.ModelCapabilities {
	return analyzer.ModelCapabilities{}
}

func TestDetectDrift_NoDocumentedFeatures_ReturnsEmpty(t *testing.T) {
	client := &driftStubClient{}
	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth", UserFacing: true}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{} // no pages mapped — auth is undocumented, not a drift candidate
	pageReader := func(url string) (string, error) { return "", nil }

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: client, typical: client, large: client}, featureMap, docsMap, pageReader, "/repo", nil, nil, nil)
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

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, "/repo", nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, "auth", findings[0].Feature)
	require.Len(t, findings[0].Issues, 1)
	assert.Equal(t, "https://docs.example.com/auth", findings[0].Issues[0].Page)
	assert.Contains(t, findings[0].Issues[0].Issue, "Email requirement")
}

// When the judge call fails for one feature, DetectDrift must log a warning
// and continue with subsequent features. Reproduces the production failure
// where a single malformed judge response (issues:"<string>" instead of
// issues:[…]) aborted a 12-minute analyze run after work on 7 prior features
// had already completed.
func TestDetectDrift_JudgeFailureIsolatesToFeature(t *testing.T) {
	// Investigator runs both features (4 messages: 2 per feature). Reusing
	// driftStubClient.responses serves them in order — note_observation, then
	// driftDone, twice.
	typical := &driftStubClient{
		responses: []analyzer.ChatMessage{
			noteObservation("https://docs.example.com/auth", "old", "new", "drift"),
			driftDone(),
			noteObservation("https://docs.example.com/search", "old s", "new s", "drift s"),
			driftDone(),
		},
	}
	// Judge fails for "auth", succeeds for "search". The prompt template at
	// drift.go:514 always emits "Feature: <name>" so we can route on it.
	goodJudge := judgeJSON("https://docs.example.com/search", "Search drift.")
	large := &driftStubClient{
		completeFunc: func(ctx context.Context, prompt string) (string, error) {
			if strings.Contains(prompt, "Feature: auth") {
				return "", errors.New("simulated judge failure")
			}
			return goodJudge(ctx, prompt)
		},
	}
	small := &driftStubClient{completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil }}

	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"a.go"}},
		{Feature: analyzer.CodeFeature{Name: "search"}, Files: []string{"s.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}},
		{Feature: "search", Pages: []string{"https://docs.example.com/search"}},
	}
	pageReader := func(_ string) (string, error) { return "# Page", nil }

	findings, err := analyzer.DetectDrift(
		context.Background(),
		&fakeTiering{small: small, typical: typical, large: large},
		featureMap, docsMap, pageReader, "/repo",
		nil, nil, nil,
	)
	require.NoError(t, err, "judge failure for one feature must not abort the run")
	require.Len(t, findings, 1, "auth dropped, search retained")
	assert.Equal(t, "search", findings[0].Feature)
	require.Len(t, findings[0].Issues, 1)
	assert.Contains(t, findings[0].Issues[0].Issue, "Search drift.")
}

// Per the project decision (don't cache failures): when a judge call fails
// for a feature, onFeatureDone must NOT fire for it. The next run will
// re-investigate from scratch instead of inheriting a permanent silent
// zero-finding cache that would mask a recurring model regression.
func TestDetectDrift_JudgeFailureSkipsCacheCallback(t *testing.T) {
	typical := &driftStubClient{
		responses: []analyzer.ChatMessage{
			noteObservation("https://docs.example.com/auth", "old", "new", "drift"),
			driftDone(),
			noteObservation("https://docs.example.com/search", "old s", "new s", "drift s"),
			driftDone(),
		},
	}
	goodJudge := judgeJSON("https://docs.example.com/search", "Search drift.")
	large := &driftStubClient{
		completeFunc: func(ctx context.Context, prompt string) (string, error) {
			if strings.Contains(prompt, "Feature: auth") {
				return "", errors.New("simulated judge failure")
			}
			return goodJudge(ctx, prompt)
		},
	}
	small := &driftStubClient{completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil }}

	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"a.go"}},
		{Feature: analyzer.CodeFeature{Name: "search"}, Files: []string{"s.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}},
		{Feature: "search", Pages: []string{"https://docs.example.com/search"}},
	}
	pageReader := func(_ string) (string, error) { return "# Page", nil }

	var seen []string
	onFeatureDone := func(name string, _, _, _ []string, _ []analyzer.DriftIssue) error {
		seen = append(seen, name)
		return nil
	}

	_, err := analyzer.DetectDrift(
		context.Background(),
		&fakeTiering{small: small, typical: typical, large: large},
		featureMap, docsMap, pageReader, "/repo",
		nil, nil, onFeatureDone,
	)
	require.NoError(t, err)
	assert.NotContains(t, seen, "auth", "failed feature must not be cached")
	assert.Contains(t, seen, "search", "successful feature must still be cached")
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

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, "/repo", nil, nil, nil)
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

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), nil, nil, nil)
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
	_, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), nil, nil, nil)
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

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), nil, nil, nil)
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

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), nil, nil, nil)
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

	_, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), nil, nil, nil)
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

	_, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), nil, nil, nil)
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

	_, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), nil, nil, nil)
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

	_, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: client, large: client}, featureMap, docsMap, pageReader, t.TempDir(), nil, nil, nil)
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

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), nil, nil, nil)
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

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), nil, nil, nil)
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

	_, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), nil, nil, nil)
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

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), nil, nil, nil)
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

	_, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), nil, nil, nil)
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
					"issues": []map[string]string{{
						"page": "", "issue": "issue for auth",
						"priority": "medium", "priority_reason": "test stub",
					}},
				})
				return string(body), nil
			}
			body, _ := json.Marshal(map[string]any{
				"issues": []map[string]string{{
					"page": "", "issue": "issue for search",
					"priority": "medium", "priority_reason": "test stub",
				}},
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

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), nil, onFinding, nil)
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

	_, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), nil, onFinding, nil)
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
	// Feature "auth": 1 note_observation (round 1) + 9 tool calls (rounds 2..10),
	// exhausting the dynamic budget for a (1 file, 1 page) feature
	// (1 + 1 + 5 + 3 = 10) with one accumulated observation.
	// Feature "search": 1 note_observation + 1 driftDone (loop exits cleanly).
	responses := make([]analyzer.ChatMessage, 0, 12)
	responses = append(responses, noteObservation("", "doc says auth", "code does auth", "partial auth issue"))
	for i := 0; i < 9; i++ {
		responses = append(responses, toolCallResponse)
	}
	responses = append(responses, noteObservation("", "doc says search", "code does search", "issue for search"))
	responses = append(responses, driftDone())
	typical := &driftStubClient{responses: responses}
	large := &driftStubClient{
		completeFunc: func(_ context.Context, prompt string) (string, error) {
			if strings.Contains(prompt, "auth") && !strings.Contains(prompt, "search") {
				return string(mustJSON(map[string]any{
					"issues": []map[string]string{{
						"page": "", "issue": "partial auth issue",
						"priority": "medium", "priority_reason": "test stub",
					}},
				})), nil
			}
			return string(mustJSON(map[string]any{
				"issues": []map[string]string{{
					"page": "", "issue": "issue for search",
					"priority": "medium", "priority_reason": "test stub",
				}},
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

	findings, err := analyzer.DetectDrift(context.Background(), &fakeTiering{small: small, typical: typical, large: large}, featureMap, docsMap, pageReader, t.TempDir(), nil, nil, nil)
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
			return `{"issues":[{"page":"https://docs/x","issue":"docs are stale","priority":"medium","priority_reason":"test stub"}]}`, nil
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

	findings, err := analyzer.DetectDrift(context.Background(), tiering, featureMap, docsMap, pageReader, t.TempDir(), nil, nil, nil)
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

	findings, err := analyzer.DetectDrift(context.Background(), tiering, featureMap, docsMap, pageReader, t.TempDir(), nil, nil, nil)
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

func (fakeNonToolClient) CompleteJSONMultimodal(_ context.Context, _ []analyzer.ChatMessage, _ analyzer.JSONSchema) (json.RawMessage, error) {
	return nil, nil
}

func (fakeNonToolClient) Capabilities() analyzer.ModelCapabilities {
	return analyzer.ModelCapabilities{}
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

	_, err := analyzer.DetectDrift(context.Background(), tiering, featureMap, docsMap, pageReader, t.TempDir(), nil, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool use")
	assert.Contains(t, err.Error(), "typical")
}

// fakeToolClient is a minimal ToolLLMClient that captures the messages slice
// fed into CompleteWithTools and immediately returns a "done" result without
// running the agent loop. Used by cache-breakpoint tests.
type fakeToolClient struct {
	complete func(ctx context.Context, msgs []analyzer.ChatMessage, tools []analyzer.Tool, opts ...analyzer.AgentOption) (analyzer.AgentResult, error)
}

func (f *fakeToolClient) Complete(_ context.Context, _ string) (string, error) { return "", nil }

func (f *fakeToolClient) CompleteJSON(_ context.Context, _ string, _ analyzer.JSONSchema) (json.RawMessage, error) {
	return nil, nil
}

func (f *fakeToolClient) CompleteJSONMultimodal(_ context.Context, _ []analyzer.ChatMessage, _ analyzer.JSONSchema) (json.RawMessage, error) {
	return nil, nil
}

func (f *fakeToolClient) Capabilities() analyzer.ModelCapabilities {
	return analyzer.ModelCapabilities{}
}

func (f *fakeToolClient) CompleteWithTools(ctx context.Context, msgs []analyzer.ChatMessage, tools []analyzer.Tool, opts ...analyzer.AgentOption) (analyzer.AgentResult, error) {
	return f.complete(ctx, msgs, tools, opts...)
}

func TestInvestigateFeatureDrift_MarksFirstMessageAsCacheBreakpoint(t *testing.T) {
	var capturedMsgs [][]analyzer.ChatMessage
	fake := &fakeToolClient{
		complete: func(_ context.Context, msgs []analyzer.ChatMessage, _ []analyzer.Tool, _ ...analyzer.AgentOption) (analyzer.AgentResult, error) {
			snapshot := make([]analyzer.ChatMessage, len(msgs))
			copy(snapshot, msgs)
			capturedMsgs = append(capturedMsgs, snapshot)
			return analyzer.AgentResult{FinalMessage: analyzer.ChatMessage{Role: "assistant", Content: "done"}, Rounds: 1}, nil
		},
	}

	entry := analyzer.FeatureEntry{
		Feature: analyzer.CodeFeature{Name: "auth", Description: "login"},
		Files:   []string{"auth.go"},
		Symbols: []string{"Login"},
	}
	pages := []string{"https://example.com/page"}
	pageReader := func(string) (string, error) { return "stub", nil }

	_, err := analyzer.InvestigateFeatureDrift(context.Background(), fake, entry, pages, pageReader, "/repo")
	require.NoError(t, err)
	require.Len(t, capturedMsgs, 1)
	require.NotEmpty(t, capturedMsgs[0])
	require.True(t, capturedMsgs[0][0].CacheBreakpoint, "investigator's first user message must be marked as cache breakpoint")
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

// validatingStubClient mirrors BifrostClient's CompleteJSON: it runs the canned
// response through schema.ValidateResponse so tests cover the strip-and-clean
// path that production callers rely on.
type validatingStubClient struct {
	driftStubClient
}

func (s *validatingStubClient) CompleteJSON(ctx context.Context, prompt string, schema analyzer.JSONSchema) (json.RawMessage, error) {
	raw, err := s.driftStubClient.CompleteJSON(ctx, prompt, schema)
	if err != nil {
		return nil, err
	}
	cleaned, err := schema.ValidateResponse(raw)
	if err != nil {
		return nil, fmt.Errorf("validatingStubClient: %w", err)
	}
	return cleaned, nil
}

// Reproduces the production failure: the judge LLM hallucinates an
// `issue_dup_of` key that is not declared in drift_judge_issues' schema.
// Anthropic's tool input_schema is advisory, so additionalProperties:false
// alone doesn't stop the model. ValidateResponse must strip the extra key
// rather than fail the run.
func TestJudgeFeatureDrift_StripsExtraFieldsFromJudgeResponse(t *testing.T) {
	client := &validatingStubClient{
		driftStubClient: driftStubClient{
			completeFunc: func(_ context.Context, _ string) (string, error) {
				return `{"issues":[{"page":"https://docs/x","issue":"stale signature","priority":"medium","priority_reason":"test stub","issue_dup_of":2}]}`, nil
			},
		},
	}
	feature := analyzer.CodeFeature{Name: "auth", Description: "login"}
	obs := []analyzer.DriftObservation{
		{Page: "https://docs/x", DocQuote: "doc says X", CodeQuote: "code does Y", Concern: "mismatch"},
	}

	issues, err := analyzer.JudgeFeatureDrift(context.Background(), client, feature, obs)
	require.NoError(t, err, "extra fields must be stripped, not fail the call")
	require.Len(t, issues, 1)
	assert.Equal(t, "https://docs/x", issues[0].Page)
	assert.Equal(t, "stale signature", issues[0].Issue)
	assert.Equal(t, analyzer.PriorityMedium, issues[0].Priority)
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
			return `{"issues":[{"page":"https://docs/x","issue":"docs claim X but code does Y","priority":"medium","priority_reason":"test stub"}]}`, nil
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

func TestDetectDrift_CacheHit_SkipsLLM(t *testing.T) {
	// Investigator (typical tier) and judge (large tier) must NOT run when
	// the cache supplies an entry whose post-classify files+pages match the
	// current run. The classifier (small tier) still runs per design — page
	// classification is not cached in v1; see .plans/2026-04-29-drift-cache-design.md.
	typical := &driftStubClient{} // empty responses; any call exhausts and panics
	large := &driftStubClient{}   // judge must never run
	small := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil },
	}

	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}},
	}
	pageReader := func(_ string) (string, error) { return "# Auth", nil }

	cached := map[string]analyzer.CachedDriftEntry{
		"auth": {
			Files:         []string{"auth.go"},
			FilteredPages: []string{"https://docs.example.com/auth"},
			Pages:         []string{"https://docs.example.com/auth"},
			Issues: []analyzer.DriftIssue{{
				Page: "https://docs.example.com/auth", Issue: "stale signature",
				Priority: analyzer.PriorityMedium, PriorityReason: "test stub",
			}},
		},
	}

	findings, err := analyzer.DetectDrift(
		context.Background(),
		&fakeTiering{small: small, typical: typical, large: large},
		featureMap, docsMap, pageReader, "/repo",
		cached, nil, nil,
	)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, "auth", findings[0].Feature)
	require.Len(t, findings[0].Issues, 1)
	assert.Equal(t, "stale signature", findings[0].Issues[0].Issue)
	assert.Equal(t, 0, typical.calls, "investigator must not run on cache hit")
	assert.Equal(t, 0, large.completeCalls, "judge must not run on cache hit")
	assert.Equal(t, 0, small.completeCalls, "classifier must not run on cache hit")
}

func TestDetectDrift_CacheMissByFiles_RecomputesFresh(t *testing.T) {
	// Cached entry's files don't match the current feature's files → recompute.
	typical := &driftStubClient{
		responses: []analyzer.ChatMessage{
			noteObservation("https://docs.example.com/auth", "old", "new", "drift"),
			driftDone(),
		},
	}
	large := &driftStubClient{completeFunc: judgeJSON("https://docs.example.com/auth", "Login signature changed.")}
	small := &driftStubClient{completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil }}

	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go", "session.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}},
	}
	pageReader := func(_ string) (string, error) { return "# Auth", nil }

	// Cached entry from a prior run when the feature only had auth.go.
	cached := map[string]analyzer.CachedDriftEntry{
		"auth": {
			Files:         []string{"auth.go"}, // mismatch — current run has [auth.go, session.go]
			FilteredPages: []string{"https://docs.example.com/auth"},
			Pages:         []string{"https://docs.example.com/auth"},
			Issues:        nil,
		},
	}

	findings, err := analyzer.DetectDrift(
		context.Background(),
		&fakeTiering{small: small, typical: typical, large: large},
		featureMap, docsMap, pageReader, "/repo",
		cached, nil, nil,
	)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Contains(t, findings[0].Issues[0].Issue, "Login signature changed")
	assert.Greater(t, typical.calls, 0, "investigator must run on cache miss")
}

func TestDetectDrift_CacheMissByPages_RecomputesFresh(t *testing.T) {
	typical := &driftStubClient{
		responses: []analyzer.ChatMessage{
			noteObservation("https://docs.example.com/auth", "old", "new", "drift"),
			driftDone(),
		},
	}
	large := &driftStubClient{completeFunc: judgeJSON("https://docs.example.com/auth", "Pages drifted.")}
	small := &driftStubClient{completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil }}

	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}},
	}
	pageReader := func(_ string) (string, error) { return "# Auth", nil }

	cached := map[string]analyzer.CachedDriftEntry{
		"auth": {
			Files:         []string{"auth.go"},
			FilteredPages: []string{"https://docs.example.com/old"}, // mismatch on the cache key
			Pages:         []string{"https://docs.example.com/old"},
			Issues:        nil,
		},
	}

	findings, err := analyzer.DetectDrift(
		context.Background(),
		&fakeTiering{small: small, typical: typical, large: large},
		featureMap, docsMap, pageReader, "/repo",
		cached, nil, nil,
	)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Greater(t, typical.calls, 0, "investigator must run on page mismatch")
}

func TestDetectDrift_OnFeatureDone_FiresForAllCompletions(t *testing.T) {
	// Three features:
	// 1. "fresh-with-issues"  — no cache entry, investigator emits an observation, judge issues.
	// 2. "fresh-empty"        — no cache entry, investigator emits zero observations.
	// 3. "cached"             — cache hit, returns prior issues.
	// onFeatureDone must fire exactly 3 times, with the right names and issue counts.
	typical := &driftStubClient{
		responses: []analyzer.ChatMessage{
			// fresh-with-issues:
			noteObservation("https://docs.example.com/a", "old", "new", "drift"),
			driftDone(),
			// fresh-empty:
			driftDone(),
			// cached: investigator must not be invoked.
		},
	}
	large := &driftStubClient{completeFunc: judgeJSON("https://docs.example.com/a", "Drift A.")}
	small := &driftStubClient{completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil }}

	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "fresh-with-issues"}, Files: []string{"a.go"}},
		{Feature: analyzer.CodeFeature{Name: "fresh-empty"}, Files: []string{"b.go"}},
		{Feature: analyzer.CodeFeature{Name: "cached"}, Files: []string{"c.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "fresh-with-issues", Pages: []string{"https://docs.example.com/a"}},
		{Feature: "fresh-empty", Pages: []string{"https://docs.example.com/b"}},
		{Feature: "cached", Pages: []string{"https://docs.example.com/c"}},
	}
	pageReader := func(_ string) (string, error) { return "# Page", nil }

	cached := map[string]analyzer.CachedDriftEntry{
		"cached": {
			Files:         []string{"c.go"},
			FilteredPages: []string{"https://docs.example.com/c"},
			Pages:         []string{"https://docs.example.com/c"},
			Issues: []analyzer.DriftIssue{{
				Page: "https://docs.example.com/c", Issue: "Cached drift.",
				Priority: analyzer.PriorityMedium, PriorityReason: "test stub",
			}},
		},
	}

	type record struct {
		name        string
		issuesCount int
	}
	var recorded []record
	onFeatureDone := func(name string, _, _, _ []string, issues []analyzer.DriftIssue) error {
		recorded = append(recorded, record{name: name, issuesCount: len(issues)})
		return nil
	}

	_, err := analyzer.DetectDrift(
		context.Background(),
		&fakeTiering{small: small, typical: typical, large: large},
		featureMap, docsMap, pageReader, "/repo",
		cached, nil, onFeatureDone,
	)
	require.NoError(t, err)
	require.Len(t, recorded, 3)
	// Order matches featureMap iteration order.
	assert.Equal(t, "fresh-with-issues", recorded[0].name)
	assert.Equal(t, 1, recorded[0].issuesCount)
	assert.Equal(t, "fresh-empty", recorded[1].name)
	assert.Equal(t, 0, recorded[1].issuesCount)
	assert.Equal(t, "cached", recorded[2].name)
	assert.Equal(t, 1, recorded[2].issuesCount)
}

func TestDetectDrift_OnFeatureDoneError_Aborts(t *testing.T) {
	typical := &driftStubClient{responses: []analyzer.ChatMessage{driftDone()}}
	large := &driftStubClient{}
	small := &driftStubClient{completeFunc: func(_ context.Context, _ string) (string, error) { return "no", nil }}

	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"a.go"}},
		{Feature: analyzer.CodeFeature{Name: "search"}, Files: []string{"b.go"}}, // must not be processed
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://docs.example.com/a"}},
		{Feature: "search", Pages: []string{"https://docs.example.com/b"}},
	}
	pageReader := func(_ string) (string, error) { return "# Page", nil }

	calls := 0
	onFeatureDone := func(_ string, _, _, _ []string, _ []analyzer.DriftIssue) error {
		calls++
		return errors.New("disk full")
	}

	_, err := analyzer.DetectDrift(
		context.Background(),
		&fakeTiering{small: small, typical: typical, large: large},
		featureMap, docsMap, pageReader, "/repo",
		nil, nil, onFeatureDone,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk full")
	assert.Equal(t, 1, calls, "second feature must not be processed after onFeatureDone error")
}

func TestDetectDrift_CacheHit_DoesNotCallClassifier(t *testing.T) {
	// On a per-feature cache hit, the Small-tier classifier must not run.
	// Today the classifier fires before the cache check; this test pins the
	// new behavior: cache key now uses FilteredPages (post-filterDriftPages,
	// pre-classify), so a hit short-circuits classifier+investigator+judge.
	typical := &driftStubClient{}
	large := &driftStubClient{}
	classifierCalls := 0
	small := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) {
			classifierCalls++
			return "no", nil
		},
	}

	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}},
	}
	pageReader := func(_ string) (string, error) { return "# Auth", nil }

	cached := map[string]analyzer.CachedDriftEntry{
		"auth": {
			Files:         []string{"auth.go"},
			FilteredPages: []string{"https://docs.example.com/auth"},
			Pages:         []string{"https://docs.example.com/auth"},
			Issues: []analyzer.DriftIssue{{
				Page: "https://docs.example.com/auth", Issue: "Cached drift.",
				Priority: analyzer.PriorityMedium, PriorityReason: "test stub",
			}},
		},
	}

	findings, err := analyzer.DetectDrift(
		context.Background(),
		&fakeTiering{small: small, typical: typical, large: large},
		featureMap, docsMap, pageReader, "/repo",
		cached, nil, nil,
	)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, 0, classifierCalls, "cache hit must not invoke the Small-tier classifier")
	assert.Equal(t, 0, typical.calls, "cache hit must not invoke the investigator")
	assert.Equal(t, 0, large.completeCalls, "cache hit must not invoke the judge")
}

func TestDetectDrift_OnFeatureDone_ReceivesFilteredAndClassifiedPages(t *testing.T) {
	// The DriftFeatureDoneFunc receives both the post-filter (pre-classify)
	// page list and the post-classify page list. The first is the cache key,
	// the second is what investigator+judge actually saw.
	//
	// Setup: two pages survive filterDriftPages (neither URL matches a
	// release-note pattern). The LLM classifier drops the one whose content
	// looks like a blog post; the other survives. So filteredPages contains
	// both, pages contains only the survivor.
	typical := &driftStubClient{responses: []analyzer.ChatMessage{driftDone()}}
	large := &driftStubClient{}
	small := &driftStubClient{
		completeFunc: func(_ context.Context, prompt string) (string, error) {
			if strings.Contains(prompt, "blog post") {
				return "yes", nil
			}
			return "no", nil
		},
	}

	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{
			"https://docs.example.com/auth",
			"https://docs.example.com/blog/post1",
		}},
	}
	pageReader := func(url string) (string, error) {
		if strings.Contains(url, "blog") {
			return "This is a blog post about authentication.", nil
		}
		return "# Auth\nLogin reference.", nil
	}

	type record struct {
		filtered, pages []string
	}
	var got record
	onFeatureDone := func(_ string, _, filtered, pages []string, _ []analyzer.DriftIssue) error {
		got = record{filtered: filtered, pages: pages}
		return nil
	}

	_, err := analyzer.DetectDrift(
		context.Background(),
		&fakeTiering{small: small, typical: typical, large: large},
		featureMap, docsMap, pageReader, "/repo",
		nil, nil, onFeatureDone,
	)
	require.NoError(t, err)
	assert.Contains(t, got.filtered, "https://docs.example.com/blog/post1",
		"filtered list is post-URL-pattern-filter, pre-LLM-classify — blog URL survives the regex filter")
	assert.Contains(t, got.filtered, "https://docs.example.com/auth")
	assert.NotContains(t, got.pages, "https://docs.example.com/blog/post1",
		"classified list excludes the LLM-flagged blog page")
	assert.Contains(t, got.pages, "https://docs.example.com/auth",
		"classified list keeps the surviving page")
}

func TestDetectDrift_EmptyAfterClassify_StillCachesFilteredPages(t *testing.T) {
	// When classifyDriftPages drops every page (every page classified as
	// release notes), the feature must still record a cache entry so the
	// next run skips the classifier. Pre-fix behavior: feature has no cache
	// entry, classifier re-runs forever.
	typical := &driftStubClient{}
	large := &driftStubClient{}

	// Run 1: classifier says "yes" to everything → all pages dropped.
	classifierCalls := 0
	small := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) {
			classifierCalls++
			return "yes", nil
		},
	}

	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}},
	}
	pageReader := func(_ string) (string, error) { return "# anything", nil }

	type record struct {
		filtered, pages []string
		issues          []analyzer.DriftIssue
	}
	var got record
	var doneCalls int
	onFeatureDone := func(_ string, _, filtered, pages []string, issues []analyzer.DriftIssue) error {
		doneCalls++
		got = record{filtered: filtered, pages: pages, issues: issues}
		return nil
	}

	_, err := analyzer.DetectDrift(
		context.Background(),
		&fakeTiering{small: small, typical: typical, large: large},
		featureMap, docsMap, pageReader, "/repo",
		nil, nil, onFeatureDone,
	)
	require.NoError(t, err)
	assert.Equal(t, 1, doneCalls, "onFeatureDone must fire even when classifier drops every page")
	assert.NotEmpty(t, got.filtered, "filtered list must be populated so the cache can key on it")
	assert.Empty(t, got.pages, "pages list is empty because classifier dropped everything")
	assert.Empty(t, got.issues, "no issues when no pages survive classification")
	classifierCallsRun1 := classifierCalls

	// Run 2: feed the run-1 result back in as the cache. Classifier must not run.
	cached := map[string]analyzer.CachedDriftEntry{
		"auth": {
			Files:         []string{"auth.go"},
			FilteredPages: got.filtered,
			Pages:         got.pages,
			Issues:        nil,
		},
	}
	classifierCalls = 0
	doneCalls = 0
	_, err = analyzer.DetectDrift(
		context.Background(),
		&fakeTiering{small: small, typical: typical, large: large},
		featureMap, docsMap, pageReader, "/repo",
		cached, nil, onFeatureDone,
	)
	require.NoError(t, err)
	assert.Greater(t, classifierCallsRun1, 0, "sanity: run 1 must have called the classifier at least once")
	assert.Equal(t, 0, classifierCalls, "run 2 must hit the cache and skip the classifier entirely")
	assert.Equal(t, 1, doneCalls, "onFeatureDone fires on cache hit too")
}

func TestDetectDrift_CacheWithoutFilteredPages_RecomputesOnce(t *testing.T) {
	// Pre-upgrade caches have Files+Pages+Issues but no FilteredPages.
	// The cache-key check at drift.go must miss (nil != non-empty
	// sortedFiltered) so the entry recomputes once. After the recompute the
	// callback should receive a non-nil filteredPages so the next run hits.
	typical := &driftStubClient{
		responses: []analyzer.ChatMessage{
			noteObservation("https://docs.example.com/auth", "old", "new", "drift"),
			driftDone(),
		},
	}
	large := &driftStubClient{completeFunc: judgeJSON("https://docs.example.com/auth", "Recomputed.")}
	classifierCalls := 0
	small := &driftStubClient{
		completeFunc: func(_ context.Context, _ string) (string, error) {
			classifierCalls++
			return "no", nil
		},
	}

	featureMap := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}},
	}
	pageReader := func(_ string) (string, error) { return "# Auth", nil }

	cached := map[string]analyzer.CachedDriftEntry{
		"auth": {
			Files: []string{"auth.go"},
			// FilteredPages absent (nil) — old cache shape.
			Pages: []string{"https://docs.example.com/auth"},
			Issues: []analyzer.DriftIssue{{
				Page: "https://docs.example.com/auth", Issue: "Old issue.",
				Priority: analyzer.PriorityMedium, PriorityReason: "stale",
			}},
		},
	}

	var captured analyzer.CachedDriftEntry
	onFeatureDone := func(_ string, files, filtered, pages []string, issues []analyzer.DriftIssue) error {
		captured = analyzer.CachedDriftEntry{
			Files: files, FilteredPages: filtered, Pages: pages, Issues: issues,
		}
		return nil
	}

	findings, err := analyzer.DetectDrift(
		context.Background(),
		&fakeTiering{small: small, typical: typical, large: large},
		featureMap, docsMap, pageReader, "/repo",
		cached, nil, onFeatureDone,
	)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, "Recomputed.", findings[0].Issues[0].Issue, "old-shape entry must recompute, not return the cached issue")
	assert.Greater(t, classifierCalls, 0, "miss path classifier must run")
	assert.Greater(t, typical.calls, 0, "miss path investigator must run")

	// After the recompute, the persisted entry must carry FilteredPages so
	// the *next* run hits the cache and skips classifier+investigator+judge.
	require.NotNil(t, captured.FilteredPages, "after recompute, FilteredPages must be populated")
	assert.Equal(t, []string{"https://docs.example.com/auth"}, captured.FilteredPages)
}

func TestBudgetForFeature(t *testing.T) {
	cases := []struct {
		name         string
		files, pages int
		want         int
	}{
		{"minimum", 1, 1, 10},
		{"medium", 8, 4, 20},
		{"grows past old cap", 15, 10, 33},
		{"large but uncapped", 40, 30, 78},
		{"one below ceiling", 45, 46, 99},
		{"exactly at ceiling", 46, 46, 100},
		{"clamped above ceiling", 60, 50, 100},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := analyzer.ExportedBudgetForFeature(tc.files, tc.pages)
			assert.Equal(t, tc.want, got, "budgetForFeature(%d, %d)", tc.files, tc.pages)
		})
	}
}
