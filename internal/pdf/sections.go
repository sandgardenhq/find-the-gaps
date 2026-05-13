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
	doc.SetTextColor(colorBodyR, colorBodyG, colorBodyB)
	doc.CellFormat(0, 0.32, entry.Feature.Name, "", 1, "L", false, 0, "")

	if entry.Feature.Description != "" {
		doc.SetFont("Helvetica", "I", fontSizeBody)
		doc.SetTextColor(colorMutedR, colorMutedG, colorMutedB)
		width := pageWidth(doc) - 0.4
		doc.SetX(marginLeft + 0.2)
		doc.MultiCell(width, 0.22, entry.Feature.Description, "", "L", false)
	}

	doc.SetFont("Helvetica", "", fontSizeBody)
	doc.SetTextColor(colorBodyR, colorBodyG, colorBodyB)

	userFacing := "no"
	if entry.Feature.UserFacing {
		userFacing = "yes"
	}
	docStatus := "undocumented"
	if len(docPages) > 0 {
		docStatus = "documented"
	}

	if entry.Feature.Layer != "" {
		labelValue(doc, "Layer", entry.Feature.Layer)
	}
	labelValue(doc, "User-facing", userFacing)
	labelValue(doc, "Documentation status", docStatus)
	if len(entry.Files) > 0 {
		labelValue(doc, "Implemented in", strings.Join(entry.Files, ", "))
	}
	if len(entry.Symbols) > 0 {
		labelValue(doc, "Symbols", strings.Join(entry.Symbols, ", "))
	}
	if len(docPages) > 0 {
		labelValue(doc, "Documented on", strings.Join(docPages, ", "))
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

// priorityHeading writes the sub-heading for one priority bucket. The
// rendered label is the title-cased priority value ("Large", "Medium",
// "Small").
func priorityHeading(doc *fpdf.Fpdf, p analyzer.Priority) {
	doc.Ln(0.1)
	doc.SetFont("Helvetica", "B", fontSizeH2)
	doc.SetTextColor(priorityRGB(p))
	doc.CellFormat(0, 0.3, priorityLabel(p), "", 1, "L", false, 0, "")
	doc.SetTextColor(colorBodyR, colorBodyG, colorBodyB)
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

// priorityRGB returns the foreground colour used for the priority
// sub-heading. Matches the priority pill palette in style.go.
func priorityRGB(p analyzer.Priority) (int, int, int) {
	switch p {
	case analyzer.PriorityLarge:
		return colorLargeR, colorLargeG, colorLargeB
	case analyzer.PriorityMedium:
		return colorMediumR, colorMediumG, colorMediumB
	}
	return colorSmallR, colorSmallG, colorSmallB
}

// renderDriftFinding writes one finding line: clickable feature name +
// issue text + priority reason. Feature name is rendered as a clickable
// internal link to its feat-<slug> anchor when the feature exists in the
// mapping; features unknown to the mapping (which can happen if the
// drift LLM names a feature the mapping didn't enumerate) fall back to
// plain text.
func renderDriftFinding(doc *fpdf.Fpdf, anchors *anchorTable, featAnchors map[string]string, b bucketedDrift) {
	doc.SetX(marginLeft + 0.2)
	doc.SetFont("Helvetica", "", fontSizeBody)
	doc.SetTextColor(colorBodyR, colorBodyG, colorBodyB)

	featureLabel := b.Feature
	if anchor, ok := featAnchors[b.Feature]; ok {
		linkID := anchors.Get(anchor)
		// Render the feature name as a clickable underlined span. fpdf's
		// Cell with the link parameter wires the whole cell rectangle to
		// the link target.
		featW := doc.GetStringWidth(featureLabel) + 0.02
		doc.SetTextColor(colorBrandR, colorBrandG, colorBrandB)
		doc.CellFormat(featW, 0.22, featureLabel, "", 0, "L", false, linkID, "")
		doc.SetTextColor(colorBodyR, colorBodyG, colorBodyB)
	} else {
		featW := doc.GetStringWidth(featureLabel) + 0.02
		doc.CellFormat(featW, 0.22, featureLabel, "", 0, "L", false, 0, "")
	}

	// Issue text after the feature label.
	doc.CellFormat(0, 0.22, "  -  "+b.Issue.Issue, "", 1, "L", false, 0, "")

	// Priority reason and source page on a secondary line, indented and
	// muted.
	doc.SetX(marginLeft + 0.4)
	doc.SetTextColor(colorMutedR, colorMutedG, colorMutedB)
	doc.SetFont("Helvetica", "I", fontSizeMeta)
	secondary := b.Issue.PriorityReason
	if b.Issue.Page != "" {
		secondary += "   (" + b.Issue.Page + ")"
	}
	doc.CellFormat(0, 0.2, secondary, "", 1, "L", false, 0, "")
	doc.SetTextColor(colorBodyR, colorBodyG, colorBodyB)
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
	doc.SetTextColor(colorBodyR, colorBodyG, colorBodyB)
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
// Possibly Covered). PageURL is rendered as the heading; should_show,
// suggested_alt, and insertion_hint follow on indented lines.
func renderMissingGap(
	doc *fpdf.Fpdf,
	anchors *anchorTable,
	featAnchors map[string]string,
	pageToFeatures map[string][]string,
	g analyzer.ScreenshotGap,
) {
	emitPageReference(doc, anchors, featAnchors, pageToFeatures, g.PageURL)

	if g.ShouldShow != "" {
		secondaryLine(doc, "Should show: "+g.ShouldShow)
	}
	if g.SuggestedAlt != "" {
		secondaryLine(doc, "Suggested alt: "+g.SuggestedAlt)
	}
	if g.InsertionHint != "" {
		secondaryLine(doc, "Insertion: "+g.InsertionHint)
	}
	if g.PriorityReason != "" {
		secondaryLine(doc, "Why: "+g.PriorityReason)
	}
}

// renderImageIssue writes one ImageIssue (Src / Reason / SuggestedAction).
func renderImageIssue(
	doc *fpdf.Fpdf,
	anchors *anchorTable,
	featAnchors map[string]string,
	pageToFeatures map[string][]string,
	i analyzer.ImageIssue,
) {
	emitPageReference(doc, anchors, featAnchors, pageToFeatures, i.PageURL)

	if i.Src != "" {
		secondaryLine(doc, "Image: "+i.Src)
	}
	if i.Reason != "" {
		secondaryLine(doc, "Issue: "+i.Reason)
	}
	if i.SuggestedAction != "" {
		secondaryLine(doc, "Action: "+i.SuggestedAction)
	}
	if i.PriorityReason != "" {
		secondaryLine(doc, "Why: "+i.PriorityReason)
	}
}

// emitPageReference renders the page URL as a clickable cross-link when
// the URL resolves to exactly one feature in the docs map. Pages that
// resolve to zero or multiple features are rendered as plain text — the
// reader doesn't get a misleading "this finding owns one specific
// feature" hint.
func emitPageReference(
	doc *fpdf.Fpdf,
	anchors *anchorTable,
	featAnchors map[string]string,
	pageToFeatures map[string][]string,
	pageURL string,
) {
	doc.SetX(marginLeft + 0.2)
	doc.SetFont("Helvetica", "B", fontSizeBody)
	doc.SetTextColor(colorBodyR, colorBodyG, colorBodyB)

	owners := pageToFeatures[pageURL]
	if len(owners) == 1 {
		if anchor, ok := featAnchors[owners[0]]; ok {
			linkID := anchors.Get(anchor)
			doc.SetTextColor(colorBrandR, colorBrandG, colorBrandB)
			doc.CellFormat(0, 0.24, pageURL, "", 1, "L", false, linkID, "")
			doc.SetTextColor(colorBodyR, colorBodyG, colorBodyB)
			return
		}
	}
	doc.CellFormat(0, 0.24, pageURL, "", 1, "L", false, 0, "")
}

// secondaryLine writes an indented muted line under a screenshot finding
// (Should show / Suggested alt / Insertion / Why / etc.).
func secondaryLine(doc *fpdf.Fpdf, text string) {
	doc.SetX(marginLeft + 0.4)
	doc.SetFont("Helvetica", "", fontSizeMeta)
	doc.SetTextColor(colorMutedR, colorMutedG, colorMutedB)
	doc.CellFormat(0, 0.2, text, "", 1, "L", false, 0, "")
	doc.SetTextColor(colorBodyR, colorBodyG, colorBodyB)
}

// sectionHeading writes a top-level section title in the brand accent
// color. Shared by every renderer so the visual treatment stays
// consistent.
func sectionHeading(doc *fpdf.Fpdf, title string) {
	doc.SetFont("Helvetica", "B", fontSizeH1)
	doc.SetTextColor(colorBrandR, colorBrandG, colorBrandB)
	doc.CellFormat(0, 0.5, title, "", 1, "L", false, 0, "")
	doc.SetTextColor(colorBodyR, colorBodyG, colorBodyB)
	doc.Ln(0.1)
}
