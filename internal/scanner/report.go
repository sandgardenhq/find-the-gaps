package scanner

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// GenerateReport produces a project.md string from a ProjectScan.
func GenerateReport(scan *ProjectScan) string {
	var b strings.Builder

	repoName := filepath.Base(scan.RepoPath)
	fmt.Fprintf(&b, "# %s\n\n", repoName)
	fmt.Fprintf(&b, "**Path:** `%s`\n", scan.RepoPath)
	if !scan.ScannedAt.IsZero() {
		fmt.Fprintf(&b, "**Scanned:** %s\n", scan.ScannedAt.Format("2006-01-02 15:04:05 UTC"))
	}
	if len(scan.Languages) > 0 {
		fmt.Fprintf(&b, "**Languages:** %s\n", strings.Join(scan.Languages, ", "))
	}
	b.WriteString("\n")

	// Summary
	totalSymbols := 0
	for _, f := range scan.Files {
		totalSymbols += len(f.Symbols)
	}
	b.WriteString("## Summary\n\n")
	b.WriteString("| Metric | Count |\n|--------|-------|\n")
	fmt.Fprintf(&b, "| Files | %d |\n", len(scan.Files))
	fmt.Fprintf(&b, "| Exported Symbols | %d |\n", totalSymbols)
	fmt.Fprintf(&b, "| Internal Dependencies | %d |\n\n", len(scan.Graph.Edges))

	// Directory structure
	if len(scan.Files) > 0 {
		b.WriteString("## Directory Structure\n\n")
		b.WriteString("| Path | Language | Lines | Key Exports |\n|------|----------|-------|-------------|\n")
		dirs := make(map[string][]ScannedFile)
		for _, f := range scan.Files {
			dir := filepath.Dir(f.Path)
			dirs[dir] = append(dirs[dir], f)
		}
		dirKeys := make([]string, 0, len(dirs))
		for k := range dirs {
			dirKeys = append(dirKeys, k)
		}
		sort.Strings(dirKeys)
		for _, dir := range dirKeys {
			files := dirs[dir]
			var lang string
			var lines int
			var exports []string
			for _, f := range files {
				lang = f.Language
				lines += f.Lines
				for _, s := range f.Symbols {
					exports = append(exports, fmt.Sprintf("`%s`", s.Name))
				}
			}
			exportStr := strings.Join(exports, ", ")
			if exportStr == "" {
				exportStr = "—"
			}
			fmt.Fprintf(&b, "| `%s` | %s | %d | %s |\n", dir, lang, lines, exportStr)
		}
		b.WriteString("\n")
	}

	// Public API
	if totalSymbols > 0 {
		b.WriteString("## Public API\n\n")
		for _, f := range scan.Files {
			if len(f.Symbols) == 0 {
				continue
			}
			fmt.Fprintf(&b, "### `%s` (%s)\n\n", filepath.Dir(f.Path), f.Language)
			for _, s := range f.Symbols {
				fmt.Fprintf(&b, "#### `%s`\n\n", s.Signature)
				if s.DocComment != "" {
					fmt.Fprintf(&b, "> %s\n\n", s.DocComment)
				}
			}
		}
	}

	// Import graph
	b.WriteString("## Import Graph\n\n")
	if len(scan.Graph.Edges) > 0 {
		b.WriteString("```mermaid\ngraph TD\n")
		for _, e := range scan.Graph.Edges {
			fmt.Fprintf(&b, "  %q --> %q\n", e.From, e.To)
		}
		b.WriteString("```\n\n")
	} else {
		b.WriteString("_No internal dependencies detected._\n\n")
	}

	// Files table
	if len(scan.Files) > 0 {
		b.WriteString("## Files\n\n")
		b.WriteString("| File | Language | Lines | Exports | Imports |\n|------|----------|-------|---------|--------|\n")
		for _, f := range scan.Files {
			fmt.Fprintf(&b, "| `%s` | %s | %d | %d | %d |\n",
				f.Path, f.Language, f.Lines, len(f.Symbols), len(f.Imports))
		}
		b.WriteString("\n")
	}

	return b.String()
}
