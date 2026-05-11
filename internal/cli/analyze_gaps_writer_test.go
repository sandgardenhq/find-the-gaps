package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/reporter"
	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
	"github.com/sandgardenhq/find-the-gaps/internal/spider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAnalyze_finalGapsMdMatchesWriteGaps pins the GapsWriter wiring on the
// live drift path: the bytes flushed by the writer at Close must match what
// reporter.WriteGaps would produce against the same final findings slice. The
// fixture seeds drift.json with three cache entries (and no completion
// sentinel) so DetectDrift takes the live path, hits each cached entry, and
// streams findings through the writer's Push callback.
func TestAnalyze_finalGapsMdMatchesWriteGaps(t *testing.T) {
	docsURL := "https://docs.example.com/page"
	repoDir := t.TempDir()
	cacheBase := t.TempDir()
	projectName := filepath.Base(filepath.Clean(repoDir))
	projectDir := filepath.Join(cacheBase, projectName)

	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "main.go"),
		[]byte("package main\nfunc Run() {}\nfunc Two() {}\nfunc Three() {}\n"), 0o644))

	docsDir := filepath.Join(projectDir, "docs")
	require.NoError(t, os.MkdirAll(docsDir, 0o755))
	idx, err := spider.LoadIndex(docsDir)
	require.NoError(t, err)
	filename := spider.URLToFilename(docsURL)
	require.NoError(t, os.WriteFile(filepath.Join(docsDir, filename),
		[]byte("# Doc page\n\nFeature one, feature two, feature three are documented.\n"), 0o644))
	require.NoError(t, idx.Record(docsURL, filename))
	require.NoError(t, idx.RecordAnalysis(docsURL, "Covers all features.",
		[]string{"feature-one", "feature-two", "feature-three"}, true, "reference"))
	require.NoError(t, idx.SetProductSummary("A test product.",
		[]string{"feature-one", "feature-two", "feature-three"}))

	require.NoError(t, os.MkdirAll(projectDir, 0o755))
	codeFeatures := []analyzer.CodeFeature{
		{Name: "feature-one", Description: "One.", Layer: "cli", UserFacing: true},
		{Name: "feature-two", Description: "Two.", Layer: "cli", UserFacing: true},
		{Name: "feature-three", Description: "Three.", Layer: "cli", UserFacing: false},
	}
	scanForCache := &scanner.ProjectScan{Files: []scanner.ScannedFile{{Path: "main.go"}}}
	require.NoError(t, saveCodeFeaturesCache(filepath.Join(projectDir, "codefeatures.json"),
		scanForCache, codeFeatures))

	featureMap := analyzer.FeatureMap{
		{Feature: codeFeatures[0], Files: []string{"main.go"}, Symbols: []string{"Run"}},
		{Feature: codeFeatures[1], Files: []string{"main.go"}, Symbols: []string{"Two"}},
		{Feature: codeFeatures[2], Files: []string{"main.go"}, Symbols: []string{"Three"}},
	}
	require.NoError(t, saveFeatureMapCache(filepath.Join(projectDir, "featuremap.json"),
		codeFeatures, featureMap))

	docsFM := analyzer.DocsFeatureMap{
		{Feature: "feature-one", Pages: []string{docsURL}},
		{Feature: "feature-two", Pages: []string{docsURL}},
		{Feature: "feature-three", Pages: []string{docsURL}},
	}
	require.NoError(t, saveDocsFeatureMapCache(filepath.Join(projectDir, "docsfeaturemap.json"),
		[]string{"feature-one", "feature-two", "feature-three"}, docsFM))

	// Seed drift.json with cache entries (no Complete sentinel) so DetectDrift
	// takes the live path and onFinding fires for each cache hit.
	driftCache := map[string]analyzer.CachedDriftEntry{
		"feature-one": {
			Files:         []string{"main.go"},
			FilteredPages: []string{docsURL},
			Pages:         []string{docsURL},
			Issues: []analyzer.DriftIssue{{
				Page:           docsURL,
				Issue:          "feature-one signature out of date",
				Priority:       analyzer.PriorityLarge,
				PriorityReason: "user-facing API change",
			}},
		},
		"feature-two": {
			Files:         []string{"main.go"},
			FilteredPages: []string{docsURL},
			Pages:         []string{docsURL},
			Issues: []analyzer.DriftIssue{{
				Page:           docsURL,
				Issue:          "feature-two example missing argument",
				Priority:       analyzer.PriorityMedium,
				PriorityReason: "example doesn't compile",
			}},
		},
		"feature-three": {
			Files:         []string{"main.go"},
			FilteredPages: []string{docsURL},
			Pages:         []string{docsURL},
			Issues: []analyzer.DriftIssue{{
				Page:           docsURL,
				Issue:          "feature-three deprecated note absent",
				Priority:       analyzer.PrioritySmall,
				PriorityReason: "minor",
			}},
		},
	}
	driftCachePath := filepath.Join(projectDir, "drift.json")
	require.NoError(t, saveDriftCache(driftCachePath, driftCache))

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
		"--docs", docsURL,
		"--no-site",
	}

	var stdout, stderr bytes.Buffer
	if code := run(&stdout, &stderr, args); code != 0 {
		t.Fatalf("analyze run failed (code=%d): stdout=%q stderr=%q",
			code, stdout.String(), stderr.String())
	}

	// Cache hits short-circuit the investigator; tool calls must be zero.
	require.Equal(t, int64(0), stub.toolCalls.Load(),
		"investigator must not fire when every feature hits the drift cache")

	got, err := os.ReadFile(filepath.Join(projectDir, "gaps.md"))
	require.NoError(t, err)

	// Expected bytes: drive WriteGaps with the same featureMap, doc-covered
	// features, and findings in featureMap insertion order (which is what the
	// live path produces).
	docCovered := []string{"feature-one", "feature-two", "feature-three"}
	expectedFindings := make([]analyzer.DriftFinding, 0, len(featureMap))
	for _, e := range featureMap {
		c, ok := driftCache[e.Feature.Name]
		if !ok || len(c.Issues) == 0 {
			continue
		}
		expectedFindings = append(expectedFindings, analyzer.DriftFinding{
			Feature: e.Feature.Name,
			Issues:  c.Issues,
		})
	}

	expectedDir := t.TempDir()
	require.NoError(t, reporter.WriteGaps(expectedDir, featureMap, docCovered, expectedFindings))
	want, err := os.ReadFile(filepath.Join(expectedDir, "gaps.md"))
	require.NoError(t, err)

	assert.Equal(t, string(want), string(got),
		"final gaps.md from the GapsWriter must match reporter.WriteGaps byte-for-byte")

	// Belt-and-braces: the writer leaves no .tmp file behind.
	tmps, _ := filepath.Glob(filepath.Join(projectDir, "gaps.md.tmp"))
	assert.Empty(t, tmps, "writer must rename .tmp on flush; none should remain")

	// Sort sanity: every priority bucket header that appears in gaps.md should
	// appear in canonical order.
	body := string(got)
	headers := []string{"### Large", "### Medium", "### Small"}
	positions := []int{}
	for _, h := range headers {
		if i := bytes.Index([]byte(body), []byte(h)); i >= 0 {
			positions = append(positions, i)
		}
	}
	sortedCheck := append([]int(nil), positions...)
	sort.Ints(sortedCheck)
	assert.Equal(t, sortedCheck, positions, "priority headers must be in canonical order")
}
