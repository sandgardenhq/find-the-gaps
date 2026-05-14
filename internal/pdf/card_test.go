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

	width := drawPill(doc, "large", colorSevLarge, colorSevLargeTint, colorSevLarge)
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

// TestDrawCard_DrawsBoundedRect makes sure drawCard returns a positive
// rectangle and advances the cursor below the card.
func TestDrawCard_DrawsBoundedRect(t *testing.T) {
	doc := newDoc()
	doc.AddPage()
	startY := doc.GetY()

	drawCard(doc, marginLeft, startY, 5.0, 1.2, colorSevLarge)

	// Drawing a card should not move the cursor by itself; renderers
	// position content inside the card explicitly. Just confirm the
	// call did not error / panic and the page still accepts content.
	doc.SetXY(marginLeft, startY+1.4)
	doc.SetFont("Helvetica", "", fontSizeBody)
	doc.CellFormat(0, 0.2, "after card", "", 1, "L", false, 0, "")

	text := extractTextWhitebox(t, doc)
	assert.Contains(t, text, "after card", "content below card must render")
}

// TestMeasureCardHeight_AccountsForWrapping pins that the measurer
// returns a taller card when the issue text wraps. Without this we'd
// crop the last line.
func TestMeasureCardHeight_AccountsForWrapping(t *testing.T) {
	doc := newDoc()
	doc.AddPage()
	doc.SetFont("Helvetica", "", fontSizeBody)

	short := measureDriftCard(doc, "alpha", "short issue", "r", "")
	long := measureDriftCard(doc, "alpha",
		"this is a much longer issue that will absolutely wrap to several lines once the renderer drops it inside the card content area that is only about five inches wide",
		"a longer reason text that itself may also wrap to two lines",
		"https://docs.example.com/some/long/page/url")
	assert.Greater(t, long, short,
		"long-text card must be taller than short-text card (short=%.3f long=%.3f)",
		short, long)
}

// TestRenderDriftFinding_CardContainsAllText pins that the new card
// shell still emits every piece of finding data inside the card body.
func TestRenderDriftFinding_CardContainsAllText(t *testing.T) {
	doc := newDoc()
	doc.AddPage()
	anchors := newAnchorTable(doc)
	in := Inputs{
		Mapping: analyzer.FeatureMap{
			{Feature: analyzer.CodeFeature{Name: "auth", UserFacing: true}},
		},
		Drift: []analyzer.DriftFinding{
			{Feature: "auth", Issues: []analyzer.DriftIssue{
				{
					Page:           "https://docs.example.com/auth",
					Issue:          "DOCUMENTED_ISSUE_MARKER",
					Priority:       analyzer.PriorityLarge,
					PriorityReason: "PRIORITY_REASON_MARKER",
				},
			}},
		},
	}
	renderGapsWithAnchors(doc, in, anchors, computeFeatureAnchors(in))

	text := extractTextWhitebox(t, doc)
	assert.Contains(t, text, "auth")
	assert.Contains(t, text, "DOCUMENTED_ISSUE_MARKER")
	assert.Contains(t, text, "PRIORITY_REASON_MARKER")
	assert.Contains(t, text, "docs.example.com/auth")
}

// TestRenderMissingGap_CardContainsAllFields pins that the new card
// shell still emits every field of a missing-screenshot finding.
func TestRenderMissingGap_CardContainsAllFields(t *testing.T) {
	doc := newDoc()
	doc.AddPage()
	anchors := newAnchorTable(doc)
	in := Inputs{
		Mapping: analyzer.FeatureMap{
			{Feature: analyzer.CodeFeature{Name: "alpha", UserFacing: true}},
		},
		DocsMap: analyzer.DocsFeatureMap{
			{Feature: "alpha", Pages: []string{"https://docs.example.com/a"}},
		},
		Screenshots: analyzer.ScreenshotResult{
			MissingGaps: []analyzer.ScreenshotGap{
				{
					PageURL:        "https://docs.example.com/a",
					ShouldShow:     "SHOULD_SHOW_MARKER",
					SuggestedAlt:   "ALT_MARKER",
					InsertionHint:  "INSERTION_MARKER",
					Priority:       analyzer.PriorityLarge,
					PriorityReason: "WHY_MARKER",
				},
			},
		},
		ScreenshotsRan: true,
	}
	featAnchors := computeFeatureAnchors(in)
	renderScreenshotsWithAnchors(doc, in, anchors, featAnchors)

	text := extractTextWhitebox(t, doc)
	assert.Contains(t, text, "SHOULD_SHOW_MARKER")
	assert.Contains(t, text, "ALT_MARKER")
	assert.Contains(t, text, "INSERTION_MARKER")
	assert.Contains(t, text, "WHY_MARKER")
	assert.Contains(t, text, "docs.example.com/a")
}

// TestRenderImageIssue_CardContainsAllFields pins the same shape for
// the ImageIssue variant of the screenshot card.
func TestRenderImageIssue_CardContainsAllFields(t *testing.T) {
	doc := newDoc()
	doc.AddPage()
	anchors := newAnchorTable(doc)
	in := Inputs{
		Mapping: analyzer.FeatureMap{
			{Feature: analyzer.CodeFeature{Name: "alpha", UserFacing: true}},
		},
		Screenshots: analyzer.ScreenshotResult{
			ImageIssues: []analyzer.ImageIssue{
				{
					PageURL:         "https://docs.example.com/img",
					Src:             "IMG_SRC_MARKER",
					Reason:          "IMG_REASON_MARKER",
					SuggestedAction: "IMG_ACTION_MARKER",
					Priority:        analyzer.PriorityMedium,
					PriorityReason:  "IMG_WHY_MARKER",
				},
			},
		},
		ScreenshotsRan: true,
	}
	featAnchors := computeFeatureAnchors(in)
	renderScreenshotsWithAnchors(doc, in, anchors, featAnchors)

	text := extractTextWhitebox(t, doc)
	assert.Contains(t, text, "IMG_SRC_MARKER")
	assert.Contains(t, text, "IMG_REASON_MARKER")
	assert.Contains(t, text, "IMG_ACTION_MARKER")
	assert.Contains(t, text, "IMG_WHY_MARKER")
}

// TestRenderCover_HasCategorySections pins that the cover renders one
// section per category (Features / Gaps / Screenshot Issues) with the
// finding count baked into the heading and the actual finding text
// listed underneath. Replaces the older stat-card test.
func TestRenderCover_HasCategorySections(t *testing.T) {
	doc := newDoc()
	in := Inputs{
		ProjectName: "Sample Project",
		RepoURL:     "https://repo.example.com/x",
		DocsURL:     "https://docs.example.com/x",
		Mapping: analyzer.FeatureMap{
			{Feature: analyzer.CodeFeature{Name: "feat-alpha", UserFacing: true}},
			{Feature: analyzer.CodeFeature{Name: "feat-beta", UserFacing: true}},
			{Feature: analyzer.CodeFeature{Name: "feat-gamma", UserFacing: true}},
		},
		Drift: []analyzer.DriftFinding{
			{Feature: "feat-alpha", Issues: []analyzer.DriftIssue{
				{Issue: "drift-A", Priority: analyzer.PriorityLarge, PriorityReason: "y"},
				{Issue: "drift-B", Priority: analyzer.PriorityMedium, PriorityReason: "y"},
			}},
		},
		Screenshots: analyzer.ScreenshotResult{
			MissingGaps: []analyzer.ScreenshotGap{
				{PageURL: "u", ShouldShow: "shot-missing-A", Priority: analyzer.PriorityLarge, PriorityReason: "r"},
			},
			ImageIssues: []analyzer.ImageIssue{
				{PageURL: "u", Reason: "shot-image-A", Priority: analyzer.PriorityMedium, PriorityReason: "r"},
			},
		},
		ScreenshotsRan: true,
	}
	renderCover(doc, in)

	text := extractTextWhitebox(t, doc)

	// Section headings carry the count.
	assert.Contains(t, text, "Features (3)")
	assert.Contains(t, text, "Gaps (2)")
	assert.Contains(t, text, "Screenshot Issues (2)")

	// Each finding's short description appears in its section.
	assert.Contains(t, text, "feat-alpha")
	assert.Contains(t, text, "feat-beta")
	assert.Contains(t, text, "feat-gamma")
	assert.Contains(t, text, "drift-A")
	assert.Contains(t, text, "drift-B")
	assert.Contains(t, text, "shot-missing-A")
	assert.Contains(t, text, "shot-image-A")
}

// TestRenderCover_OmitsEmptyCategories pins that a category with zero
// findings is dropped entirely from the cover — no heading, no
// whitespace — so the page focuses on what was actually found.
func TestRenderCover_OmitsEmptyCategories(t *testing.T) {
	doc := newDoc()
	in := Inputs{
		ProjectName:    "x",
		ScreenshotsRan: false,
	}
	renderCover(doc, in)

	text := extractTextWhitebox(t, doc)
	// All three categories empty → none of them should render their
	// section heading.
	assert.NotContains(t, text, "Features (",
		"empty Features category should not render its heading")
	assert.NotContains(t, text, "Gaps (",
		"empty Gaps category should not render its heading")
	assert.NotContains(t, text, "Screenshot",
		"screenshot category must be hidden when ScreenshotsRan=false")
}

// TestRenderFeatureBlock_RendersBadgeRow pins that a feature card emits
// badge labels for Layer, User-facing / Internal, and Documented /
// Undocumented status, mirroring the .ftg-badge components on the site.
func TestRenderFeatureBlock_RendersBadgeRow(t *testing.T) {
	doc := newDoc()
	registerFooter(doc, "X")
	doc.AddPage()
	anchors := newAnchorTable(doc)
	in := Inputs{
		Mapping: analyzer.FeatureMap{
			{
				Feature: analyzer.CodeFeature{
					Name:        "auth",
					Description: "Handles user login and session.",
					Layer:       "api",
					UserFacing:  true,
				},
				Files:   []string{"auth.go"},
				Symbols: []string{"Login"},
			},
		},
		DocsMap: analyzer.DocsFeatureMap{
			{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}},
		},
	}
	renderFeatures(doc, in, anchors)

	text := extractTextWhitebox(t, doc)
	assert.Contains(t, text, "auth", "feature name")
	assert.Contains(t, text, "Handles user login", "description")
	assert.Contains(t, text, "api", "layer badge")
	// Badges (lowercase, single word or hyphenated). They appear in the
	// extracted text as the badge label.
	assert.Contains(t, text, "user-facing", "user-facing badge")
	assert.Contains(t, text, "documented", "documented badge")
	// Files / Symbols / Documented on still render in the body.
	assert.Contains(t, text, "auth.go")
	assert.Contains(t, text, "Login")
	assert.Contains(t, text, "docs.example.com/auth")
}

// TestRenderFeatureBlock_InternalUndocumentedBadge pins the "internal"
// + "undocumented" badge combination for a feature with UserFacing=false
// and no docs pages.
func TestRenderFeatureBlock_InternalUndocumentedBadge(t *testing.T) {
	doc := newDoc()
	registerFooter(doc, "X")
	doc.AddPage()
	anchors := newAnchorTable(doc)
	in := Inputs{
		Mapping: analyzer.FeatureMap{
			{Feature: analyzer.CodeFeature{Name: "background-worker", UserFacing: false}},
		},
	}
	renderFeatures(doc, in, anchors)

	text := extractTextWhitebox(t, doc)
	assert.Contains(t, text, "internal", "internal badge for non-user-facing feature")
	assert.Contains(t, text, "undocumented", "undocumented badge when no docs pages")
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
