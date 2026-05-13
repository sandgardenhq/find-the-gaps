package pdf

import (
	"fmt"
	"strings"

	"github.com/go-pdf/fpdf"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// renderFeatures emits the Features section: one block per feature with
// description, layer, user-facing flag, documentation status, files,
// symbols, and documented-on pages. Mirrors the field set in
// reporter.WriteMapping so the PDF and mapping.md stay in sync.
// Anchors of the form feat-<slug> are registered so cross-references
// from Gaps and Screenshots can link back into this section.
func renderFeatures(doc *fpdf.Fpdf, in Inputs, anchors *anchorTable) {
	sectionHeading(doc, "Features")

	pagesByFeature := make(map[string][]string, len(in.DocsMap))
	for _, e := range in.DocsMap {
		pagesByFeature[e.Feature] = e.Pages
	}

	// Track slug usage so collisions get a -2, -3, ... suffix.
	used := make(map[string]int, len(in.Mapping))

	for _, entry := range in.Mapping {
		anchor := featureAnchor(entry.Feature.Name, used)
		anchors.Mark(anchor)
		renderFeatureBlock(doc, entry, pagesByFeature[entry.Feature.Name])
	}
}

// featureAnchor returns a unique anchor name for the feature, suffixing
// "-2", "-3", ... when an earlier feature in this run already claimed the
// base slug.
func featureAnchor(name string, used map[string]int) string {
	base := slugify(name)
	used[base]++
	if used[base] == 1 {
		return "feat-" + base
	}
	return fmt.Sprintf("feat-%s-%d", base, used[base])
}

// renderFeatureBlock emits one feature: heading, description, key/value
// fields, files, symbols, and documented-on pages.
func renderFeatureBlock(doc *fpdf.Fpdf, entry analyzer.FeatureEntry, docPages []string) {
	doc.Ln(0.05)

	// Feature name as a sub-heading.
	doc.SetFont("Helvetica", "B", fontSizeH2)
	doc.SetTextColor(colorBodyR, colorBodyG, colorBodyB)
	doc.CellFormat(0, 0.32, entry.Feature.Name, "", 1, "L", false, 0, "")

	if entry.Feature.Description != "" {
		doc.SetFont("Helvetica", "I", fontSizeBody)
		doc.SetTextColor(colorMutedR, colorMutedG, colorMutedB)
		width := pageWidth(doc) - 0.4
		doc.SetX(marginLeft + 0.2)
		doc.MultiCell(width, 0.22, entry.Feature.Description, "", "L", false)
	}

	doc.SetFont("Helvetica", "", fontSizeBody)
	doc.SetTextColor(colorBodyR, colorBodyG, colorBodyB)

	userFacing := "no"
	if entry.Feature.UserFacing {
		userFacing = "yes"
	}
	docStatus := "undocumented"
	if len(docPages) > 0 {
		docStatus = "documented"
	}

	if entry.Feature.Layer != "" {
		labelValue(doc, "Layer", entry.Feature.Layer)
	}
	labelValue(doc, "User-facing", userFacing)
	labelValue(doc, "Documentation status", docStatus)
	if len(entry.Files) > 0 {
		labelValue(doc, "Implemented in", strings.Join(entry.Files, ", "))
	}
	if len(entry.Symbols) > 0 {
		labelValue(doc, "Symbols", strings.Join(entry.Symbols, ", "))
	}
	if len(docPages) > 0 {
		labelValue(doc, "Documented on", strings.Join(docPages, ", "))
	}
	doc.Ln(0.15)
}

// labelValue prints a single "Label: Value" row. Rendered as one cell so
// the label and value share a single text run — text extractors keep them
// on one line, which lets parse-back tests assert on the rendered string
// directly. Label weight is conveyed by the colon-and-space prefix rather
// than a font swap (a font swap mid-row creates two text objects, which
// the extractor then splits across lines).
func labelValue(doc *fpdf.Fpdf, label, value string) {
	doc.SetX(marginLeft)
	doc.SetFont("Helvetica", "", fontSizeBody)
	doc.CellFormat(0, 0.22, label+": "+value, "", 1, "L", false, 0, "")
}

// pageWidth returns the writable width of the current page, in inches,
// based on the configured margins.
func pageWidth(doc *fpdf.Fpdf) float64 {
	w, _ := doc.GetPageSize()
	return w - marginLeft - marginRight
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
