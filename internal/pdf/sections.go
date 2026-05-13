package pdf

import (
	"github.com/go-pdf/fpdf"
)

// renderFeatures emits the Features section. Stubbed in Task 4; filled in
// with real content in Task 5.
func renderFeatures(doc *fpdf.Fpdf, in Inputs, anchors *anchorTable) {
	sectionHeading(doc, "Features")
}

// renderGaps emits the Gaps section, priority-bucketed and cross-linked to
// features. Stubbed in Task 4; filled in with real content in Task 6.
func renderGaps(doc *fpdf.Fpdf, in Inputs, anchors *anchorTable) {
	sectionHeading(doc, "Gaps")
}

// renderScreenshots emits the Screenshots section (missing, image issues,
// possibly covered). Caller is responsible for gating on
// in.ScreenshotsRan; this function assumes the section should render.
// Stubbed in Task 4; filled in with real content in Task 7.
func renderScreenshots(doc *fpdf.Fpdf, in Inputs, anchors *anchorTable) {
	sectionHeading(doc, "Screenshots")
}

// sectionHeading writes a top-level section title in the brand accent
// color. Shared by every renderer so the visual treatment stays
// consistent.
func sectionHeading(doc *fpdf.Fpdf, title string) {
	doc.SetFont("Helvetica", "B", fontSizeH1)
	doc.SetTextColor(colorBrandR, colorBrandG, colorBrandB)
	doc.CellFormat(0, 0.5, title, "", 1, "L", false, 0, "")
	doc.SetTextColor(colorBodyR, colorBodyG, colorBodyB)
	doc.Ln(0.1)
}
