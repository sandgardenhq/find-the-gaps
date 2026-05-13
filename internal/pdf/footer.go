package pdf

import (
	"fmt"

	"github.com/go-pdf/fpdf"
)

// registerFooter wires a page-footer renderer into doc. The footer is
// suppressed on page 1 (the cover page) and renders "<projectName> - page N
// of M" on every subsequent page. When projectName is empty the prefix is
// omitted, leaving just the page-of-page-count phrase.
//
// fpdf's "{nb}" alias is substituted with the final page count at output
// time, so we don't need a second pass to compute totals.
func registerFooter(doc *fpdf.Fpdf, projectName string) {
	doc.AliasNbPages("{nb}")
	doc.SetFooterFunc(func() {
		if doc.PageNo() == 1 {
			return
		}
		doc.SetY(-marginBottom + 0.3)
		doc.SetFont("Helvetica", "", fontSizeFooter)
		doc.SetTextColor(colorMutedR, colorMutedG, colorMutedB)
		doc.CellFormat(0, 0.25, footerText(projectName, doc.PageNo()), "", 0, "C", false, 0, "")
	})
}

// footerText returns the centered footer string. "{nb}" is rewritten by
// fpdf at output time to the total page count.
func footerText(projectName string, page int) string {
	if projectName == "" {
		return fmt.Sprintf("page %d of {nb}", page)
	}
	return fmt.Sprintf("%s - page %d of {nb}", projectName, page)
}
