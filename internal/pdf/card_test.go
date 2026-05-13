package pdf

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-pdf/fpdf"
	pdfreader "github.com/ledongthuc/pdf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// extractTextWhitebox writes the in-progress fpdf doc to a temp file and
// returns its plain-text rendering. Used by tests in package pdf
// (white-box); the equivalent black-box helper in cover_test.go lives in
// package pdf_test and is not visible here.
func extractTextWhitebox(t *testing.T, doc *fpdf.Fpdf) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "card_test.pdf")
	require.NoError(t, doc.OutputFileAndClose(path))

	f, r, err := pdfreader.Open(path)
	require.NoError(t, err)
	defer f.Close()

	rd, err := r.GetPlainText()
	require.NoError(t, err)
	b, err := io.ReadAll(rd)
	require.NoError(t, err)
	return string(b)
}

// TestPill_DrawsLabel writes a single pill onto a fresh page and asserts
// that the extracted text contains the uppercase label.
func TestPill_DrawsLabel(t *testing.T) {
	doc := newDoc()
	doc.AddPage()
	doc.SetXY(marginLeft, marginTop)

	width := drawPill(doc, "large", colorBadFg, colorBadBg, colorBadBorder)
	assert.Greater(t, width, 0.0, "pill width must be positive")

	text := extractTextWhitebox(t, doc)
	assert.Contains(t, text, "LARGE", "pill must render its label in uppercase")
}

// TestPill_WidthScalesWithLabel makes sure a longer label gets a wider
// pill. Without this the right edge of "MEDIUM" pills wraps incorrectly.
func TestPill_WidthScalesWithLabel(t *testing.T) {
	doc := newDoc()
	doc.AddPage()
	doc.SetFont("Helvetica", "B", fontSizePill)

	short := pillWidth(doc, "small")
	long := pillWidth(doc, "medium")
	assert.Greater(t, long, short,
		"medium pill should be wider than small (got short=%.3f long=%.3f)", short, long)
}

// TestRenderGapsWithAnchors_UsesPillHeading pins that the priority
// sub-heading inside the gaps section is the new uppercase pill, not
// the old title-cased text.
func TestRenderGapsWithAnchors_UsesPillHeading(t *testing.T) {
	doc := newDoc()
	registerFooter(doc, "X")
	anchors := newAnchorTable(doc)
	doc.AddPage()
	in := Inputs{
		Mapping: analyzer.FeatureMap{
			{Feature: analyzer.CodeFeature{Name: "alpha", UserFacing: true}},
		},
		Drift: []analyzer.DriftFinding{
			{Feature: "alpha", Issues: []analyzer.DriftIssue{
				{Issue: "an issue", Priority: analyzer.PriorityLarge, PriorityReason: "r"},
			}},
		},
	}
	featAnchors := computeFeatureAnchors(in)
	renderGapsWithAnchors(doc, in, anchors, featAnchors)

	text := extractTextWhitebox(t, doc)
	require.Contains(t, text, "LARGE")
	// Body-only render: no TOC was emitted in this test, so the
	// title-cased "Large" must not appear.
	assert.False(t, strings.Contains(text, "Large"),
		"body pill must be uppercase only; got: %q", text)
}
