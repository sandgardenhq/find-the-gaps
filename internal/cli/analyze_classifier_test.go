package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
	"github.com/sandgardenhq/find-the-gaps/internal/spider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAnalyzeEndToEnd_FiltersNonDocs exercises the docs-classifier pipeline
// end-to-end. Three URLs are seeded into the spider cache: an API reference,
// a team page, and a changelog. The fake LLM server classifies the team page
// as non-docs (is_docs=false) via AnalyzePage; the test then asserts that:
//
//  1. The classification audit log line reports 2 docs / 1 non-docs.
//  2. gaps.md does NOT contain the team URL.
//  3. screenshots.md does NOT contain the team URL.
//  4. mapping.md does NOT mention the team URL or the team page's features.
//  5. The analyze command exits with code 0.
//
// Trade-off (deliberate, per the task plan): the docs-side feature map is
// pre-cached so MapFeaturesToDocs does not run. That means Task 6's
// docsAnalyses/docsPages filter is NOT exercised end-to-end here — it is
// covered by TestFilterDocsAnalyses_ExcludesNotDocs in analyze_test.go.
// Pre-caching keeps this test maintainable: the alternative is faking a
// fourth LLM schema (map_page_response) on top of the three already in play
// (analyze_page_response, screenshot_gaps_response, and the docsFM cache
// itself). The screenshot filter (Task 7) and the audit log line (Task 9)
// ARE exercised end-to-end, which the task description names as the most
// critical assertions.
func TestAnalyzeEndToEnd_FiltersNonDocs(t *testing.T) {
	apiURL := "https://docs.example.com/api"
	teamURL := "https://docs.example.com/team"
	changelogURL := "https://docs.example.com/changelog"

	repoDir := t.TempDir()
	cacheBase := t.TempDir()
	projectName := filepath.Base(filepath.Clean(repoDir))
	projectDir := filepath.Join(cacheBase, projectName)

	// Tiny Go file with one exported function.
	if err := os.WriteFile(filepath.Join(repoDir, "main.go"), []byte("package main\nfunc Run() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Seed the spider docs cache with three markdown files and Record (NOT
	// RecordAnalysis) all three URLs. Crawl will skip mdfetch (entries already
	// in idx) and AnalyzePage will fire fresh for each URL.
	docsDir := filepath.Join(projectDir, "docs")
	require.NoError(t, os.MkdirAll(docsDir, 0o755))
	idx, err := spider.LoadIndex(docsDir)
	require.NoError(t, err)

	seed := func(rawURL, body string) {
		t.Helper()
		filename := spider.URLToFilename(rawURL)
		require.NoError(t, os.WriteFile(filepath.Join(docsDir, filename), []byte(body), 0o644))
		require.NoError(t, idx.Record(rawURL, filename))
	}
	seed(apiURL, "# API reference\n\nGET /v1/widgets returns the widget list.\n")
	seed(teamURL, "# Our Team\n\nMeet the people behind the product.\n")
	seed(changelogURL, "# Changelog\n\n## v1.2 — added Run() helper.\n")

	// Pre-cache the product summary so SynthesizeProduct is skipped.
	require.NoError(t, idx.SetProductSummary("A test product.", []string{"feature-one"}))

	// Pre-cache code features and the code-side feature map so
	// ExtractFeaturesFromCode and MapFeaturesToCode do not need LLM calls.
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

	// Pre-cache the docs feature map with feature-one having NO pages. With no
	// pages, DetectDrift skips the feature → no drift LLM calls. The team URL
	// is therefore absent from gaps.md and mapping.md trivially; assertions 2
	// and 4 remain meaningful (they pin the no-leak invariant) but the load-
	// bearing end-to-end coverage is on assertions 1, 3, and 5.
	docsFM := analyzer.DocsFeatureMap{{Feature: "feature-one", Pages: nil}}
	require.NoError(t, saveDocsFeatureMapCache(
		filepath.Join(projectDir, "docsfeaturemap.json"),
		[]string{"feature-one"}, docsFM))

	// Fake LLM server: dispatches by inspecting the request body. AnalyzePage
	// includes the URL and "is_docs" guidance in the prompt, so we can match
	// on URL substring + the prompt's "is_docs" anchor. The screenshot pass
	// uses the screenshot_gaps_response schema and a distinct prompt; we
	// detect it by the "screenshot is ESSENTIAL" anchor.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		s := string(body)

		respond := func(content string) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{"message": map[string]any{"content": content}},
				},
			})
		}

		// Dispatch primarily on the response_format schema name so we don't
		// accidentally match on prompt text fragments. Within the page-level
		// AnalyzePage calls, dispatch on the URL the prompt is targeting.
		switch {
		case strings.Contains(s, `"name":"screenshot_gaps_response"`),
			strings.Contains(s, `"name": "screenshot_gaps_response"`):
			// Screenshot pass: empty gaps regardless of page.
			respond(`{"gaps":[]}`)
		case strings.Contains(s, `"name":"synthesize_response"`),
			strings.Contains(s, `"name": "synthesize_response"`):
			// Synthesize fires because at least one page was analyzed fresh
			// this run; return a stable summary so the run progresses.
			respond(`{"description":"A test product.","features":["feature-one"]}`)
		case strings.Contains(s, `"name":"analyze_page_response"`),
			strings.Contains(s, `"name": "analyze_page_response"`):
			switch {
			case strings.Contains(s, teamURL):
				respond(`{"summary":"Meet the team.","features":[],"is_docs":false}`)
			case strings.Contains(s, apiURL):
				respond(`{"summary":"API reference.","features":["widgets api"],"is_docs":true}`)
			case strings.Contains(s, changelogURL):
				respond(`{"summary":"Changelog.","features":["run helper"],"is_docs":true}`)
			default:
				t.Errorf("unexpected analyze_page request URL; body=%s", s)
				http.Error(w, "unexpected", http.StatusInternalServerError)
			}
		default:
			// No other LLM schema should fire — surface unexpected requests
			// so test failures point at the missing dispatch case.
			t.Errorf("unexpected LLM request body: %s", s)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	t.Setenv("OLLAMA_BASE_URL", srv.URL)
	t.Setenv("ANTHROPIC_API_KEY", "fake-key")

	// docsURL must be one of the seeded URLs so Crawl finds it in the index.
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"analyze",
		"--repo", repoDir,
		"--cache-dir", cacheBase,
		"--docs-url", apiURL,
		"--llm-small", "ollama/test-model",
		"--llm-typical", "anthropic/claude-test",
		"--llm-large", "anthropic/claude-test",
		"--no-site",
	})
	combined := stdout.String() + stderr.String()
	require.Equal(t, 0, code, "analyze must exit 0; combined output:\n%s", combined)

	// Assertion 1: classification audit log line.
	assert.Contains(t, combined, "classified: 2 docs, 1 non-docs",
		"expected classification summary line in output; got:\n%s", combined)

	// Assertion 3 (load-bearing): screenshots.md exists and excludes the
	// team URL. The screenshot pass ran live; buildScreenshotDocPages must
	// have filtered the team page out before any LLM call.
	screenshotsPath := filepath.Join(projectDir, "screenshots.md")
	screenshotsBytes, err := os.ReadFile(screenshotsPath)
	require.NoError(t, err, "screenshots.md must be written when the screenshot pass ran")
	assert.NotContains(t, string(screenshotsBytes), teamURL,
		"screenshots.md must not reference the team URL")

	// Assertion 2: gaps.md does not contain the team URL. Drift detection
	// produced no findings (empty docs feature map by design — see the
	// trade-off note above), so the team URL cannot leak in via stale-doc
	// findings either. This pins the no-leak invariant against future
	// regressions in the gaps writer.
	gapsPath := filepath.Join(projectDir, "gaps.md")
	gapsBytes, err := os.ReadFile(gapsPath)
	require.NoError(t, err)
	assert.NotContains(t, string(gapsBytes), teamURL,
		"gaps.md must not reference the team URL")

	// Assertion 4: mapping.md does not reference the team URL nor the
	// team-page feature list (which is empty by design from our fake
	// server). Again pinned as a no-leak invariant.
	mappingPath := filepath.Join(projectDir, "mapping.md")
	mappingBytes, err := os.ReadFile(mappingPath)
	require.NoError(t, err)
	assert.NotContains(t, string(mappingBytes), teamURL,
		"mapping.md must not reference the team URL")
}
