package reporter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// WriteMapping writes mapping.md to dir.
func WriteMapping(dir string, summary analyzer.ProductSummary, mapping analyzer.FeatureMap, pages []analyzer.PageAnalysis) error {
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

		// Compute doc status and collect matching page URLs.
		docStatus := "undocumented"
		var docPages []string
		for _, p := range pages {
			for _, f := range p.Features {
				if f == entry.Feature.Name {
					docStatus = "documented"
					docPages = append(docPages, p.URL)
					break
				}
			}
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
func WriteGaps(dir string, mapping analyzer.FeatureMap, allDocFeatures []string, drift []analyzer.DriftFinding) error {
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
					fmt.Fprintf(&sb, "- **Page:** %s\n  %s\n\n", issue.Page, issue.Issue)
				} else {
					fmt.Fprintf(&sb, "- %s\n\n", issue.Issue)
				}
			}
		}
	}

	return os.WriteFile(filepath.Join(dir, "gaps.md"), []byte(sb.String()), 0o644)
}
