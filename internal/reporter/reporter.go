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

	// Stale documentation: inaccuracies found in pages that DO cover a feature.
	sb.WriteString("\n## Stale Documentation\n\n")
	if len(drift) == 0 {
		sb.WriteString("_None found._\n")
	} else {
		for _, finding := range drift {
			fmt.Fprintf(&sb, "### %s\n\n", finding.Feature)
			for _, issue := range finding.Issues {
				if issue.Page != "" {
					fmt.Fprintf(&sb, "- %s — %s\n\n", issue.Page, issue.Issue)
				} else {
					fmt.Fprintf(&sb, "- %s\n\n", issue.Issue)
				}
			}
		}
	}

	return os.WriteFile(filepath.Join(dir, "gaps.md"), []byte(sb.String()), 0o644)
}

// WriteScreenshots writes screenshots.md to dir. Call ONLY when the screenshot
// pass actually ran — a skipped pass must produce NO file. Zero-length gaps is
// valid and produces a "_None found._" body.
func WriteScreenshots(dir string, gaps []analyzer.ScreenshotGap) error {
	var sb strings.Builder
	sb.WriteString("# Missing Screenshots\n\n")

	if len(gaps) == 0 {
		sb.WriteString("_None found._\n")
		return os.WriteFile(filepath.Join(dir, "screenshots.md"), []byte(sb.String()), 0o644)
	}

	// Preserve first-occurrence page order.
	seen := map[string]bool{}
	var order []string
	byPage := map[string][]analyzer.ScreenshotGap{}
	for _, g := range gaps {
		if !seen[g.PageURL] {
			seen[g.PageURL] = true
			order = append(order, g.PageURL)
		}
		byPage[g.PageURL] = append(byPage[g.PageURL], g)
	}
	for _, page := range order {
		fmt.Fprintf(&sb, "### %s\n\n", page)
		for _, g := range byPage[page] {
			fmt.Fprintf(&sb, "- **Passage:** %q\n", g.QuotedPassage)
			fmt.Fprintf(&sb, "  - **Screenshot should show:** %s\n", g.ShouldShow)
			fmt.Fprintf(&sb, "  - **Alt text:** %s\n", g.SuggestedAlt)
			fmt.Fprintf(&sb, "  - **Insert:** %s\n\n", g.InsertionHint)
		}
	}
	return os.WriteFile(filepath.Join(dir, "screenshots.md"), []byte(sb.String()), 0o644)
}
