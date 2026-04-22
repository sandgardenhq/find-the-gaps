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
		fmt.Fprintf(&sb, "### %s\n", entry.Feature.Name)
		if len(entry.Files) > 0 {
			fmt.Fprintf(&sb, "- **Implemented in:** %s\n", strings.Join(entry.Files, ", "))
		}
		if len(entry.Symbols) > 0 {
			fmt.Fprintf(&sb, "- **Symbols:** %s\n", strings.Join(entry.Symbols, ", "))
		}
		// Find doc pages that mention this feature
		for _, p := range pages {
			for _, f := range p.Features {
				if f == entry.Feature.Name {
					fmt.Fprintf(&sb, "- **Documented on:** %s\n", p.URL)
					break
				}
			}
		}
		sb.WriteString("\n")
	}

	return os.WriteFile(filepath.Join(dir, "mapping.md"), []byte(sb.String()), 0o644)
}

// WriteGaps writes gaps.md to dir.
// Undocumented Code: features with a code implementation but no documentation page.
// Unmapped Features: features mentioned in docs with no code match.
func WriteGaps(dir string, mapping analyzer.FeatureMap, allDocFeatures []string) error {
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
	sb.WriteString("## Undocumented Code\n\n")
	found := false
	for _, entry := range mapping {
		if len(entry.Files) > 0 && !docFeatures[entry.Feature.Name] {
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

	return os.WriteFile(filepath.Join(dir, "gaps.md"), []byte(sb.String()), 0o644)
}
