package pdf_test

import (
	"io"
	"path/filepath"
	"testing"
	"time"

	pdfreader "github.com/ledongthuc/pdf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/pdf"
)

// extractText returns the entire text content of the PDF at path, joining
// all pages. Core-font (Helvetica/Courier) ASCII output extracts cleanly;
// non-ASCII glyphs may not survive, so test assertions stick to ASCII.
func extractText(t *testing.T, path string) string {
	t.Helper()
	f, r, err := pdfreader.Open(path)
	require.NoError(t, err)
	defer f.Close()

	rd, err := r.GetPlainText()
	require.NoError(t, err)
	b, err := io.ReadAll(rd)
	require.NoError(t, err)
	return string(b)
}

func TestRenderCover_ContainsMetadata(t *testing.T) {
	dir := t.TempDir()

	in := pdf.Inputs{
		ProjectName: "Test Project",
		RepoURL:     "https://github.com/foo/bar",
		DocsURL:     "https://docs.foo.example",
		GeneratedAt: time.Date(2026, 5, 13, 14, 32, 0, 0, time.UTC),
		Mapping: analyzer.FeatureMap{
			{Feature: analyzer.CodeFeature{Name: "alpha", UserFacing: true}},
			{Feature: analyzer.CodeFeature{Name: "beta", UserFacing: true}},
			{Feature: analyzer.CodeFeature{Name: "gamma", UserFacing: false}},
		},
		Drift: []analyzer.DriftFinding{
			{Feature: "alpha", Issues: []analyzer.DriftIssue{
				{Issue: "stale signature", Priority: analyzer.PriorityLarge, PriorityReason: "blocks integration"},
				{Issue: "removed param", Priority: analyzer.PrioritySmall, PriorityReason: "cosmetic"},
			}},
			{Feature: "beta", Issues: []analyzer.DriftIssue{
				{Issue: "wrong example", Priority: analyzer.PriorityMedium, PriorityReason: "misleads users"},
			}},
		},
		Screenshots: analyzer.ScreenshotResult{
			MissingGaps: []analyzer.ScreenshotGap{
				{PageURL: "https://docs.foo.example/a", ShouldShow: "dialog", Priority: analyzer.PriorityLarge, PriorityReason: "primary flow"},
				{PageURL: "https://docs.foo.example/b", ShouldShow: "form", Priority: analyzer.PriorityMedium, PriorityReason: "secondary"},
			},
			ImageIssues: []analyzer.ImageIssue{
				{PageURL: "https://docs.foo.example/c", Src: "img.png", Reason: "wrong image", Priority: analyzer.PrioritySmall, PriorityReason: "edge"},
			},
		},
		ScreenshotsRan: true,
	}

	err := pdf.WriteReport(dir, in)
	require.NoError(t, err)

	text := extractText(t, filepath.Join(dir, "report.pdf"))

	assert.Contains(t, text, "Test Project", "cover must include project name")
	assert.Contains(t, text, "github.com/foo/bar", "cover must include repo URL")
	assert.Contains(t, text, "docs.foo.example", "cover must include docs URL")
	assert.Contains(t, text, "2026-05-13", "cover must include date")
	assert.Contains(t, text, "14:32", "cover must include time")
	assert.Contains(t, text, "UTC", "cover must include timezone marker")

	// Category-summary sections render counts inside their headings
	// ("Features (3)", "Gaps (3)", "Screenshot Issues (3)") and list
	// the findings underneath.
	assert.Contains(t, text, "Features (3)", "cover must show Features count")
	assert.Contains(t, text, "Gaps (3)", "cover must show Gaps count")
	assert.Contains(t, text, "Screenshot Issues (3)", "cover must show Screenshot Issues count")
	// Item lines appear: at least one feature, one drift, one screenshot.
	assert.Contains(t, text, "alpha", "feature name in summary")
	assert.Contains(t, text, "stale signature", "drift issue in summary")
	assert.Contains(t, text, "dialog", "missing-screenshot ShouldShow in summary")
}

func TestRenderCover_ScreenshotCountOmittedWhenNotRun(t *testing.T) {
	dir := t.TempDir()

	in := pdf.Inputs{
		ProjectName:    "No Screenshots",
		GeneratedAt:    time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC),
		ScreenshotsRan: false,
	}

	err := pdf.WriteReport(dir, in)
	require.NoError(t, err)

	text := extractText(t, filepath.Join(dir, "report.pdf"))
	// "Screenshot" appears in body section pages even when no findings,
	// so scope this check to the cover-page heading specifically.
	assert.NotContains(t, text, "Screenshot Issues (",
		"cover must omit Screenshot Issues section when screenshots did not run")
}
