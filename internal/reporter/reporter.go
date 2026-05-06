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
// Undocumented Code: features with a code implementation but no documentation page.
// Unmapped Features: features mentioned in docs with no code match.
// Stale Documentation: inaccuracies found in pages that DO cover a feature.
func WriteGaps(
	dir string,
	mapping analyzer.FeatureMap,
	allDocFeatures []string,
	drift []analyzer.DriftFinding,
) error {
	codeFeatures := make(map[string]bool)
	for _, entry := range mapping {
		if len(entry.Files) > 0 {
			codeFeatures[entry.Feature.Name] = true
		}
	}
	docFeatures := make(map[string]bool)
	for _, f := range allDocFeatures {
		docFeatures[f] = true
	}

	var sb strings.Builder
	sb.WriteString("# Gaps Found\n\n")

	// Undocumented code: features implemented in code but missing from docs.
	// Split into user-facing and not user-facing subsections.
	sb.WriteString("## Undocumented Code\n\n")

	sb.WriteString("### User-facing\n\n")
	found := false
	for _, entry := range mapping {
		if len(entry.Files) > 0 && !docFeatures[entry.Feature.Name] && entry.Feature.UserFacing {
			fmt.Fprintf(&sb, "- \"%s\" has code implementation but no documentation page\n", entry.Feature.Name)
			found = true
		}
	}
	if !found {
		sb.WriteString("_None found._\n")
	}

	sb.WriteString("\n### Not user-facing\n\n")
	found = false
	for _, entry := range mapping {
		if len(entry.Files) > 0 && !docFeatures[entry.Feature.Name] && !entry.Feature.UserFacing {
			fmt.Fprintf(&sb, "- \"%s\" has code implementation but no documentation page\n", entry.Feature.Name)
			found = true
		}
	}
	if !found {
		sb.WriteString("_None found._\n")
	}

	// Unmapped features: documented features with no code match.
	sb.WriteString("\n## Unmapped Features\n\n")
	found = false
	for _, feat := range allDocFeatures {
		if !codeFeatures[feat] {
			fmt.Fprintf(&sb, "- \"%s\" mentioned in docs — no code match found\n", feat)
			found = true
		}
	}
	if !found {
		sb.WriteString("_None found._\n")
	}

	// Stale documentation: inaccuracies found in pages that DO cover a feature,
	// grouped by priority (Large → Medium → Small). Empty buckets are omitted.
	// Within a bucket, findings appear in stable original order (feature first,
	// then issue position within that feature).
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
			fmt.Fprintf(&sb, "### %s\n\n", priorityHeading(p))
			for _, item := range bucket {
				if item.Issue.Page != "" {
					fmt.Fprintf(&sb, "- **%s** — %s — %s\n", item.Feature, item.Issue.Page, item.Issue.Issue)
				} else {
					fmt.Fprintf(&sb, "- **%s** — %s\n", item.Feature, item.Issue.Issue)
				}
				if item.Issue.PriorityReason != "" {
					fmt.Fprintf(&sb, "  _why: %s_\n", item.Issue.PriorityReason)
				}
				sb.WriteString("\n")
			}
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

// gapRenderer renders one ScreenshotGap into the builder. Used to differentiate
// missing-screenshot rendering (Insert hint) from possibly-covered rendering
// (Insert if uncovered hint) without duplicating bucket plumbing.
type gapRenderer func(*strings.Builder, analyzer.ScreenshotGap)

// writeGapBuckets emits "### Large/Medium/Small" sub-headings, omitting empty
// buckets, then within each bucket emits "#### <pageURL>" page headings (in
// stable input order) followed by the rendered gap items via render. Anchors
// are emitted only on the missing-screenshots side because the existing inline
// permalinks/TOC integration only reads anchors from that section.
func writeGapBuckets(sb *strings.Builder, gaps []analyzer.ScreenshotGap, render gapRenderer, emitAnchor bool) {
	for _, p := range []analyzer.Priority{analyzer.PriorityLarge, analyzer.PriorityMedium, analyzer.PrioritySmall} {
		bucket := filterGapsByPriority(gaps, p)
		if len(bucket) == 0 {
			continue
		}
		fmt.Fprintf(sb, "### %s\n\n", priorityHeading(p))
		// Page grouping inside the bucket, first-occurrence order.
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
				render(sb, g)
			}
		}
	}
}

func renderMissingGap(sb *strings.Builder, g analyzer.ScreenshotGap) {
	fmt.Fprintf(sb, "- **Passage:**\n\n")
	fmt.Fprintf(sb, "%s\n\n", fencedCodeBlock(g.QuotedPassage))
	fmt.Fprintf(sb, "  - **Screenshot should show:** %s\n", g.ShouldShow)
	fmt.Fprintf(sb, "  - **Alt text:** %s\n", g.SuggestedAlt)
	fmt.Fprintf(sb, "  - **Insert:** %s\n", g.InsertionHint)
	if g.PriorityReason != "" {
		fmt.Fprintf(sb, "  - **Why:** %s\n", g.PriorityReason)
	}
	sb.WriteString("\n")
}

func renderPossiblyCoveredGap(sb *strings.Builder, g analyzer.ScreenshotGap) {
	fmt.Fprintf(sb, "- **Passage:**\n\n")
	fmt.Fprintf(sb, "%s\n\n", fencedCodeBlock(g.QuotedPassage))
	fmt.Fprintf(sb, "  - **Would have suggested:** %s\n", g.ShouldShow)
	fmt.Fprintf(sb, "  - **Insert (if uncovered):** %s\n", g.InsertionHint)
	if g.PriorityReason != "" {
		fmt.Fprintf(sb, "  - **Why:** %s\n", g.PriorityReason)
	}
	sb.WriteString("\n")
}

// writeImageIssueBuckets emits Large/Medium/Small sub-headings for image
// issues, then within each bucket groups by page (first-occurrence order).
func writeImageIssueBuckets(sb *strings.Builder, issues []analyzer.ImageIssue) {
	for _, p := range []analyzer.Priority{analyzer.PriorityLarge, analyzer.PriorityMedium, analyzer.PrioritySmall} {
		bucket := filterImageIssuesByPriority(issues, p)
		if len(bucket) == 0 {
			continue
		}
		fmt.Fprintf(sb, "### %s\n\n", priorityHeading(p))
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
				fmt.Fprintf(sb, "- **Image:** ![%s](%s)\n", ii.Index, ii.Src)
				fmt.Fprintf(sb, "  **Issue:** %s\n", ii.Reason)
				fmt.Fprintf(sb, "  **Suggested action:** %s\n", ii.SuggestedAction)
				if ii.PriorityReason != "" {
					fmt.Fprintf(sb, "  **Why:** %s\n", ii.PriorityReason)
				}
				sb.WriteString("\n")
			}
		}
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
