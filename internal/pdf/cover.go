package pdf

import (
	"fmt"

	"github.com/go-pdf/fpdf"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// Cover layout constants. All values are inches.
const (
	coverTitleY = 1.4  // "Find the Gaps" baseline below the top margin
	coverMetaY  = 2.7  // Repo / Docs / Generated block start
	statCardW   = 1.85 // each stat card width
	statCardH   = 1.30 // each stat card height
	statCardGap = 0.20 // gap between stat cards
	statCardsY  = 4.2  // row baseline for the three stat cards
)

// renderCover writes the cover page: centered title block + project
// name, a left-aligned run-metadata block, and a centered row of stat
// cards. Card order matches the canonical Gaps → Screenshots → Features
// ordering used throughout the report (TOC entries, body section
// order, summary lines). Screenshots is omitted when the screenshot
// pass did not run.
func renderCover(doc *fpdf.Fpdf, in Inputs) {
	doc.AddPage()

	pageW, _ := doc.GetPageSize()

	// Title block (centered, Poppins).
	doc.SetY(coverTitleY)
	doc.SetFont(titleFont, "B", fontSizeTitle)
	setTextColor(doc, colorInk)
	doc.CellFormat(0, 0.5, "Find the Gaps", "", 1, "C", false, 0, "")

	if in.ProjectName != "" {
		doc.SetFont(titleFont, "", fontSizeH1)
		setTextColor(doc, colorInkMute)
		doc.CellFormat(0, 0.4, sanitize(in.ProjectName), "", 1, "C", false, 0, "")
	}

	// Metadata block (left-aligned).
	doc.SetY(coverMetaY)
	doc.SetFont(bodyFont, "", fontSizeMeta)
	setTextColor(doc, colorInk)
	if in.RepoURL != "" {
		doc.SetX(marginLeft)
		doc.CellFormat(0, 0.25, "Repo:  "+sanitize(in.RepoURL), "", 1, "L", false, 0, "")
	}
	if in.DocsURL != "" {
		doc.SetX(marginLeft)
		doc.CellFormat(0, 0.25, "Docs:  "+sanitize(in.DocsURL), "", 1, "L", false, 0, "")
	}
	if !in.GeneratedAt.IsZero() {
		doc.SetX(marginLeft)
		ts := in.GeneratedAt.UTC().Format("2006-01-02 15:04 UTC")
		doc.CellFormat(0, 0.25, "Generated: "+ts, "", 1, "L", false, 0, "")
	}

	// Stat cards row.
	renderStatRow(doc, in, pageW)
}

// renderStatRow draws the row of stat cards (Gaps → Screenshots →
// Features) centered horizontally at statCardsY. The Screenshots card
// is omitted when the screenshot pass did not run.
func renderStatRow(doc *fpdf.Fpdf, in Inputs, pageW float64) {
	stats := buildStats(in)
	n := float64(len(stats))
	rowW := n*statCardW + (n-1)*statCardGap
	startX := (pageW - rowW) / 2

	for i, s := range stats {
		x := startX + float64(i)*(statCardW+statCardGap)
		drawStatCard(doc, x, statCardsY, statCardW, statCardH, s)
	}
}

// statCard models one of the cover's at-a-glance counters.
type statCard struct {
	number int
	label  string
	stripe int // packed-hex foreground colour
}

// buildStats produces the slice of stat cards to render on the cover.
// Order is Gaps → Screenshots → Features, the canonical ordering used
// throughout the report. Colour rules mirror the brand block in
// custom.css: Gaps and Screenshots use sev-small (green) when at zero
// (clean run) and sev-large (magenta) when positive; Features uses the
// neutral rule colour.
func buildStats(in Inputs) []statCard {
	cards := []statCard{
		{number: totalDriftIssues(in.Drift), label: "gaps",
			stripe: countStripe(totalDriftIssues(in.Drift))},
	}
	if in.ScreenshotsRan {
		n := totalScreenshotIssues(in.Screenshots)
		cards = append(cards, statCard{
			number: n,
			label:  "screenshot issues",
			stripe: countStripe(n),
		})
	}
	cards = append(cards, statCard{
		number: len(in.Mapping),
		label:  "features",
		stripe: colorRule,
	})
	return cards
}

// countStripe maps a count to a stripe colour: 0 reads as a clean run
// (sev-small green); any positive count reads as work to do (sev-large
// magenta).
func countStripe(n int) int {
	if n == 0 {
		return colorSevSmall
	}
	return colorSevLarge
}

// drawStatCard renders one stat-card box at (x, y). Big number on top,
// muted label below. Surface fill, neutral border, 4pt severity-coloured
// left stripe.
func drawStatCard(doc *fpdf.Fpdf, x, y, w, h float64, s statCard) {
	drawCard(doc, x, y, w, h, s.stripe)

	// Number centered in the upper portion of the card.
	doc.SetXY(x, y+0.20)
	doc.SetFont(titleFont, "B", fontSizeStat)
	setTextColor(doc, s.stripe)
	doc.CellFormat(w, 0.6, fmt.Sprintf("%d", s.number), "", 1, "C", false, 0, "")

	// Label below.
	doc.SetXY(x, y+0.85)
	doc.SetFont(bodyFont, "", fontSizeMeta)
	setTextColor(doc, colorInkMute)
	doc.CellFormat(w, 0.3, s.label, "", 1, "C", false, 0, "")

	setTextColor(doc, colorInk)
}

// summaryLine remains for callers that prefer the inline-summary form
// (e.g. a CLI banner). Kept so future surfaces have one place to pull
// the same counts the cover renders. Order matches the canonical
// Gaps → Screenshots → Features sequence.
func summaryLine(in Inputs) string {
	parts := []string{
		fmt.Sprintf("%d gaps", totalDriftIssues(in.Drift)),
	}
	if in.ScreenshotsRan {
		parts = append(parts, fmt.Sprintf("%d screenshot issues", totalScreenshotIssues(in.Screenshots)))
	}
	parts = append(parts, fmt.Sprintf("%d features", len(in.Mapping)))

	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "  -  "
		}
		out += p
	}
	return out
}

func totalDriftIssues(findings []analyzer.DriftFinding) int {
	n := 0
	for _, f := range findings {
		n += len(f.Issues)
	}
	return n
}

func totalScreenshotIssues(r analyzer.ScreenshotResult) int {
	return len(r.MissingGaps) + len(r.ImageIssues) + len(r.PossiblyCovered)
}
