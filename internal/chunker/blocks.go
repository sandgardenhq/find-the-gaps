package chunker

import "strings"

type blockKind int

const (
	blockParagraph blockKind = iota
	blockHeading
	blockCode
	blockTable
	blockList
	blockBlank
)

// block describes one classified line. depth is heading depth for blockHeading,
// 0 otherwise. fenceID groups lines that belong to the same atomic block
// (fenced code, table, list item) so the splitter treats them as one unit.
type block struct {
	kind    blockKind
	depth   int
	text    string
	fenceID int
}

// classifyLines walks markdown line-by-line and tags each with its block kind.
// State machine handles fenced code, tables, and list nesting.
func classifyLines(s string) []block {
	lines := strings.Split(s, "\n")
	out := make([]block, 0, len(lines))
	var (
		inFence  bool
		fenceTag int
		nextID   int
	)
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		// Fenced code block toggle. A ``` line is part of the fence itself.
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			if !inFence {
				nextID++
				fenceTag = nextID
				inFence = true
			} else {
				out = append(out, block{kind: blockCode, text: line, fenceID: fenceTag})
				inFence = false
				continue
			}
			out = append(out, block{kind: blockCode, text: line, fenceID: fenceTag})
			continue
		}
		if inFence {
			out = append(out, block{kind: blockCode, text: line, fenceID: fenceTag})
			continue
		}
		if trim == "" {
			out = append(out, block{kind: blockBlank, text: ""})
			continue
		}
		if strings.HasPrefix(trim, "#") {
			depth := 0
			for depth < len(trim) && trim[depth] == '#' {
				depth++
			}
			out = append(out, block{kind: blockHeading, depth: depth, text: line})
			continue
		}
		if strings.HasPrefix(trim, "|") && strings.Contains(trim, "|") {
			// Heuristic: a row that starts AND contains pipes is a table row.
			// Group consecutive table rows under one fenceID.
			if len(out) > 0 && out[len(out)-1].kind == blockTable {
				out = append(out, block{kind: blockTable, text: line, fenceID: out[len(out)-1].fenceID})
			} else {
				nextID++
				out = append(out, block{kind: blockTable, text: line, fenceID: nextID})
			}
			continue
		}
		if isListMarker(trim) {
			if len(out) > 0 && out[len(out)-1].kind == blockList {
				out = append(out, block{kind: blockList, text: line, fenceID: out[len(out)-1].fenceID})
			} else {
				nextID++
				out = append(out, block{kind: blockList, text: line, fenceID: nextID})
			}
			continue
		}
		out = append(out, block{kind: blockParagraph, text: line})
	}
	return out
}

func isListMarker(s string) bool {
	if strings.HasPrefix(s, "- ") || strings.HasPrefix(s, "* ") || strings.HasPrefix(s, "+ ") {
		return true
	}
	// numeric markers "1. ", "12. "
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return i > 0 && i+1 < len(s) && s[i] == '.' && s[i+1] == ' '
}
