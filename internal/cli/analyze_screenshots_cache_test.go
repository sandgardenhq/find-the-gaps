package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

// screenshotsStubClient is a counting stub for the analyze command's
// screenshots cache tests. It tracks how many times each LLM entry-point is
// invoked so the test can prove the cache short-circuit fires (zero LLM
// calls on a sentinel match) and conversely that a partial cache forces only
// missing pages through the model.
type screenshotsStubClient struct {
	jsonCalls atomic.Int64
}

func (s *screenshotsStubClient) Complete(_ context.Context, _ string) (string, error) {
	return "no", nil
}

func (s *screenshotsStubClient) CompleteWithTools(_ context.Context, _ []analyzer.ChatMessage, _ []analyzer.Tool, _ ...analyzer.AgentOption) (analyzer.AgentResult, error) {
	return analyzer.AgentResult{
		FinalMessage: analyzer.ChatMessage{Role: "assistant", Content: "done"},
		Rounds:       1,
	}, nil
}

func (s *screenshotsStubClient) CompleteJSON(_ context.Context, _ string, schema analyzer.JSONSchema) (json.RawMessage, error) {
	if schema.Name == "screenshot_gaps_response" {
		s.jsonCalls.Add(1)
		return json.RawMessage(`{"gaps":[],"suppressed_by_image":[],"suppressed_by_code_block":[]}`), nil
	}
	switch schema.Name {
	case "code_features_response":
		return json.RawMessage(`{"features":[{"name":"feature-one","description":"Does feature one.","layer":"cli","user_facing":true}]}`), nil
	case "map_response":
		return json.RawMessage(`{"entries":[{"feature":"feature-one","files":["main.go"],"symbols":["Run"]}]}`), nil
	case "map_page_response":
		return json.RawMessage(`{"features":["feature-one"]}`), nil
	}
	return json.RawMessage(`{}`), nil
}

func (s *screenshotsStubClient) CompleteJSONMultimodal(_ context.Context, _ []analyzer.ChatMessage, _ analyzer.JSONSchema) (json.RawMessage, error) {
	return json.RawMessage(`{"image_issues":[],"verdicts":[]}`), nil
}

func (s *screenshotsStubClient) Capabilities() analyzer.ModelCapabilities {
	return analyzer.ModelCapabilities{}
}

type screenshotsStubTiering struct {
	client  *screenshotsStubClient
	counter analyzer.TokenCounter
}

func (t *screenshotsStubTiering) Small() analyzer.LLMClient             { return t.client }
func (t *screenshotsStubTiering) Typical() analyzer.LLMClient           { return t.client }
func (t *screenshotsStubTiering) Large() analyzer.LLMClient             { return t.client }
func (t *screenshotsStubTiering) SmallCounter() analyzer.TokenCounter   { return t.counter }
func (t *screenshotsStubTiering) TypicalCounter() analyzer.TokenCounter { return t.counter }
func (t *screenshotsStubTiering) LargeCounter() analyzer.TokenCounter   { return t.counter }

// seedScreenshotsFixture mirrors seedSkipDriftFixture but registers the
// supplied number of docs pages so the screenshot pass has multiple URLs to
// iterate. Each page's content is a fixed template so the test can compute
// the expected ContentHash deterministically.
func seedScreenshotsFixture(t *testing.T, repoDir, projectDir string, urls []string, contentByURL map[string]string) {
	t.Helper()

	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "main.go"),
		[]byte("package main\nfunc Run() {}\n"), 0o644))

	docsDir := filepath.Join(projectDir, "docs")
	require.NoError(t, os.MkdirAll(docsDir, 0o755))
	idx, err := spider.LoadIndex(docsDir)
	require.NoError(t, err)
	for _, url := range urls {
		filename := spider.URLToFilename(url)
		require.NoError(t, os.WriteFile(filepath.Join(docsDir, filename), []byte(contentByURL[url]), 0o644))
		require.NoError(t, idx.Record(url, filename))
		require.NoError(t, idx.RecordAnalysis(url, "doc page.", []string{"feature-one"}, true))
	}
	require.NoError(t, idx.SetProductSummary("A test product.", []string{"feature-one"}))

	require.NoError(t, os.MkdirAll(projectDir, 0o755))
	codeFeatures := []analyzer.CodeFeature{
		{Name: "feature-one", Description: "Does feature one.", Layer: "cli", UserFacing: true},
	}
	scanForCache := &scanner.ProjectScan{Files: []scanner.ScannedFile{{Path: "main.go"}}}
	require.NoError(t, saveCodeFeaturesCache(filepath.Join(projectDir, "codefeatures.json"),
		scanForCache, codeFeatures))

	fmCache := analyzer.FeatureMap{
		{Feature: codeFeatures[0], Files: []string{"main.go"}, Symbols: []string{"Run"}},
	}
	require.NoError(t, saveFeatureMapCache(filepath.Join(projectDir, "featuremap.json"), codeFeatures, fmCache))

	docsFM := analyzer.DocsFeatureMap{{Feature: "feature-one", Pages: urls}}
	require.NoError(t, saveDocsFeatureMapCache(filepath.Join(projectDir, "docsfeaturemap.json"), []string{"feature-one"}, docsFM))
}

// hashStr is the same SHA-256 hex used by the screenshots cache helpers.
func hashStr(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// TestAnalyze_screenshotsSkipOnCachedComplete pre-seeds screenshots.json with
// a completion sentinel matching the input hash and a screenshots.md file.
// The expected behavior: zero detection-pass LLM calls fire and screenshots.md
// is left untouched (mtime preserved).
func TestAnalyze_screenshotsSkipOnCachedComplete(t *testing.T) {
	docsURL := "https://docs.example.com/page"
	pageContent := "# Page\n\nClick Save.\n"
	repoDir := t.TempDir()
	cacheBase := t.TempDir()
	projectName := filepath.Base(filepath.Clean(repoDir))
	projectDir := filepath.Join(cacheBase, projectName)

	seedScreenshotsFixture(t, repoDir, projectDir,
		[]string{docsURL},
		map[string]string{docsURL: pageContent})

	llmSmall := "ollama/test"
	docPages := []analyzer.DocPage{{
		URL:     docsURL,
		Path:    filepath.Join(projectDir, "docs", spider.URLToFilename(docsURL)),
		Content: pageContent,
	}}
	wantHash := computeScreenshotsInputHash(docPages, llmSmall)

	screenshotsCachePath := filepath.Join(projectDir, "screenshots-cache.json")
	live := map[string]screenshotsCacheEntry{
		screenshotsCacheKey(docsURL, hashStr(pageContent)): {
			URL:         docsURL,
			ContentHash: hashStr(pageContent),
			Stats: analyzer.ScreenshotPageStats{
				PageURL:            docsURL,
				MissingScreenshots: 0,
			},
			Missing:     []analyzer.ScreenshotGap{},
			Possibly:    []analyzer.ScreenshotGap{},
			ImageIssues: []analyzer.ImageIssue{},
		},
	}
	require.NoError(t, saveScreenshotsCacheComplete(screenshotsCachePath, live, &screenshotsComplete{
		Hash:        wantHash,
		CompletedAt: time.Now().UTC(),
	}))

	// Pre-seed screenshots.md with a sentinel body so we can detect tampering.
	screenshotsMdPath := filepath.Join(projectDir, "screenshots.md")
	const sentinelBody = "# Sentinel screenshots.md\n"
	require.NoError(t, os.WriteFile(screenshotsMdPath, []byte(sentinelBody), 0o644))
	beforeStat, err := os.Stat(screenshotsMdPath)
	require.NoError(t, err)
	beforeMtime := beforeStat.ModTime()

	stub := &screenshotsStubClient{}
	prevFactory := tieringFactory
	t.Cleanup(func() { tieringFactory = prevFactory })
	tieringFactory = func(_, _, _ string) (analyzer.LLMTiering, error) {
		return &screenshotsStubTiering{client: stub, counter: analyzer.NewTiktokenCounter()}, nil
	}

	args := []string{
		"analyze",
		"--repo", repoDir,
		"--cache-dir", cacheBase,
		"--docs", docsURL,
		"--llm-small", llmSmall,
		"--no-site",
		"--experimental-check-screenshots",
	}

	var stdout, stderr bytes.Buffer
	if code := run(&stdout, &stderr, args); code != 0 {
		t.Fatalf("run failed (code=%d): stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "screenshots: cache complete, skipping") {
		t.Errorf("expected skip log line; got: %s", combined)
	}
	assert.EqualValues(t, 0, stub.jsonCalls.Load(),
		"detection-pass LLM call must NOT fire on sentinel match")

	afterStat, err := os.Stat(screenshotsMdPath)
	require.NoError(t, err)
	assert.True(t, afterStat.ModTime().Equal(beforeMtime),
		"screenshots.md mtime must be unchanged on skip; before=%v after=%v",
		beforeMtime, afterStat.ModTime())

	// File body must still be the sentinel — proving the writer didn't run.
	body, err := os.ReadFile(screenshotsMdPath)
	require.NoError(t, err)
	assert.Equal(t, sentinelBody, string(body))
}

// TestAnalyze_screenshotsResumesAfterPartialCache pre-seeds screenshots.json
// with entries for 1 of 2 pages and NO completion sentinel. The expected
// behavior: 1 fresh detection call fires (only the un-cached page) and the
// final cache contains both pages plus a completion sentinel.
func TestAnalyze_screenshotsResumesAfterPartialCache(t *testing.T) {
	docsURLA := "https://docs.example.com/a"
	docsURLB := "https://docs.example.com/b"
	contentA := "# A\n\nClick Save.\n"
	contentB := "# B\n\nFill the form.\n"
	repoDir := t.TempDir()
	cacheBase := t.TempDir()
	projectName := filepath.Base(filepath.Clean(repoDir))
	projectDir := filepath.Join(cacheBase, projectName)

	seedScreenshotsFixture(t, repoDir, projectDir,
		[]string{docsURLA, docsURLB},
		map[string]string{docsURLA: contentA, docsURLB: contentB})

	// Pre-seed cache with the A entry only; no completion sentinel.
	screenshotsCachePath := filepath.Join(projectDir, "screenshots-cache.json")
	live := map[string]screenshotsCacheEntry{
		screenshotsCacheKey(docsURLA, hashStr(contentA)): {
			URL:         docsURLA,
			ContentHash: hashStr(contentA),
			Stats: analyzer.ScreenshotPageStats{
				PageURL:            docsURLA,
				MissingScreenshots: 0,
			},
			Missing:     []analyzer.ScreenshotGap{},
			Possibly:    []analyzer.ScreenshotGap{},
			ImageIssues: []analyzer.ImageIssue{},
		},
	}
	require.NoError(t, saveScreenshotsCache(screenshotsCachePath, live))

	stub := &screenshotsStubClient{}
	prevFactory := tieringFactory
	t.Cleanup(func() { tieringFactory = prevFactory })
	tieringFactory = func(_, _, _ string) (analyzer.LLMTiering, error) {
		return &screenshotsStubTiering{client: stub, counter: analyzer.NewTiktokenCounter()}, nil
	}

	args := []string{
		"analyze",
		"--repo", repoDir,
		"--cache-dir", cacheBase,
		"--docs", docsURLA, // crawl seed; tells the spider where to start
		"--llm-small", "ollama/test",
		"--no-site",
		"--experimental-check-screenshots",
	}

	var stdout, stderr bytes.Buffer
	if code := run(&stdout, &stderr, args); code != 0 {
		t.Fatalf("run failed (code=%d): stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	// Exactly one fresh detection call: page B was missing from cache.
	assert.EqualValues(t, 1, stub.jsonCalls.Load(),
		"exactly one fresh detection call expected (1 of 2 pages was cached)")

	// Final cache must contain both entries and a completion sentinel.
	file, ok := loadScreenshotsCacheFile(screenshotsCachePath)
	require.True(t, ok, "screenshots.json must exist after run")
	require.NotNil(t, file.Complete, "completion sentinel must be stamped")
	require.NotEmpty(t, file.Complete.Hash)
	assert.Len(t, file.Entries, 2, "final cache must contain both pages")
}

