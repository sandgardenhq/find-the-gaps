package reporter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// WriteMapping writes mapping.md to dir.
//
// docsMap is the canonical feature → pages mapping produced by
// analyzer.MapFeaturesToDocs. It is the single source of truth for
// documentation status; per-page feature lists from AnalyzePage are NOT used
// here because those names come from an independent LLM pass and rarely match
// the canonical code feature names exactly.
func WriteMapping(dir string, summary analyzer.ProductSummary, mapping analyzer.FeatureMap, docsMap analyzer.DocsFeatureMap) error {
	pagesByFeature := make(map[string][]string, len(docsMap))
	for _, e := range docsMap {
		pagesByFeature[e.Feature] = e.Pages
	}

	var sb strings.Builder

	sb.WriteString("# Feature Map\n\n")
	sb.WriteString("## Product Summary\n\n")
	sb.WriteString(summary.Description)
	sb.WriteString("\n\n")

	sb.WriteString("## Features\n\n")
	for _, entry := range mapping {
		fmt.Fprintf(&sb, "### %s\n\n", entry.Feature.Name)

		if entry.Feature.Description != "" {
			fmt.Fprintf(&sb, "> %s\n\n", entry.Feature.Description)
		}

		userFacingStr := "no"
		if entry.Feature.UserFacing {
			userFacingStr = "yes"
		}

		docPages := pagesByFeature[entry.Feature.Name]
		docStatus := "undocumented"
		if len(docPages) > 0 {
			docStatus = "documented"
		}

		if entry.Feature.Layer != "" {
			fmt.Fprintf(&sb, "- **Layer:** %s\n", entry.Feature.Layer)
		}
		fmt.Fprintf(&sb, "- **User-facing:** %s\n", userFacingStr)
		fmt.Fprintf(&sb, "- **Documentation status:** %s\n", docStatus)
		if len(entry.Files) > 0 {
			fmt.Fprintf(&sb, "- **Implemented in:** %s\n", strings.Join(entry.Files, ", "))
		}
		if len(entry.Symbols) > 0 {
			fmt.Fprintf(&sb, "- **Symbols:** %s\n", strings.Join(entry.Symbols, ", "))
		}
		if len(docPages) > 0 {
			fmt.Fprintf(&sb, "- **Documented on:** %s\n", strings.Join(docPages, ", "))
		} else {
			fmt.Fprintf(&sb, "- **Documented on:** _(none)_\n")
		}
		sb.WriteString("\n")
	}

	return os.WriteFile(filepath.Join(dir, "mapping.md"), []byte(sb.String()), 0o644)
}

// WriteGaps writes gaps.md to dir.
// Undocumented Features: user-facing features with a code implementation but no documentation page.
// Stale Documentation: inaccuracies found in pages that DO cover a feature.
//
// Internal (non-user-facing) features are intentionally excluded from the
// "Undocumented" section — the user-impact bar is what matters for docs.
// Documented-but-unmapped features are also no longer reported: in practice
// they were dominated by docs-side hallucinations and added more noise than
// signal.
func WriteGaps(
	dir string,
	mapping analyzer.FeatureMap,
	allDocFeatures []string,
	drift []analyzer.DriftFinding,
) error {
	docFeatures := make(map[string]bool)
	for _, f := range allDocFeatures {
		docFeatures[f] = true
	}

	var sb strings.Builder
	sb.WriteString("# Gaps Found\n\n")

	// Undocumented features: user-facing features implemented in code but
	// missing from docs. Each is rendered as a problem callout via the
	// .ftg-undoc CSS class so the rendered site reads as a list of issues
	// rather than a plain bullet list.
	sb.WriteString("## Undocumented Features\n\n")
	var undoc []analyzer.FeatureEntry
	for _, entry := range mapping {
		if len(entry.Files) > 0 && !docFeatures[entry.Feature.Name] && entry.Feature.UserFacing {
			undoc = append(undoc, entry)
		}
	}
	if len(undoc) == 0 {
		sb.WriteString("_None found._\n")
	} else {
		sb.WriteString(`<div class="ftg-undoc-list">` + "\n\n")
		for _, entry := range undoc {
			fmt.Fprintf(&sb, `<div class="ftg-undoc"><span class="ftg-undoc-name">%s</span><span class="ftg-undoc-msg"> — has code implementation but no documentation page</span></div>`+"\n\n", entry.Feature.Name)
		}
		sb.WriteString("</div>\n")
	}

	// Stale documentation: inaccuracies found in pages that DO cover a feature,
	// grouped by priority (Large → Medium → Small). Empty buckets are omitted.
	// Each finding renders as a card via .ftg-stale; priority sub-headings are
	// wrapped in .ftg-priority--{large,medium,small} so custom.css can color
	// them. Within a bucket, findings appear in stable original order
	// (feature first, then issue position within that feature).
	sb.WriteString("\n## Stale Documentation\n\n")
	flat := flattenDrift(drift)
	if len(flat) == 0 {
		sb.WriteString("_None found._\n")
	} else {
		for _, p := range []analyzer.Priority{analyzer.PriorityLarge, analyzer.PriorityMedium, analyzer.PrioritySmall} {
			bucket := filterDriftByPriority(flat, p)
			if len(bucket) == 0 {
				continue
			}
			fmt.Fprintf(&sb, "<div class=\"ftg-priority ftg-priority--%s\">\n\n### %s\n\n</div>\n\n", priorityClass(p), priorityHeading(p))
			fmt.Fprintf(&sb, `<div class="ftg-stale-list">`+"\n\n")
			for _, item := range bucket {
				fmt.Fprintf(&sb, `<div class="ftg-stale ftg-stale--%s">`+"\n", priorityClass(p))
				fmt.Fprintf(&sb, `<span class="ftg-stale-feature">%s</span>`+"\n", item.Feature)
				if item.Issue.Page != "" {
					fmt.Fprintf(&sb, `<a class="ftg-stale-page" href="%s">%s</a>`+"\n", item.Issue.Page, item.Issue.Page)
				}
				fmt.Fprintf(&sb, `<span class="ftg-stale-issue">%s</span>`+"\n", item.Issue.Issue)
				if item.Issue.PriorityReason != "" {
					fmt.Fprintf(&sb, `<span class="ftg-stale-why">why: %s</span>`+"\n", item.Issue.PriorityReason)
				}
				sb.WriteString("</div>\n\n")
			}
			sb.WriteString("</div>\n\n")
		}
	}

	return os.WriteFile(filepath.Join(dir, "gaps.md"), []byte(sb.String()), 0o644)
}

// WriteScreenshots writes screenshots.md to dir. Call ONLY when the screenshot
// pass actually ran — a skipped pass must produce NO file. Zero-length
// MissingGaps is valid and produces a "_None found._" body for the missing-
// screenshots section.
//
// The "## Image Issues" section is appended only when at least one page in
// res.AuditStats has VisionEnabled=true. When vision ran but produced no
// issues, the header is rendered with a "_No image issues detected._" marker
// so users can see the pass actually ran. When vision did not run on any
// page, the header is omitted entirely.
func WriteScreenshots(dir string, res analyzer.ScreenshotResult) error {
	var sb strings.Builder
	sb.WriteString("# Missing Screenshots\n\n")

	if len(res.MissingGaps) == 0 {
		sb.WriteString("_None found._\n")
	} else {
		writeGapBuckets(&sb, res.MissingGaps, renderMissingGap, true /*emitAnchor*/)
	}

	if len(res.PossiblyCovered) > 0 {
		sb.WriteString("\n## Possibly Covered\n\n")
		sb.WriteString("Suppressed because an unanalyzable but plausibly screenshot-shaped image is already on the page. Quick visual check is enough to confirm or override.\n\n")
		writeGapBuckets(&sb, res.PossiblyCovered, renderPossiblyCoveredGap, false /*emitAnchor*/)
	}

	visionRan := false
	for _, s := range res.AuditStats {
		if s.VisionEnabled {
			visionRan = true
			break
		}
	}
	if visionRan {
		sb.WriteString("\n## Image Issues\n\n")
		if len(res.ImageIssues) == 0 {
			sb.WriteString("_No image issues detected._\n")
		} else {
			writeImageIssueBuckets(&sb, res.ImageIssues)
		}
	}

	return os.WriteFile(filepath.Join(dir, "screenshots.md"), []byte(sb.String()), 0o644)
}

// gapRenderer renders one ScreenshotGap into the builder. The third argument
// is the priority class suffix ("large" / "medium" / "small") so the
// renderer can stamp the correct `.ftg-shot--*` modifier onto its card.
// Used to differentiate missing-screenshot rendering (Insert hint) from
// possibly-covered rendering (Insert if uncovered hint) without duplicating
// bucket plumbing.
type gapRenderer func(*strings.Builder, analyzer.ScreenshotGap, string)

// writeGapBuckets emits priority sub-headings (wrapped in
// `.ftg-priority--{large,medium,small}` for color) and renders each finding
// inside a `.ftg-shot` card. The card carries the page URL, the quoted
// passage, and the field labels in one visual unit so a reader sees
// page → passage → what to show → where to put it together. Anchors are
// emitted only on the missing-screenshots side because the existing inline
// permalinks/TOC integration only reads anchors from that section.
func writeGapBuckets(sb *strings.Builder, gaps []analyzer.ScreenshotGap, render gapRenderer, emitAnchor bool) {
	for _, p := range []analyzer.Priority{analyzer.PriorityLarge, analyzer.PriorityMedium, analyzer.PrioritySmall} {
		bucket := filterGapsByPriority(gaps, p)
		if len(bucket) == 0 {
			continue
		}
		fmt.Fprintf(sb, "<div class=\"ftg-priority ftg-priority--%s\">\n\n### %s\n\n</div>\n\n", priorityClass(p), priorityHeading(p))
		sb.WriteString(`<div class="ftg-shot-list">` + "\n\n")
		// Page grouping inside the bucket, first-occurrence order. The page
		// header stays as an `<h4>` heading (#### markdown) so Hugo's TOC
		// continues to pick it up — but the per-finding card carries its
		// own page label too so a card scanned in isolation makes sense.
		seen := map[string]bool{}
		var order []string
		byPage := map[string][]analyzer.ScreenshotGap{}
		for _, g := range bucket {
			if !seen[g.PageURL] {
				seen[g.PageURL] = true
				order = append(order, g.PageURL)
			}
			byPage[g.PageURL] = append(byPage[g.PageURL], g)
		}
		for _, page := range order {
			if emitAnchor {
				fmt.Fprintf(sb, "#### %s {#%s}\n\n", page, urlAnchor(page))
			} else {
				fmt.Fprintf(sb, "#### %s\n\n", page)
			}
			for _, g := range byPage[page] {
				render(sb, g, priorityClass(p))
			}
		}
		sb.WriteString("</div>\n\n")
	}
}

func renderMissingGap(sb *strings.Builder, g analyzer.ScreenshotGap, prio string) {
	fmt.Fprintf(sb, `<div class="ftg-shot ftg-shot--%s">`+"\n", prio)
	fmt.Fprintf(sb, `<div class="ftg-shot-head"><a href="%s">%s</a></div>`+"\n", g.PageURL, g.PageURL)
	sb.WriteString(`<div class="ftg-shot-body">` + "\n\n")
	sb.WriteString(`<div class="ftg-shot-section ftg-shot-passage"><span class="ftg-shot-label">Passage</span>` + "\n\n")
	fmt.Fprintf(sb, "%s\n\n", fencedCodeBlock(g.QuotedPassage))
	sb.WriteString("</div>\n")
	fmt.Fprintf(sb, `<div class="ftg-shot-section"><span class="ftg-shot-label">Should show</span>%s</div>`+"\n", g.ShouldShow)
	fmt.Fprintf(sb, `<div class="ftg-shot-section"><span class="ftg-shot-label">Alt text</span><code>%s</code></div>`+"\n", g.SuggestedAlt)
	fmt.Fprintf(sb, `<div class="ftg-shot-section"><span class="ftg-shot-label">Insert</span>%s</div>`+"\n", g.InsertionHint)
	if g.PriorityReason != "" {
		fmt.Fprintf(sb, `<div class="ftg-shot-section"><span class="ftg-shot-label">Why</span>%s</div>`+"\n", g.PriorityReason)
	}
	sb.WriteString("</div>\n</div>\n\n")
}

func renderPossiblyCoveredGap(sb *strings.Builder, g analyzer.ScreenshotGap, prio string) {
	fmt.Fprintf(sb, `<div class="ftg-shot ftg-shot--%s">`+"\n", prio)
	fmt.Fprintf(sb, `<div class="ftg-shot-head"><a href="%s">%s</a></div>`+"\n", g.PageURL, g.PageURL)
	sb.WriteString(`<div class="ftg-shot-body">` + "\n\n")
	sb.WriteString(`<div class="ftg-shot-section ftg-shot-passage"><span class="ftg-shot-label">Passage</span>` + "\n\n")
	fmt.Fprintf(sb, "%s\n\n", fencedCodeBlock(g.QuotedPassage))
	sb.WriteString("</div>\n")
	fmt.Fprintf(sb, `<div class="ftg-shot-section"><span class="ftg-shot-label">Would have suggested</span>%s</div>`+"\n", g.ShouldShow)
	fmt.Fprintf(sb, `<div class="ftg-shot-section"><span class="ftg-shot-label">Insert (if uncovered)</span>%s</div>`+"\n", g.InsertionHint)
	if g.PriorityReason != "" {
		fmt.Fprintf(sb, `<div class="ftg-shot-section"><span class="ftg-shot-label">Why</span>%s</div>`+"\n", g.PriorityReason)
	}
	sb.WriteString("</div>\n</div>\n\n")
}

// writeImageIssueBuckets emits priority sub-headings (color-wrapped) and
// renders each issue as an `.ftg-shot` card so the screenshots page has a
// single visual language for findings.
func writeImageIssueBuckets(sb *strings.Builder, issues []analyzer.ImageIssue) {
	for _, p := range []analyzer.Priority{analyzer.PriorityLarge, analyzer.PriorityMedium, analyzer.PrioritySmall} {
		bucket := filterImageIssuesByPriority(issues, p)
		if len(bucket) == 0 {
			continue
		}
		fmt.Fprintf(sb, "<div class=\"ftg-priority ftg-priority--%s\">\n\n### %s\n\n</div>\n\n", priorityClass(p), priorityHeading(p))
		sb.WriteString(`<div class="ftg-shot-list">` + "\n\n")
		seen := map[string]bool{}
		var order []string
		byPage := map[string][]analyzer.ImageIssue{}
		for _, ii := range bucket {
			if !seen[ii.PageURL] {
				seen[ii.PageURL] = true
				order = append(order, ii.PageURL)
			}
			byPage[ii.PageURL] = append(byPage[ii.PageURL], ii)
		}
		for _, page := range order {
			fmt.Fprintf(sb, "#### %s\n\n", page)
			for _, ii := range byPage[page] {
				fmt.Fprintf(sb, `<div class="ftg-shot ftg-shot--%s">`+"\n", priorityClass(p))
				fmt.Fprintf(sb, `<div class="ftg-shot-head"><a href="%s">%s</a></div>`+"\n", ii.PageURL, ii.PageURL)
				sb.WriteString(`<div class="ftg-shot-body">` + "\n\n")
				sb.WriteString(`<div class="ftg-shot-section ftg-shot-image"><span class="ftg-shot-label">Image</span>` + "\n\n")
				fmt.Fprintf(sb, "![%s](%s)\n\n", ii.Index, ii.Src)
				sb.WriteString("</div>\n")
				fmt.Fprintf(sb, `<div class="ftg-shot-section"><span class="ftg-shot-label">Issue</span>%s</div>`+"\n", ii.Reason)
				fmt.Fprintf(sb, `<div class="ftg-shot-section"><span class="ftg-shot-label">Suggested action</span>%s</div>`+"\n", ii.SuggestedAction)
				if ii.PriorityReason != "" {
					fmt.Fprintf(sb, `<div class="ftg-shot-section"><span class="ftg-shot-label">Why</span>%s</div>`+"\n", ii.PriorityReason)
				}
				sb.WriteString("</div>\n</div>\n\n")
			}
		}
		sb.WriteString("</div>\n\n")
	}
}

// urlAnchor produces a deterministic, lowercase, kebab-case anchor id from a
// URL. Hugo's default heading-id generator returns an empty id when a heading
// is composed entirely of an autolinked URL — the inline permalink span and
// the right-hand TOC then both end up pointing at "#". Emitting an explicit
// `{#anchor}` heading attribute bypasses the auto-id step and gives the page
// stable per-section anchors.
func urlAnchor(s string) string {
	var b strings.Builder
	prevDash := true
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 32)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// fencedCodeBlock wraps s in a markdown code fence whose backtick run is one
// longer than the longest backtick run inside s. Why: passages quoted from
// docs frequently contain triple-backtick fenced blocks of their own; a fixed
// 3-backtick fence would terminate prematurely on the inner ```. Newlines,
// tabs, quotes, and backslashes are preserved verbatim — unlike the %q format
// verb, which escapes them and produced literal `\n`, `\"`, `\\` text in the
// rendered output.
func fencedCodeBlock(s string) string {
	longest, current := 0, 0
	for _, r := range s {
		if r == '`' {
			current++
			if current > longest {
				longest = current
			}
		} else {
			current = 0
		}
	}
	fenceLen := max(longest+1, 3)
	fence := strings.Repeat("`", fenceLen)
	return fence + "\n" + s + "\n" + fence
}
