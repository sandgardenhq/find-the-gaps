package pdf

import (
	"path/filepath"
	"strings"
	"testing"

	pdfreader "github.com/ledongthuc/pdf"
	"github.com/stretchr/testify/require"
)

// TestRegisterFooter_PageNumbersOnAllButCover renders a doc with three
// pages, the first being the cover page. After write, page 1 must NOT
// carry the footer; pages 2 and 3 must carry "page N of 3".
func TestRegisterFooter_PageNumbersOnAllButCover(t *testing.T) {
	doc := newDoc()
	registerFooter(doc, "Test Project")

	// Page 1 = cover.
	renderCover(doc, Inputs{ProjectName: "Test Project"})

	// Pages 2 and 3 = filler.
	doc.AddPage()
	doc.SetFont("Helvetica", "", fontSizeBody)
	doc.CellFormat(0, 0.25, "page 2 body", "", 1, "L", false, 0, "")

	doc.AddPage()
	doc.CellFormat(0, 0.25, "page 3 body", "", 1, "L", false, 0, "")

	path := filepath.Join(t.TempDir(), "test.pdf")
	require.NoError(t, doc.OutputFileAndClose(path))

	f, r, err := pdfreader.Open(path)
	require.NoError(t, err)
	defer f.Close()
	require.Equal(t, 3, r.NumPage(), "expected 3 pages")

	page1, err := r.Page(1).GetPlainText(nil)
	require.NoError(t, err)
	page2, err := r.Page(2).GetPlainText(nil)
	require.NoError(t, err)
	page3, err := r.Page(3).GetPlainText(nil)
	require.NoError(t, err)

	// Page 1 (cover) must have no footer at all.
	if strings.Contains(page1, "page 1 of") || strings.Contains(page1, "Test Project") && strings.Contains(page1, "of 3") {
		t.Errorf("cover page must not show a footer; got:\n%s", page1)
	}

	// Pages 2 and 3 must show the footer with project name and page numbers.
	require.Contains(t, page2, "Test Project", "footer must include project name")
	require.Contains(t, page2, "page 2 of 3", "footer must show 'page 2 of 3'")

	require.Contains(t, page3, "page 3 of 3", "footer must show 'page 3 of 3'")
}

// TestRegisterFooter_OmitsProjectNameWhenEmpty makes sure the footer falls
// back to just the page count when no project name is supplied.
func TestRegisterFooter_OmitsProjectNameWhenEmpty(t *testing.T) {
	doc := newDoc()
	registerFooter(doc, "")

	renderCover(doc, Inputs{}) // cover (page 1, no footer)
	doc.AddPage()              // page 2, footer fires

	path := filepath.Join(t.TempDir(), "test.pdf")
	require.NoError(t, doc.OutputFileAndClose(path))

	f, r, err := pdfreader.Open(path)
	require.NoError(t, err)
	defer f.Close()

	page2, err := r.Page(2).GetPlainText(nil)
	require.NoError(t, err)
	require.Contains(t, page2, "page 2 of 2")
}

// TestWriteReport_RegistersFooter is the integration assertion: WriteReport
// wires registerFooter into its newDoc() call. The cover page (page 1) must
// not carry a footer; subsequent pages must.
func TestWriteReport_RegistersFooter(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, WriteReport(dir, Inputs{ProjectName: "X"}))

	f, r, err := pdfreader.Open(filepath.Join(dir, "report.pdf"))
	require.NoError(t, err)
	defer f.Close()

	require.GreaterOrEqual(t, r.NumPage(), 2, "expected at least cover + TOC pages")

	page1, err := r.Page(1).GetPlainText(nil)
	require.NoError(t, err)
	if strings.Contains(page1, "page 1 of") {
		t.Errorf("cover page must not carry a footer; got:\n%s", page1)
	}

	page2, err := r.Page(2).GetPlainText(nil)
	require.NoError(t, err)
	require.Contains(t, page2, "page 2 of", "non-cover pages must carry the footer")
}
