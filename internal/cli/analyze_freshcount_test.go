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

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/spider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// freshCountStubClient is a counting stub used to pin the freshCount semantics.
// It separately tallies invocations of analyze_page_response (the per-page
// pass) and synthesize_product_response (the product synthesis call we want
// to assert fires, or doesn't, depending on freshCount).
//
// failPageAnalysis switches the per-page schema to return invalid JSON, which
// drives analyzer.AnalyzePage to error. The dispatch loop in analyze.go logs-
// and-skips that error, so analyses for those pages are never appended.
type freshCountStubClient struct {
	pageCalls        atomic.Int32
	synthesizeCalls  atomic.Int32
	failPageAnalysis bool
}

func (s *freshCountStubClient) Complete(_ context.Context, _ string) (string, error) {
	// Small tier hits this for the release-note classifier; "no" means
	// "not release notes" so the page is included downstream.
	return "no", nil
}

func (s *freshCountStubClient) CompleteWithTools(_ context.Context, _ []analyzer.ChatMessage, _ []analyzer.Tool, _ ...analyzer.AgentOption) (analyzer.AgentResult, error) {
	return analyzer.AgentResult{
		FinalMessage: analyzer.ChatMessage{Role: "assistant", Content: "done"},
		Rounds:       1,
	}, nil
}

func (s *freshCountStubClient) CompleteJSON(_ context.Context, _ string, schema analyzer.JSONSchema) (json.RawMessage, error) {
	switch schema.Name {
	case "analyze_page_response":
		s.pageCalls.Add(1)
		if s.failPageAnalysis {
			// Invalid JSON shape → AnalyzePage returns an error → dispatcher
			// logs and skips the page (so analyses is not appended).
			return nil, errors.New("simulated upstream LLM failure")
		}
		return json.RawMessage(`{"summary":"page summary","features":["feature-one"],"is_docs":true}`), nil
	case "synthesize_response":
		s.synthesizeCalls.Add(1)
		return json.RawMessage(`{"description":"A test product (newly synthesized).","features":["feature-one"]}`), nil
	case "code_features_response":
		return json.RawMessage(`{"features":[{"name":"feature-one","description":"Does feature one.","layer":"cli","user_facing":true}]}`), nil
	case "map_response":
		return json.RawMessage(`{"entries":[{"feature":"feature-one","files":["main.go"],"symbols":["Run"]}]}`), nil
	case "map_page_response":
		return json.RawMessage(`{"features":["feature-one"]}`), nil
	}
	return json.RawMessage(`{}`), nil
}

func (s *freshCountStubClient) CompleteJSONMultimodal(_ context.Context, _ []analyzer.ChatMessage, _ analyzer.JSONSchema) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (s *freshCountStubClient) Capabilities() analyzer.ModelCapabilities {
	return analyzer.ModelCapabilities{}
}

// seedFreshCountFixture pre-populates a project tree with `cacheHits` page
// cache entries already analyzed (so freshCount's cacheHitCount baseline is
// non-zero) plus `freshURLs` whose page-on-disk content exists but whose
// analysis has not been recorded (so they hit the dispatch loop). It also
// pre-seeds the product summary so the cache-short-circuit branch has
// something to fall back to when freshCount is 0.
func seedFreshCountFixture(t *testing.T, repoDir, projectDir string, cacheHitURLs, freshURLs []string) string {
	t.Helper()

	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "main.go"), []byte("package main\nfunc Run() {}\n"), 0o644))

	docsDir := filepath.Join(projectDir, "docs")
	require.NoError(t, os.MkdirAll(docsDir, 0o755))
	idx, err := spider.LoadIndex(docsDir)
	require.NoError(t, err)

	for _, u := range cacheHitURLs {
		filename := spider.URLToFilename(u)
		require.NoError(t, os.WriteFile(filepath.Join(docsDir, filename), []byte("# Cached page\n\nFeature one.\n"), 0o644))
		require.NoError(t, idx.Record(u, filename))
		require.NoError(t, idx.RecordAnalysis(u, "Cached summary.", []string{"feature-one"}, true))
	}
	for _, u := range freshURLs {
		filename := spider.URLToFilename(u)
		require.NoError(t, os.WriteFile(filepath.Join(docsDir, filename), []byte("# Fresh page\n\nFeature one details.\n"), 0o644))
		require.NoError(t, idx.Record(u, filename))
	}
	// Pre-seed product summary so the freshCount==0 branch can short-circuit
	// to the cached value when we want to assert SynthesizeProduct is not called.
	const cachedDesc = "A pre-existing cached product summary."
	require.NoError(t, idx.SetProductSummary(cachedDesc, []string{"feature-one"}))
	return cachedDesc
}

// TestFreshCount_AllPagesFail_UsesCachedProductSummary is the regression test
// for the freshCount bug. With cacheHits>0 and a pre-seeded product summary,
// when every fresh AnalyzePage errors-and-is-skipped, the dispatch loop
// produces zero new analyses. freshCount must therefore evaluate to zero so
// the cache short-circuit engages and SynthesizeProduct is NOT invoked
// against the (cache-hit-only) analyses slice.
//
// Pre-fix this would fail: pageNum was incremented before AnalyzePage, so
// freshCount was the count of dispatched jobs (== 2), the cache branch was
// skipped, and SynthesizeProduct fired against the cache-hit-only set.
func TestFreshCount_AllFreshPagesFail_UsesCachedProductSummary(t *testing.T) {
	repoDir := t.TempDir()
	cacheBase := t.TempDir()
	projectName := filepath.Base(filepath.Clean(repoDir))
	projectDir := filepath.Join(cacheBase, projectName)

	cacheHitURLs := []string{
		"https://docs.example.com/cached-1",
		"https://docs.example.com/cached-2",
		"https://docs.example.com/cached-3",
	}
	freshURLs := []string{
		"https://docs.example.com/fresh-1",
		"https://docs.example.com/fresh-2",
	}
	cachedDesc := seedFreshCountFixture(t, repoDir, projectDir, cacheHitURLs, freshURLs)

	stub := &freshCountStubClient{failPageAnalysis: true}
	prevFactory := tieringFactory
	t.Cleanup(func() { tieringFactory = prevFactory })
	tieringFactory = func(_, _, _ string) (analyzer.LLMTiering, error) {
		return &stubTiering{client: stub, counter: analyzer.NewTiktokenCounter()}, nil
	}

	args := []string{
		"analyze",
		"--repo", repoDir,
		"--cache-dir", cacheBase,
		"--docs-url", cacheHitURLs[0], // any seeded URL works; spider skips fetch when entries exist
		"--workers", "2",
		"--no-site",
	}

	var stdout, stderr bytes.Buffer
	if code := run(&stdout, &stderr, args); code != 0 {
		t.Fatalf("analyze run failed (code=%d): stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	assert.Equal(t, int32(len(freshURLs)), stub.pageCalls.Load(),
		"every fresh page must have hit the per-page LLM (and failed)")
	assert.Equal(t, int32(0), stub.synthesizeCalls.Load(),
		"SynthesizeProduct must NOT fire when every fresh AnalyzePage failed; "+
			"freshCount should resolve to 0 and the cached product summary should be used")

	// Belt-and-braces: confirm the on-disk product summary was not overwritten.
	idx, err := spider.LoadIndex(filepath.Join(projectDir, "docs"))
	require.NoError(t, err)
	gotDesc, _ := idx.ProductInfo()
	assert.Equal(t, cachedDesc, gotDesc,
		"on-disk product summary must remain the pre-seeded cached value (SetProductSummary should not have been called)")
}

// TestFreshCount_SomeFreshPagesSucceed_RunsSynthesize is the companion test:
// when at least one fresh AnalyzePage succeeds, freshCount > 0 and
// SynthesizeProduct must fire (overwriting the cached summary).
func TestFreshCount_SomeFreshPagesSucceed_RunsSynthesize(t *testing.T) {
	repoDir := t.TempDir()
	cacheBase := t.TempDir()
	projectName := filepath.Base(filepath.Clean(repoDir))
	projectDir := filepath.Join(cacheBase, projectName)

	cacheHitURLs := []string{
		"https://docs.example.com/cached-1",
		"https://docs.example.com/cached-2",
		"https://docs.example.com/cached-3",
	}
	freshURLs := []string{
		"https://docs.example.com/fresh-1",
		"https://docs.example.com/fresh-2",
	}
	cachedDesc := seedFreshCountFixture(t, repoDir, projectDir, cacheHitURLs, freshURLs)

	stub := &freshCountStubClient{failPageAnalysis: false}
	prevFactory := tieringFactory
	t.Cleanup(func() { tieringFactory = prevFactory })
	tieringFactory = func(_, _, _ string) (analyzer.LLMTiering, error) {
		return &stubTiering{client: stub, counter: analyzer.NewTiktokenCounter()}, nil
	}

	args := []string{
		"analyze",
		"--repo", repoDir,
		"--cache-dir", cacheBase,
		"--docs-url", cacheHitURLs[0],
		"--workers", "2",
		"--no-site",
	}

	var stdout, stderr bytes.Buffer
	if code := run(&stdout, &stderr, args); code != 0 {
		t.Fatalf("analyze run failed (code=%d): stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	assert.Equal(t, int32(len(freshURLs)), stub.pageCalls.Load(),
		"every fresh page must have hit the per-page LLM")
	assert.Equal(t, int32(1), stub.synthesizeCalls.Load(),
		"SynthesizeProduct must fire exactly once when fresh pages produced new analyses")

	idx, err := spider.LoadIndex(filepath.Join(projectDir, "docs"))
	require.NoError(t, err)
	gotDesc, _ := idx.ProductInfo()
	assert.NotEqual(t, cachedDesc, gotDesc,
		"product summary on disk must have been overwritten by the fresh synthesize run")
	assert.True(t, strings.Contains(gotDesc, "newly synthesized"),
		"on-disk product summary should reflect the stubbed fresh response, got %q", gotDesc)
}

// TestFreshCount_PerPageLogPrefix asserts the parallel log line uses just the
// URL (no leading "[N]" sequence number), since dispatch order is not
// meaningful in parallel mode and the pre-fix counter no longer exists.
func TestFreshCount_PerPageLogPrefix(t *testing.T) {
	repoDir := t.TempDir()
	cacheBase := t.TempDir()
	projectName := filepath.Base(filepath.Clean(repoDir))
	projectDir := filepath.Join(cacheBase, projectName)

	freshURLs := []string{
		"https://docs.example.com/page-A",
		"https://docs.example.com/page-B",
	}
	seedFreshCountFixture(t, repoDir, projectDir, nil, freshURLs)

	stub := &freshCountStubClient{}
	prevFactory := tieringFactory
	t.Cleanup(func() { tieringFactory = prevFactory })
	tieringFactory = func(_, _, _ string) (analyzer.LLMTiering, error) {
		return &stubTiering{client: stub, counter: analyzer.NewTiktokenCounter()}, nil
	}

	args := []string{
		"analyze",
		"--repo", repoDir,
		"--cache-dir", cacheBase,
		"--docs-url", freshURLs[0],
		"--workers", "2",
		"--no-site",
		"-v",
	}

	var stdout, stderr bytes.Buffer
	if code := run(&stdout, &stderr, args); code != 0 {
		t.Fatalf("analyze run failed (code=%d): stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	combined := stdout.String() + stderr.String()
	for i := 1; i <= len(freshURLs); i++ {
		needle := fmt.Sprintf("[%d]", i)
		assert.NotContains(t, combined, needle,
			"per-page log line must not include misleading sequence-number prefix %q in parallel mode", needle)
	}
}
