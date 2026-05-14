package pdf

import (
	"fmt"

	"github.com/go-pdf/fpdf"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// tocEntry is one row in the table of contents. Depth 0 is a top-level
// section heading (Features / Gaps / Screenshots); depth 1 is a
// sub-section (a feature, a priority bucket, or a screenshots sub-section
// like "Missing Screenshots"); depth 2 is a priority bucket *inside* a
// screenshots sub-section. Anchor is the name registered in anchorTable.
type tocEntry struct {
	Label  string
	Anchor string
	Depth  int
}

// tocRow records where on the TOC page a row's page-number column lives
// so finalizeTOC can come back and stamp the resolved page number after
// section rendering.
type tocRow struct {
	entry  tocEntry
	page   int     // TOC page (so we can SetPage back to it)
	pageY  float64 // y position of the row baseline
	target int     // resolved page number of the anchor target (filled by finalizeTOC)
}

// collectTOCEntries returns every row the TOC must render, in document
// order. Top-level sections are depth 0; per-feature, per-bucket, and
// per-sub-section rows are depth 1; the Large/Medium/Small buckets *inside*
// a Missing Screenshots / Image Issues / Possibly Covered sub-section are
// depth 2. Empty buckets and empty sub-sections are pruned so the TOC
// matches the rendered body exactly.
func collectTOCEntries(in Inputs) []tocEntry {
	var entries []tocEntry

	// Features
	entries = append(entries, tocEntry{Label: "Features", Anchor: "features", Depth: 0})
	featAnchors := computeFeatureAnchors(in)
	for _, entry := range in.Mapping {
		entries = append(entries, tocEntry{
			Label:  entry.Feature.Name,
			Anchor: featAnchors[entry.Feature.Name],
			Depth:  1,
		})
	}

	// Gaps
	entries = append(entries, tocEntry{Label: "Gaps", Anchor: "gaps", Depth: 0})
	driftBuckets := bucketDrift(in.Drift)
	for _, p := range priorityOrder() {
		if len(driftBuckets[p]) == 0 {
			continue
		}
		entries = append(entries, tocEntry{
			Label:  priorityLabel(p),
			Anchor: gapsBucketAnchor(p),
			Depth:  1,
		})
	}

	// Screenshots
	if in.ScreenshotsRan {
		entries = append(entries, tocEntry{Label: "Screenshots", Anchor: "screenshots", Depth: 0})
		entries = append(entries, screenshotSubEntries("Missing Screenshots", "missing", in.Screenshots.MissingGaps)...)
		entries = append(entries, imageIssueSubEntries("Image Issues", "image-issues", in.Screenshots.ImageIssues)...)
		entries = append(entries, screenshotSubEntries("Possibly Covered", "possibly-covered", in.Screenshots.PossiblyCovered)...)
	}

	return entries
}

// screenshotSubEntries returns the TOC entries for one ScreenshotGap
// sub-section (Missing Screenshots or Possibly Covered): the sub-section
// row plus a row per non-empty priority bucket. Returns nil when gaps is
// empty so the entire sub-section is omitted.
func screenshotSubEntries(label, slug string, gaps []analyzer.ScreenshotGap) []tocEntry {
	if len(gaps) == 0 {
		return nil
	}
	entries := []tocEntry{
		{Label: label, Anchor: "screenshots-" + slug, Depth: 1},
	}
	counts := map[analyzer.Priority]int{}
	for _, g := range gaps {
		if isKnownPriority(g.Priority) {
			counts[g.Priority]++
		}
	}
	for _, p := range priorityOrder() {
		if counts[p] == 0 {
			continue
		}
		entries = append(entries, tocEntry{
			Label:  priorityLabel(p),
			Anchor: "screenshots-" + slug + "-" + string(p),
			Depth:  2,
		})
	}
	return entries
}

// imageIssueSubEntries is the ImageIssue counterpart to
// screenshotSubEntries. ImageIssue has a different shape so the bucketing
// counts come from a different slice type.
func imageIssueSubEntries(label, slug string, issues []analyzer.ImageIssue) []tocEntry {
	if len(issues) == 0 {
		return nil
	}
	entries := []tocEntry{
		{Label: label, Anchor: "screenshots-" + slug, Depth: 1},
	}
	counts := map[analyzer.Priority]int{}
	for _, i := range issues {
		if isKnownPriority(i.Priority) {
			counts[i.Priority]++
		}
	}
	for _, p := range priorityOrder() {
		if counts[p] == 0 {
			continue
		}
		entries = append(entries, tocEntry{
			Label:  priorityLabel(p),
			Anchor: "screenshots-" + slug + "-" + string(p),
			Depth:  2,
		})
	}
	return entries
}

// gapsBucketAnchor returns the anchor name for one priority bucket inside
// the top-level Gaps section.
func gapsBucketAnchor(p analyzer.Priority) string {
	return "gaps-" + string(p)
}

// screenshotsBucketAnchor returns the anchor name for one priority bucket
// inside a screenshots sub-section (Missing / Image Issues / Possibly
// Covered).
func screenshotsBucketAnchor(slug string, p analyzer.Priority) string {
	return "screenshots-" + slug + "-" + string(p)
}

// renderTOC emits the TOC page (or pages) and returns a slice of row
// records that finalizeTOC will use to stamp resolved page numbers after
// section rendering. Anchor link IDs are allocated up front so section
// renderers can reference them before they exist on the page. The TOC may
// spill across multiple pages — autoPageBreak handles overflow.
func renderTOC(doc *fpdf.Fpdf, anchors *anchorTable, entries []tocEntry) []tocRow {
	doc.AddPage()

	setTextColor(doc, colorInk)
	doc.SetFont(titleFont, "B", fontSizeH1)
	doc.CellFormat(0, 0.5, "Table of Contents", "", 1, "L", false, 0, "")
	doc.Ln(0.15)

	rows := make([]tocRow, 0, len(entries))
	for _, e := range entries {
		// Allocate the link ID up front; section renderers will Mark it later.
		linkID := anchors.Get(e.Anchor)

		indent := float64(e.Depth) * 0.25

		// Depth 0 entries render in bold for visual separation.
		fontStyle := ""
		if e.Depth == 0 {
			fontStyle = "B"
		}
		doc.SetFont(bodyFont, fontStyle, fontSizeBody)
		doc.SetX(marginLeft + indent)
		labelWidth := 5.5 - indent
		doc.CellFormat(labelWidth, 0.3, e.Label, "", 0, "L", false, linkID, "")

		// Capture the current page+y of this row so finalizeTOC can come
		// back and stamp the resolved page number after sections render.
		rowPage := doc.PageNo()
		rowY := doc.GetY()
		doc.CellFormat(0, 0.3, "...", "", 1, "R", false, linkID, "")

		rows = append(rows, tocRow{entry: e, page: rowPage, pageY: rowY})
	}

	return rows
}

// finalizeTOC returns to each row's TOC page and overwrites the placeholder
// page number with the resolved target page. anchors.Page resolves each
// row's anchor to the page it was Marked on; rows whose anchor was never
// marked are silently skipped (the "..." placeholder remains).
func finalizeTOC(doc *fpdf.Fpdf, rows []tocRow, anchors *anchorTable) {
	if len(rows) == 0 {
		return
	}
	curPage := doc.PageNo()
	curX, curY := doc.GetX(), doc.GetY()
	defer func() {
		doc.SetPage(curPage)
		doc.SetXY(curX, curY)
	}()

	for _, row := range rows {
		page, ok := anchors.Page(row.entry.Anchor)
		if !ok {
			continue
		}
		doc.SetPage(row.page)
		doc.SetXY(marginLeft+5.5, row.pageY)
		// Paint the page-number column in the body background colour
		// before stamping so the "..." placeholder underneath gets fully
		// erased. Using the paper-warm body background means the patch
		// is invisible against the rest of the page; an earlier version
		// used pure white, which left a visible white rectangle on the
		// tinted body.
		setFillColor(doc, colorPaperWarm)
		doc.CellFormat(0, 0.3, "", "", 0, "L", true, 0, "")
		doc.SetXY(marginLeft+5.5, row.pageY)
		doc.SetFont(bodyFont, "", fontSizeBody)
		setTextColor(doc, colorInk)
		doc.CellFormat(0, 0.3, fmt.Sprintf("%d", page), "", 1, "R", false, 0, "")
	}
}
