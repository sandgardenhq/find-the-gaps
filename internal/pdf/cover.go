package pdf

import (
	"fmt"

	"github.com/go-pdf/fpdf"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// renderCover writes the cover page: title block + run metadata + summary
// counts. It is the first page of the PDF and intentionally has no header
// or footer; the footer renderer suppresses itself on page 1.
func renderCover(doc *fpdf.Fpdf, in Inputs) {
	doc.AddPage()

	doc.SetTextColor(colorBodyR, colorBodyG, colorBodyB)
	doc.SetFont("Helvetica", "B", fontSizeTitle)
	doc.Ln(0.8)
	doc.CellFormat(0, 0.5, "Find the Gaps", "", 1, "L", false, 0, "")

	if in.ProjectName != "" {
		doc.SetFont("Helvetica", "", fontSizeH1)
		doc.SetTextColor(colorMutedR, colorMutedG, colorMutedB)
		doc.CellFormat(0, 0.35, sanitize(in.ProjectName), "", 1, "L", false, 0, "")
	}

	doc.Ln(0.5)
	doc.SetFont("Helvetica", "", fontSizeMeta)
	doc.SetTextColor(colorBodyR, colorBodyG, colorBodyB)

	if in.RepoURL != "" {
		doc.CellFormat(0, 0.25, "Repo:  "+sanitize(in.RepoURL), "", 1, "L", false, 0, "")
	}
	if in.DocsURL != "" {
		doc.CellFormat(0, 0.25, "Docs:  "+sanitize(in.DocsURL), "", 1, "L", false, 0, "")
	}
	if !in.GeneratedAt.IsZero() {
		ts := in.GeneratedAt.UTC().Format("2006-01-02 15:04 UTC")
		doc.CellFormat(0, 0.25, "Generated: "+ts, "", 1, "L", false, 0, "")
	}

	doc.Ln(0.4)
	doc.SetFont("Helvetica", "", fontSizeBody)
	doc.CellFormat(0, 0.25, summaryLine(in), "", 1, "L", false, 0, "")
}

// summaryLine returns the human-readable count summary printed on the cover
// page. Screenshot counts are only included when the screenshot pass ran.
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
