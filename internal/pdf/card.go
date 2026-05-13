package pdf

import (
	"strings"

	"github.com/go-pdf/fpdf"
)

// Geometry constants for the card / pill primitives. Values chosen to
// match the visual rhythm of the site's .ftg-priority and .ftg-stale
// components (see custom.css).
const (
	pillPadX    = 0.16 // horizontal padding inside a pill
	pillHeight  = 0.28 // pill outer height
	pillRadius  = 0.06 // pill corner radius
	pillBorderW = 0.6  // pill border stroke width in points

	cardPadX     = 0.18 // body padding to the right of the left stripe
	cardPadY     = 0.18 // top/bottom padding inside a card
	cardRadius   = 0.08 // card corner radius
	cardStripeW  = 0.06 // colored left stripe width
	cardBorderW  = 0.4  // card border stroke width in points
)

// drawPill renders one tinted pill at the current (x, y) cursor and
// advances the cursor past the right edge of the pill. The label is
// uppercased (matching custom.css `text-transform: uppercase`). Returns
// the width drawn so callers can compose multiple pills on one line.
func drawPill(doc *fpdf.Fpdf, label string, fg, bg, border int) float64 {
	label = strings.ToUpper(label)

	doc.SetFont("Helvetica", "B", fontSizePill)
	w := pillWidth(doc, label)
	x, y := doc.GetX(), doc.GetY()

	// Save state we will reset before returning.
	oldLineW := doc.GetLineWidth()
	defer doc.SetLineWidth(oldLineW)

	setFillColor(doc, bg)
	setDrawColor(doc, border)
	doc.SetLineWidth(pointsToInches(pillBorderW))
	doc.RoundedRect(x, y, w, pillHeight, pillRadius, "1234", "FD")

	setTextColor(doc, fg)
	doc.SetXY(x, y)
	doc.CellFormat(w, pillHeight, label, "", 0, "C", false, 0, "")

	// Restore body text colour so subsequent calls don't inherit the pill fg.
	setTextColor(doc, colorBodyFg)
	doc.SetXY(x+w, y)
	return w
}

// pillWidth returns the width drawPill would render for the given label,
// in inches. The label is uppercased before measurement so the caller can
// pass either case.
func pillWidth(doc *fpdf.Fpdf, label string) float64 {
	label = strings.ToUpper(label)
	// Save and restore font state so we don't perturb the caller.
	doc.SetFont("Helvetica", "B", fontSizePill)
	return doc.GetStringWidth(label) + 2*pillPadX
}

// pointsToInches converts a points value (used for fpdf stroke widths) to
// inches (the document's unit). 72 points per inch.
func pointsToInches(pts float64) float64 {
	return pts / 72.0
}
