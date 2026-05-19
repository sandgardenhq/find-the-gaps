package pdf

import (
	"fmt"

	"github.com/go-pdf/fpdf"

	"github.com/sandgardenhq/find-the-gaps/internal/linkcheck"
)

// renderDeadLinksWithAnchors emits the Dead Links section: three flat
// sub-sections (Broken / Auth Required / Redirected). The caller gates
// rendering on totalDeadLinks > 0; this function assumes the section
// should render. Sub-section anchors are marked so the TOC sub-entries
// resolve. No priority bucketing — dead-link findings render flat.
func renderDeadLinksWithAnchors(doc *fpdf.Fpdf, rep linkcheck.Report, anchors *anchorTable) {
	sectionHeading(doc, "Dead Links")

	if len(rep.Broken) > 0 {
		anchors.Mark("deadlinks-broken")
		subSectionHeading(doc, "Broken")
		for _, f := range rep.Broken {
			renderDeadLinkBlock(doc, f, "")
		}
	}
	if len(rep.Auth) > 0 {
		anchors.Mark("deadlinks-auth")
		subSectionHeading(doc, "Auth Required")
		for _, f := range rep.Auth {
			renderDeadLinkBlock(doc, f, "")
		}
	}
	if len(rep.Redirected) > 0 {
		anchors.Mark("deadlinks-redirected")
		subSectionHeading(doc, "Redirected")
		for _, f := range rep.Redirected {
			renderDeadLinkBlock(doc, f, f.FinalURL)
		}
	}
}

// renderDeadLinkBlock emits one Finding as a plain text block: bold URL
// header, optional reason and redirects-to lines, and a wrapped list of
// referencing pages. Mirrors the markdown layout in links.md.
func renderDeadLinkBlock(doc *fpdf.Fpdf, f linkcheck.Finding, redirectsTo string) {
	innerW := pageWidth(doc)

	doc.Ln(0.08)
	doc.SetFont(bodyFont, "B", fontSizeBody)
	setTextColor(doc, colorInk)
	doc.MultiCell(innerW, 0.24, sanitize(f.URL), "", "L", false)

	doc.SetFont(bodyFont, "", fontSizeMeta)
	setTextColor(doc, colorInkMute)
	if f.Detail != "" {
		doc.MultiCell(innerW, 0.20, "Reason: "+sanitize(f.Detail), "", "L", false)
	}
	if redirectsTo != "" {
		doc.MultiCell(innerW, 0.20, "Redirects to: "+sanitize(redirectsTo), "", "L", false)
	}
	if len(f.Pages) > 0 {
		doc.MultiCell(innerW, 0.20, fmt.Sprintf("Referenced on %d page(s):", len(f.Pages)), "", "L", false)
		for _, p := range f.Pages {
			doc.MultiCell(innerW, 0.18, "  - "+sanitize(p), "", "L", false)
		}
	}
	setTextColor(doc, colorInk)
	doc.Ln(0.04)
}

// totalDeadLinks returns the combined count of findings across all three
// buckets. Zero means the Dead Links section is omitted entirely.
func totalDeadLinks(rep linkcheck.Report) int {
	return len(rep.Broken) + len(rep.Auth) + len(rep.Redirected)
}
