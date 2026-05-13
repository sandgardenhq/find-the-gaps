package pdf

import (
	"path/filepath"
	"testing"
	"time"

	pdfreader "github.com/ledongthuc/pdf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestTOC_ListsTopLevelSections(t *testing.T) {
	dir := t.TempDir()

	in := Inputs{
		ProjectName: "TOC Project",
		GeneratedAt: time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC),
		Mapping: analyzer.FeatureMap{
			{Feature: analyzer.CodeFeature{Name: "alpha", UserFacing: true}},
		},
		Drift: []analyzer.DriftFinding{
			{Feature: "alpha", Issues: []analyzer.DriftIssue{
				{Issue: "x", Priority: analyzer.PriorityLarge, PriorityReason: "y"},
			}},
		},
		Screenshots: analyzer.ScreenshotResult{
			MissingGaps: []analyzer.ScreenshotGap{
				{PageURL: "u", ShouldShow: "s", Priority: analyzer.PriorityLarge, PriorityReason: "r"},
			},
		},
		ScreenshotsRan: true,
	}

	require.NoError(t, WriteReport(dir, in))

	f, r, err := pdfreader.Open(filepath.Join(dir, "report.pdf"))
	require.NoError(t, err)
	defer f.Close()
	require.GreaterOrEqual(t, r.NumPage(), 2, "expected cover + TOC pages")

	tocText, err := r.Page(2).GetPlainText(nil)
	require.NoError(t, err)

	assert.Contains(t, tocText, "Table of Contents", "TOC heading must appear")
	assert.Contains(t, tocText, "Features", "TOC must list Features section")
	assert.Contains(t, tocText, "Gaps", "TOC must list Gaps section")
	assert.Contains(t, tocText, "Screenshots", "TOC must list Screenshots section")
}

func TestTOC_OmitsScreenshotsWhenNotRun(t *testing.T) {
	// Use a neutral project name so the footer (printed on this page) cannot
	// confound the "Screenshots" substring check below.
	dir := t.TempDir()

	in := Inputs{
		ProjectName:    "Project X",
		Mapping:        analyzer.FeatureMap{{Feature: analyzer.CodeFeature{Name: "a", UserFacing: true}}},
		ScreenshotsRan: false,
	}

	require.NoError(t, WriteReport(dir, in))

	f, r, err := pdfreader.Open(filepath.Join(dir, "report.pdf"))
	require.NoError(t, err)
	defer f.Close()

	tocText, err := r.Page(2).GetPlainText(nil)
	require.NoError(t, err)

	assert.Contains(t, tocText, "Features")
	assert.Contains(t, tocText, "Gaps")
	assert.NotContains(t, tocText, "Screenshots", "Screenshots entry must be omitted when ScreenshotsRan=false")
}

func TestAnchorTable_StableIDs(t *testing.T) {
	doc := newDoc()
	a := newAnchorTable(doc)

	id1 := a.Get("features")
	id2 := a.Get("features")
	assert.Equal(t, id1, id2, "Get must return stable link IDs for the same anchor name")

	id3 := a.Get("gaps")
	assert.NotEqual(t, id1, id3, "different anchor names must have different link IDs")
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"hello world", "hello-world"},
		{"  Trim  Me  ", "trim-me"},
		{"Auth/OAuth 2.0!", "auth-oauth-2-0"},
		{"already-slug", "already-slug"},
		{"___", ""},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, slugify(tc.in), "slugify(%q)", tc.in)
	}
}

func TestFinalizeTOC_NoOpOnEmptyRows(t *testing.T) {
	doc := newDoc()
	doc.AddPage() // ensure the doc has a current page
	// Should not panic and should leave the document writable.
	finalizeTOC(doc, nil, nil)
	finalizeTOC(doc, []tocRow{}, map[string]int{})
	doc.SetFont("Helvetica", "", fontSizeBody)
	doc.CellFormat(0, 0.25, "still works", "", 1, "L", false, 0, "")
}

func TestFinalizeTOC_SkipsRowWithoutTarget(t *testing.T) {
	doc := newDoc()
	doc.AddPage()
	anchors := newAnchorTable(doc)
	rows := renderTOC(doc, anchors, []tocEntry{
		{Label: "Features", Anchor: "features", Depth: 0},
		{Label: "Gaps", Anchor: "gaps", Depth: 0},
	})

	// Provide a target only for "features"; "gaps" should be silently left
	// as the "..." placeholder rather than crashing or corrupting the doc.
	finalizeTOC(doc, rows, map[string]int{"features": 3})

	path := filepath.Join(t.TempDir(), "ok.pdf")
	require.NoError(t, doc.OutputFileAndClose(path))
}
