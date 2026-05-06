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
	"sync/atomic"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
	"github.com/sandgardenhq/find-the-gaps/internal/spider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// visionFixturePage is the docs page used by both vision end-to-end tests.
// It contains 6 markdown image references; img-3 is the planted mismatch
// (alt text describes a dashboard, surrounding prose calls it the Settings
// page). 5 + 1 splits cleanly into the relevance pass's 5-image batches so we
// can assert relevance_batches=2 in test 1.
const visionFixturePage = `# Settings page

## Overview

The Settings page lets you configure billing, security, and integrations.

![Top nav](https://docs.example.com/img/top-nav.png)

## Billing

Billing options live in the upper-right corner of the page.

![Billing card](https://docs.example.com/img/billing.png)

## Profile widget

A small profile card sits beside the billing area.

![Profile card](https://docs.example.com/img/profile.png)

## Settings overview screenshot

Below is the Settings page itself, with all controls visible.

![Dashboard with project metrics and sparkline charts](https://docs.example.com/img/dashboard.png)

## Security panel

The security panel contains keys and audit log links.

![Security panel](https://docs.example.com/img/security.png)

## Integrations

Integrations are listed in a grid at the bottom of the page.

![Integrations grid](https://docs.example.com/img/integrations.png)
`

// TestVisionScreenshotEndToEnd_VisionOnEmitsImageIssues exercises the vision
// branch of DetectScreenshotGaps end-to-end with a Vision=true small tier
// (groq/meta-llama/llama-4-scout-17b-16e-instruct, see capabilities.go). The
// fake LLM server dispatches by JSON-Schema name in the request body and
// returns canned relevance + detection responses; the test asserts:
//
//  1. Two relevance-pass requests fire (6 images / batch size 5 → batches of
//     5 and 1) and one detection-pass request fires.
//  2. The audit log emits vision=on, relevance_batches=2, images_seen=6,
//     image_issues=1 — pinning the wiring between the screenshot pipeline and
//     the audit log.
//  3. screenshots.md contains the `## Image Issues` section and the planted
//     img-3 mismatch text — pinning the reporter's vision rendering path.
//
// We pick groq (not anthropic) because Bifrost's anthropic client does not
// honor a baseURL override, so an httptest server cannot intercept anthropic
// traffic without production changes; groq routes through Bifrost's OpenAI
// provider with a configurable BaseURL via GROQ_BASE_URL. Both are Vision=true
// in the registry, so the wiring under test is identical.
func TestVisionScreenshotEndToEnd_VisionOnEmitsImageIssues(t *testing.T) {
	pageURL := "https://docs.example.com/settings"

	repoDir := t.TempDir()
	cacheBase := t.TempDir()
	projectName := filepath.Base(filepath.Clean(repoDir))
	projectDir := filepath.Join(cacheBase, projectName)

	// Tiny Go file so the scanner has something to map.
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "main.go"),
		[]byte("package main\nfunc Run() {}\n"), 0o644))

	// Seed the spider docs cache with one page containing six images.
	docsDir := filepath.Join(projectDir, "docs")
	require.NoError(t, os.MkdirAll(docsDir, 0o755))
	idx, err := spider.LoadIndex(docsDir)
	require.NoError(t, err)
	filename := spider.URLToFilename(pageURL)
	require.NoError(t, os.WriteFile(filepath.Join(docsDir, filename),
		[]byte(visionFixturePage), 0o644))
	require.NoError(t, idx.Record(pageURL, filename))

	// Pre-cache product summary, code features, code feature map, and docs
	// feature map so the only LLM passes that run are page analysis on this
	// single URL plus screenshot relevance + detection. Mirrors the
	// pre-caching in TestAnalyzeEndToEnd_FiltersNonDocs.
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
	docsFM := analyzer.DocsFeatureMap{{Feature: "feature-one", Pages: nil}}
	require.NoError(t, saveDocsFeatureMapCache(
		filepath.Join(projectDir, "docsfeaturemap.json"),
		[]string{"feature-one"}, docsFM))

	// Counters distinguish the two relevance batches and assert the
	// detection pass fires exactly once.
	var (
		relevanceCalls int64
		detectionCalls int64
	)

	// Fake LLM server: dispatches by JSON-Schema name. The first
	// screenshot_image_relevance request is batch 1 (img-1..img-5, with img-3
	// flagged as an image_issue); the second is batch 2 (img-6 only,
	// matches=true, no issues).
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

		switch {
		case strings.Contains(s, `"name":"screenshot_image_relevance"`),
			strings.Contains(s, `"name": "screenshot_image_relevance"`):
			n := atomic.AddInt64(&relevanceCalls, 1)
			if n == 1 {
				// Batch 1: 5 images. img-3 (the dashboard-vs-Settings-page
				// mismatch) is the only image_issue.
				respond(`{"image_issues":[{"index":"img-3","src":"https://docs.example.com/img/profile.png","reason":"Image shows a dashboard but the prose describes the Settings page.","suggested_action":"replace","priority":"medium","priority_reason":"test stub"}],"verdicts":[{"index":"img-1","matches":true},{"index":"img-2","matches":true},{"index":"img-3","matches":false},{"index":"img-4","matches":true},{"index":"img-5","matches":true}]}`)
				return
			}
			// Batch 2: 1 image, matches=true, no issues.
			respond(`{"image_issues":[],"verdicts":[{"index":"img-6","matches":true}]}`)
		case strings.Contains(s, `"name":"screenshot_gaps_response"`),
			strings.Contains(s, `"name": "screenshot_gaps_response"`):
			atomic.AddInt64(&detectionCalls, 1)
			respond(`{"gaps":[],"suppressed_by_image":[]}`)
		case strings.Contains(s, `"name":"synthesize_response"`),
			strings.Contains(s, `"name": "synthesize_response"`):
			respond(`{"description":"A test product.","features":["feature-one"]}`)
		case strings.Contains(s, `"name":"analyze_page_response"`),
			strings.Contains(s, `"name": "analyze_page_response"`):
			respond(`{"summary":"Settings page.","features":["settings"],"is_docs":true}`)
		default:
			t.Errorf("unexpected LLM request body: %s", s)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	t.Setenv("GROQ_BASE_URL", srv.URL)
	t.Setenv("GROQ_API_KEY", "fake-key")
	t.Setenv("ANTHROPIC_API_KEY", "fake-key")

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"analyze",
		"--repo", repoDir,
		"--cache-dir", cacheBase,
		"--docs-url", pageURL,
		"--llm-small", "groq/meta-llama/llama-4-scout-17b-16e-instruct",
		"--llm-typical", "anthropic/claude-haiku-4-5",
		"--llm-large", "anthropic/claude-haiku-4-5",
		"--experimental-check-screenshots",
		"--no-site",
	})
	combined := stdout.String() + stderr.String()
	require.Equal(t, 0, code, "analyze must exit 0; combined output:\n%s", combined)

	// Assertion 1: fake server saw exactly 2 relevance + 1 detection calls.
	assert.EqualValues(t, 2, atomic.LoadInt64(&relevanceCalls),
		"expected 2 relevance-pass calls (6 images / batch 5 → batches of 5 and 1); combined output:\n%s", combined)
	assert.EqualValues(t, 1, atomic.LoadInt64(&detectionCalls),
		"expected 1 detection-pass call; combined output:\n%s", combined)

	// Assertion 2: per-page audit log line names vision=on with the right
	// relevance / image counts. The log goes through charmbracelet/log to
	// stderr (root.go sets log.SetOutput(cmd.ErrOrStderr())).
	auditLog := stderr.String()
	for _, want := range []string{
		"vision=on",
		"relevance_batches=2",
		"images_seen=6",
		"image_issues=1",
	} {
		assert.Contains(t, auditLog, want,
			"audit log missing %q; full stderr:\n%s", want, auditLog)
	}

	// Assertion 3: screenshots.md exists, contains the `## Image Issues`
	// section header, and surfaces the planted img-3 mismatch reason.
	screenshotsBytes, err := os.ReadFile(filepath.Join(projectDir, "screenshots.md"))
	require.NoError(t, err, "screenshots.md must be written when the screenshot pass ran")
	assert.Contains(t, string(screenshotsBytes), "## Image Issues",
		"screenshots.md must include the Image Issues section when vision ran")
	assert.Contains(t, string(screenshotsBytes),
		"Image shows a dashboard but the prose describes the Settings page.",
		"screenshots.md must surface the planted img-3 mismatch reason")
}

// TestVisionScreenshotEndToEnd_VisionOffSkipsRelevancePass exercises the
// vision-OFF path: a non-Vision small tier (ollama wildcard → Vision=false)
// must skip the relevance pass entirely. The fake LLM server fails the test
// if any screenshot_image_relevance request arrives. The detection pass still
// runs and produces a missing-screenshot finding, which screenshots.md
// renders without an `## Image Issues` section.
func TestVisionScreenshotEndToEnd_VisionOffSkipsRelevancePass(t *testing.T) {
	pageURL := "https://docs.example.com/settings"

	repoDir := t.TempDir()
	cacheBase := t.TempDir()
	projectName := filepath.Base(filepath.Clean(repoDir))
	projectDir := filepath.Join(cacheBase, projectName)

	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "main.go"),
		[]byte("package main\nfunc Run() {}\n"), 0o644))

	docsDir := filepath.Join(projectDir, "docs")
	require.NoError(t, os.MkdirAll(docsDir, 0o755))
	idx, err := spider.LoadIndex(docsDir)
	require.NoError(t, err)
	filename := spider.URLToFilename(pageURL)
	require.NoError(t, os.WriteFile(filepath.Join(docsDir, filename),
		[]byte(visionFixturePage), 0o644))
	require.NoError(t, idx.Record(pageURL, filename))

	// Same pre-caching as the vision-on test so only screenshot detection +
	// page analysis fire as live calls.
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
	docsFM := analyzer.DocsFeatureMap{{Feature: "feature-one", Pages: nil}}
	require.NoError(t, saveDocsFeatureMapCache(
		filepath.Join(projectDir, "docsfeaturemap.json"),
		[]string{"feature-one"}, docsFM))

	var (
		relevanceCalls int64
		detectionCalls int64
	)

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

		switch {
		case strings.Contains(s, `"name":"screenshot_image_relevance"`),
			strings.Contains(s, `"name": "screenshot_image_relevance"`):
			// The relevance pass MUST NOT fire on a non-Vision client. If it
			// does, fail loudly so the test pinpoints the regression.
			atomic.AddInt64(&relevanceCalls, 1)
			t.Errorf("vision-off run must not issue screenshot_image_relevance requests; body=%s", s)
			http.Error(w, "vision-off must not call relevance pass", http.StatusInternalServerError)
		case strings.Contains(s, `"name":"screenshot_gaps_response"`),
			strings.Contains(s, `"name": "screenshot_gaps_response"`):
			atomic.AddInt64(&detectionCalls, 1)
			// Emit one missing-screenshot finding so screenshots.md has body
			// content to render under # Missing Screenshots; suppressed_by_image
			// stays empty (no verdicts on the vision-off path).
			respond(`{"gaps":[{"quoted_passage":"The Settings page lets you configure billing, security, and integrations.","should_show":"Full Settings page with billing, security, and integrations panels visible.","suggested_alt":"Settings page overview","insertion_hint":"after the heading","priority":"medium","priority_reason":"test stub"}],"suppressed_by_image":[]}`)
		case strings.Contains(s, `"name":"synthesize_response"`),
			strings.Contains(s, `"name": "synthesize_response"`):
			respond(`{"description":"A test product.","features":["feature-one"]}`)
		case strings.Contains(s, `"name":"analyze_page_response"`),
			strings.Contains(s, `"name": "analyze_page_response"`):
			respond(`{"summary":"Settings page.","features":["settings"],"is_docs":true}`)
		default:
			t.Errorf("unexpected LLM request body: %s", s)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	t.Setenv("OLLAMA_BASE_URL", srv.URL)
	t.Setenv("ANTHROPIC_API_KEY", "fake-key")

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"analyze",
		"--repo", repoDir,
		"--cache-dir", cacheBase,
		"--docs-url", pageURL,
		"--llm-small", "ollama/llama3",
		"--llm-typical", "anthropic/claude-haiku-4-5",
		"--llm-large", "anthropic/claude-haiku-4-5",
		"--experimental-check-screenshots",
		"--no-site",
	})
	combined := stdout.String() + stderr.String()
	require.Equal(t, 0, code, "analyze must exit 0; combined output:\n%s", combined)

	// Assertion 1: zero relevance-pass calls; one detection-pass call.
	assert.EqualValues(t, 0, atomic.LoadInt64(&relevanceCalls),
		"vision-off run must issue zero screenshot_image_relevance calls; combined output:\n%s", combined)
	assert.EqualValues(t, 1, atomic.LoadInt64(&detectionCalls),
		"vision-off run must still issue one detection-pass call; combined output:\n%s", combined)

	// Assertion 2: audit log line says vision=off, relevance_batches=0.
	auditLog := stderr.String()
	assert.Contains(t, auditLog, "vision=off",
		"audit log must report vision=off when small tier lacks Vision capability; full stderr:\n%s", auditLog)
	assert.Contains(t, auditLog, "relevance_batches=0",
		"audit log must report relevance_batches=0 when vision is off; full stderr:\n%s", auditLog)

	// Assertion 3: screenshots.md exists, omits the `## Image Issues`
	// header, and contains the missing-screenshot finding from the
	// detection pass.
	screenshotsBytes, err := os.ReadFile(filepath.Join(projectDir, "screenshots.md"))
	require.NoError(t, err, "screenshots.md must be written when the screenshot pass ran")
	out := string(screenshotsBytes)
	assert.NotContains(t, out, "## Image Issues",
		"vision-off screenshots.md must NOT include the Image Issues section")
	assert.Contains(t, out, "The Settings page lets you configure billing, security, and integrations.",
		"vision-off screenshots.md must contain the detection-pass missing-screenshot finding")
}
