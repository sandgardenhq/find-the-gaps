package pdf

import (
	"fmt"
	"strings"

	"github.com/go-pdf/fpdf"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// renderFeatures emits the Features section: one block per feature with
// description, layer, user-facing flag, documentation status, files,
// symbols, and documented-on pages. Mirrors the field set in
// reporter.WriteMapping so the PDF and mapping.md stay in sync.
// Anchors of the form feat-<slug> are registered so cross-references
// from Gaps and Screenshots can link back into this section.
func renderFeatures(doc *fpdf.Fpdf, in Inputs, anchors *anchorTable) {
	sectionHeading(doc, "Features")

	pagesByFeature := make(map[string][]string, len(in.DocsMap))
	for _, e := range in.DocsMap {
		pagesByFeature[e.Feature] = e.Pages
	}

	featAnchors := computeFeatureAnchors(in)
	for _, entry := range in.Mapping {
		anchors.Mark(featAnchors[entry.Feature.Name])
		renderFeatureBlock(doc, entry, pagesByFeature[entry.Feature.Name])
	}
}

// computeFeatureAnchors walks the feature map once and produces the
// stable feature-name → anchor-name mapping. Slug collisions are
// disambiguated with "-2", "-3", ... suffixes, in the order features
// appear in in.Mapping. Called by both renderFeatures (which Marks each
// anchor as it emits the block) and renderGaps / renderScreenshots
// (which Get the anchor to create cross-link clickable regions).
func computeFeatureAnchors(in Inputs) map[string]string {
	out := make(map[string]string, len(in.Mapping))
	used := make(map[string]int, len(in.Mapping))
	for _, entry := range in.Mapping {
		out[entry.Feature.Name] = featureAnchor(entry.Feature.Name, used)
	}
	return out
}

// featureAnchor returns a unique anchor name for the feature, suffixing
// "-2", "-3", ... when an earlier feature in this run already claimed the
// base slug.
func featureAnchor(name string, used map[string]int) string {
	base := slugify(name)
	used[base]++
	if used[base] == 1 {
		return "feat-" + base
	}
	return fmt.Sprintf("feat-%s-%d", base, used[base])
}

// renderFeatureBlock emits one feature inside a card. Header is the
// feature name, optionally followed by an italic muted description, a
// single row of metadata badges (Layer / User-facing-or-Internal /
// Documented-or-Undocumented), and the wrapped Files / Symbols /
// Documented-on lines. Card stripe colour reflects documentation status:
// green for documented, red for undocumented user-facing, neutral
// otherwise. Mirrors the .ftg-feature-card component in custom.css.
func renderFeatureBlock(doc *fpdf.Fpdf, entry analyzer.FeatureEntry, docPages []string) {
	name := sanitize(entry.Feature.Name)
	desc := sanitize(entry.Feature.Description)
	files := ""
	if len(entry.Files) > 0 {
		files = "Implemented in: " + sanitize(strings.Join(entry.Files, ", "))
	}
	symbols := ""
	if len(entry.Symbols) > 0 {
		symbols = "Symbols: " + sanitize(strings.Join(entry.Symbols, ", "))
	}
	docPagesStr := ""
	if len(docPages) > 0 {
		docPagesStr = "Documented on: " + sanitize(strings.Join(docPages, ", "))
	}

	cardX := marginLeft
	cardY := doc.GetY()
	cardW := pageWidth(doc)
	cardH := measureFeatureCard(doc, name, desc, files, symbols, docPagesStr)

	_, pageH := doc.GetPageSize()
	if cardY+cardH > pageH-marginBottom {
		doc.AddPage()
		cardY = doc.GetY()
	}

	stripe := featureStripeColor(entry.Feature.UserFacing, len(docPages) > 0)
	drawCard(doc, cardX, cardY, cardW, cardH, stripe)

	innerX := cardContentX()
	innerW := cardContentWidth(doc)

	// Heading (serif).
	doc.SetXY(innerX, cardY+cardPadY)
	doc.SetFont(titleFont, "B", fontSizeH2)
	setTextColor(doc, colorInk)
	doc.MultiCell(innerW, 0.32, name, "", "L", false)

	// Description (italic, muted).
	if desc != "" {
		doc.SetX(innerX)
		doc.SetFont(bodyFont, "I", fontSizeBody)
		setTextColor(doc, colorInkMute)
		doc.MultiCell(innerW, 0.22, desc, "", "L", false)
	}

	// Badge row.
	doc.SetXY(innerX, doc.GetY()+0.04)
	renderFeatureBadges(doc, entry.Feature, len(docPages) > 0)
	doc.Ln(badgeHeight + 0.10)

	// Files / Symbols / Documented-on.
	doc.SetFont(bodyFont, "", fontSizeMeta)
	setTextColor(doc, colorInk)
	for _, ln := range []string{files, symbols, docPagesStr} {
		if ln == "" {
			continue
		}
		doc.SetX(innerX)
		doc.MultiCell(innerW, 0.20, ln, "", "L", false)
	}

	doc.SetY(cardY + cardH + 0.18)
}

// featureStripeColor picks the colour for the card's left stripe based
// on documentation status. Mirrors the documented / undocumented
// modifiers in .ftg-feature-card.
func featureStripeColor(userFacing, documented bool) int {
	switch {
	case documented:
		return colorSevSmall
	case userFacing:
		return colorSevLarge
	}
	return colorRule
}

// renderFeatureBadges emits the metadata badge row for a feature card:
// Layer (when present), User-facing / Internal, and Documented /
// Undocumented. Colours follow the .ftg-badge--* modifiers in
// custom.css (line 805-812 + the dark-mode overrides further down).
func renderFeatureBadges(doc *fpdf.Fpdf, f analyzer.CodeFeature, documented bool) {
	if f.Layer != "" {
		drawBadge(doc, sanitize(f.Layer), colorBadgeLayerFg, colorBadgeLayerBg, colorBadgeLayerBg)
		doc.SetX(doc.GetX() + 0.08)
	}
	if f.UserFacing {
		drawBadge(doc, "user-facing", colorBadgeUserFg, colorBadgeUserBg, colorBadgeUserBg)
	} else {
		drawBadge(doc, "internal", colorBadgeInternalFg, colorBadgeInternalBg, colorBadgeInternalBg)
	}
	doc.SetX(doc.GetX() + 0.08)
	if documented {
		drawBadge(doc, "documented", colorBadgeDocFg, colorBadgeDocBg, colorBadgeDocBg)
	} else {
		drawBadge(doc, "undocumented", colorSevLarge, colorSevLargeTint, colorSevLargeTint)
	}
}

// pageWidth returns the writable width of the current page, in inches,
// based on the configured margins.
func pageWidth(doc *fpdf.Fpdf) float64 {
	w, _ := doc.GetPageSize()
	return w - marginLeft - marginRight
}

// renderGapsWithAnchors emits Gaps with caller-supplied feature anchors.
// Findings are bucketed by priority (Large → Medium → Small); empty
// buckets are omitted along with their sub-heading. Each non-empty bucket
// marks `gaps-<priority>` so the TOC sub-entries resolve to the right
// page.
//
// Page-break logic for the per-finding cards lives here (rather than
// inside renderDriftFinding) so the priority pill and the first card of
// its bucket stay on the same page: the pill cannot be orphaned at the
// bottom of a page while its findings start on the next one.
func renderGapsWithAnchors(doc *fpdf.Fpdf, in Inputs, anchors *anchorTable, featAnchors map[string]string) {
	sectionHeading(doc, "Gaps")

	buckets := bucketDrift(in.Drift)
	for _, p := range priorityOrder() {
		findings := buckets[p]
		if len(findings) == 0 {
			continue
		}
		for i, f := range findings {
			cardH := measureDriftCard(doc,
				sanitize(f.Feature), sanitize(f.Issue.Issue),
				sanitize(f.Issue.PriorityReason), sanitize(f.Issue.Page))
			needsPill := i == 0
			ensureSpace(doc, cardH, needsPill)
			if needsPill {
				anchors.Mark(gapsBucketAnchor(p))
				priorityHeading(doc, p)
			}
			renderDriftFinding(doc, anchors, featAnchors, f)
		}
		doc.Ln(0.1)
	}
}

// ensureSpace page-breaks when the current cursor + reserved height
// would cross the bottom margin. If withPill is true, an additional
// pill-height + spacing is included so a priority pill and its first
// card stay together on the same page.
func ensureSpace(doc *fpdf.Fpdf, cardH float64, withPill bool) {
	extra := 0.0
	if withPill {
		// Pill height + ln() spacing emitted by priorityHeading.
		extra = pillHeight + 0.20
	}
	_, pageH := doc.GetPageSize()
	if doc.GetY()+cardH+extra > pageH-marginBottom {
		doc.AddPage()
	}
}

// bucketedDrift pairs one DriftIssue with its owning feature so the
// renderer can group across DriftFindings into per-priority buckets.
type bucketedDrift struct {
	Feature string
	Issue   analyzer.DriftIssue
}

// bucketDrift groups every drift issue by its priority. Issues that lack a
// recognised priority value are skipped — the upstream analyzer guards
// against this but the renderer fails closed rather than emitting an
// "unknown bucket" header.
func bucketDrift(findings []analyzer.DriftFinding) map[analyzer.Priority][]bucketedDrift {
	out := map[analyzer.Priority][]bucketedDrift{}
	for _, f := range findings {
		for _, iss := range f.Issues {
			if !isKnownPriority(iss.Priority) {
				continue
			}
			out[iss.Priority] = append(out[iss.Priority], bucketedDrift{Feature: f.Feature, Issue: iss})
		}
	}
	return out
}

// priorityOrder returns the priority enums in the order the report
// renders them (highest impact first).
func priorityOrder() []analyzer.Priority {
	return []analyzer.Priority{analyzer.PriorityLarge, analyzer.PriorityMedium, analyzer.PrioritySmall}
}

func isKnownPriority(p analyzer.Priority) bool {
	switch p {
	case analyzer.PriorityLarge, analyzer.PriorityMedium, analyzer.PrioritySmall:
		return true
	}
	return false
}

// priorityHeading writes the sub-heading for one priority bucket as an
// uppercase tinted pill, matching the .ftg-priority component in
// custom.css. The cursor advances onto a fresh line below the pill so
// finding cards start cleanly.
func priorityHeading(doc *fpdf.Fpdf, p analyzer.Priority) {
	doc.Ln(0.12)
	doc.SetX(marginLeft)
	drawPill(doc, priorityLabel(p),
		priorityForeground(p), priorityBackground(p), priorityBorder(p))
	// drawPill advances X past the pill but stays on the same Y. Drop to
	// the next line so finding cards start cleanly underneath.
	doc.Ln(pillHeight + 0.08)
}

// priorityLabel returns the human-readable bucket name.
func priorityLabel(p analyzer.Priority) string {
	switch p {
	case analyzer.PriorityLarge:
		return "Large"
	case analyzer.PriorityMedium:
		return "Medium"
	case analyzer.PrioritySmall:
		return "Small"
	}
	return string(p)
}

// priorityForeground returns the foreground (text/icon) hex colour for
// one priority bucket. Mirrors --ftg-sev-{large,medium,small} in
// custom.css.
func priorityForeground(p analyzer.Priority) int {
	switch p {
	case analyzer.PriorityLarge:
		return colorSevLarge
	case analyzer.PriorityMedium:
		return colorSevMedium
	}
	return colorSevSmall
}

// priorityBackground returns the pill fill colour for one priority
// bucket. Uses the pre-blended sev-*-tint values from style.go.
func priorityBackground(p analyzer.Priority) int {
	switch p {
	case analyzer.PriorityLarge:
		return colorSevLargeTint
	case analyzer.PriorityMedium:
		return colorSevMediumTint
	}
	return colorSevSmallTint
}

// priorityBorder returns the pill border colour for one priority
// bucket. The site uses the same hue as the foreground but at the
// tint's saturation; we approximate by re-using the foreground hex.
func priorityBorder(p analyzer.Priority) int {
	return priorityForeground(p)
}

// priorityStripe returns the card-stripe colour for one priority
// bucket. Mirrors .ftg-stale--large / --medium / --small in
// custom.css.
func priorityStripe(p analyzer.Priority) int {
	return priorityForeground(p)
}


// renderDriftFinding writes one finding inside a severity card. The card
// shell carries a coloured left stripe in the priority's foreground hex;
// body text wraps inside the card's content area. The feature label is a
// clickable cross-link into the Features section when the feature exists
// in the mapping, otherwise plain text.
func renderDriftFinding(doc *fpdf.Fpdf, anchors *anchorTable, featAnchors map[string]string, b bucketedDrift) {
	featureLabel := sanitize(b.Feature)
	issue := sanitize(b.Issue.Issue)
	reason := sanitize(b.Issue.PriorityReason)
	page := sanitize(b.Issue.Page)

	cardX := marginLeft
	cardY := doc.GetY()
	cardW := pageWidth(doc)
	cardH := measureDriftCard(doc, featureLabel, issue, reason, page)
	// Page-break logic lives in renderGapsWithAnchors so the priority
	// pill and the first card of its bucket stay on the same page.

	drawCard(doc, cardX, cardY, cardW, cardH, priorityStripe(b.Issue.Priority))

	innerX := cardContentX()
	innerW := cardContentWidth(doc)

	// Header line: clickable feature name + en-dash + (truncated) issue head.
	doc.SetXY(innerX, cardY+cardPadY)
	doc.SetFont(bodyFont, "B", fontSizeBody)
	if anchor, ok := featAnchors[b.Feature]; ok {
		linkID := anchors.Get(anchor)
		featW := doc.GetStringWidth(featureLabel) + 0.02
		setTextColor(doc, colorMagenta)
		doc.CellFormat(featW, 0.22, featureLabel, "", 0, "L", false, linkID, "")
		setTextColor(doc, colorInk)
	} else {
		featW := doc.GetStringWidth(featureLabel) + 0.02
		doc.CellFormat(featW, 0.22, featureLabel, "", 0, "L", false, 0, "")
	}
	doc.CellFormat(0, 0.22, " - ", "", 1, "L", false, 0, "")

	// Issue body — wraps.
	doc.SetX(innerX)
	doc.SetFont(bodyFont, "", fontSizeBody)
	setTextColor(doc, colorInk)
	doc.MultiCell(innerW, 0.22, issue, "", "L", false)

	// Reason + page reference — wraps, italic muted.
	doc.SetX(innerX)
	setTextColor(doc, colorInkMute)
	doc.SetFont(bodyFont, "I", fontSizeMeta)
	secondary := reason
	if page != "" {
		secondary += "   (" + page + ")"
	}
	doc.MultiCell(innerW, 0.20, secondary, "", "L", false)
	setTextColor(doc, colorInk)

	// Drop cursor to just below the card with a small gap before the
	// next sibling card. doc.SetY is what later renderers consult.
	doc.SetY(cardY + cardH + 0.12)
}

// renderScreenshotsWithAnchors emits the Screenshots section (Missing
// Screenshots, Image Issues, Possibly Covered). The caller is responsible
// for gating on in.ScreenshotsRan; this function assumes the section
// should render. Each sub-section is priority-bucketed; empty
// sub-sections and their headings are omitted entirely. Sub-section and
// per-bucket anchors are marked so the TOC sub-entries resolve.
//
// Sub-section headings are emitted lazily by the bucket dispatcher
// (renderGapBuckets / renderImageIssueBuckets) so the heading + the
// pill + the first card stay on one page: a heading cannot be
// orphaned at the bottom of a page while its content starts on the
// next one.
func renderScreenshotsWithAnchors(doc *fpdf.Fpdf, in Inputs, anchors *anchorTable, featAnchors map[string]string) {
	sectionHeading(doc, "Screenshots")

	pageToFeatures := computePageToFeatures(in)

	if len(in.Screenshots.MissingGaps) > 0 {
		renderGapBuckets(doc, anchors, featAnchors, pageToFeatures, "missing",
			in.Screenshots.MissingGaps, renderMissingGap,
			func() {
				anchors.Mark("screenshots-missing")
				subSectionHeading(doc, "Missing Screenshots")
			})
	}
	if len(in.Screenshots.ImageIssues) > 0 {
		renderImageIssueBuckets(doc, anchors, featAnchors, pageToFeatures, "image-issues",
			in.Screenshots.ImageIssues,
			func() {
				anchors.Mark("screenshots-image-issues")
				subSectionHeading(doc, "Image Issues")
			})
	}
	if len(in.Screenshots.PossiblyCovered) > 0 {
		renderGapBuckets(doc, anchors, featAnchors, pageToFeatures, "possibly-covered",
			in.Screenshots.PossiblyCovered, renderMissingGap,
			func() {
				anchors.Mark("screenshots-possibly-covered")
				subSectionHeading(doc, "Possibly Covered")
			})
	}
}

// subSectionHeadingHeight is the vertical room subSectionHeading consumes
// (Ln + heading cell). Used by the bucket dispatcher to account for the
// heading in its page-break math.
const subSectionHeadingHeight = 0.42

// computePageToFeatures inverts the docs feature map so callers can
// resolve a docs page URL to the features that cover it. Used by the
// screenshot renderer to decide whether to cross-link a finding to a
// single feature anchor.
func computePageToFeatures(in Inputs) map[string][]string {
	out := map[string][]string{}
	for _, e := range in.DocsMap {
		for _, p := range e.Pages {
			out[p] = append(out[p], e.Feature)
		}
	}
	return out
}

// subSectionHeading writes a screenshots sub-section header (Missing
// Screenshots, Image Issues, Possibly Covered).
func subSectionHeading(doc *fpdf.Fpdf, title string) {
	doc.Ln(0.1)
	doc.SetFont(titleFont, "B", fontSizeH2)
	setTextColor(doc, colorInk)
	doc.CellFormat(0, 0.32, title, "", 1, "L", false, 0, "")
}

// renderGapBuckets walks ScreenshotGap items, groups by priority, and
// renders each non-empty bucket. preamble emits the sub-section heading
// (and any associated anchor mark); it is invoked lazily — right before
// the first card actually renders — so the heading shares a page with
// its pill and first card. Without this, a heading at the very bottom
// of a page would be orphaned from the content it labels.
func renderGapBuckets(
	doc *fpdf.Fpdf,
	anchors *anchorTable,
	featAnchors map[string]string,
	pageToFeatures map[string][]string,
	slug string,
	gaps []analyzer.ScreenshotGap,
	renderOne func(*fpdf.Fpdf, *anchorTable, map[string]string, map[string][]string, analyzer.ScreenshotGap),
	preamble func(),
) {
	buckets := map[analyzer.Priority][]analyzer.ScreenshotGap{}
	for _, g := range gaps {
		if !isKnownPriority(g.Priority) {
			continue
		}
		buckets[g.Priority] = append(buckets[g.Priority], g)
	}
	preambleEmitted := false
	for _, p := range priorityOrder() {
		batch := buckets[p]
		if len(batch) == 0 {
			continue
		}
		for i, g := range batch {
			cardH := measureScreenshotCardForGap(doc, g)
			needsPill := i == 0
			extra := 0.0
			if needsPill {
				extra += pillHeight + 0.20
			}
			if !preambleEmitted {
				extra += subSectionHeadingHeight
			}
			ensureSpaceFor(doc, cardH+extra)
			if !preambleEmitted {
				preamble()
				preambleEmitted = true
			}
			if needsPill {
				anchors.Mark(screenshotsBucketAnchor(slug, p))
				priorityHeading(doc, p)
			}
			renderOne(doc, anchors, featAnchors, pageToFeatures, g)
		}
		doc.Ln(0.1)
	}
}

// renderImageIssueBuckets is the ImageIssue counterpart to
// renderGapBuckets. preamble has the same lazy-emit semantics so the
// sub-section heading is never orphaned from its first card.
func renderImageIssueBuckets(
	doc *fpdf.Fpdf,
	anchors *anchorTable,
	featAnchors map[string]string,
	pageToFeatures map[string][]string,
	slug string,
	issues []analyzer.ImageIssue,
	preamble func(),
) {
	buckets := map[analyzer.Priority][]analyzer.ImageIssue{}
	for _, ii := range issues {
		if !isKnownPriority(ii.Priority) {
			continue
		}
		buckets[ii.Priority] = append(buckets[ii.Priority], ii)
	}
	preambleEmitted := false
	for _, p := range priorityOrder() {
		batch := buckets[p]
		if len(batch) == 0 {
			continue
		}
		for i, ii := range batch {
			cardH := measureScreenshotCardForImageIssue(doc, ii)
			needsPill := i == 0
			extra := 0.0
			if needsPill {
				extra += pillHeight + 0.20
			}
			if !preambleEmitted {
				extra += subSectionHeadingHeight
			}
			ensureSpaceFor(doc, cardH+extra)
			if !preambleEmitted {
				preamble()
				preambleEmitted = true
			}
			if needsPill {
				anchors.Mark(screenshotsBucketAnchor(slug, p))
				priorityHeading(doc, p)
			}
			renderImageIssue(doc, anchors, featAnchors, pageToFeatures, ii)
		}
		doc.Ln(0.1)
	}
}

// ensureSpaceFor page-breaks when the current cursor + h would cross
// the bottom margin. Generic counterpart to ensureSpace; takes the full
// height directly so callers can mix in extras (pill, sub-section
// heading) at the call site.
func ensureSpaceFor(doc *fpdf.Fpdf, h float64) {
	_, pageH := doc.GetPageSize()
	if doc.GetY()+h > pageH-marginBottom {
		doc.AddPage()
	}
}

// measureScreenshotCardForGap and measureScreenshotCardForImageIssue
// project the variable-field finding shapes into the (pageURL, lines)
// signature that measureScreenshotCard takes. Hoisting these out of
// the renderers means the bucket-loop page-break check uses the same
// height the renderer will draw.
func measureScreenshotCardForGap(doc *fpdf.Fpdf, g analyzer.ScreenshotGap) float64 {
	lines := buildScreenshotLines(
		"Should show", g.ShouldShow,
		"Suggested alt", g.SuggestedAlt,
		"Insertion", g.InsertionHint,
		"Why", g.PriorityReason,
	)
	return measureScreenshotCard(doc, sanitize(g.PageURL), lines)
}

func measureScreenshotCardForImageIssue(doc *fpdf.Fpdf, i analyzer.ImageIssue) float64 {
	lines := buildScreenshotLines(
		"Image", i.Src,
		"Issue", i.Reason,
		"Action", i.SuggestedAction,
		"Why", i.PriorityReason,
	)
	return measureScreenshotCard(doc, sanitize(i.PageURL), lines)
}

// renderMissingGap writes one ScreenshotGap (used for both Missing and
// Possibly Covered) inside a severity card. PageURL is rendered as the
// header (clickable when it resolves to a single feature owner). Below
// the header, four optional "Label: value" lines describe the gap.
func renderMissingGap(
	doc *fpdf.Fpdf,
	anchors *anchorTable,
	featAnchors map[string]string,
	pageToFeatures map[string][]string,
	g analyzer.ScreenshotGap,
) {
	pageURL := sanitize(g.PageURL)
	lines := buildScreenshotLines(
		"Should show", g.ShouldShow,
		"Suggested alt", g.SuggestedAlt,
		"Insertion", g.InsertionHint,
		"Why", g.PriorityReason,
	)
	renderScreenshotCard(doc, anchors, featAnchors, pageToFeatures,
		g.PageURL, pageURL, lines, priorityStripe(g.Priority))
}

// renderImageIssue writes one ImageIssue inside a severity card with
// the same shell shape as renderMissingGap. The body lines are the
// Src / Reason / Action / Why fields.
func renderImageIssue(
	doc *fpdf.Fpdf,
	anchors *anchorTable,
	featAnchors map[string]string,
	pageToFeatures map[string][]string,
	i analyzer.ImageIssue,
) {
	pageURL := sanitize(i.PageURL)
	lines := buildScreenshotLines(
		"Image", i.Src,
		"Issue", i.Reason,
		"Action", i.SuggestedAction,
		"Why", i.PriorityReason,
	)
	renderScreenshotCard(doc, anchors, featAnchors, pageToFeatures,
		i.PageURL, pageURL, lines, priorityStripe(i.Priority))
}

// buildScreenshotLines collects non-empty "label: value" pairs into a
// flat slice of formatted lines. Empty values are skipped so the card
// height shrinks for findings with sparse data. The label/value pairs
// arrive as alternating arguments to keep the call site terse.
func buildScreenshotLines(labelValuePairs ...string) []string {
	var out []string
	for i := 0; i+1 < len(labelValuePairs); i += 2 {
		label, value := labelValuePairs[i], labelValuePairs[i+1]
		if value == "" {
			continue
		}
		out = append(out, label+": "+sanitize(value))
	}
	return out
}

// renderScreenshotCard draws the card shell and emits the header (page
// URL, possibly linked to a feature anchor) plus body lines. Used by
// both renderMissingGap and renderImageIssue so the shape stays
// identical across the three screenshot sub-sections.
func renderScreenshotCard(
	doc *fpdf.Fpdf,
	anchors *anchorTable,
	featAnchors map[string]string,
	pageToFeatures map[string][]string,
	pageURLRaw string, // original URL (used to resolve feature owners)
	pageURL string, // sanitized URL (used to render text)
	lines []string,
	stripeHex int,
) {
	cardX := marginLeft
	cardY := doc.GetY()
	cardW := pageWidth(doc)
	cardH := measureScreenshotCard(doc, pageURL, lines)
	// Page-break logic lives in the bucket dispatchers (renderGapBuckets,
	// renderImageIssueBuckets) so a priority pill and the first card of
	// its bucket are placed together.

	drawCard(doc, cardX, cardY, cardW, cardH, stripeHex)

	innerX := cardContentX()
	innerW := cardContentWidth(doc)

	// Header: page URL. Clickable when the URL maps to exactly one feature.
	doc.SetXY(innerX, cardY+cardPadY)
	doc.SetFont(bodyFont, "B", fontSizeBody)
	owners := pageToFeatures[pageURLRaw]
	if len(owners) == 1 {
		if anchor, ok := featAnchors[owners[0]]; ok {
			linkID := anchors.Get(anchor)
			setTextColor(doc, colorMagenta)
			doc.CellFormat(innerW, 0.24, pageURL, "", 1, "L", false, linkID, "")
			setTextColor(doc, colorInk)
		} else {
			setTextColor(doc, colorInk)
			doc.CellFormat(innerW, 0.24, pageURL, "", 1, "L", false, 0, "")
		}
	} else {
		setTextColor(doc, colorInk)
		doc.CellFormat(innerW, 0.24, pageURL, "", 1, "L", false, 0, "")
	}

	// Body lines.
	doc.SetFont(bodyFont, "", fontSizeMeta)
	setTextColor(doc, colorInkMute)
	for _, ln := range lines {
		doc.SetX(innerX)
		doc.MultiCell(innerW, 0.20, ln, "", "L", false)
	}
	setTextColor(doc, colorInk)

	// Move below card with a small inter-card gap.
	doc.SetY(cardY + cardH + 0.12)
}

// sectionHeading writes a top-level section title in the brand magenta.
// Uses the Poppins titleFont to anchor the typographic hierarchy
// against the Inter body.
func sectionHeading(doc *fpdf.Fpdf, title string) {
	doc.SetFont(titleFont, "B", fontSizeH1)
	setTextColor(doc, colorMagenta)
	doc.CellFormat(0, 0.4, title, "", 1, "L", false, 0, "")
	setTextColor(doc, colorInk)
	doc.Ln(0.06)
}
