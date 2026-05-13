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

// renderFeatureBlock emits one feature: heading, description, key/value
// fields, files, symbols, and documented-on pages.
func renderFeatureBlock(doc *fpdf.Fpdf, entry analyzer.FeatureEntry, docPages []string) {
	doc.Ln(0.05)

	// Feature name as a sub-heading.
	doc.SetFont("Helvetica", "B", fontSizeH2)
	setTextColor(doc, colorBodyFg)
	doc.CellFormat(0, 0.32, sanitize(entry.Feature.Name), "", 1, "L", false, 0, "")

	if entry.Feature.Description != "" {
		doc.SetFont("Helvetica", "I", fontSizeBody)
		setTextColor(doc, colorMutedFg)
		width := pageWidth(doc) - 0.4
		doc.SetX(marginLeft + 0.2)
		doc.MultiCell(width, 0.22, sanitize(entry.Feature.Description), "", "L", false)
	}

	doc.SetFont("Helvetica", "", fontSizeBody)
	setTextColor(doc, colorBodyFg)

	userFacing := "no"
	if entry.Feature.UserFacing {
		userFacing = "yes"
	}
	docStatus := "undocumented"
	if len(docPages) > 0 {
		docStatus = "documented"
	}

	if entry.Feature.Layer != "" {
		labelValue(doc, "Layer", sanitize(entry.Feature.Layer))
	}
	labelValue(doc, "User-facing", userFacing)
	labelValue(doc, "Documentation status", docStatus)
	if len(entry.Files) > 0 {
		labelValue(doc, "Implemented in", sanitize(strings.Join(entry.Files, ", ")))
	}
	if len(entry.Symbols) > 0 {
		labelValue(doc, "Symbols", sanitize(strings.Join(entry.Symbols, ", ")))
	}
	if len(docPages) > 0 {
		labelValue(doc, "Documented on", sanitize(strings.Join(docPages, ", ")))
	}
	doc.Ln(0.15)
}

// labelValue prints a single "Label: Value" row. Rendered as one cell so
// the label and value share a single text run — text extractors keep them
// on one line, which lets parse-back tests assert on the rendered string
// directly. Label weight is conveyed by the colon-and-space prefix rather
// than a font swap (a font swap mid-row creates two text objects, which
// the extractor then splits across lines).
func labelValue(doc *fpdf.Fpdf, label, value string) {
	doc.SetX(marginLeft)
	doc.SetFont("Helvetica", "", fontSizeBody)
	doc.CellFormat(0, 0.22, label+": "+value, "", 1, "L", false, 0, "")
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
func renderGapsWithAnchors(doc *fpdf.Fpdf, in Inputs, anchors *anchorTable, featAnchors map[string]string) {
	sectionHeading(doc, "Gaps")

	buckets := bucketDrift(in.Drift)
	for _, p := range priorityOrder() {
		findings := buckets[p]
		if len(findings) == 0 {
			continue
		}
		anchors.Mark(gapsBucketAnchor(p))
		priorityHeading(doc, p)
		for _, f := range findings {
			renderDriftFinding(doc, anchors, featAnchors, f)
		}
		doc.Ln(0.1)
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

// priorityForeground returns the foreground (text/icon) hex colour
// associated with one priority bucket. Mirrors the --ftg-bad / --ftg-warn /
// --ftg-neutral mappings in custom.css.
func priorityForeground(p analyzer.Priority) int {
	switch p {
	case analyzer.PriorityLarge:
		return colorBadFg
	case analyzer.PriorityMedium:
		return colorWarnFg
	}
	return colorNeutralFg
}

// priorityBackground returns the pill fill colour for one priority bucket.
func priorityBackground(p analyzer.Priority) int {
	switch p {
	case analyzer.PriorityLarge:
		return colorBadBg
	case analyzer.PriorityMedium:
		return colorWarnBg
	}
	return colorNeutralBg
}

// priorityBorder returns the pill border colour for one priority bucket.
// The Small bucket uses the neutral *border* token (not the neutral
// foreground) so its pill matches the lighter outline used on the site.
func priorityBorder(p analyzer.Priority) int {
	switch p {
	case analyzer.PriorityLarge:
		return colorBadBorder
	case analyzer.PriorityMedium:
		return colorWarnBorder
	}
	return colorNeutralBorder
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

	// If the card would cross the bottom margin, force a page break first
	// so the whole card stays on one page.
	_, pageH := doc.GetPageSize()
	if cardY+cardH > pageH-marginBottom {
		doc.AddPage()
		cardY = doc.GetY()
	}

	drawCard(doc, cardX, cardY, cardW, cardH, priorityForeground(b.Issue.Priority))

	innerX := cardContentX()
	innerW := cardContentWidth(doc)

	// Header line: clickable feature name + en-dash + (truncated) issue head.
	doc.SetXY(innerX, cardY+cardPadY)
	doc.SetFont("Helvetica", "B", fontSizeBody)
	if anchor, ok := featAnchors[b.Feature]; ok {
		linkID := anchors.Get(anchor)
		featW := doc.GetStringWidth(featureLabel) + 0.02
		setTextColor(doc, colorLinkFg)
		doc.CellFormat(featW, 0.22, featureLabel, "", 0, "L", false, linkID, "")
		setTextColor(doc, colorBodyFg)
	} else {
		featW := doc.GetStringWidth(featureLabel) + 0.02
		doc.CellFormat(featW, 0.22, featureLabel, "", 0, "L", false, 0, "")
	}
	doc.CellFormat(0, 0.22, " - ", "", 1, "L", false, 0, "")

	// Issue body — wraps.
	doc.SetX(innerX)
	doc.SetFont("Helvetica", "", fontSizeBody)
	setTextColor(doc, colorBodyFg)
	doc.MultiCell(innerW, 0.22, issue, "", "L", false)

	// Reason + page reference — wraps, italic muted.
	doc.SetX(innerX)
	setTextColor(doc, colorMutedFg)
	doc.SetFont("Helvetica", "I", fontSizeMeta)
	secondary := reason
	if page != "" {
		secondary += "   (" + page + ")"
	}
	doc.MultiCell(innerW, 0.20, secondary, "", "L", false)
	setTextColor(doc, colorBodyFg)

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
func renderScreenshotsWithAnchors(doc *fpdf.Fpdf, in Inputs, anchors *anchorTable, featAnchors map[string]string) {
	sectionHeading(doc, "Screenshots")

	pageToFeatures := computePageToFeatures(in)

	if len(in.Screenshots.MissingGaps) > 0 {
		anchors.Mark("screenshots-missing")
		subSectionHeading(doc, "Missing Screenshots")
		renderGapBuckets(doc, anchors, featAnchors, pageToFeatures, "missing", in.Screenshots.MissingGaps, renderMissingGap)
	}
	if len(in.Screenshots.ImageIssues) > 0 {
		anchors.Mark("screenshots-image-issues")
		subSectionHeading(doc, "Image Issues")
		renderImageIssueBuckets(doc, anchors, featAnchors, pageToFeatures, "image-issues", in.Screenshots.ImageIssues)
	}
	if len(in.Screenshots.PossiblyCovered) > 0 {
		anchors.Mark("screenshots-possibly-covered")
		subSectionHeading(doc, "Possibly Covered")
		renderGapBuckets(doc, anchors, featAnchors, pageToFeatures, "possibly-covered", in.Screenshots.PossiblyCovered, renderMissingGap)
	}
}

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
	doc.SetFont("Helvetica", "B", fontSizeH2)
	setTextColor(doc, colorBodyFg)
	doc.CellFormat(0, 0.32, title, "", 1, "L", false, 0, "")
}

// renderGapBuckets walks ScreenshotGap items, groups by priority, and
// dispatches to renderOne per item. Used for both Missing Screenshots
// and Possibly Covered, which share the gap shape. slug is the
// sub-section identifier ("missing", "possibly-covered") used to build
// per-bucket anchor names so the TOC sub-entries resolve.
func renderGapBuckets(
	doc *fpdf.Fpdf,
	anchors *anchorTable,
	featAnchors map[string]string,
	pageToFeatures map[string][]string,
	slug string,
	gaps []analyzer.ScreenshotGap,
	renderOne func(*fpdf.Fpdf, *anchorTable, map[string]string, map[string][]string, analyzer.ScreenshotGap),
) {
	buckets := map[analyzer.Priority][]analyzer.ScreenshotGap{}
	for _, g := range gaps {
		if !isKnownPriority(g.Priority) {
			continue
		}
		buckets[g.Priority] = append(buckets[g.Priority], g)
	}
	for _, p := range priorityOrder() {
		batch := buckets[p]
		if len(batch) == 0 {
			continue
		}
		anchors.Mark(screenshotsBucketAnchor(slug, p))
		priorityHeading(doc, p)
		for _, g := range batch {
			renderOne(doc, anchors, featAnchors, pageToFeatures, g)
		}
		doc.Ln(0.1)
	}
}

// renderImageIssueBuckets is the ImageIssue counterpart to
// renderGapBuckets. ImageIssue has a different shape so it gets its own
// dispatcher.
func renderImageIssueBuckets(
	doc *fpdf.Fpdf,
	anchors *anchorTable,
	featAnchors map[string]string,
	pageToFeatures map[string][]string,
	slug string,
	issues []analyzer.ImageIssue,
) {
	buckets := map[analyzer.Priority][]analyzer.ImageIssue{}
	for _, i := range issues {
		if !isKnownPriority(i.Priority) {
			continue
		}
		buckets[i.Priority] = append(buckets[i.Priority], i)
	}
	for _, p := range priorityOrder() {
		batch := buckets[p]
		if len(batch) == 0 {
			continue
		}
		anchors.Mark(screenshotsBucketAnchor(slug, p))
		priorityHeading(doc, p)
		for _, i := range batch {
			renderImageIssue(doc, anchors, featAnchors, pageToFeatures, i)
		}
		doc.Ln(0.1)
	}
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
		g.PageURL, pageURL, lines, priorityForeground(g.Priority))
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
		i.PageURL, pageURL, lines, priorityForeground(i.Priority))
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

	_, pageH := doc.GetPageSize()
	if cardY+cardH > pageH-marginBottom {
		doc.AddPage()
		cardY = doc.GetY()
	}

	drawCard(doc, cardX, cardY, cardW, cardH, stripeHex)

	innerX := cardContentX()
	innerW := cardContentWidth(doc)

	// Header: page URL. Clickable when the URL maps to exactly one feature.
	doc.SetXY(innerX, cardY+cardPadY)
	doc.SetFont("Helvetica", "B", fontSizeBody)
	owners := pageToFeatures[pageURLRaw]
	if len(owners) == 1 {
		if anchor, ok := featAnchors[owners[0]]; ok {
			linkID := anchors.Get(anchor)
			setTextColor(doc, colorLinkFg)
			doc.CellFormat(innerW, 0.24, pageURL, "", 1, "L", false, linkID, "")
			setTextColor(doc, colorBodyFg)
		} else {
			setTextColor(doc, colorBodyFg)
			doc.CellFormat(innerW, 0.24, pageURL, "", 1, "L", false, 0, "")
		}
	} else {
		setTextColor(doc, colorBodyFg)
		doc.CellFormat(innerW, 0.24, pageURL, "", 1, "L", false, 0, "")
	}

	// Body lines.
	doc.SetFont("Helvetica", "", fontSizeMeta)
	setTextColor(doc, colorMutedFg)
	for _, ln := range lines {
		doc.SetX(innerX)
		doc.MultiCell(innerW, 0.20, ln, "", "L", false)
	}
	setTextColor(doc, colorBodyFg)

	// Move below card with a small inter-card gap.
	doc.SetY(cardY + cardH + 0.12)
}

// sectionHeading writes a top-level section title in the brand accent
// color. Shared by every renderer so the visual treatment stays
// consistent.
func sectionHeading(doc *fpdf.Fpdf, title string) {
	doc.SetFont("Helvetica", "B", fontSizeH1)
	setTextColor(doc, colorLinkFg)
	doc.CellFormat(0, 0.5, title, "", 1, "L", false, 0, "")
	setTextColor(doc, colorBodyFg)
	doc.Ln(0.1)
}
