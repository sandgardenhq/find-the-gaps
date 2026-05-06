package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
	"github.com/sandgardenhq/find-the-gaps/internal/spider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunBothMapsInParallel(t *testing.T) {
	authFeature := analyzer.CodeFeature{Name: "auth", Description: "Auth.", Layer: "cli", UserFacing: true}
	codeMap, docsMap, err := runBothMaps(
		context.Background(),
		stubTieringOn(&stubLLMClient{
			codeResp: `{"entries":[{"feature":"auth","files":["auth.go"],"symbols":["Login"]}]}`,
			docsResp: `{"features":["auth"]}`,
		}),
		[]analyzer.CodeFeature{authFeature},
		stubScan(),
		map[string]string{
			"https://example.com/auth": writeTempFile(t, "auth content"),
		},
		2,      // workers
		10_000, // docsTokenBudget
		false,  // filesOnly
		nil,    // onCodeBatch
		nil,    // onDocsPage
	)
	require.NoError(t, err)
	require.Len(t, codeMap, 1)
	assert.Equal(t, "auth", codeMap[0].Feature.Name)
	require.Len(t, docsMap, 1)
	assert.Equal(t, "auth", docsMap[0].Feature)
}

func TestRunBothMaps_CodeMapError(t *testing.T) {
	authFeature := analyzer.CodeFeature{Name: "auth", Description: "Auth.", Layer: "cli", UserFacing: true}
	_, _, err := runBothMaps(
		context.Background(),
		stubTieringOn(&stubLLMClient{
			codeResp: `not json`, // forces MapFeaturesToCode to fail
			docsResp: `[]`,
		}),
		[]analyzer.CodeFeature{authFeature},
		stubScan(),
		map[string]string{
			"https://example.com/auth": writeTempFile(t, "content"),
		},
		1, 10_000, false, nil, nil,
	)
	require.Error(t, err)
}

func TestRunBothMaps_DocsMapError(t *testing.T) {
	authFeature := analyzer.CodeFeature{Name: "auth", Description: "Auth.", Layer: "cli", UserFacing: true}
	_, _, err := runBothMaps(
		context.Background(),
		stubTieringOn(&stubLLMClient{
			codeResp: `{"entries":[{"feature":"auth","files":[],"symbols":[]}]}`,
			docsResp: `not json`, // forces MapFeaturesToDocs page call to fail
		}),
		[]analyzer.CodeFeature{authFeature},
		stubScan(),
		map[string]string{
			"https://example.com/auth": writeTempFile(t, "content"),
		},
		1, 10_000, false, nil, nil,
	)
	// docs page errors are logged and skipped, not propagated — result is empty pages
	require.NoError(t, err)
}

// TestRunBothMaps_DocsMapError_ViaOnPageCallback exercises the docsRes.err != nil
// path in runBothMaps. MapFeaturesToDocs only propagates an error when the onPage
// progress callback itself returns an error; individual page LLM errors are logged
// and skipped. We pass a callback that always returns an error so the docs goroutine
// returns a non-nil error, triggering the second error guard in runBothMaps.
func TestRunBothMaps_DocsMapError_ViaOnPageCallback(t *testing.T) {
	callbackErr := errors.New("onPage callback error")
	onDocsPage := func(_ analyzer.DocsFeatureMap) error {
		return callbackErr
	}

	authFeature := analyzer.CodeFeature{Name: "auth", Description: "Auth.", Layer: "cli", UserFacing: true}
	_, _, err := runBothMaps(
		context.Background(),
		stubTieringOn(&stubLLMClient{
			codeResp: `{"entries":[]}`, // empty but valid JSON — code map succeeds immediately
			docsResp: `{"features":["auth"]}`,
		}),
		[]analyzer.CodeFeature{authFeature},
		// Empty scan so MapFeaturesToCode returns immediately (no LLM call, no error).
		&scanner.ProjectScan{Files: []scanner.ScannedFile{}},
		map[string]string{
			"https://example.com/auth": writeTempFile(t, "auth content"),
		},
		1,
		10_000,
		false,
		nil,
		onDocsPage, // docs progress callback that errors → propagated to runBothMaps
	)
	require.Error(t, err, "expected error propagated from onDocsPage callback")
}

func TestRunBothMaps_FilesOnly_PassedThrough(t *testing.T) {
	authFeature := analyzer.CodeFeature{Name: "auth", Description: "Auth.", Layer: "cli", UserFacing: true}
	client := &stubLLMClient{
		codeResp: `{"entries":[{"feature":"auth","files":["auth.go"]}]}`,
		docsResp: `{"features":["auth"]}`,
	}
	codeMap, _, err := runBothMaps(
		context.Background(),
		stubTieringOn(client),
		[]analyzer.CodeFeature{authFeature},
		stubScan(),
		map[string]string{
			"https://example.com/auth": writeTempFile(t, "auth content"),
		},
		2,
		10_000,
		true, // filesOnly
		nil,
		nil,
	)
	require.NoError(t, err)
	require.Len(t, codeMap, 1)
	assert.Empty(t, codeMap[0].Symbols, "filesOnly mode must produce empty Symbols")
}

func TestAnalyzeCmd_NoSymbolsFlag_Registered(t *testing.T) {
	cmd := newAnalyzeCmd()
	flag := cmd.Flags().Lookup("no-symbols")
	if flag == nil {
		t.Fatal("--no-symbols flag not registered on analyze command")
	}
	if flag.DefValue != "false" {
		t.Errorf("--no-symbols default should be false, got %q", flag.DefValue)
	}
}

// --- stubs ---

// stubTiering wraps a single LLMClient into all three tiers so tests that
// only care about one code path can reuse existing stubLLMClient fixtures.
type stubTiering struct {
	client  analyzer.LLMClient
	counter analyzer.TokenCounter
}

func (s *stubTiering) Small() analyzer.LLMClient             { return s.client }
func (s *stubTiering) Typical() analyzer.LLMClient           { return s.client }
func (s *stubTiering) Large() analyzer.LLMClient             { return s.client }
func (s *stubTiering) SmallCounter() analyzer.TokenCounter   { return s.counter }
func (s *stubTiering) TypicalCounter() analyzer.TokenCounter { return s.counter }
func (s *stubTiering) LargeCounter() analyzer.TokenCounter   { return s.counter }

func stubTieringOn(client analyzer.LLMClient) *stubTiering {
	return &stubTiering{client: client, counter: analyzer.NewTiktokenCounter()}
}

type stubLLMClient struct {
	codeResp string
	docsResp string
}

func (s *stubLLMClient) Complete(_ context.Context, prompt string) (string, error) {
	if strings.Contains(prompt, "Code symbols (format:") || strings.Contains(prompt, "Code files:\n") {
		return s.codeResp, nil
	}
	return s.docsResp, nil
}

func (s *stubLLMClient) CompleteWithTools(_ context.Context, _ []analyzer.ChatMessage, _ []analyzer.Tool, _ ...analyzer.AgentOption) (analyzer.AgentResult, error) {
	return analyzer.AgentResult{
		FinalMessage: analyzer.ChatMessage{Role: "assistant", Content: "[]"},
		Rounds:       1,
	}, nil
}

func (s *stubLLMClient) CompleteJSON(ctx context.Context, prompt string, _ analyzer.JSONSchema) (json.RawMessage, error) {
	raw, err := s.Complete(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func (s *stubLLMClient) CompleteJSONMultimodal(_ context.Context, _ []analyzer.ChatMessage, _ analyzer.JSONSchema) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (s *stubLLMClient) Capabilities() analyzer.ModelCapabilities {
	return analyzer.ModelCapabilities{}
}

func stubScan() *scanner.ProjectScan {
	return &scanner.ProjectScan{
		RepoPath:  "/fake",
		ScannedAt: time.Now(),
		Files: []scanner.ScannedFile{
			{
				Path:     "auth.go",
				Language: "go",
				Symbols:  []scanner.Symbol{{Name: "Login", Kind: scanner.KindFunc}},
			},
		},
	}
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "page.md")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	return p
}

// peakInFlightStubClient observes the maximum number of concurrent
// CompleteJSON callers that hit the analyze_page_response schema. Each call
// holds for ~20ms so a serial loop tops out at 1 in flight, while a parallel
// dispatcher with workers >= 2 reliably hits >= 2.
type peakInFlightStubClient struct {
	inFlight   atomic.Int32
	peak       atomic.Int32
	pageCalls  atomic.Int32
	holdMillis int
}

func (s *peakInFlightStubClient) Complete(_ context.Context, _ string) (string, error) {
	return "no", nil
}

func (s *peakInFlightStubClient) CompleteWithTools(_ context.Context, _ []analyzer.ChatMessage, _ []analyzer.Tool, _ ...analyzer.AgentOption) (analyzer.AgentResult, error) {
	return analyzer.AgentResult{
		FinalMessage: analyzer.ChatMessage{Role: "assistant", Content: "done"},
		Rounds:       1,
	}, nil
}

func (s *peakInFlightStubClient) CompleteJSON(_ context.Context, _ string, schema analyzer.JSONSchema) (json.RawMessage, error) {
	switch schema.Name {
	case "analyze_page_response":
		cur := s.inFlight.Add(1)
		for {
			p := s.peak.Load()
			if cur <= p || s.peak.CompareAndSwap(p, cur) {
				break
			}
		}
		time.Sleep(time.Duration(s.holdMillis) * time.Millisecond)
		s.inFlight.Add(-1)
		s.pageCalls.Add(1)
		return json.RawMessage(`{"summary":"page summary","features":["feature-one"],"is_docs":true}`), nil
	case "code_features_response":
		return json.RawMessage(`{"features":[{"name":"feature-one","description":"Does feature one.","layer":"cli","user_facing":true}]}`), nil
	case "map_response":
		return json.RawMessage(`{"entries":[{"feature":"feature-one","files":["main.go"],"symbols":["Run"]}]}`), nil
	case "map_page_response":
		return json.RawMessage(`{"features":["feature-one"]}`), nil
	case "synthesize_product_response":
		return json.RawMessage(`{"description":"A test product.","features":["feature-one"]}`), nil
	}
	return json.RawMessage(`{}`), nil
}

func (s *peakInFlightStubClient) CompleteJSONMultimodal(_ context.Context, _ []analyzer.ChatMessage, _ analyzer.JSONSchema) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (s *peakInFlightStubClient) Capabilities() analyzer.ModelCapabilities {
	return analyzer.ModelCapabilities{}
}

// TestAnalyze_pageAnalysisRunsConcurrently asserts that the per-page LLM call
// in the analyze pipeline runs concurrently when --workers > 1. The stub
// records peak in-flight callers; with the serial loop this is 1, and the
// parallel implementation must observe >= 2.
func TestAnalyze_pageAnalysisRunsConcurrently(t *testing.T) {
	repoDir := t.TempDir()
	cacheBase := t.TempDir()
	projectName := filepath.Base(filepath.Clean(repoDir))
	projectDir := filepath.Join(cacheBase, projectName)

	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "main.go"), []byte("package main\nfunc Run() {}\n"), 0o644))

	docsDir := filepath.Join(projectDir, "docs")
	require.NoError(t, os.MkdirAll(docsDir, 0o755))
	idx, err := spider.LoadIndex(docsDir)
	require.NoError(t, err)

	const numPages = 8
	pageURLs := make([]string, numPages)
	for i := 0; i < numPages; i++ {
		u := fmt.Sprintf("https://docs.example.com/page-%02d", i)
		pageURLs[i] = u
		filename := spider.URLToFilename(u)
		require.NoError(t, os.WriteFile(filepath.Join(docsDir, filename), []byte(fmt.Sprintf("# Page %d\n\nContent for page %d.\n", i, i)), 0o644))
		require.NoError(t, idx.Record(u, filename))
	}

	stub := &peakInFlightStubClient{holdMillis: 20}
	prevFactory := tieringFactory
	t.Cleanup(func() { tieringFactory = prevFactory })
	tieringFactory = func(_, _, _ string) (analyzer.LLMTiering, error) {
		return &stubTiering{client: stub, counter: analyzer.NewTiktokenCounter()}, nil
	}

	args := []string{
		"analyze",
		"--repo", repoDir,
		"--cache-dir", cacheBase,
		"--docs-url", pageURLs[0],
		"--workers", "4",
		"--no-site",
	}

	var stdout, stderr bytes.Buffer
	if code := run(&stdout, &stderr, args); code != 0 {
		t.Fatalf("analyze run failed (code=%d): stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	assert.Equal(t, int32(numPages), stub.pageCalls.Load(), "every page must hit the small-tier LLM")
	assert.GreaterOrEqual(t, stub.peak.Load(), int32(2),
		"page-analysis loop must run pages concurrently when --workers > 1; peak in-flight=%d", stub.peak.Load())
}
