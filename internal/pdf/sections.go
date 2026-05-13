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

// renderGaps emits the Gaps section, priority-bucketed and cross-linked
// to features. Caller-side variant uses the default feature-anchor map;
// renderGapsWithAnchors lets the dispatcher reuse a precomputed map.
func renderGaps(doc *fpdf.Fpdf, in Inputs, anchors *anchorTable) {
	renderGapsWithAnchors(doc, in, anchors, computeFeatureAnchors(in))
}

// renderGapsWithAnchors emits Gaps with caller-supplied feature anchors.
// Findings are bucketed by priority (Large → Medium → Small); empty
// buckets are omitted along with their sub-heading.
func renderGapsWithAnchors(doc *fpdf.Fpdf, in Inputs, anchors *anchorTable, featAnchors map[string]string) {
	sectionHeading(doc, "Gaps")

	buckets := bucketDrift(in.Drift)
	for _, p := range priorityOrder() {
		findings := buckets[p]
		if len(findings) == 0 {
			continue
		}
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

// renderScreenshots emits the Screenshots section (missing, image issues,
// possibly covered). Caller is responsible for gating on
// in.ScreenshotsRan; this function assumes the section should render.
// Stubbed in Task 4; filled in with real content in Task 7.
func renderScreenshots(doc *fpdf.Fpdf, in Inputs, anchors *anchorTable) {
	sectionHeading(doc, "Screenshots")
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
