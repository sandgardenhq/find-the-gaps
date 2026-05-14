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

	doc.SetFont(bodyFont, "B", fontSizePill)
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
	setTextColor(doc, colorInk)
	doc.SetXY(x+w, y)
	return w
}

// pillWidth returns the width drawPill would render for the given label,
// in inches. The label is uppercased before measurement so the caller can
// pass either case.
func pillWidth(doc *fpdf.Fpdf, label string) float64 {
	label = strings.ToUpper(label)
	// Save and restore font state so we don't perturb the caller.
	doc.SetFont(bodyFont, "B", fontSizePill)
	return doc.GetStringWidth(label) + 2*pillPadX
}

// pointsToInches converts a points value (used for fpdf stroke widths) to
// inches (the document's unit). 72 points per inch.
func pointsToInches(pts float64) float64 {
	return pts / 72.0
}

// Badge geometry. Smaller than priority pills; the site uses these for
// the metadata row on each feature card.
const (
	badgePadX    = 0.10
	badgeHeight  = 0.22
	badgeRadius  = 0.11 // 9999px on the site → full circle ends; approximate as half-height.
	badgeBorderW = 0.4
)

// drawBadge renders a small tinted metadata pill at the current (x, y)
// cursor and advances the cursor past the right edge. Unlike drawPill,
// the label is rendered as supplied (no upper-casing), the font weight
// is regular (mirroring `.ftg-badge` font-weight: 500), and padding is
// tighter so badges sit compactly on a single row.
func drawBadge(doc *fpdf.Fpdf, label string, fg, bg, border int) float64 {
	doc.SetFont(bodyFont, "", fontSizeBadge)
	w := doc.GetStringWidth(label) + 2*badgePadX
	x, y := doc.GetX(), doc.GetY()

	oldLineW := doc.GetLineWidth()
	defer doc.SetLineWidth(oldLineW)

	setFillColor(doc, bg)
	setDrawColor(doc, border)
	doc.SetLineWidth(pointsToInches(badgeBorderW))
	doc.RoundedRect(x, y, w, badgeHeight, badgeRadius, "1234", "FD")

	setTextColor(doc, fg)
	doc.SetXY(x, y)
	doc.CellFormat(w, badgeHeight, label, "", 0, "C", false, 0, "")

	setTextColor(doc, colorInk)
	doc.SetXY(x+w, y)
	return w
}

// measureFeatureCard returns the height a feature card needs to hold the
// header line, optional italic description, a one-row badge strip, and
// the optional Files / Symbols / Documented-on rows. Sized so the card
// shell can be drawn before any content fills it.
func measureFeatureCard(doc *fpdf.Fpdf, name, description, files, symbols, docPages string) float64 {
	w := cardContentWidth(doc)

	doc.SetFont(titleFont, "B", fontSizeH2)
	nameLines := countWrappedLines(doc, name, w)

	doc.SetFont(bodyFont, "I", fontSizeBody)
	descLines := countWrappedLines(doc, description, w)

	doc.SetFont(bodyFont, "", fontSizeMeta)
	filesLines := countWrappedLines(doc, files, w)
	symbolsLines := countWrappedLines(doc, symbols, w)
	docLines := countWrappedLines(doc, docPages, w)

	headLineH := 0.32
	descLineH := 0.22
	badgeRowH := badgeHeight + 0.10
	metaLineH := 0.20

	h := cardPadY
	h += headLineH * float64(nameLines)
	h += descLineH * float64(descLines)
	h += badgeRowH
	h += metaLineH * float64(filesLines+symbolsLines+docLines)
	h += cardPadY
	return h
}

// drawCard renders the shell of a finding/feature card at (x, y) with the
// given total width + height. The card has a white fill, a thin neutral
// border, an 8-point-radius corner, and a coloured 4-point-wide left
// stripe in stripeHex. Cursor is left unchanged; callers position content
// inside the card themselves.
func drawCard(doc *fpdf.Fpdf, x, y, w, h float64, stripeHex int) {
	oldLineW := doc.GetLineWidth()
	defer doc.SetLineWidth(oldLineW)

	// Outer rounded rect.
	setFillColor(doc, colorSurface)
	setDrawColor(doc, colorRule)
	doc.SetLineWidth(pointsToInches(cardBorderW))
	doc.RoundedRect(x, y, w, h, cardRadius, "1234", "FD")

	// Coloured left stripe. Drawn as a filled rect that hugs the inside
	// of the rounded rect's left edge, slightly inset so the corner
	// curves still show.
	setFillColor(doc, stripeHex)
	setDrawColor(doc, stripeHex)
	doc.Rect(x, y, cardStripeW, h, "FD")
}

// cardContentX / cardContentWidth report the inner bounds (text safe
// zone) for a card whose outer rectangle starts at the left margin and
// fills the page width.
func cardContentX() float64 {
	return marginLeft + cardStripeW + cardPadX
}

func cardContentWidth(doc *fpdf.Fpdf) float64 {
	return pageWidth(doc) - cardStripeW - 2*cardPadX
}

// measureDriftCard returns the height in inches that a drift card
// containing the given feature label / issue / reason / page reference
// will occupy. Used by renderDriftFinding to size the card shell before
// drawing it, so the rounded-rect can be drawn before any text fills it.
//
// Heights are derived from the same fpdf state the renderer will use:
// body font for the feature header line, body font for the wrapped
// issue, italic meta font for the wrapped reason+page line.
func measureDriftCard(doc *fpdf.Fpdf, feature, issue, reason, page string) float64 {
	w := cardContentWidth(doc)

	doc.SetFont(bodyFont, "B", fontSizeBody)
	headLines := countWrappedLines(doc, feature+"  -", w)

	doc.SetFont(bodyFont, "", fontSizeBody)
	issueLines := countWrappedLines(doc, issue, w)

	secondary := reason
	if page != "" {
		secondary = strings.TrimSpace(reason + "   (" + page + ")")
	}
	doc.SetFont(bodyFont, "I", fontSizeMeta)
	reasonLines := countWrappedLines(doc, secondary, w)

	bodyLineH := 0.22
	metaLineH := 0.20
	return cardPadY + bodyLineH*float64(headLines+issueLines) + metaLineH*float64(reasonLines) + cardPadY
}

// countWrappedLines counts how many lines fpdf's SplitText would produce
// for s at width w under the doc's current font. Returns at least 1 even
// for empty strings so callers reserve a baseline of vertical room.
func countWrappedLines(doc *fpdf.Fpdf, s string, w float64) int {
	if strings.TrimSpace(s) == "" {
		return 0
	}
	n := len(doc.SplitText(s, w))
	if n < 1 {
		return 1
	}
	return n
}

// measureScreenshotCard returns the height the screenshot card needs to
// hold a page-URL header line plus a slice of "Label: value" lines, all
// wrapped to the card's content width. Used by renderMissingGap and
// renderImageIssue to pre-size the card shell.
func measureScreenshotCard(doc *fpdf.Fpdf, pageURL string, lines []string) float64 {
	w := cardContentWidth(doc)

	doc.SetFont(bodyFont, "B", fontSizeBody)
	headLines := countWrappedLines(doc, pageURL, w)

	doc.SetFont(bodyFont, "", fontSizeMeta)
	bodyLines := 0
	for _, ln := range lines {
		bodyLines += countWrappedLines(doc, ln, w)
	}

	headLineH := 0.24
	bodyLineH := 0.20
	return cardPadY + headLineH*float64(headLines) + bodyLineH*float64(bodyLines) + cardPadY
}
