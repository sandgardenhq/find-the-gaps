package reporter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
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
		fmt.Fprintf(&sb, "### %s\n", entry.Feature)
		if len(entry.Files) > 0 {
			fmt.Fprintf(&sb, "- **Implemented in:** %s\n", strings.Join(entry.Files, ", "))
		}
		if len(entry.Symbols) > 0 {
			fmt.Fprintf(&sb, "- **Symbols:** %s\n", strings.Join(entry.Symbols, ", "))
		}
		// Find doc pages that mention this feature
		for _, p := range pages {
			for _, f := range p.Features {
				if f == entry.Feature {
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
// It identifies exported symbols with no feature mapping and features with no code mapping.
func WriteGaps(dir string, scan *scanner.ProjectScan, mapping analyzer.FeatureMap, allDocFeatures []string) error {
	// Build set of symbols that appear in any feature mapping
	mappedSymbols := make(map[string]bool)
	for _, entry := range mapping {
		for _, sym := range entry.Symbols {
			mappedSymbols[sym] = true
		}
	}

	// Build set of features that have at least one file mapped
	mappedFeatures := make(map[string]bool)
	for _, entry := range mapping {
		if len(entry.Files) > 0 {
			mappedFeatures[entry.Feature] = true
		}
	}

	var sb strings.Builder
	sb.WriteString("# Gaps Found\n\n")

	// Undocumented code: exported symbols not in any feature mapping
	sb.WriteString("## Undocumented Code\n\n")
	found := false
	for _, f := range scan.Files {
		for _, sym := range f.Symbols {
			if sym.Kind != scanner.KindFunc && sym.Kind != scanner.KindType && sym.Kind != scanner.KindInterface {
				continue
			}
			if isExported(sym.Name) && !mappedSymbols[sym.Name] {
				fmt.Fprintf(&sb, "- `%s` in `%s` — no documentation page covers this symbol\n", sym.Name, f.Path)
				found = true
			}
		}
	}
	if !found {
		sb.WriteString("_None found._\n")
	}

	// Unmapped features: doc features with no code match
	sb.WriteString("\n## Unmapped Features\n\n")
	found = false
	for _, feat := range allDocFeatures {
		if !mappedFeatures[feat] {
			fmt.Fprintf(&sb, "- \"%s\" mentioned in docs — no code match found\n", feat)
			found = true
		}
	}
	if !found {
		sb.WriteString("_None found._\n")
	}

	return os.WriteFile(filepath.Join(dir, "gaps.md"), []byte(sb.String()), 0o644)
}

func isExported(name string) bool {
	if len(name) == 0 {
		return false
	}
	return name[0] >= 'A' && name[0] <= 'Z'
}
