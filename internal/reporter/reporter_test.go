package reporter_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/reporter"
)

func TestWriteMapping_CreatesFile(t *testing.T) {
	dir := t.TempDir()

	summary := analyzer.ProductSummary{
		Description: "A CLI tool for finding doc gaps.",
		Features:    []string{"gap analysis", "doctor command"},
	}
	mapping := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "gap analysis", Description: "Finds gaps.", Layer: "analysis engine", UserFacing: true}, Files: []string{"internal/analyzer/analyzer.go"}, Symbols: []string{"AnalyzePage"}},
		{Feature: analyzer.CodeFeature{Name: "doctor command", Description: "Checks deps.", Layer: "cli", UserFacing: true}, Files: []string{"internal/cli/doctor.go"}, Symbols: []string{}},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "gap analysis", Pages: []string{"https://docs.example.com/gap"}},
		{Feature: "doctor command", Pages: []string{}},
	}

	if err := reporter.WriteMapping(dir, summary, mapping, docsMap); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "mapping.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "gap analysis") {
		t.Error("mapping.md must mention 'gap analysis'")
	}
	if !strings.Contains(content, "A CLI tool") {
		t.Error("mapping.md must include product summary")
	}
	if !strings.Contains(content, "internal/analyzer/analyzer.go") {
		t.Error("mapping.md must include file paths")
	}
}

func TestWriteGaps_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	mapping := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth", Description: "Auth.", Layer: "cli", UserFacing: true}, Files: []string{"auth.go"}},
	}
	if err := reporter.WriteGaps(dir, mapping, []string{"search"}, []analyzer.DriftFinding{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "gaps.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Error("gaps.md must not be empty")
	}
}

func TestWriteMapping_EmptyMapping_Succeeds(t *testing.T) {
	dir := t.TempDir()
	err := reporter.WriteMapping(dir,
		analyzer.ProductSummary{Description: "Product.", Features: []string{}},
		analyzer.FeatureMap{},
		analyzer.DocsFeatureMap{},
	)
	if err != nil {
		t.Fatal(err)
	}
}

// TestWriteGaps_NoneFound verifies "_None found._" when every feature has both
// a code implementation and a documentation page.
func TestWriteGaps_NoneFound(t *testing.T) {
	dir := t.TempDir()
	mapping := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "gap analysis", Description: "Finds gaps.", Layer: "analysis engine", UserFacing: true}, Files: []string{"internal/foo/bar.go"}},
	}
	allFeatures := []string{"gap analysis"} // documented AND implemented → no gaps

	if err := reporter.WriteGaps(dir, mapping, allFeatures, []analyzer.DriftFinding{}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "gaps.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "_None found._") {
		t.Errorf("expected '_None found._' when all features are covered, got:\n%s", string(data))
	}
}

// TestWriteGaps_UndocumentedCode verifies that a feature with code but no
// documentation page appears in the Undocumented Code section.
func TestWriteGaps_UndocumentedCode(t *testing.T) {
	dir := t.TempDir()
	mapping := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth", Description: "Auth.", Layer: "cli", UserFacing: true}, Files: []string{"auth.go"}},
	}
	// "auth" exists in code but is absent from docs
	if err := reporter.WriteGaps(dir, mapping, []string{}, []analyzer.DriftFinding{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "gaps.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "auth") {
		t.Error("gaps.md must list 'auth' in Undocumented Code")
	}
	if !strings.Contains(content, "no documentation page") {
		t.Error("gaps.md must describe the undocumented code gap")
	}
}

// TestWriteGaps_FeatureNoFiles_NotUndocumented verifies that a feature the LLM
// could not map to any file is not listed as undocumented code.
func TestWriteGaps_FeatureNoFiles_NotUndocumented(t *testing.T) {
	dir := t.TempDir()
	mapping := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth", Description: "Auth.", Layer: "cli", UserFacing: true}, Files: []string{}}, // no files — not "implemented"
	}
	if err := reporter.WriteGaps(dir, mapping, []string{}, []analyzer.DriftFinding{}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "gaps.md"))
	content := string(data)
	if strings.Contains(content, "no documentation page") {
		t.Error("features with no mapped files must not appear in Undocumented Code")
	}
}

// TestWriteMapping_RichFields_Documented asserts that description blockquote,
// Layer, User-facing, and Documentation status fields appear in mapping.md when
// a matching PageAnalysis covers the feature.
func TestWriteMapping_RichFields_Documented(t *testing.T) {
	dir := t.TempDir()

	summary := analyzer.ProductSummary{
		Description: "A CLI tool.",
		Features:    []string{"CLI command routing"},
	}
	mapping := analyzer.FeatureMap{
		{
			Feature: analyzer.CodeFeature{
				Name:        "CLI command routing",
				Description: "Provides top-level command structure.",
				Layer:       "cli",
				UserFacing:  true,
			},
			Files:   []string{"cmd/find-the-gaps/main.go"},
			Symbols: []string{"NewRootCmd"},
		},
	}
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "CLI command routing", Pages: []string{"https://docs.example.com/cli"}},
	}

	if err := reporter.WriteMapping(dir, summary, mapping, docsMap); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "mapping.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "> Provides top-level command structure.") {
		t.Errorf("mapping.md must render description as blockquote, got:\n%s", content)
	}
	if !strings.Contains(content, "**Layer:** cli") {
		t.Errorf("mapping.md must render Layer field, got:\n%s", content)
	}
	if !strings.Contains(content, "**User-facing:** yes") {
		t.Errorf("mapping.md must render User-facing: yes for UserFacing=true, got:\n%s", content)
	}
	if !strings.Contains(content, "**Documentation status:** documented") {
		t.Errorf("mapping.md must render Documentation status: documented when page covers feature, got:\n%s", content)
	}
	if !strings.Contains(content, "https://docs.example.com/cli") {
		t.Errorf("mapping.md must include the doc page URL, got:\n%s", content)
	}
}

// TestWriteMapping_EmptyDescriptionAndLayer verifies that the conditional
// guards in WriteMapping do not emit a blockquote or Layer line when those
// fields are empty strings.
func TestWriteMapping_EmptyDescriptionAndLayer(t *testing.T) {
	dir := t.TempDir()

	summary := analyzer.ProductSummary{
		Description: "A CLI tool.",
		Features:    []string{"minimal feature"},
	}
	mapping := analyzer.FeatureMap{
		{
			Feature: analyzer.CodeFeature{
				Name:        "minimal feature",
				Description: "",
				Layer:       "",
				UserFacing:  false,
			},
			Files:   []string{"internal/foo/bar.go"},
			Symbols: []string{},
		},
	}

	if err := reporter.WriteMapping(dir, summary, mapping, analyzer.DocsFeatureMap{}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "mapping.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "### minimal feature") {
		t.Errorf("mapping.md must include feature heading '### minimal feature', got:\n%s", content)
	}
	if !strings.Contains(content, "**User-facing:** no") {
		t.Errorf("mapping.md must include '**User-facing:** no' for UserFacing=false, got:\n%s", content)
	}
	if !strings.Contains(content, "**Documentation status:** undocumented") {
		t.Errorf("mapping.md must include '**Documentation status:** undocumented' when no pages match, got:\n%s", content)
	}
	if strings.Contains(content, "> ") {
		t.Errorf("mapping.md must NOT contain '> ' blockquote when Description is empty, got:\n%s", content)
	}
	if strings.Contains(content, "**Layer:**") {
		t.Errorf("mapping.md must NOT contain '**Layer:**' when Layer is empty, got:\n%s", content)
	}
}

func TestWriteGaps_StaleDocumentation_RendersFindings(t *testing.T) {
	dir := t.TempDir()
	mapping := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth", UserFacing: true}, Files: []string{"auth.go"}},
	}
	drift := []analyzer.DriftFinding{
		{
			Feature: "auth",
			Issues: []analyzer.DriftIssue{
				{Page: "https://docs.example.com/auth", Issue: "The email field requirement is not documented."},
				{Page: "", Issue: "The error response format differs from what is described."},
			},
		},
	}
	if err := reporter.WriteGaps(dir, mapping, []string{"auth"}, drift); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "gaps.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "## Stale Documentation") {
		t.Errorf("gaps.md must contain '## Stale Documentation' section, got:\n%s", content)
	}
	if !strings.Contains(content, "### auth") {
		t.Errorf("gaps.md must contain '### auth' under Stale Documentation, got:\n%s", content)
	}
	if !strings.Contains(content, "email field requirement") {
		t.Errorf("gaps.md must contain the drift issue text, got:\n%s", content)
	}
	if !strings.Contains(content, "https://docs.example.com/auth") {
		t.Errorf("gaps.md must cite the page URL for issues with a page, got:\n%s", content)
	}
}

func TestWriteGaps_StaleDocumentation_NoneFound(t *testing.T) {
	dir := t.TempDir()
	mapping := analyzer.FeatureMap{}
	if err := reporter.WriteGaps(dir, mapping, []string{}, []analyzer.DriftFinding{}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "gaps.md"))
	content := string(data)
	if !strings.Contains(content, "## Stale Documentation") {
		t.Errorf("section must always be present, got:\n%s", content)
	}
	if !strings.Contains(content, "_None found._") {
		t.Errorf("must show _None found._ when no drift, got:\n%s", content)
	}
}

// TestWriteMapping_RichFields_Undocumented asserts that a feature with no
// matching PageAnalysis is rendered with Documentation status: undocumented
// and Documented on: _(none)_.
func TestWriteMapping_RichFields_Undocumented(t *testing.T) {
	dir := t.TempDir()

	summary := analyzer.ProductSummary{
		Description: "A CLI tool.",
		Features:    []string{"token batching"},
	}
	mapping := analyzer.FeatureMap{
		{
			Feature: analyzer.CodeFeature{
				Name:        "token batching",
				Description: "Splits symbol indexes into token-budget-sized chunks.",
				Layer:       "analysis engine",
				UserFacing:  false,
			},
			Files:   []string{"internal/analyzer/mapper.go"},
			Symbols: []string{"batchSymbols"},
		},
	}
	// No page covers "token batching"
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "token batching", Pages: []string{}},
	}

	if err := reporter.WriteMapping(dir, summary, mapping, docsMap); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "mapping.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "> Splits symbol indexes into token-budget-sized chunks.") {
		t.Errorf("mapping.md must render description as blockquote, got:\n%s", content)
	}
	if !strings.Contains(content, "**Layer:** analysis engine") {
		t.Errorf("mapping.md must render Layer field, got:\n%s", content)
	}
	if !strings.Contains(content, "**User-facing:** no") {
		t.Errorf("mapping.md must render User-facing: no for UserFacing=false, got:\n%s", content)
	}
	if !strings.Contains(content, "**Documentation status:** undocumented") {
		t.Errorf("mapping.md must render Documentation status: undocumented when no page covers feature, got:\n%s", content)
	}
	if !strings.Contains(content, "_(none)_") {
		t.Errorf("mapping.md must render '_(none)_' for Documented on when no pages match, got:\n%s", content)
	}
}

// TestWriteMapping_DocStatusUsesCanonicalMap locks in the contract that
// documentation status is driven exclusively by DocsFeatureMap (canonical
// feature names). The canonical feature name here is "gap analysis"; the
// DocsFeatureMap entry uses that canonical name and points to a real page.
// This must render as documented — even if a per-page extractor produced
// completely different raw phrases (which is the original bug).
func TestWriteMapping_DocStatusUsesCanonicalMap(t *testing.T) {
	dir := t.TempDir()

	summary := analyzer.ProductSummary{
		Description: "A CLI tool.",
		Features:    []string{"gap analysis"},
	}
	mapping := analyzer.FeatureMap{
		{
			Feature: analyzer.CodeFeature{Name: "gap analysis", UserFacing: true},
			Files:   []string{"internal/analyzer/analyzer.go"},
		},
	}
	// Canonical "gap analysis" → a real docs page.
	docsMap := analyzer.DocsFeatureMap{
		{Feature: "gap analysis", Pages: []string{"https://docs.example.com/gaps"}},
	}

	if err := reporter.WriteMapping(dir, summary, mapping, docsMap); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "mapping.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "**Documentation status:** documented") {
		t.Errorf("expected 'documented' when DocsFeatureMap has pages for the canonical feature, got:\n%s", content)
	}
	if !strings.Contains(content, "https://docs.example.com/gaps") {
		t.Errorf("expected the mapped page URL to appear in mapping.md, got:\n%s", content)
	}
}

func TestWriteGaps_MissingScreenshotsSection(t *testing.T) {
	dir := t.TempDir()
	gaps := []analyzer.ScreenshotGap{
		{
			PageURL:       "https://example.com/quickstart",
			PagePath:      "/cache/quickstart.md",
			QuotedPassage: "Run the command and see the output.",
			ShouldShow:    "Terminal showing the analyze summary with findings count.",
			SuggestedAlt:  "Terminal output of find-the-gaps analyze",
			InsertionHint: "after the paragraph ending '...see the output.'",
		},
		{
			PageURL:       "https://example.com/quickstart",
			PagePath:      "/cache/quickstart.md",
			QuotedPassage: "The dashboard shows open PRs.",
			ShouldShow:    "Dashboard with two open PRs visible.",
			SuggestedAlt:  "Dashboard with open PRs",
			InsertionHint: "after the heading '## Dashboard'",
		},
		{
			PageURL:       "https://example.com/setup",
			PagePath:      "/cache/setup.md",
			QuotedPassage: "Configure the CLI.",
			ShouldShow:    "The config file open in an editor.",
			SuggestedAlt:  "Configuration file",
			InsertionHint: "after the code block",
		},
	}
	_ = gaps // obsolete — Task 7 deletes this test wholesale.
	require.NoError(t, reporter.WriteGaps(dir, nil, nil, nil))
	body, err := os.ReadFile(filepath.Join(dir, "gaps.md"))
	require.NoError(t, err)
	s := string(body)
	assert.Contains(t, s, "## Missing Screenshots")
	// Grouped per page, in order of first occurrence.
	assert.Regexp(t, `### https://example.com/quickstart[\s\S]*### https://example.com/setup`, s)
	// Each gap shows its four fields.
	assert.Contains(t, s, "Run the command and see the output.")
	assert.Contains(t, s, "Terminal showing the analyze summary")
	assert.Contains(t, s, "Terminal output of find-the-gaps analyze")
	assert.Contains(t, s, "after the paragraph ending '...see the output.'")
}

func TestWriteScreenshots_CreatesFile_WithFindings(t *testing.T) {
	dir := t.TempDir()
	gaps := []analyzer.ScreenshotGap{
		{
			PageURL:       "https://example.com/quickstart",
			PagePath:      "/cache/quickstart.md",
			QuotedPassage: "Run the command and see the output.",
			ShouldShow:    "Terminal showing the analyze summary with findings count.",
			SuggestedAlt:  "Terminal output of find-the-gaps analyze",
			InsertionHint: "after the paragraph ending '...see the output.'",
		},
		{
			PageURL:       "https://example.com/quickstart",
			PagePath:      "/cache/quickstart.md",
			QuotedPassage: "The dashboard shows open PRs.",
			ShouldShow:    "Dashboard with two open PRs visible.",
			SuggestedAlt:  "Dashboard with open PRs",
			InsertionHint: "after the heading '## Dashboard'",
		},
		{
			PageURL:       "https://example.com/setup",
			PagePath:      "/cache/setup.md",
			QuotedPassage: "Configure the CLI.",
			ShouldShow:    "The config file open in an editor.",
			SuggestedAlt:  "Configuration file",
			InsertionHint: "after the code block",
		},
	}
	require.NoError(t, reporter.WriteScreenshots(dir, gaps))
	body, err := os.ReadFile(filepath.Join(dir, "screenshots.md"))
	require.NoError(t, err)
	s := string(body)
	// Root heading is promoted to # (the doc is now its own root).
	assert.Contains(t, s, "# Missing Screenshots")
	// Page grouping, first-occurrence order.
	assert.Regexp(t, `### https://example.com/quickstart[\s\S]*### https://example.com/setup`, s)
	// Each gap's four fields render.
	assert.Contains(t, s, "Run the command and see the output.")
	assert.Contains(t, s, "Terminal showing the analyze summary")
	assert.Contains(t, s, "Terminal output of find-the-gaps analyze")
	assert.Contains(t, s, "after the paragraph ending '...see the output.'")
}

func TestWriteScreenshots_Empty_WritesNoneFound(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, reporter.WriteScreenshots(dir, nil))

	body, err := os.ReadFile(filepath.Join(dir, "screenshots.md"))
	require.NoError(t, err)
	s := string(body)

	assert.Contains(t, s, "# Missing Screenshots")
	assert.Contains(t, s, "_None found._")
	// No per-page headers when there are no findings.
	assert.NotContains(t, s, "### ")
}

func TestWriteScreenshots_PreservesPageOrder(t *testing.T) {
	dir := t.TempDir()
	gaps := []analyzer.ScreenshotGap{
		{PageURL: "https://example.com/second", QuotedPassage: "second-page passage."},
		{PageURL: "https://example.com/first", QuotedPassage: "first-page passage."},
		{PageURL: "https://example.com/second", QuotedPassage: "second-page passage 2."},
	}
	require.NoError(t, reporter.WriteScreenshots(dir, gaps))
	body, err := os.ReadFile(filepath.Join(dir, "screenshots.md"))
	require.NoError(t, err)
	s := string(body)
	// /second appears first because it shows up first in the input.
	assert.Regexp(t, `### https://example.com/second[\s\S]*### https://example.com/first`, s)
}

func TestWriteGaps_MissingScreenshotsEmpty_OmitsSection(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, reporter.WriteGaps(dir, nil, nil, nil))
	body, err := os.ReadFile(filepath.Join(dir, "gaps.md"))
	require.NoError(t, err)
	assert.NotContains(t, string(body), "## Missing Screenshots")
}

func TestWriteGaps_NoLongerRendersScreenshotsSection(t *testing.T) {
	dir := t.TempDir()
	mapping := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "search"}, Files: []string{"search.go"}},
	}
	// New 4-arg signature — no screenshot argument at all.
	require.NoError(t, reporter.WriteGaps(dir, mapping, []string{"search"}, nil))

	body, err := os.ReadFile(filepath.Join(dir, "gaps.md"))
	require.NoError(t, err)
	s := string(body)
	assert.NotContains(t, s, "Missing Screenshots")
}
