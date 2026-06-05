package docholiday

import (
	"fmt"
	"strings"
)

// renderUnitFindings produces the deterministic facts block appended to the
// skill body to form the full LLM prompt. It is the "user message" half of the
// call (analyzer.LLMClient.Complete takes one flat string, so we concatenate).
func renderUnitFindings(u unit) string {
	var sb strings.Builder
	switch u.category {
	case CategoryStale:
		page := u.page
		if page == crossPageKey {
			page = "(cross-cutting; affects multiple or unspecified pages)"
		}
		fmt.Fprintf(&sb, "Documentation page: %s\n\n", page)
		sb.WriteString("Claims on this page that no longer match the code:\n")
		for _, it := range u.staleItems {
			fmt.Fprintf(&sb, "- [%s] %s\n", it.feature, it.issue)
		}
	case CategoryMissing:
		f := u.feature
		fmt.Fprintf(&sb, "Undocumented feature: %s\n", f.Feature.Name)
		if f.Feature.Description != "" {
			fmt.Fprintf(&sb, "Description: %s\n", f.Feature.Description)
		}
		if len(f.Files) > 0 {
			fmt.Fprintf(&sb, "Implemented in:\n")
			for _, file := range f.Files {
				fmt.Fprintf(&sb, "- %s\n", file)
			}
		}
		if len(f.Symbols) > 0 {
			fmt.Fprintf(&sb, "Key symbols: %s\n", strings.Join(f.Symbols, ", "))
		}
		if strings.TrimSpace(u.rationale) != "" {
			fmt.Fprintf(&sb, "Why it matters: %s\n", u.rationale)
		}
	}
	return sb.String()
}

// unitHeading is the human-readable "####" heading shown above the prompt block.
func unitHeading(u unit) string {
	switch u.category {
	case CategoryMissing:
		return "New page: " + u.feature.Feature.Name
	default:
		page := u.page
		if page == crossPageKey {
			page = "Cross-cutting documentation"
		}
		h := page
		if extra := len(u.staleItems) - 1; extra > 0 {
			h += fmt.Sprintf(" (+%d more)", extra)
		}
		if u.parts > 1 {
			h += fmt.Sprintf(" (part %d of %d)", u.part, u.parts)
		}
		return h
	}
}

// unitNote is the one-line italic note rendered under the heading.
func unitNote(u unit) string {
	switch u.category {
	case CategoryMissing:
		return "No docs page covers this user-facing feature."
	default:
		n := len(u.staleItems)
		plural := "issue"
		if n != 1 {
			plural = "issues"
		}
		return fmt.Sprintf("Addresses %d stale-doc %s on this page.", n, plural)
	}
}
