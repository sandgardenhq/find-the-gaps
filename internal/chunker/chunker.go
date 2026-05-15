// Package chunker provides structure-aware markdown splitting for LLM prompts.
// It uses the same cl100k_base tokenizer as internal/analyzer so estimates
// agree across packages.
package chunker

import (
	"strings"

	"github.com/tiktoken-go/tokenizer"
)

var enc = mustEncoder()

func mustEncoder() tokenizer.Codec {
	c, err := tokenizer.Get(tokenizer.Cl100kBase)
	if err != nil {
		panic("chunker: cl100k_base load failed: " + err.Error())
	}
	return c
}

// EstimateTokens returns the approximate cl100k_base token count for s.
// Used at every call site to decide whether to chunk before an LLM call.
func EstimateTokens(s string) int {
	if s == "" {
		return 0
	}
	ids, _, err := enc.Encode(s)
	if err != nil {
		return 1
	}
	return len(ids)
}

// Chunk splits content into chunks each at or under maxTokens, preferring
// boundaries in order: H1, H2, H3, paragraph (blank line), sentence.
// Atomic blocks (fenced code, table, list) are never split mid-block; if
// one exceeds maxTokens on its own it is emitted as a single oversize chunk.
// Callers receive the chunks in source order.
func Chunk(content string, maxTokens int) []string {
	if maxTokens <= 0 || EstimateTokens(content) <= maxTokens {
		return []string{content}
	}
	blocks := classifyLines(content)
	groups := groupAtomic(blocks)

	var (
		chunks           []string
		current          strings.Builder
		curTok           int
		curAwaitingBody  bool
	)
	flush := func() {
		if current.Len() == 0 {
			return
		}
		chunks = append(chunks, strings.TrimRight(current.String(), "\n"))
		current.Reset()
		curTok = 0
		curAwaitingBody = false
	}
	for _, g := range groups {
		gText := renderGroup(g)
		gTok := EstimateTokens(gText)
		// Heading at preferred depth (1-3) is a strong boundary — flush first
		// if we already have content so the heading starts a fresh chunk.
		// BUT skip the flush when the current chunk is still awaiting body —
		// we don't want to orphan a parent heading from its following content
		// (e.g. a `## API` followed by a `### Endpoints` subheading).
		if isHeadingBoundary(g) && current.Len() > 0 && !curAwaitingBody {
			flush()
		}
		// Budget check: flush if adding this group would exceed maxTokens.
		// BUT skip the flush when the current chunk is still awaiting body —
		// we keep a heading attached to its following content even if the
		// combination overflows the budget (better to emit one oversize
		// "heading + body" chunk than a bare heading followed by an orphaned
		// body).
		if curTok+gTok > maxTokens && current.Len() > 0 && !curAwaitingBody {
			flush()
		}
		if current.Len() > 0 {
			current.WriteString("\n")
		}
		current.WriteString(gText)
		curTok += gTok
		// Track whether the current chunk has body content yet. Headings and
		// blank lines leave the chunk "awaiting body"; once any non-heading,
		// non-blank content lands, the flag clears.
		isBlank := len(g.blocks) > 0 && g.blocks[0].kind == blockBlank
		if isHeadingBoundary(g) {
			curAwaitingBody = true
		} else if !isBlank {
			curAwaitingBody = false
		}
	}
	flush()
	return chunks
}

// group is a contiguous run of blocks that the splitter treats as one unit.
// Atomic blocks (code/table/list) become a single group; paragraphs and
// headings each form their own one-element group.
type group struct {
	blocks []block
}

func groupAtomic(bs []block) []group {
	var out []group
	i := 0
	for i < len(bs) {
		b := bs[i]
		if b.fenceID != 0 {
			j := i + 1
			for j < len(bs) && bs[j].fenceID == b.fenceID {
				j++
			}
			out = append(out, group{blocks: bs[i:j]})
			i = j
			continue
		}
		out = append(out, group{blocks: bs[i : i+1]})
		i++
	}
	return out
}

func renderGroup(g group) string {
	parts := make([]string, 0, len(g.blocks))
	for _, b := range g.blocks {
		parts = append(parts, b.text)
	}
	return strings.Join(parts, "\n")
}

func isHeadingBoundary(g group) bool {
	if len(g.blocks) != 1 {
		return false
	}
	b := g.blocks[0]
	return b.kind == blockHeading && b.depth >= 1 && b.depth <= 3
}
