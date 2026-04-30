package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
	"github.com/sandgardenhq/find-the-gaps/internal/spider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// skipDriftStubClient counts CompleteWithTools invocations and returns a single
// "done" plain-text turn so the investigator records zero observations and the
// judge is skipped.
type skipDriftStubClient struct {
	toolCalls atomic.Int64
}

func (s *skipDriftStubClient) Complete(_ context.Context, _ string) (string, error) {
	// Small tier hits this for the release-note classifier; "no" means
	// "not release notes" so the page is included in drift.
	return "no", nil
}

func (s *skipDriftStubClient) CompleteWithTools(_ context.Context, _ []analyzer.ChatMessage, _ []analyzer.Tool, _ ...analyzer.AgentOption) (analyzer.AgentResult, error) {
	s.toolCalls.Add(1)
	// Return a single plain-text turn with no tool calls so the investigator
	// loop terminates immediately. The judge is then skipped because zero
	// observations were recorded.
	return analyzer.AgentResult{
		FinalMessage: analyzer.ChatMessage{Role: "assistant", Content: "done"},
		Rounds:       1,
	}, nil
}

func (s *skipDriftStubClient) CompleteJSON(_ context.Context, _ string, _ analyzer.JSONSchema) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

// skipDriftStubTiering wires the same client into all three tiers; mirrors
// stubTiering in analyze_parallel_test.go but uses an analyzer.LLMTiering
// implementation so the tieringFactory hook accepts it directly.
type skipDriftStubTiering struct {
	client  *skipDriftStubClient
	counter analyzer.TokenCounter
}

func (t *skipDriftStubTiering) Small() analyzer.LLMClient             { return t.client }
func (t *skipDriftStubTiering) Typical() analyzer.LLMClient           { return t.client }
func (t *skipDriftStubTiering) Large() analyzer.LLMClient             { return t.client }
func (t *skipDriftStubTiering) SmallCounter() analyzer.TokenCounter   { return t.counter }
func (t *skipDriftStubTiering) TypicalCounter() analyzer.TokenCounter { return t.counter }
func (t *skipDriftStubTiering) LargeCounter() analyzer.TokenCounter   { return t.counter }

// seedSkipDriftFixture pre-populates a project tree so analyze can reach the
// drift step entirely from cache. The single feature has Files (non-empty) and
// the docs map has Pages (non-empty) so DetectDrift will exercise the
// investigator path on a cold run.
func seedSkipDriftFixture(t *testing.T, repoDir, projectDir, docsURL string) {
	t.Helper()

	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "main.go"), []byte("package main\nfunc Run() {}\n"), 0o644))

	docsDir := filepath.Join(projectDir, "docs")
	require.NoError(t, os.MkdirAll(docsDir, 0o755))
	idx, err := spider.LoadIndex(docsDir)
	require.NoError(t, err)
	filename := spider.URLToFilename(docsURL)
	require.NoError(t, os.WriteFile(filepath.Join(docsDir, filename), []byte("# Doc page\n\nFeature one is documented.\n"), 0o644))
	require.NoError(t, idx.Record(docsURL, filename))
	require.NoError(t, idx.RecordAnalysis(docsURL, "Covers feature one.", []string{"feature-one"}))
	require.NoError(t, idx.SetProductSummary("A test product.", []string{"feature-one"}))

	require.NoError(t, os.MkdirAll(projectDir, 0o755))
	codeFeatures := []analyzer.CodeFeature{
		{Name: "feature-one", Description: "Does feature one.", Layer: "cli", UserFacing: true},
	}
	scanForCache := &scanner.ProjectScan{Files: []scanner.ScannedFile{{Path: "main.go"}}}
	require.NoError(t, saveCodeFeaturesCache(filepath.Join(projectDir, "codefeatures.json"), scanForCache, codeFeatures))

	fmCache := analyzer.FeatureMap{
		{Feature: codeFeatures[0], Files: []string{"main.go"}, Symbols: []string{"Run"}},
	}
	require.NoError(t, saveFeatureMapCache(filepath.Join(projectDir, "featuremap.json"), codeFeatures, fmCache))

	docsFM := analyzer.DocsFeatureMap{{Feature: "feature-one", Pages: []string{docsURL}}}
	require.NoError(t, saveDocsFeatureMapCache(filepath.Join(projectDir, "docsfeaturemap.json"), []string{"feature-one"}, docsFM))
}

// TestAnalyzeSkipsDriftOnSecondRun exercises the four scenarios from the plan:
//   1. Cold run — drift runs, gaps.md written, drift.json gets a complete sentinel.
//   2. Warm run — same inputs, drift skipped, gaps.md untouched.
//   3. Mutate inputs — featuremap.json changed, drift re-runs, hash changes.
//   4. Delete gaps.md — even with an unchanged hash, the absence of gaps.md
//      forces drift to re-run.
//
// Investigator activity is measured by counting CompleteWithTools calls on the
// injected stub — that is the clearest signal that DetectDrift actually fired.
func TestAnalyzeSkipsDriftOnSecondRun(t *testing.T) {
	docsURL := "https://docs.example.com/page"
	repoDir := t.TempDir()
	cacheBase := t.TempDir()
	projectName := filepath.Base(filepath.Clean(repoDir))
	projectDir := filepath.Join(cacheBase, projectName)

	seedSkipDriftFixture(t, repoDir, projectDir, docsURL)

	stub := &skipDriftStubClient{}
	prevFactory := tieringFactory
	t.Cleanup(func() { tieringFactory = prevFactory })
	tieringFactory = func(_, _, _ string) (analyzer.LLMTiering, error) {
		return &skipDriftStubTiering{client: stub, counter: analyzer.NewTiktokenCounter()}, nil
	}

	args := []string{
		"analyze",
		"--repo", repoDir,
		"--cache-dir", cacheBase,
		"--docs-url", docsURL,
		"--skip-screenshot-check",
		"--no-site",
	}

	gapsPath := filepath.Join(projectDir, "gaps.md")
	driftPath := filepath.Join(projectDir, "drift.json")

	// --- Scenario 1: cold run ---
	var stdout1, stderr1 bytes.Buffer
	if code := run(&stdout1, &stderr1, args); code != 0 {
		t.Fatalf("cold run failed (code=%d): stdout=%q stderr=%q", code, stdout1.String(), stderr1.String())
	}
	coldCalls := stub.toolCalls.Load()
	require.Greater(t, coldCalls, int64(0), "investigator must fire on cold run")

	gapsInfo, err := os.Stat(gapsPath)
	require.NoError(t, err, "gaps.md must exist after cold run")
	coldMtime := gapsInfo.ModTime()

	coldFile, ok := loadDriftCacheFile(driftPath)
	require.True(t, ok, "drift.json must exist after cold run")
	require.NotNil(t, coldFile.Complete, "drift.json must carry a completion sentinel")
	require.NotEmpty(t, coldFile.Complete.Hash, "completion hash must be set")
	coldHash := coldFile.Complete.Hash

	combined1 := stdout1.String() + stderr1.String()
	if strings.Contains(combined1, "(cached, drift unchanged)") {
		t.Errorf("cold run must NOT annotate gaps.md as cached; got: %s", combined1)
	}

	// --- Scenario 2: warm run, same inputs ---
	var stdout2, stderr2 bytes.Buffer
	if code := run(&stdout2, &stderr2, args); code != 0 {
		t.Fatalf("warm run failed (code=%d): stdout=%q stderr=%q", code, stdout2.String(), stderr2.String())
	}
	require.Equal(t, coldCalls, stub.toolCalls.Load(),
		"investigator must NOT fire on warm run (skip path should engage)")

	warmInfo, err := os.Stat(gapsPath)
	require.NoError(t, err)
	require.True(t, warmInfo.ModTime().Equal(coldMtime),
		"gaps.md mtime must be unchanged on warm run; before=%v after=%v",
		coldMtime, warmInfo.ModTime())

	warmFile, ok := loadDriftCacheFile(driftPath)
	require.True(t, ok)
	require.NotNil(t, warmFile.Complete)
	assert.Equal(t, coldHash, warmFile.Complete.Hash, "completion hash must be unchanged on warm run")

	combined2 := stdout2.String() + stderr2.String()
	if !strings.Contains(combined2, "(cached, drift unchanged)") {
		t.Errorf("warm run must annotate gaps.md as cached; got: %s", combined2)
	}

	// --- Scenario 3: mutate the upstream featureMap so the hash changes ---
	codeFeatures := []analyzer.CodeFeature{
		{Name: "feature-one", Description: "Does feature one.", Layer: "cli", UserFacing: true},
	}
	mutatedFM := analyzer.FeatureMap{
		{
			Feature: codeFeatures[0],
			Files:   []string{"main.go", "extra.go"}, // added file → new hash
			Symbols: []string{"Run"},
		},
	}
	require.NoError(t, saveFeatureMapCache(filepath.Join(projectDir, "featuremap.json"), codeFeatures, mutatedFM))

	beforeMutate := stub.toolCalls.Load()

	var stdout3, stderr3 bytes.Buffer
	if code := run(&stdout3, &stderr3, args); code != 0 {
		t.Fatalf("mutated run failed (code=%d): stdout=%q stderr=%q", code, stdout3.String(), stderr3.String())
	}
	require.Greater(t, stub.toolCalls.Load(), beforeMutate,
		"investigator MUST fire when the input hash changes")

	mutatedFile, ok := loadDriftCacheFile(driftPath)
	require.True(t, ok)
	require.NotNil(t, mutatedFile.Complete)
	assert.NotEqual(t, coldHash, mutatedFile.Complete.Hash,
		"completion hash must change after the featureMap mutation")
	mutatedHash := mutatedFile.Complete.Hash

	// --- Scenario 4: restore inputs, delete gaps.md, re-run ---
	require.NoError(t, saveFeatureMapCache(filepath.Join(projectDir, "featuremap.json"),
		codeFeatures, analyzer.FeatureMap{
			{Feature: codeFeatures[0], Files: []string{"main.go"}, Symbols: []string{"Run"}},
		}))
	// Re-run once with the restored inputs to land a matching sentinel.
	var stdout4a, stderr4a bytes.Buffer
	if code := run(&stdout4a, &stderr4a, args); code != 0 {
		t.Fatalf("restore run failed (code=%d): stdout=%q stderr=%q", code, stdout4a.String(), stderr4a.String())
	}
	restoredFile, ok := loadDriftCacheFile(driftPath)
	require.True(t, ok)
	require.NotNil(t, restoredFile.Complete)
	assert.NotEqual(t, mutatedHash, restoredFile.Complete.Hash,
		"hash must shift back when inputs are restored")
	require.NoError(t, os.Remove(gapsPath))
	if _, statErr := os.Stat(gapsPath); !os.IsNotExist(statErr) {
		t.Fatalf("gaps.md should have been removed; stat err=%v", statErr)
	}

	var stdout4, stderr4 bytes.Buffer
	if code := run(&stdout4, &stderr4, args); code != 0 {
		t.Fatalf("missing-gaps run failed (code=%d): stdout=%q stderr=%q", code, stdout4.String(), stderr4.String())
	}
	combined4 := stdout4.String() + stderr4.String()
	// The skip path must NOT engage when gaps.md is missing, even if the hash
	// matches. The clearest external signal is that the run does not annotate
	// gaps.md as "(cached, drift unchanged)" and that gaps.md is re-created.
	if strings.Contains(combined4, "(cached, drift unchanged)") {
		t.Errorf("missing gaps.md must force drift to re-run; skip path should NOT engage. stdout=%s", combined4)
	}
	if _, err := os.Stat(gapsPath); err != nil {
		t.Errorf("gaps.md must be re-created after a forced re-run: %v", err)
	}
}
