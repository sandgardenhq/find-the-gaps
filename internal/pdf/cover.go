package pdf

import (
	"fmt"

	"github.com/go-pdf/fpdf"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// Cover layout constants. All values are inches.
const (
	coverTitleY    = 1.4 // "Find the Gaps" baseline below the top margin
	coverMetaY     = 2.7 // Repo / Docs / Generated block start
	coverSummaryY  = 3.7 // first category-section heading start
	coverItemIndent = 0.15 // bullet-line indent under each section heading
)

// renderCover writes the cover page: centered title block + project
// name, a left-aligned run-metadata block, then category sections
// (Features / Gaps / Screenshot Issues) each with their count in the
// heading and a short description of every finding underneath. The
// previous stat-card row is gone — the user wanted concrete preview
// of what the report contains, not a numeric dashboard.
//
// Zero-count categories are omitted entirely so the cover focuses on
// what was actually found.
func renderCover(doc *fpdf.Fpdf, in Inputs) {
	doc.AddPage()

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

	// Category summaries.
	doc.SetY(coverSummaryY)
	renderCoverSection(doc, "Features", featureSummary(in))
	renderCoverSection(doc, "Gaps", gapsSummary(in))
	if in.ScreenshotsRan {
		renderCoverSection(doc, "Screenshot Issues", screenshotsSummary(in))
	}
}

// renderCoverSection emits one category block on the cover: a Poppins
// heading "Title (count)" in magenta, then each item on its own line in
// body text. A category with zero items is omitted entirely (no
// heading, no whitespace) so the cover stays focused on findings the
// reader actually has.
func renderCoverSection(doc *fpdf.Fpdf, title string, items []string) {
	if len(items) == 0 {
		return
	}
	doc.SetX(marginLeft)
	doc.SetFont(titleFont, "B", fontSizeH2)
	setTextColor(doc, colorMagenta)
	doc.CellFormat(0, 0.30, fmt.Sprintf("%s (%d)", title, len(items)), "", 1, "L", false, 0, "")

	doc.SetFont(bodyFont, "", fontSizeBody)
	setTextColor(doc, colorInk)
	width := pageWidth(doc) - coverItemIndent
	for _, item := range items {
		doc.SetX(marginLeft + coverItemIndent)
		doc.MultiCell(width, 0.20, item, "", "L", false)
	}
	doc.Ln(0.12)
}

// featureSummary returns one entry per feature: the feature name. The
// reader gets a quick "what does this product do" rundown right on the
// cover without paging to the Features section.
func featureSummary(in Inputs) []string {
	out := make([]string, 0, len(in.Mapping))
	for _, e := range in.Mapping {
		out = append(out, sanitize(e.Feature.Name))
	}
	return out
}

// gapsSummary returns one entry per drift issue: "feature - issue text".
// The owning feature name is preserved so a reader can map back to the
// Features section without having to read every Gaps card. Items with
// an unrecognized priority are skipped — the body renderer filters
// those out via isKnownPriority, and the cover must match the body so
// it doesn't list findings the reader will never see further in.
func gapsSummary(in Inputs) []string {
	var out []string
	for _, f := range in.Drift {
		for _, iss := range f.Issues {
			if !isKnownPriority(iss.Priority) {
				continue
			}
			out = append(out, sanitize(f.Feature)+" - "+sanitize(iss.Issue))
		}
	}
	return out
}

// screenshotsSummary returns one entry per screenshot finding, prefixed
// with the category. Items with an unrecognized priority are skipped so
// the cover's count matches what the body actually renders.
func screenshotsSummary(in Inputs) []string {
	var out []string
	for _, g := range in.Screenshots.MissingGaps {
		if !isKnownPriority(g.Priority) {
			continue
		}
		out = append(out, "Missing: "+sanitize(g.ShouldShow))
	}
	for _, ii := range in.Screenshots.ImageIssues {
		if !isKnownPriority(ii.Priority) {
			continue
		}
		out = append(out, "Image issue: "+sanitize(ii.Reason))
	}
	for _, g := range in.Screenshots.PossiblyCovered {
		if !isKnownPriority(g.Priority) {
			continue
		}
		out = append(out, "Possibly covered: "+sanitize(g.ShouldShow))
	}
	return out
}

// summaryLine remains for callers that prefer the inline-summary form
// (e.g. a CLI banner). Kept so future surfaces have one place to pull
// the same counts the cover renders.
func summaryLine(in Inputs) string {
	parts := []string{
		fmt.Sprintf("%d features", len(in.Mapping)),
		fmt.Sprintf("%d gaps", totalDriftIssues(in.Drift)),
	}
	if in.ScreenshotsRan {
		parts = append(parts, fmt.Sprintf("%d screenshot issues", totalScreenshotIssues(in.Screenshots)))
	}
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
