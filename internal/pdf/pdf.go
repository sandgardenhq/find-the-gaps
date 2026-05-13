// Package pdf renders an analyze run as a single self-contained PDF report
// (report.pdf). It mirrors the data flow of internal/reporter and
// internal/site: the caller passes in-memory analyzer structs; this package
// emits a file under the project directory.
package pdf

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/go-pdf/fpdf"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// Inputs bundles every piece of data required to render the report. Mirrors
// site.Inputs plus run-metadata fields the cover page needs. All fields are
// optional; a zero value yields a minimal placeholder PDF.
type Inputs struct {
	ProjectName    string
	RepoURL        string
	DocsURL        string
	GeneratedAt    time.Time
	Summary        analyzer.ProductSummary
	Mapping        analyzer.FeatureMap
	DocsMap        analyzer.DocsFeatureMap
	Drift          []analyzer.DriftFinding
	Screenshots    analyzer.ScreenshotResult
	ScreenshotsRan bool
}

// WriteReport renders the report PDF into dir as "report.pdf".
func WriteReport(dir string, in Inputs) error {
	doc := newDoc()
	registerFooter(doc, in.ProjectName)
	anchors := newAnchorTable(doc)

	renderCover(doc, in)
	tocRows := renderTOC(doc, anchors, collectTopLevelTOC(in))

	// Render section bodies. For Task 4 each section is a stub that simply
	// starts a new page and marks its anchor; Tasks 5-7 will fill them in
	// with real content.
	targets := renderSections(doc, anchors, in)

	finalizeTOC(doc, tocRows, targets)

	out := filepath.Join(dir, "report.pdf")
	if err := doc.OutputFileAndClose(out); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}
	return nil
}

// renderSections renders every top-level section and returns a map from
// anchor name to the page number the section starts on. The map feeds
// finalizeTOC so the table of contents shows the right page for each
// section. Sections are stubbed until Tasks 5-7 land.
func renderSections(doc *fpdf.Fpdf, anchors *anchorTable, in Inputs) map[string]int {
	targets := map[string]int{}

	doc.AddPage()
	anchors.Mark("features")
	targets["features"] = doc.PageNo()
	renderFeatures(doc, in, anchors)

	doc.AddPage()
	anchors.Mark("gaps")
	targets["gaps"] = doc.PageNo()
	renderGaps(doc, in, anchors)

	if in.ScreenshotsRan {
		doc.AddPage()
		anchors.Mark("screenshots")
		targets["screenshots"] = doc.PageNo()
		renderScreenshots(doc, in, anchors)
	}

	return targets
}

// newDoc constructs the fpdf document the renderer writes into. Letter size,
// portrait, inch-based units, no embedded fonts (core fonts only).
func newDoc() *fpdf.Fpdf {
	doc := fpdf.New("P", "in", "Letter", "")
	doc.SetMargins(marginLeft, marginTop, marginRight)
	doc.SetAutoPageBreak(true, marginBottom)
	return doc
}
