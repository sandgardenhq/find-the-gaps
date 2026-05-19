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
	"github.com/sandgardenhq/find-the-gaps/internal/linkcheck"
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
	DeadLinks      linkcheck.Report
}

// WriteReport renders the report PDF into dir as "report.pdf".
func WriteReport(dir string, in Inputs) error {
	doc := newDoc()
	registerFooter(doc, in.ProjectName)
	anchors := newAnchorTable(doc)
	featAnchors := computeFeatureAnchors(in)

	renderCover(doc, in)
	tocRows := renderTOC(doc, anchors, collectTOCEntries(in))
	renderSections(doc, anchors, featAnchors, in)
	finalizeTOC(doc, tocRows, anchors)

	out := filepath.Join(dir, "report.pdf")
	if err := doc.OutputFileAndClose(out); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}
	return nil
}

// renderSections renders every top-level section in the canonical
// order Gaps → Screenshots → Features. Each section starts on a fresh
// page and marks its top-level anchor; the section renderers themselves
// mark per-feature / per-bucket / per-sub-section anchors. The page
// numbers needed by the TOC are read back from the shared anchor table
// after rendering completes.
func renderSections(doc *fpdf.Fpdf, anchors *anchorTable, featAnchors map[string]string, in Inputs) {
	doc.AddPage()
	anchors.Mark("gaps")
	renderGapsWithAnchors(doc, in, anchors, featAnchors)

	if in.ScreenshotsRan {
		doc.AddPage()
		anchors.Mark("screenshots")
		renderScreenshotsWithAnchors(doc, in, anchors, featAnchors)
	}

	if totalDeadLinks(in.DeadLinks) > 0 {
		doc.AddPage()
		anchors.Mark("deadlinks")
		renderDeadLinksWithAnchors(doc, in.DeadLinks, anchors)
	}

	doc.AddPage()
	anchors.Mark("features")
	renderFeatures(doc, in, anchors)
}

// newDoc constructs the fpdf document the renderer writes into. Letter
// size, portrait, inch-based units, Inter as the body face and Poppins
// as the display face (registered via embedded TTFs in fonts.go so
// report.pdf renders the same typography as the Hextra-rendered site).
//
// A header function paints the warm-paper background on every new page
// so the document body matches `--ftg-paper-warm` from custom.css. The
// paint runs before any content draws, so cards and pills sit on the
// tinted background just like they do on the site.
func newDoc() *fpdf.Fpdf {
	doc := fpdf.New("P", "in", "Letter", "")
	doc.SetMargins(marginLeft, marginTop, marginRight)
	doc.SetAutoPageBreak(true, marginBottom)
	registerFonts(doc)

	doc.SetHeaderFunc(func() {
		w, h := doc.GetPageSize()
		setFillColor(doc, colorPaperWarm)
		doc.Rect(0, 0, w, h, "F")
		// SetHeaderFunc would otherwise leave the cursor wherever the
		// fill ended; reset to the top margin so the first content
		// draw starts where the page expects.
		doc.SetXY(marginLeft, marginTop)
	})

	return doc
}
