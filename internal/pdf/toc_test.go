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

func TestTOC_HasSubEntries(t *testing.T) {
	dir := t.TempDir()

	in := Inputs{
		ProjectName: "Sub TOC",
		Mapping: analyzer.FeatureMap{
			{Feature: analyzer.CodeFeature{Name: "alpha", UserFacing: true}},
			{Feature: analyzer.CodeFeature{Name: "beta", UserFacing: true}},
		},
		DocsMap: analyzer.DocsFeatureMap{
			{Feature: "alpha", Pages: []string{"https://docs.example.com/a"}},
		},
		Drift: []analyzer.DriftFinding{
			{Feature: "alpha", Issues: []analyzer.DriftIssue{
				{Issue: "large issue", Priority: analyzer.PriorityLarge, PriorityReason: "r"},
			}},
			{Feature: "beta", Issues: []analyzer.DriftIssue{
				{Issue: "medium issue", Priority: analyzer.PriorityMedium, PriorityReason: "r"},
			}},
		},
		Screenshots: analyzer.ScreenshotResult{
			MissingGaps: []analyzer.ScreenshotGap{
				{PageURL: "https://docs.example.com/a", ShouldShow: "x", Priority: analyzer.PrioritySmall, PriorityReason: "r"},
			},
		},
		ScreenshotsRan: true,
	}

	require.NoError(t, WriteReport(dir, in))

	f, r, err := pdfreader.Open(filepath.Join(dir, "report.pdf"))
	require.NoError(t, err)
	defer f.Close()

	// TOC is page 2.
	tocText, err := r.Page(2).GetPlainText(nil)
	require.NoError(t, err)

	// Features section with two feature rows.
	assert.Contains(t, tocText, "Features")
	assert.Contains(t, tocText, "alpha")
	assert.Contains(t, tocText, "beta")

	// Gaps section with Large + Medium rows but NOT Small (the bucket is empty).
	assert.Contains(t, tocText, "Gaps")
	assert.Contains(t, tocText, "Large")
	assert.Contains(t, tocText, "Medium")
	// Small priority bucket of drift is empty; the screenshot Missing bucket
	// IS Small, so "Small" SHOULD appear, but the drift-Small TOC row
	// should not. We don't have a clean way to distinguish via text alone,
	// so we instead verify the Screenshots side has the right entries.

	// Screenshots section with Missing Screenshots row + Small sub-bucket;
	// no Image Issues, no Possibly Covered.
	assert.Contains(t, tocText, "Screenshots")
	assert.Contains(t, tocText, "Missing Screenshots")
	assert.Contains(t, tocText, "Small")
	assert.NotContains(t, tocText, "Image Issues", "Image Issues TOC entry must be omitted when ImageIssues empty")
	assert.NotContains(t, tocText, "Possibly Covered", "Possibly Covered TOC entry must be omitted when PossiblyCovered empty")
}

func TestCollectTOCEntries_DepthsMatchStructure(t *testing.T) {
	in := Inputs{
		Mapping: analyzer.FeatureMap{
			{Feature: analyzer.CodeFeature{Name: "alpha", UserFacing: true}},
			{Feature: analyzer.CodeFeature{Name: "beta", UserFacing: true}},
		},
		Drift: []analyzer.DriftFinding{
			{Feature: "alpha", Issues: []analyzer.DriftIssue{
				{Issue: "x", Priority: analyzer.PriorityLarge, PriorityReason: "r"},
			}},
		},
		Screenshots: analyzer.ScreenshotResult{
			MissingGaps: []analyzer.ScreenshotGap{
				{PageURL: "u", ShouldShow: "x", Priority: analyzer.PrioritySmall, PriorityReason: "r"},
			},
		},
		ScreenshotsRan: true,
	}

	entries := collectTOCEntries(in)

	// Build expected sequence in the canonical Gaps -> Screenshots ->
	// Features order: Gaps(0), Large(1), Screenshots(0), Missing
	// Screenshots(1), Small(2), Features(0), alpha(1), beta(1).
	type want struct {
		label string
		depth int
	}
	expected := []want{
		{"Gaps", 0},
		{"Large", 1},
		{"Screenshots", 0},
		{"Missing Screenshots", 1},
		{"Small", 2},
		{"Features", 0},
		{"alpha", 1},
		{"beta", 1},
	}
	require.Equal(t, len(expected), len(entries), "got %d entries, want %d: %#v", len(entries), len(expected), entries)
	for i, e := range expected {
		assert.Equal(t, e.label, entries[i].Label, "entries[%d].Label", i)
		assert.Equal(t, e.depth, entries[i].Depth, "entries[%d].Depth (Label=%s)", i, entries[i].Label)
	}
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
	anchors := newAnchorTable(doc)
	// Should not panic and should leave the document writable.
	finalizeTOC(doc, nil, anchors)
	finalizeTOC(doc, []tocRow{}, anchors)
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

	// Mark only "features"; "gaps" stays unmarked and finalizeTOC must
	// silently leave its "..." placeholder rather than crash.
	doc.AddPage()
	anchors.Mark("features")
	finalizeTOC(doc, rows, anchors)

	path := filepath.Join(t.TempDir(), "ok.pdf")
	require.NoError(t, doc.OutputFileAndClose(path))
}
