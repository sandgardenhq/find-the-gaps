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

// TestWriteGaps_UndocumentedFeatures verifies that a user-facing feature with
// code but no documentation page appears in the Undocumented Features section
// as a plain-markdown block: feature heading, optional description blockquote,
// and a "Why document this:" rationale paragraph.
func TestWriteGaps_UndocumentedFeatures(t *testing.T) {
	dir := t.TempDir()
	mapping := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth", Description: "Login and session management.", Layer: "cli", UserFacing: true}, Files: []string{"auth.go"}},
	}
	if err := reporter.WriteGaps(dir, mapping, []string{}, []analyzer.DriftFinding{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "gaps.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{
		"## Undocumented Features",
		"### auth",
		"> Login and session management.",
		"**Why document this:**",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q in gaps.md:\n%s", want, content)
		}
	}
	// No HTML.
	if strings.Contains(content, "<div") || strings.Contains(content, "<span") {
		t.Errorf("gaps.md must be plain markdown, no <div>/<span>; got:\n%s", content)
	}
	// "Undocumented Code" was renamed; the section heading must not appear.
	if strings.Contains(content, "## Undocumented Code") {
		t.Error("gaps.md must not contain the old `## Undocumented Code` heading")
	}
}

// TestWriteGaps_UndocumentedFeaturesNoDescription pins that an undocumented
// feature with an empty Description still renders the feature heading and the
// rationale paragraph, and omits the blockquote rather than emitting an empty
// one.
func TestWriteGaps_UndocumentedFeaturesNoDescription(t *testing.T) {
	dir := t.TempDir()
	mapping := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth", Description: "", UserFacing: true}, Files: []string{"auth.go"}},
	}
	if err := reporter.WriteGaps(dir, mapping, []string{}, []analyzer.DriftFinding{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "gaps.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "### auth") {
		t.Errorf("expected feature heading even without description; got:\n%s", content)
	}
	if !strings.Contains(content, "**Why document this:**") {
		t.Errorf("expected rationale even without description; got:\n%s", content)
	}
	if strings.Contains(content, "> \n") || strings.Contains(content, "> \n\n") {
		t.Errorf("empty description must not render an empty blockquote; got:\n%s", content)
	}
}

// TestWriteGaps_OnlyUserFacingUndocumented pins the policy that
// non-user-facing undocumented features no longer appear in gaps.md.
func TestWriteGaps_OnlyUserFacingUndocumented(t *testing.T) {
	dir := t.TempDir()
	mapping := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "internal-thing", Layer: "infra", UserFacing: false}, Files: []string{"x.go"}},
		{Feature: analyzer.CodeFeature{Name: "user-thing", Layer: "ui", UserFacing: true}, Files: []string{"y.go"}},
	}
	if err := reporter.WriteGaps(dir, mapping, []string{}, []analyzer.DriftFinding{}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "gaps.md"))
	content := string(data)
	if strings.Contains(content, "internal-thing") {
		t.Errorf("non-user-facing features must not appear in gaps.md; got:\n%s", content)
	}
	if !strings.Contains(content, "user-thing") {
		t.Errorf("user-facing undocumented features must appear; got:\n%s", content)
	}
	if strings.Contains(content, "Not user-facing") {
		t.Errorf("the `Not user-facing` sub-heading must be removed; got:\n%s", content)
	}
}

// TestWriteGaps_UnmappedFeaturesRemoved pins removal of the
// "## Unmapped Features" section. Documented-but-unmapped features in
// allDocFeatures are no longer reported.
func TestWriteGaps_UnmappedFeaturesRemoved(t *testing.T) {
	dir := t.TempDir()
	mapping := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "real", UserFacing: true}, Files: []string{"r.go"}},
	}
	allDocFeatures := []string{"hallucinated"}
	if err := reporter.WriteGaps(dir, mapping, allDocFeatures, []analyzer.DriftFinding{}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "gaps.md"))
	content := string(data)
	if strings.Contains(content, "## Unmapped Features") {
		t.Errorf("the `## Unmapped Features` section must be removed; got:\n%s", content)
	}
	if strings.Contains(content, "hallucinated") {
		t.Errorf("documented-but-unmapped features must no longer be reported; got:\n%s", content)
	}
}

// TestWriteGaps_FeatureNoFiles_NotUndocumented verifies that a feature the LLM
// could not map to any file is not listed as undocumented.
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
		t.Error("features with no mapped files must not appear as undocumented")
	}
}

// TestWriteGaps_StaleDocBulletAndPriorityHeader pins the shape for stale-doc
// findings: each entry renders as a markdown bullet under a priority
// sub-heading, with the page rendered as a markdown link and a nested
// "Why" sub-bullet for the priority reason.
func TestWriteGaps_StaleDocBulletAndPriorityHeader(t *testing.T) {
	dir := t.TempDir()
	mapping := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "search", UserFacing: true}, Files: []string{"s.go"}},
	}
	drift := []analyzer.DriftFinding{
		{Feature: "search", Issues: []analyzer.DriftIssue{
			{Page: "https://docs.example.com/search", Issue: "old signature", Priority: analyzer.PriorityLarge, PriorityReason: "user-impact"},
		}},
	}
	if err := reporter.WriteGaps(dir, mapping, []string{"search"}, drift); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "gaps.md"))
	content := string(data)
	for _, want := range []string{
		"### Large",
		"- **search** — [https://docs.example.com/search](https://docs.example.com/search) — old signature",
		"  - _Why:_ user-impact",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q in gaps.md:\n%s", want, content)
		}
	}
	if strings.Contains(content, "<div") || strings.Contains(content, "<span") {
		t.Errorf("gaps.md must be plain markdown; got:\n%s", content)
	}
}

// TestWriteGaps_PreservesRawSpecialChars pins that LLM-derived text fields are
// emitted verbatim into the plain-markdown output — no HTML escaping, since
// there's no raw-HTML interpolation site to protect anymore.
func TestWriteGaps_PreservesRawSpecialChars(t *testing.T) {
	dir := t.TempDir()
	mapping := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "search & index", UserFacing: true}, Files: []string{"s.go"}},
	}
	drift := []analyzer.DriftFinding{
		{Feature: "search & index", Issues: []analyzer.DriftIssue{
			{
				Page:           "https://docs.example.com/search?a=1&b=<2>",
				Issue:          "signature changed from Foo() to Foo[T any]() <breaking>",
				Priority:       analyzer.PriorityLarge,
				PriorityReason: "user-impact: callers must update <T>",
			},
		}},
	}
	if err := reporter.WriteGaps(dir, mapping, []string{}, drift); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "gaps.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	for _, want := range []string{
		"search & index",
		"signature changed from Foo() to Foo[T any]() <breaking>",
		"user-impact: callers must update <T>",
		"https://docs.example.com/search?a=1&b=<2>",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q in gaps.md:\n%s", want, content)
		}
	}
	// No HTML entity escaping should leak into the plain-markdown output.
	for _, bad := range []string{"&amp;", "&lt;", "&gt;", "&#39;"} {
		if strings.Contains(content, bad) {
			t.Errorf("gaps.md must not contain HTML entities (%q); got:\n%s", bad, content)
		}
	}
}

// TestWriteScreenshots_PreservesRawSpecialChars pins the same plain-text
// behavior for missing-screenshot, possibly-covered, and image-issue blocks.
func TestWriteScreenshots_PreservesRawSpecialChars(t *testing.T) {
	dir := t.TempDir()
	res := analyzer.ScreenshotResult{
		MissingGaps: []analyzer.ScreenshotGap{{
			PageURL:        "https://x.example/p?a=1&b=2",
			QuotedPassage:  "open the dashboard",
			ShouldShow:     "the <Save> button",
			SuggestedAlt:   "Save & Apply",
			InsertionHint:  "after <h1>",
			Priority:       analyzer.PriorityMedium,
			PriorityReason: "user-impact <flow>",
		}},
		ImageIssues: []analyzer.ImageIssue{{
			PageURL:         "https://x.example/p?c=1&d=2",
			Index:           "img1",
			Src:             "img.png",
			Reason:          "alt text mentions <Save>",
			SuggestedAction: "rewrite alt to remove <Save>",
			Priority:        analyzer.PriorityLarge,
			PriorityReason:  "AA violation <wcag>",
		}},
		// `## Image Issues` is gated on at least one page where vision ran.
		AuditStats: []analyzer.ScreenshotPageStats{{PageURL: "https://x.example/p?c=1&d=2", VisionEnabled: true}},
	}
	if err := reporter.WriteScreenshots(dir, res); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "screenshots.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	for _, want := range []string{
		"the <Save> button",
		"Save & Apply",
		"after <h1>",
		"user-impact <flow>",
		"alt text mentions <Save>",
		"rewrite alt to remove <Save>",
		"AA violation <wcag>",
		"https://x.example/p?a=1&b=2",
		"https://x.example/p?c=1&d=2",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q in screenshots.md:\n%s", want, content)
		}
	}
	for _, bad := range []string{"&amp;", "&lt;", "&gt;", "&#39;"} {
		if strings.Contains(content, bad) {
			t.Errorf("screenshots.md must not contain HTML entities (%q); got:\n%s", bad, content)
		}
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
			Files:   []string{"cmd/ftg/main.go"},
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
				{Page: "https://docs.example.com/auth", Issue: "The email field requirement is not documented.",
					Priority: analyzer.PriorityLarge, PriorityReason: "quickstart impact"},
				{Page: "", Issue: "The error response format differs from what is described.",
					Priority: analyzer.PriorityMedium, PriorityReason: "reference page"},
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
	if !strings.Contains(content, "email field requirement") {
		t.Errorf("gaps.md must contain the drift issue text, got:\n%s", content)
	}
	if !strings.Contains(content, "https://docs.example.com/auth") {
		t.Errorf("gaps.md must cite the page URL for issues with a page, got:\n%s", content)
	}
	if !strings.Contains(content, "auth") {
		t.Errorf("gaps.md must mention the feature name, got:\n%s", content)
	}
}

// TestWriteGaps_StaleDocumentation_GroupsByPriority pins the priority-grouped
// rendering: Large first, then Medium, then Small. Empty buckets omitted.
// Stable original order preserved within each bucket.
func TestWriteGaps_StaleDocumentation_GroupsByPriority(t *testing.T) {
	dir := t.TempDir()
	mapping := analyzer.FeatureMap{}
	drift := []analyzer.DriftFinding{
		{Feature: "alpha", Issues: []analyzer.DriftIssue{
			{Page: "p1", Issue: "small-issue-A", Priority: analyzer.PrioritySmall, PriorityReason: "deep"},
			{Page: "p2", Issue: "large-issue-A", Priority: analyzer.PriorityLarge, PriorityReason: "readme"},
		}},
		{Feature: "beta", Issues: []analyzer.DriftIssue{
			{Page: "p3", Issue: "medium-issue-B", Priority: analyzer.PriorityMedium, PriorityReason: "reference"},
			{Page: "p4", Issue: "large-issue-B", Priority: analyzer.PriorityLarge, PriorityReason: "quickstart"},
		}},
	}
	if err := reporter.WriteGaps(dir, mapping, []string{}, drift); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "gaps.md"))
	content := string(b)

	// Headings appear in Large -> Medium -> Small order.
	idxLarge := strings.Index(content, "### Large")
	idxMedium := strings.Index(content, "### Medium")
	idxSmall := strings.Index(content, "### Small")
	if idxLarge < 0 || idxMedium < 0 || idxSmall < 0 {
		t.Fatalf("missing one of Large/Medium/Small headings:\n%s", content)
	}
	if idxLarge >= idxMedium || idxMedium >= idxSmall {
		t.Errorf("priority headings out of order: large=%d medium=%d small=%d", idxLarge, idxMedium, idxSmall)
	}

	// Within Large: large-issue-A appears before large-issue-B (input order).
	largeBlock := content[idxLarge:idxMedium]
	if strings.Index(largeBlock, "large-issue-A") > strings.Index(largeBlock, "large-issue-B") {
		t.Errorf("Large bucket order broken:\n%s", largeBlock)
	}

	// priority_reason renders.
	if !strings.Contains(content, "readme") {
		t.Errorf("priority_reason missing from output:\n%s", content)
	}
}

func TestWriteGaps_StaleDocumentation_OmitsEmptyBuckets(t *testing.T) {
	dir := t.TempDir()
	drift := []analyzer.DriftFinding{
		{Feature: "x", Issues: []analyzer.DriftIssue{
			{Page: "p", Issue: "med-only", Priority: analyzer.PriorityMedium, PriorityReason: "r"},
		}},
	}
	if err := reporter.WriteGaps(dir, analyzer.FeatureMap{}, []string{}, drift); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "gaps.md"))
	content := string(b)
	if strings.Contains(content, "### Large") {
		t.Errorf("empty Large bucket must be omitted:\n%s", content)
	}
	if strings.Contains(content, "### Small") {
		t.Errorf("empty Small bucket must be omitted:\n%s", content)
	}
	if !strings.Contains(content, "### Medium") {
		t.Errorf("Medium bucket must be present:\n%s", content)
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

func TestWriteScreenshots_CreatesFile_WithFindings(t *testing.T) {
	dir := t.TempDir()
	gaps := []analyzer.ScreenshotGap{
		{
			PageURL:        "https://example.com/quickstart",
			PagePath:       "/cache/quickstart.md",
			QuotedPassage:  "Run the command and see the output.",
			ShouldShow:     "Terminal showing the analyze summary with findings count.",
			SuggestedAlt:   "Terminal output of find-the-gaps analyze",
			InsertionHint:  "after the paragraph ending '...see the output.'",
			Priority:       analyzer.PriorityMedium,
			PriorityReason: "test stub",
		},
		{
			PageURL:        "https://example.com/quickstart",
			PagePath:       "/cache/quickstart.md",
			QuotedPassage:  "The dashboard shows open PRs.",
			ShouldShow:     "Dashboard with two open PRs visible.",
			SuggestedAlt:   "Dashboard with open PRs",
			InsertionHint:  "after the heading '## Dashboard'",
			Priority:       analyzer.PriorityMedium,
			PriorityReason: "test stub",
		},
		{
			PageURL:        "https://example.com/setup",
			PagePath:       "/cache/setup.md",
			QuotedPassage:  "Configure the CLI.",
			ShouldShow:     "The config file open in an editor.",
			SuggestedAlt:   "Configuration file",
			InsertionHint:  "after the code block",
			Priority:       analyzer.PriorityMedium,
			PriorityReason: "test stub",
		},
	}
	require.NoError(t, reporter.WriteScreenshots(dir, analyzer.ScreenshotResult{MissingGaps: gaps}))
	body, err := os.ReadFile(filepath.Join(dir, "screenshots.md"))
	require.NoError(t, err)
	s := string(body)
	assert.Contains(t, s, "# Missing Screenshots")
	// Page subheadings live at `#### ` inside the priority bucket and use a
	// derived label so they read as page names, not repeated URLs.
	assert.Regexp(t, `#### Quickstart[\s\S]*#### Setup`, s)
	// The URL appears inline as a markdown link.
	assert.Contains(t, s, "[https://example.com/quickstart](https://example.com/quickstart)")
	// Each gap's fields render verbatim.
	assert.Contains(t, s, "Run the command and see the output.")
	assert.Contains(t, s, "Terminal showing the analyze summary")
	assert.Contains(t, s, "Terminal output of find-the-gaps analyze")
	// Apostrophes in LLM-derived fields are NOT HTML-escaped in plain markdown.
	assert.Contains(t, s, "after the paragraph ending '...see the output.'")
	// No HTML in the output.
	assert.NotContains(t, s, "<div")
	assert.NotContains(t, s, "<span")
}

func TestWriteScreenshots_Empty_WritesNoneFound(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, reporter.WriteScreenshots(dir, analyzer.ScreenshotResult{}))

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
		{PageURL: "https://example.com/second", QuotedPassage: "second-page passage.", Priority: analyzer.PriorityMedium, PriorityReason: "r"},
		{PageURL: "https://example.com/first", QuotedPassage: "first-page passage.", Priority: analyzer.PriorityMedium, PriorityReason: "r"},
		{PageURL: "https://example.com/second", QuotedPassage: "second-page passage 2.", Priority: analyzer.PriorityMedium, PriorityReason: "r"},
	}
	require.NoError(t, reporter.WriteScreenshots(dir, analyzer.ScreenshotResult{MissingGaps: gaps}))
	body, err := os.ReadFile(filepath.Join(dir, "screenshots.md"))
	require.NoError(t, err)
	s := string(body)
	assert.Regexp(t, `#### Second[\s\S]*#### First`, s)
}

// TestWriteScreenshots_PassageWithNewlinesPreservesLineBreaks pins the
// rendering of QuotedPassage when it contains real newlines, tabs, double
// quotes, or backslashes. The output must contain the original characters
// as-is and MUST NOT contain any of the Go-syntax escape sequences `\n`,
// `\t`, or `\"` as text.
func TestWriteScreenshots_PassageWithNewlinesPreservesLineBreaks(t *testing.T) {
	dir := t.TempDir()
	const passage = "2. Click Add API Key.\n \n3. Enter a Name.\n \n4. Create the key, then run `curl -H \"Authorization: Bearer <token>\"`."
	gaps := []analyzer.ScreenshotGap{{
		PageURL:        "https://example.com/auth",
		QuotedPassage:  passage,
		ShouldShow:     "API Keys settings page",
		SuggestedAlt:   "API Keys settings",
		InsertionHint:  "after the Bearer token paragraph",
		Priority:       analyzer.PriorityMedium,
		PriorityReason: "r",
	}}
	require.NoError(t, reporter.WriteScreenshots(dir, analyzer.ScreenshotResult{MissingGaps: gaps}))
	body, err := os.ReadFile(filepath.Join(dir, "screenshots.md"))
	require.NoError(t, err)
	s := string(body)

	assert.Contains(t, s, passage,
		"the passage must round-trip with its real newlines and quotes intact")
	assert.NotContains(t, s, `\n`, "screenshots.md must not contain the literal escape `\\n`")
	assert.NotContains(t, s, `\t`, "screenshots.md must not contain the literal escape `\\t`")
	assert.NotContains(t, s, `\"`, "screenshots.md must not contain the literal escape `\\\"`")
}

// TestWriteScreenshots_PerPageHeadingHasExplicitAnchor pins the fix for a
// Hugo-rendering bug: explicit `{#anchor}` IDs survive on per-page headings
// so the inline permalink / right-rail TOC keep working after the rename.
func TestWriteScreenshots_PerPageHeadingHasExplicitAnchor(t *testing.T) {
	dir := t.TempDir()
	gaps := []analyzer.ScreenshotGap{
		{PageURL: "https://example.com/docs/start", QuotedPassage: "p", Priority: analyzer.PriorityMedium, PriorityReason: "r"},
		{PageURL: "https://example.com/docs/admin", QuotedPassage: "p", Priority: analyzer.PriorityMedium, PriorityReason: "r"},
	}
	require.NoError(t, reporter.WriteScreenshots(dir, analyzer.ScreenshotResult{MissingGaps: gaps}))
	body, err := os.ReadFile(filepath.Join(dir, "screenshots.md"))
	require.NoError(t, err)
	s := string(body)

	assert.Contains(t, s, "#### Start {#https-example-com-docs-start}")
	assert.Contains(t, s, "#### Admin {#https-example-com-docs-admin}")
}

func TestWriteScreenshots_RendersImageIssuesSection(t *testing.T) {
	tmp := t.TempDir()
	res := analyzer.ScreenshotResult{
		AuditStats: []analyzer.ScreenshotPageStats{{PageURL: "https://x/p", VisionEnabled: true}},
		ImageIssues: []analyzer.ImageIssue{{
			PageURL:         "https://x/p",
			Index:           "img-1",
			Src:             "b.png",
			Reason:          "shows dashboard but prose describes settings",
			SuggestedAction: "replace",
			Priority:        analyzer.PriorityMedium,
			PriorityReason:  "r",
		}},
	}
	require.NoError(t, reporter.WriteScreenshots(tmp, res))
	body, err := os.ReadFile(filepath.Join(tmp, "screenshots.md"))
	require.NoError(t, err)
	s := string(body)
	assert.Contains(t, s, "## Image Issues")
	assert.Contains(t, s, "shows dashboard but prose describes settings")
	assert.Contains(t, s, "https://x/p")
	assert.Contains(t, s, "b.png")
	assert.Contains(t, s, "img-1")
	assert.Contains(t, s, "replace")
}

func TestWriteScreenshots_VisionRanButNoIssues_RendersEmptyMarker(t *testing.T) {
	tmp := t.TempDir()
	res := analyzer.ScreenshotResult{
		AuditStats: []analyzer.ScreenshotPageStats{{PageURL: "https://x/p", VisionEnabled: true}},
	}
	require.NoError(t, reporter.WriteScreenshots(tmp, res))
	body, err := os.ReadFile(filepath.Join(tmp, "screenshots.md"))
	require.NoError(t, err)
	s := string(body)
	assert.Contains(t, s, "## Image Issues")
	assert.Contains(t, s, "_No image issues detected._")
}

func TestWriteScreenshots_VisionDidNotRun_OmitsImageIssuesHeader(t *testing.T) {
	tmp := t.TempDir()
	res := analyzer.ScreenshotResult{
		AuditStats: []analyzer.ScreenshotPageStats{{PageURL: "https://x/p", VisionEnabled: false}},
	}
	require.NoError(t, reporter.WriteScreenshots(tmp, res))
	body, err := os.ReadFile(filepath.Join(tmp, "screenshots.md"))
	require.NoError(t, err)
	s := string(body)
	assert.NotContains(t, s, "## Image Issues")
}

func TestWriteScreenshotsRendersPossiblyCovered(t *testing.T) {
	dir := t.TempDir()
	res := analyzer.ScreenshotResult{
		MissingGaps: []analyzer.ScreenshotGap{},
		PossiblyCovered: []analyzer.ScreenshotGap{{
			PageURL:        "https://x.com/p",
			QuotedPassage:  "Watch the upload demo.",
			ShouldShow:     "upload flow",
			SuggestedAlt:   "upload demo",
			InsertionHint:  "after the demo paragraph",
			Priority:       analyzer.PriorityMedium,
			PriorityReason: "r",
		}},
		AuditStats: []analyzer.ScreenshotPageStats{{PageURL: "https://x.com/p", VisionEnabled: true}},
	}
	if err := reporter.WriteScreenshots(dir, res); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "screenshots.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	if !strings.Contains(got, "## Possibly Covered") {
		t.Error("expected '## Possibly Covered' header in screenshots.md")
	}
	if !strings.Contains(got, "Watch the upload demo.") {
		t.Error("expected the quoted passage in the rendered output")
	}
	// Ordering: ## Possibly Covered must come AFTER ## Missing Screenshots
	// and BEFORE ## Image Issues so the file reads top-down by severity.
	missingPos := strings.Index(got, "Missing Screenshots")
	pcPos := strings.Index(got, "Possibly Covered")
	imgPos := strings.Index(got, "Image Issues")
	if missingPos >= pcPos || pcPos >= imgPos {
		t.Errorf("section ordering wrong: missing=%d possibly=%d issues=%d", missingPos, pcPos, imgPos)
	}
}

func TestWriteScreenshotsOmitsPossiblyCoveredWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	res := analyzer.ScreenshotResult{
		MissingGaps: []analyzer.ScreenshotGap{{PageURL: "https://x.com/p", QuotedPassage: "ok", Priority: analyzer.PriorityMedium, PriorityReason: "r"}},
		AuditStats:  []analyzer.ScreenshotPageStats{{PageURL: "https://x.com/p"}},
	}
	if err := reporter.WriteScreenshots(dir, res); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "screenshots.md"))
	if strings.Contains(string(body), "Possibly Covered") {
		t.Error("Possibly Covered must not render when the slice is empty")
	}
}

// TestWriteScreenshots_GroupsByPriority pins that all three sections (missing,
// possibly-covered, image-issues) render their findings under
// ### Large / ### Medium / ### Small sub-headings, in that order, with empty
// buckets omitted and stable input order preserved within a bucket.
func TestWriteScreenshots_GroupsByPriority(t *testing.T) {
	dir := t.TempDir()
	res := analyzer.ScreenshotResult{
		MissingGaps: []analyzer.ScreenshotGap{
			{PageURL: "https://x/a", QuotedPassage: "missing-small", Priority: analyzer.PrioritySmall, PriorityReason: "deep page"},
			{PageURL: "https://x/b", QuotedPassage: "missing-large", Priority: analyzer.PriorityLarge, PriorityReason: "quickstart"},
		},
		PossiblyCovered: []analyzer.ScreenshotGap{
			{PageURL: "https://x/c", QuotedPassage: "covered-medium", Priority: analyzer.PriorityMedium, PriorityReason: "ref"},
		},
		ImageIssues: []analyzer.ImageIssue{
			{PageURL: "https://x/d", Index: "img-1", Src: "x.png", Reason: "issue-large", SuggestedAction: "replace",
				Priority: analyzer.PriorityLarge, PriorityReason: "readme"},
		},
		AuditStats: []analyzer.ScreenshotPageStats{
			{PageURL: "https://x/d", VisionEnabled: true},
		},
	}
	require.NoError(t, reporter.WriteScreenshots(dir, res))
	body, err := os.ReadFile(filepath.Join(dir, "screenshots.md"))
	require.NoError(t, err)
	s := string(body)

	// Missing Screenshots: large bucket appears before small (ordering).
	missingHeader := strings.Index(s, "# Missing Screenshots")
	largePos := strings.Index(s[missingHeader:], "### Large")
	smallPos := strings.Index(s[missingHeader:], "### Small")
	if largePos < 0 || smallPos < 0 {
		t.Fatalf("missing-screenshots priority headings not found:\n%s", s)
	}
	if largePos > smallPos {
		t.Errorf("Large must appear before Small in missing-screenshots:\n%s", s)
	}
	// missing-large appears, missing-small appears.
	assert.Contains(t, s, "missing-large")
	assert.Contains(t, s, "missing-small")
	// medium bucket has nothing in missing-screenshots; ensure no `### Medium`
	// appears between # Missing Screenshots and the next ## section.
	endMissing := strings.Index(s, "## Possibly Covered")
	missingBlock := s[missingHeader:endMissing]
	if strings.Contains(missingBlock, "### Medium") {
		t.Errorf("empty Medium bucket must be omitted from missing-screenshots:\n%s", missingBlock)
	}

	// Possibly Covered: medium bucket appears.
	possiblyHeader := strings.Index(s, "## Possibly Covered")
	imageIssuesHeader := strings.Index(s, "## Image Issues")
	possiblyBlock := s[possiblyHeader:imageIssuesHeader]
	if !strings.Contains(possiblyBlock, "### Medium") {
		t.Errorf("Possibly Covered missing Medium bucket:\n%s", possiblyBlock)
	}

	// Image Issues: large bucket appears.
	imageBlock := s[imageIssuesHeader:]
	if !strings.Contains(imageBlock, "### Large") {
		t.Errorf("Image Issues missing Large bucket:\n%s", imageBlock)
	}
	if !strings.Contains(imageBlock, "issue-large") {
		t.Errorf("image-issue body not rendered:\n%s", imageBlock)
	}

	// Each section emits a why line (priority_reason).
	assert.Contains(t, s, "quickstart")
}

func TestBuildGapsStaticPrefix_includesUndocumentedFeatures(t *testing.T) {
	mapping := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "alpha", UserFacing: true}, Files: []string{"a.go"}}, // undocumented user-facing → shown
		{Feature: analyzer.CodeFeature{Name: "beta", UserFacing: false}, Files: []string{"b.go"}}, // non-user-facing → excluded by policy
		{Feature: analyzer.CodeFeature{Name: "gamma", UserFacing: true}, Files: []string{"c.go"}}, // documented (in allDocFeatures) → not undoc
		{Feature: analyzer.CodeFeature{Name: "delta", UserFacing: true}, Files: []string{}},       // no code mapping → not undoc
	}
	docFeatures := []string{"gamma", "epsilon"} // epsilon is doc-only — main removed the Unmapped Features section
	got := reporter.BuildGapsStaticPrefix(mapping, docFeatures, nil)
	assert.Contains(t, got, "# Gaps Found")
	assert.Contains(t, got, "## Undocumented Features")
	assert.Contains(t, got, "alpha")
	// beta, gamma, delta, epsilon must not appear in the prefix.
	assert.NotContains(t, got, "beta")
	assert.NotContains(t, got, "gamma")
	assert.NotContains(t, got, "delta")
	assert.NotContains(t, got, "epsilon")
	// Old section names that no longer exist.
	assert.NotContains(t, got, "## Undocumented Code")
	assert.NotContains(t, got, "## Unmapped Features")
	assert.NotContains(t, got, "## Stale Documentation")
}

// TestBuildGapsStaticPrefix_OmitsUndocumentedSectionWhenEmpty pins that
// when there are zero undocumented user-facing features, the
// "## Undocumented Features" header is dropped from gaps.md entirely
// rather than rendered with a "_None found._" body. The Gaps page
// should not advertise a section that has nothing to show.
func TestBuildGapsStaticPrefix_OmitsUndocumentedSectionWhenEmpty(t *testing.T) {
	mapping := analyzer.FeatureMap{
		// alpha is documented (in docFeatures below) → not undoc
		{Feature: analyzer.CodeFeature{Name: "alpha", UserFacing: true}, Files: []string{"a.go"}},
		// beta is not user-facing → excluded by policy
		{Feature: analyzer.CodeFeature{Name: "beta", UserFacing: false}, Files: []string{"b.go"}},
	}
	got := reporter.BuildGapsStaticPrefix(mapping, []string{"alpha"}, nil)
	assert.Contains(t, got, "# Gaps Found")
	assert.NotContains(t, got, "## Undocumented Features")
	assert.NotContains(t, got, "_None found._")
}

func TestBuildGapsStaticPrefix_omitsStaleSection(t *testing.T) {
	mapping := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "alpha", UserFacing: true}, Files: []string{"a.go"}},
	}
	got := reporter.BuildGapsStaticPrefix(mapping, []string{"alpha"}, nil)
	assert.NotContains(t, got, "## Stale Documentation")
	assert.NotContains(t, got, "_None found._\n\n## Stale Documentation")
}

func TestBuildGapsStaleSection_priorityBucketing(t *testing.T) {
	drift := []analyzer.DriftFinding{
		{Feature: "alpha", Issues: []analyzer.DriftIssue{
			{Page: "p1", Issue: "small-issue-A", Priority: analyzer.PrioritySmall, PriorityReason: "deep"},
			{Page: "p2", Issue: "large-issue-A", Priority: analyzer.PriorityLarge, PriorityReason: "readme"},
		}},
		{Feature: "beta", Issues: []analyzer.DriftIssue{
			{Page: "p3", Issue: "medium-issue-B", Priority: analyzer.PriorityMedium, PriorityReason: "reference"},
			{Page: "p4", Issue: "large-issue-B", Priority: analyzer.PriorityLarge, PriorityReason: "quickstart"},
		}},
	}
	got := reporter.BuildGapsStaleSection(drift)
	idxLarge := strings.Index(got, "### Large")
	idxMedium := strings.Index(got, "### Medium")
	idxSmall := strings.Index(got, "### Small")
	require.GreaterOrEqual(t, idxLarge, 0)
	require.GreaterOrEqual(t, idxMedium, 0)
	require.GreaterOrEqual(t, idxSmall, 0)
	assert.Less(t, idxLarge, idxMedium)
	assert.Less(t, idxMedium, idxSmall)
	// The stale-section helper does NOT include the section header itself; the
	// composing call (WriteGaps) renders "## Stale Documentation".
	assert.NotContains(t, got, "## Stale Documentation")
	// Within Large bucket, large-issue-A appears before large-issue-B.
	largeBlock := got[idxLarge:idxMedium]
	assert.Less(t, strings.Index(largeBlock, "large-issue-A"), strings.Index(largeBlock, "large-issue-B"))
}

func TestBuildGapsStaleSection_emptyInput(t *testing.T) {
	got := reporter.BuildGapsStaleSection(nil)
	assert.Contains(t, got, "_None found._")
	assert.NotContains(t, got, "### Large")
	assert.NotContains(t, got, "### Medium")
	assert.NotContains(t, got, "### Small")
}

// TestWriteGaps_byteIdenticalToBuilders is the safety net for the streaming
// GapsWriter: composing the two helpers produces exactly the bytes WriteGaps
// writes to disk. A regression here means the writer goroutine will silently
// disagree with WriteGaps and churn gaps.md by one newline on every run.
func TestWriteGaps_byteIdenticalToBuilders(t *testing.T) {
	mapping := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "alpha", UserFacing: true}, Files: []string{"a.go"}},
		{Feature: analyzer.CodeFeature{Name: "beta"}, Files: []string{"b.go"}},
		{Feature: analyzer.CodeFeature{Name: "gamma", UserFacing: true}, Files: []string{"c.go"}},
	}
	docFeatures := []string{"gamma", "delta", "epsilon"}
	drift := []analyzer.DriftFinding{
		{Feature: "alpha", Issues: []analyzer.DriftIssue{
			{Page: "p1", Issue: "large-A", Priority: analyzer.PriorityLarge, PriorityReason: "readme"},
			{Page: "p2", Issue: "small-A", Priority: analyzer.PrioritySmall, PriorityReason: "deep"},
		}},
		{Feature: "gamma", Issues: []analyzer.DriftIssue{
			{Page: "p3", Issue: "medium-G", Priority: analyzer.PriorityMedium, PriorityReason: "reference"},
		}},
	}

	dir := t.TempDir()
	require.NoError(t, reporter.WriteGaps(dir, mapping, docFeatures, drift))
	onDisk, err := os.ReadFile(filepath.Join(dir, "gaps.md"))
	require.NoError(t, err)

	composed := reporter.BuildGapsStaticPrefix(mapping, docFeatures, nil) +
		"\n## Stale Documentation\n\n" +
		reporter.BuildGapsStaleSection(drift)

	assert.Equal(t, string(onDisk), composed,
		"composing the two builders must produce byte-identical output to WriteGaps")
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
