package pdf

import (
	"fmt"

	"github.com/go-pdf/fpdf"
)

// tocEntry is one row in the table of contents. Depth zero is a top-level
// section heading; positive depth values render indented (sub-entries land
// in Task 8). Anchor is the name registered in anchorTable; LinkID is
// resolved from the same table when the row is emitted.
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

// collectTopLevelTOC returns the top-level entries that always appear in
// the TOC, conditional on the run's contents. Screenshots is omitted when
// the screenshot pass did not run; empty buckets are still emitted as
// top-level entries (the sub-bucket suppression lands in Task 8 once the
// renderers know their own emptiness).
func collectTopLevelTOC(in Inputs) []tocEntry {
	entries := []tocEntry{
		{Label: "Features", Anchor: "features", Depth: 0},
		{Label: "Gaps", Anchor: "gaps", Depth: 0},
	}
	if in.ScreenshotsRan {
		entries = append(entries, tocEntry{Label: "Screenshots", Anchor: "screenshots", Depth: 0})
	}
	return entries
}

// renderTOC emits the TOC page (or pages) and returns a slice of row
// records that downstream renderers can use, via finalizeTOC, to stamp
// the resolved page numbers. Anchor link IDs are allocated up front so
// section renderers can reference them before they exist on the page.
func renderTOC(doc *fpdf.Fpdf, anchors *anchorTable, entries []tocEntry) []tocRow {
	doc.AddPage()
	tocPage := doc.PageNo()

	doc.SetTextColor(colorBodyR, colorBodyG, colorBodyB)
	doc.SetFont("Helvetica", "B", fontSizeH1)
	doc.CellFormat(0, 0.5, "Table of Contents", "", 1, "L", false, 0, "")
	doc.Ln(0.15)

	rows := make([]tocRow, 0, len(entries))
	for _, e := range entries {
		// Allocate the link ID up front; section renderers will Mark it later.
		linkID := anchors.Get(e.Anchor)

		// Indent by depth.
		indent := float64(e.Depth) * 0.2

		// Label column.
		doc.SetFont("Helvetica", "", fontSizeBody)
		doc.SetX(marginLeft + indent)
		// 5.5" wide label cell leaves room for a right-aligned page-number column.
		doc.CellFormat(5.5, 0.3, e.Label, "", 0, "L", false, linkID, "")

		// Capture the current y so finalizeTOC can stamp the page number here.
		rowY := doc.GetY()
		// Placeholder page-number column ("...").
		doc.CellFormat(0, 0.3, "...", "", 1, "R", false, linkID, "")

		rows = append(rows, tocRow{entry: e, page: tocPage, pageY: rowY})
	}

	return rows
}

// finalizeTOC returns to the TOC page and overwrites the placeholder page
// number column for each row with the resolved target page. Called after
// every section has rendered so anchors.Mark has been issued for each
// referenced anchor and target pages are known.
func finalizeTOC(doc *fpdf.Fpdf, rows []tocRow, targets map[string]int) {
	if len(rows) == 0 {
		return
	}
	// Remember where we are in the document so we can restore.
	curPage := doc.PageNo()
	curX, curY := doc.GetX(), doc.GetY()
	defer func() {
		doc.SetPage(curPage)
		doc.SetXY(curX, curY)
	}()

	for _, row := range rows {
		page, ok := targets[row.entry.Anchor]
		if !ok {
			continue
		}
		doc.SetPage(row.page)
		doc.SetXY(marginLeft, row.pageY)
		doc.SetFont("Helvetica", "", fontSizeBody)

		// Erase the "..." placeholder by overwriting the same right-aligned
		// column with the new number. fpdf paints opaque cells so this
		// effectively clobbers the placeholder.
		doc.SetX(marginLeft + 5.5 + float64(row.entry.Depth)*0.2)
		doc.CellFormat(0, 0.3, fmt.Sprintf("%d", page), "", 1, "R", false, 0, "")
	}
}
