# Context Window Overflow Remediation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace post-failure overflow detection at every HIGH-severity LLM call site with preemptive, structure-aware chunking. Eliminate silent page skips and reactive compaction retries.

**Architecture:** A new `internal/chunker` package owns the structure-aware markdown splitter (heading/paragraph/sentence boundaries, with code-fence/table/list protection). The five HIGH-severity call sites in `internal/analyzer` switch from "send and pray, catch `ErrTokenBudgetExceeded`" to "estimate, chunk if needed, per-chunk LLM call, merge results." The drift investigator additionally compresses its system prompt and exposes full symbol/page lists via two new tools. The synthesize step adds per-page summary compression with a map-reduce fallback for very large docs sites.

**Tech Stack:** Go 1.26+, `github.com/tiktoken-go/tokenizer` (cl100k_base, already in use), testify, testscript.

**Affected files (read for context before starting):**
- `internal/analyzer/tokens.go` — existing token counter
- `internal/analyzer/analyze_page.go` — HIGH #1
- `internal/analyzer/drift.go` — HIGH #2 (investigator @ L422), HIGH #3 (judge @ L726)
- `internal/analyzer/screenshot_gaps.go` — HIGH #4 (detection @ L730, L853)
- `internal/analyzer/synthesize.go` — HIGH #5 (synthesis @ L39)
- `internal/analyzer/budgeted_client.go` — `ErrTokenBudgetExceeded` source
- `.plans/CONTEXT_OVERFLOW_AUDIT.md` — original audit (severity rationale)

**Coverage gate:** Every new file and every modified function must keep its package at ≥90% statement coverage (`go test -cover ./...`).

**Commit cadence:** Commit after every RED-GREEN-REFACTOR cycle. Never more than ~30 minutes between commits.

---

## Phase 1 — Shared chunker package

### Task 1: Create the `chunker` package skeleton

**Files:**
- Create: `internal/chunker/chunker.go`
- Create: `internal/chunker/chunker_test.go`

**Step 1: Write the failing test**

```go
package chunker

import "testing"

func TestEstimateTokens_EmptyString(t *testing.T) {
	if got := EstimateTokens(""); got != 0 {
		t.Fatalf("EstimateTokens(\"\") = %d, want 0", got)
	}
}

func TestEstimateTokens_KnownInput(t *testing.T) {
	// "hello world" tokenizes to 2 tokens under cl100k_base.
	if got := EstimateTokens("hello world"); got != 2 {
		t.Fatalf("EstimateTokens(\"hello world\") = %d, want 2", got)
	}
}
```

**Step 2: Run test, watch it fail**

```bash
go test ./internal/chunker/...
```
Expected: FAIL — `EstimateTokens` undefined.

**Step 3: Implement minimal code**

```go
// Package chunker provides structure-aware markdown splitting for LLM prompts.
// It uses the same cl100k_base tokenizer as internal/analyzer so estimates
// agree across packages.
package chunker

import "github.com/tiktoken-go/tokenizer"

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
```

**Step 4: Run test, verify it passes**

```bash
go test ./internal/chunker/...
```
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/chunker/
git commit -m "feat(chunker): scaffold package with token estimator

- RED: tests for EstimateTokens (empty + known input)
- GREEN: cl100k_base estimator mirroring internal/analyzer/tokens.go
- Status: 2 tests passing, build successful"
```

---

### Task 2: Block classifier — tag markdown lines by block type

**Why:** The chunker walks line-by-line and needs to know what kind of block each line belongs to so it can avoid splitting inside fences/tables/lists.

**Files:**
- Create: `internal/chunker/blocks.go`
- Create: `internal/chunker/blocks_test.go`

**Step 1: Write the failing tests**

```go
package chunker

import (
	"reflect"
	"testing"
)

func TestClassifyLines_HeadingsAndParagraph(t *testing.T) {
	in := "# Title\n\nIntro paragraph.\n\n## Sub\n\nBody."
	want := []block{
		{kind: blockHeading, depth: 1, text: "# Title"},
		{kind: blockBlank, text: ""},
		{kind: blockParagraph, text: "Intro paragraph."},
		{kind: blockBlank, text: ""},
		{kind: blockHeading, depth: 2, text: "## Sub"},
		{kind: blockBlank, text: ""},
		{kind: blockParagraph, text: "Body."},
	}
	got := classifyLines(in)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("classifyLines mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestClassifyLines_FencedCodeBlockIsAtomic(t *testing.T) {
	in := "Before\n\n```go\nfunc x() {}\n```\n\nAfter"
	got := classifyLines(in)
	// The fenced block (3 lines) must share one fenceID and be marked blockCode.
	var fenceID int
	codeLines := 0
	for _, b := range got {
		if b.kind == blockCode {
			codeLines++
			if fenceID == 0 {
				fenceID = b.fenceID
			} else if b.fenceID != fenceID {
				t.Fatalf("expected all code lines to share fenceID, got %d and %d", fenceID, b.fenceID)
			}
		}
	}
	if codeLines != 3 {
		t.Fatalf("expected 3 code lines, got %d", codeLines)
	}
}

func TestClassifyLines_TableAtomic(t *testing.T) {
	in := "| a | b |\n|---|---|\n| 1 | 2 |"
	got := classifyLines(in)
	for _, b := range got {
		if b.kind != blockTable {
			t.Fatalf("expected all lines to be blockTable, got %v", b.kind)
		}
	}
}
```

**Step 2: Run, watch fail**

```bash
go test ./internal/chunker/...
```
Expected: FAIL — `classifyLines`, `block`, `blockKind` undefined.

**Step 3: Implement**

```go
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
```

**Step 4: Run, verify pass**

```bash
go test ./internal/chunker/...
```
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/chunker/
git commit -m "feat(chunker): classify markdown lines by block kind

- RED: tests for headings, fenced code, table grouping
- GREEN: line-by-line state machine tagging block kind and fence groups
- Status: 5 tests passing"
```

---

### Task 3: `Chunk()` — greedy packer with boundary preference

**Files:**
- Modify: `internal/chunker/chunker.go`
- Create: `internal/chunker/chunk_test.go`

**Step 1: Write the failing tests**

```go
package chunker

import (
	"strings"
	"testing"
)

func TestChunk_SmallInput_ReturnsSinglePiece(t *testing.T) {
	in := "# Title\n\nShort paragraph."
	chunks := Chunk(in, 1000)
	if len(chunks) != 1 || chunks[0] != in {
		t.Fatalf("expected single unchanged chunk, got %d chunks: %q", len(chunks), chunks)
	}
}

func TestChunk_SplitsAtHeadingBoundary(t *testing.T) {
	// Two H2 sections (49 / 61 tokens, actual cl100k_base counts). Budget 60
	// forces a split between them.
	a := "## A\n\n" + strings.Repeat("alpha beta gamma delta ", 12)
	b := "## B\n\n" + strings.Repeat("epsilon zeta eta theta ", 12)
	in := a + "\n\n" + b
	chunks := Chunk(in, 60)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if !strings.HasPrefix(chunks[0], "## A") || !strings.HasPrefix(chunks[1], "## B") {
		t.Fatalf("chunk boundaries wrong:\n[0]=%q\n[1]=%q", chunks[0], chunks[1])
	}
}

func TestChunk_NeverSplitsInsideFencedCode(t *testing.T) {
	// A fenced block that exceeds the budget on its own must still survive as
	// one atomic chunk (we accept the overflow rather than mangle the fence).
	big := "```go\n" + strings.Repeat("func foo() {}\n", 200) + "```"
	in := "Intro paragraph.\n\n" + big + "\n\nOutro paragraph."
	chunks := Chunk(in, 50)
	for _, c := range chunks {
		opens := strings.Count(c, "```")
		if opens%2 != 0 {
			t.Fatalf("chunk has unbalanced fence:\n%s", c)
		}
	}
}

func TestChunk_PrefersHeadingOverParagraphBoundary(t *testing.T) {
	// Both boundaries are available; chunker should choose the heading first.
	in := "Intro.\n\n## Section\n\nBody one.\n\nBody two."
	chunks := Chunk(in, 4) // tight budget forces an early split
	if len(chunks) < 2 {
		t.Fatalf("expected ≥2 chunks, got %d", len(chunks))
	}
	if !strings.HasPrefix(chunks[1], "## Section") {
		t.Fatalf("expected second chunk to start at heading, got %q", chunks[1])
	}
}
```

**Step 2: Run, watch fail**

```bash
go test ./internal/chunker/...
```
Expected: FAIL — `Chunk` undefined.

**Step 3: Implement**

Add to `internal/chunker/chunker.go`:

```go
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
		chunks  []string
		current strings.Builder
		curTok  int
	)
	flush := func() {
		if current.Len() == 0 {
			return
		}
		chunks = append(chunks, strings.TrimRight(current.String(), "\n"))
		current.Reset()
		curTok = 0
	}
	for _, g := range groups {
		gText := renderGroup(g)
		gTok := EstimateTokens(gText)
		// Heading at preferred depth (1-3) is a strong boundary — flush first
		// if we already have content AND the group would extend the chunk.
		if isHeadingBoundary(g) && current.Len() > 0 {
			flush()
		}
		if curTok+gTok > maxTokens && current.Len() > 0 {
			flush()
		}
		if current.Len() > 0 {
			current.WriteString("\n")
		}
		current.WriteString(gText)
		curTok += gTok
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
```

Add `import "strings"` to the file if not present.

**Step 4: Run, verify pass**

```bash
go test ./internal/chunker/... -v
```
Expected: all tests pass.

**Step 5: Commit**

```bash
git add internal/chunker/
git commit -m "feat(chunker): structure-aware Chunk() with boundary preference

- RED: tests for small input, heading split, fence protection, heading preference
- GREEN: greedy packer using atomic-group rendering
- Status: 9 tests passing"
```

---

### Task 4: `Fit()` — single-payload truncation

**Files:**
- Modify: `internal/chunker/chunker.go`
- Modify: `internal/chunker/chunk_test.go`

**Step 1: Write the failing test**

```go
func TestFit_KeepsHeadingPrefixWhenTruncating(t *testing.T) {
	a := "## Keep\n\n" + strings.Repeat("foo bar baz ", 20)
	b := "## Drop\n\n" + strings.Repeat("qux quux corge ", 50)
	in := a + "\n\n" + b
	fitted := Fit(in, 80)
	if !strings.HasPrefix(fitted, "## Keep") {
		t.Fatalf("expected fitted output to start at H2, got %q", fitted[:30])
	}
	if strings.Contains(fitted, "## Drop") {
		t.Fatalf("expected dropped section to be excluded")
	}
	if EstimateTokens(fitted) > 80 {
		t.Fatalf("fitted output exceeds budget: %d tokens", EstimateTokens(fitted))
	}
}

func TestFit_NoOpWhenUnderBudget(t *testing.T) {
	in := "Hello world."
	if got := Fit(in, 1000); got != in {
		t.Fatalf("Fit should be a no-op under budget; got %q", got)
	}
}
```

**Step 2: Run, watch fail**

```bash
go test ./internal/chunker/...
```
Expected: FAIL — `Fit` undefined.

**Step 3: Implement**

Add to `internal/chunker/chunker.go`:

```go
// Fit returns content truncated to maxTokens at the nearest preferred
// boundary (same precedence as Chunk). If content already fits, it is
// returned unchanged. Use Fit when the caller wants exactly one payload
// instead of a chunk list (e.g. trimming an oversize feature description).
func Fit(content string, maxTokens int) string {
	chunks := Chunk(content, maxTokens)
	if len(chunks) == 0 {
		return ""
	}
	return chunks[0]
}
```

**Step 4: Run, verify pass**

```bash
go test ./internal/chunker/...
```
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/chunker/
git commit -m "feat(chunker): add Fit() for single-payload truncation

- RED: tests for heading-prefixed truncation and no-op under budget
- GREEN: Fit returns first chunk produced by Chunk
- Status: 11 tests passing"
```

---

### Task 5: Edge-case hardening (oversize atomic blocks, sentence fallback)

**Files:**
- Modify: `internal/chunker/chunker.go`
- Modify: `internal/chunker/chunk_test.go`

**Step 1: Write the failing tests**

```go
func TestChunk_SplitsOversizeCodeBlockOnInnerNewlines(t *testing.T) {
	// A 500-line code block with a 50-token budget. The chunker must emit
	// multiple chunks, each carrying balanced fence markers.
	body := strings.Repeat("var x = 1\n", 500)
	in := "```go\n" + body + "```"
	chunks := Chunk(in, 50)
	if len(chunks) < 2 {
		t.Fatalf("expected oversize code block to be split, got 1 chunk")
	}
	for i, c := range chunks {
		opens := strings.Count(c, "```")
		if opens%2 != 0 {
			t.Fatalf("chunk %d unbalanced fences:\n%s", i, c)
		}
	}
}

func TestChunk_SentenceFallback_WhenNoParagraphBoundary(t *testing.T) {
	// A single dense paragraph with no blank lines. Chunker must fall back to
	// sentence-end splits.
	in := strings.Repeat("This is sentence one. This is sentence two. ", 30)
	chunks := Chunk(in, 30)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks via sentence fallback, got %d", len(chunks))
	}
}
```

**Step 2: Run, watch fail**

```bash
go test ./internal/chunker/...
```
Expected: FAIL — oversize code block stays as one piece; dense paragraph stays as one piece.

**Step 3: Implement**

Update `Chunk` and add two helpers:

```go
// In Chunk, replace the "atomic too big" branch:
//   if gTok > maxTokens, split the group via splitOversizeGroup before packing.

// splitOversizeGroup breaks an atomic group that exceeds maxTokens into
// smaller pieces. For fenced code, it wraps each inner-line slice with the
// surrounding fence markers. For paragraphs, it falls back to sentence
// splits.
func splitOversizeGroup(g group, maxTokens int) []string {
	if len(g.blocks) == 0 {
		return nil
	}
	first := g.blocks[0]
	switch first.kind {
	case blockCode:
		return splitCodeBlock(g, maxTokens)
	default:
		return splitParagraph(renderGroup(g), maxTokens)
	}
}

func splitCodeBlock(g group, maxTokens int) []string {
	if len(g.blocks) < 2 {
		return []string{renderGroup(g)}
	}
	open := g.blocks[0].text
	close := g.blocks[len(g.blocks)-1].text
	inner := g.blocks[1 : len(g.blocks)-1]

	var (
		out     []string
		cur     strings.Builder
		curTok  int
		overhead = EstimateTokens(open) + EstimateTokens(close) + 2
	)
	for _, b := range inner {
		t := EstimateTokens(b.text) + 1
		if curTok+t+overhead > maxTokens && cur.Len() > 0 {
			out = append(out, open+"\n"+strings.TrimRight(cur.String(), "\n")+"\n"+close)
			cur.Reset()
			curTok = 0
		}
		cur.WriteString(b.text)
		cur.WriteString("\n")
		curTok += t
	}
	if cur.Len() > 0 {
		out = append(out, open+"\n"+strings.TrimRight(cur.String(), "\n")+"\n"+close)
	}
	return out
}

func splitParagraph(s string, maxTokens int) []string {
	// Naive but adequate: split on ". " then greedy-pack.
	sentences := splitSentences(s)
	var (
		out    []string
		cur    strings.Builder
		curTok int
	)
	for _, sent := range sentences {
		t := EstimateTokens(sent)
		if curTok+t > maxTokens && cur.Len() > 0 {
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
			curTok = 0
		}
		cur.WriteString(sent)
		cur.WriteString(" ")
		curTok += t
	}
	if cur.Len() > 0 {
		out = append(out, strings.TrimSpace(cur.String()))
	}
	return out
}

func splitSentences(s string) []string {
	// Split on ". " but keep the period attached.
	var out []string
	start := 0
	for i := 0; i < len(s)-1; i++ {
		if s[i] == '.' && s[i+1] == ' ' {
			out = append(out, s[start:i+1])
			start = i + 2
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
```

Then update `Chunk` to use `splitOversizeGroup` when a single group exceeds `maxTokens`:

```go
// inside the for-loop in Chunk, before the "curTok+gTok > maxTokens" check:
if gTok > maxTokens {
	flush()
	for _, piece := range splitOversizeGroup(g, maxTokens) {
		chunks = append(chunks, piece)
	}
	continue
}
```

**Step 4: Run, verify pass**

```bash
go test ./internal/chunker/... -cover
```
Expected: all 13 tests pass; coverage ≥90%.

**Step 5: Commit**

```bash
git add internal/chunker/
git commit -m "feat(chunker): handle oversize atomic blocks and sentence fallback

- RED: tests for fenced-code splitting and sentence-fallback paragraphs
- GREEN: splitOversizeGroup with fence-preserving code split + sentence packer
- Coverage: ≥90%
- Status: 13 tests passing"
```

---

## Phase 2 — HIGH-severity call sites

### Task 6: `analyze_page.go` — preemptive chunking + per-chunk merge

**Files:**
- Modify: `internal/analyzer/analyze_page.go` (current prompt site at L49)
- Modify: `internal/analyzer/analyze_page_test.go`

**Step 1: Read the existing call site**

```bash
sed -n '30,120p' internal/analyzer/analyze_page.go
```
Identify the function name (likely `analyzePage` or similar), the prompt template, the request struct, and the per-page result struct.

**Step 2: Write a failing test**

Add to `analyze_page_test.go`:

```go
// TestAnalyzePage_ChunksOversizePage verifies that a page exceeding the
// small-tier budget is split via chunker and processed in multiple LLM
// calls whose feature lists are merged. Uses fakeLLMClient that returns
// distinct features per call.
func TestAnalyzePage_ChunksOversizePage(t *testing.T) {
	// Build a page that is guaranteed to exceed 4000 tokens.
	big := strings.Repeat("## Section\n\n"+strings.Repeat("alpha beta gamma ", 200)+"\n\n", 20)

	calls := 0
	client := &fakeLLMClient{respond: func(prompt string) string {
		calls++
		return fmt.Sprintf(`{"summary":"s%d","features":[{"name":"F%d","description":"d"}],"role":"reference","is_docs":true}`, calls, calls)
	}}

	res, err := analyzePageForTest(client, "https://example.com/page", big, /*budget*/ 4000)
	if err != nil {
		t.Fatalf("analyzePage: %v", err)
	}
	if calls < 2 {
		t.Fatalf("expected ≥2 LLM calls for oversize page, got %d", calls)
	}
	if len(res.Features) < 2 {
		t.Fatalf("expected merged features from multiple chunks, got %d", len(res.Features))
	}
}
```

(If `fakeLLMClient` doesn't exist with that shape, reuse the test helper already in `internal/analyzer/testhelpers_test.go` and adapt the assertion accordingly.)

**Step 3: Run, watch fail**

```bash
go test ./internal/analyzer/ -run TestAnalyzePage_ChunksOversizePage -v
```
Expected: FAIL — either `calls == 1` (no chunking) or the helper `analyzePageForTest` is undefined.

**Step 4: Implement**

In `analyze_page.go`, around the existing call site:

```go
import (
	"github.com/sandgardenhq/find-the-gaps/internal/chunker"
)

// budgetForPageAnalysis returns the per-call content budget after subtracting
// the prompt overhead. Conservative — leaves headroom for the JSON-schema
// response.
func budgetForPageAnalysis(tier Tier) int {
	switch tier {
	case TierSmall:
		return 30_000
	case TierTypical:
		return 60_000
	default:
		return 100_000
	}
}

// analyzePageContent runs the per-page extraction prompt, chunking content
// when it exceeds the tier budget. Feature lists from each chunk are merged
// by feature name (description picks the longest; sources concatenate).
func analyzePageContent(ctx context.Context, client LLMClient, url, content string, tier Tier) (PageAnalysis, error) {
	budget := budgetForPageAnalysis(tier)
	if chunker.EstimateTokens(content) <= budget {
		return runPageAnalysisOnce(ctx, client, url, content, tier)
	}
	chunks := chunker.Chunk(content, budget)
	if len(chunks) == 1 {
		return runPageAnalysisOnce(ctx, client, url, chunks[0], tier)
	}
	var merged PageAnalysis
	for i, c := range chunks {
		part, err := runPageAnalysisOnce(ctx, client, url, c, tier)
		if err != nil {
			return PageAnalysis{}, fmt.Errorf("analyze chunk %d/%d: %w", i+1, len(chunks), err)
		}
		merged = mergePageAnalysis(merged, part)
	}
	if logger := loggerFromContext(ctx); logger != nil {
		logger.Printf("page chunked: url=%s chunks=%d features_after_merge=%d", url, len(chunks), len(merged.Features))
	}
	return merged, nil
}

// mergePageAnalysis combines two analyses. Summary: first non-empty wins
// (chunks share a page-level summary). is_docs/role: any-true / first
// non-empty. Features: dedupe by lowercase name; longest description wins;
// source-evidence lists concatenate (deduped).
func mergePageAnalysis(a, b PageAnalysis) PageAnalysis {
	out := a
	if out.Summary == "" {
		out.Summary = b.Summary
	}
	if out.Role == "" {
		out.Role = b.Role
	}
	out.IsDocs = a.IsDocs || b.IsDocs
	byName := map[string]int{}
	for i, f := range out.Features {
		byName[strings.ToLower(f.Name)] = i
	}
	for _, f := range b.Features {
		key := strings.ToLower(f.Name)
		if idx, ok := byName[key]; ok {
			existing := out.Features[idx]
			if len(f.Description) > len(existing.Description) {
				existing.Description = f.Description
			}
			existing.Evidence = appendUniqueStrings(existing.Evidence, f.Evidence...)
			out.Features[idx] = existing
		} else {
			out.Features = append(out.Features, f)
			byName[key] = len(out.Features) - 1
		}
	}
	return out
}
```

Wire the existing call site to delegate to `analyzePageContent`. Extract the current single-call code into `runPageAnalysisOnce`. Replace any `if errors.Is(err, ErrTokenBudgetExceeded{})` silent-skip with a defense-in-depth backstop that logs loudly (no behavior change beyond the log) — preemptive chunking should make it unreachable, but we keep it as a safety net.

**Step 5: Run, verify pass**

```bash
go test ./internal/analyzer/ -run TestAnalyzePage -cover
```
Expected: all `analyze_page` tests pass; package coverage ≥90%.

**Step 6: Commit**

```bash
git add internal/analyzer/analyze_page.go internal/analyzer/analyze_page_test.go
git commit -m "feat(analyzer): preemptive chunking in analyze_page

- RED: oversize page must drive ≥2 LLM calls with merged features
- GREEN: budgetForPageAnalysis + chunker.Chunk + mergePageAnalysis
- ErrTokenBudgetExceeded backstop kept; should be unreachable
- Status: all analyze_page tests passing"
```

---

### Task 7: `screenshot_gaps.go` — preemptive chunking with page-scoped image/code-block sharing

**Files:**
- Modify: `internal/analyzer/screenshot_gaps.go` (call sites at L730 + L853)
- Modify: `internal/analyzer/screenshot_gaps_test.go`

**Step 1: Read existing call sites**

```bash
sed -n '700,900p' internal/analyzer/screenshot_gaps.go
```
Identify the detection-prompt builder, the image/code-block payload assembly, and the existing `fitContentToBudget` helper that we're replacing.

**Step 2: Write a failing test**

```go
func TestScreenshotDetection_ChunksOversizePage_SharesImagesAcrossChunks(t *testing.T) {
	page := docPage{URL: "https://example.com/big", Content: bigMarkdown(40_000 /*tokens*/)}
	page.Images = []imageRef{{Src: "/a.png", Alt: "diagram"}}

	calls := 0
	client := &fakeLLMClient{respond: func(prompt string) string {
		calls++
		// Every call must include the image reference (page-scoped).
		if !strings.Contains(prompt, "/a.png") {
			t.Fatalf("call %d missing image reference", calls)
		}
		return fmt.Sprintf(`{"missing_gaps":[{"passage":"p%d","reason":"r"}]}`, calls)
	}}
	res, err := detectScreenshotGapsForTest(client, page, /*budget*/ 30_000)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if calls < 2 {
		t.Fatalf("expected ≥2 LLM calls for oversize page, got %d", calls)
	}
	if len(res.MissingGaps) != calls {
		t.Fatalf("expected concat of per-chunk findings; calls=%d findings=%d", calls, len(res.MissingGaps))
	}
}
```

**Step 3: Run, watch fail**

```bash
go test ./internal/analyzer/ -run TestScreenshotDetection_ChunksOversize -v
```
Expected: FAIL — currently only one LLM call is made and oversize content is truncated.

**Step 4: Implement**

Replace the `fitContentToBudget(content)` call with a chunker-driven path:

```go
func detectScreenshotGaps(ctx context.Context, client LLMClient, page DocPage, tier Tier) (ScreenshotResult, error) {
	overhead := EstimateScreenshotPromptOverhead(page) // images + code blocks + rubric
	budget := budgetForScreenshotDetection(tier) - overhead
	if budget <= 0 {
		return ScreenshotResult{}, fmt.Errorf("screenshot prompt overhead %d exceeds budget", overhead)
	}
	contentChunks := chunker.Chunk(page.Content, budget)
	var merged ScreenshotResult
	for i, c := range contentChunks {
		part, err := runScreenshotDetectionOnce(ctx, client, page, c, tier)
		if err != nil {
			return ScreenshotResult{}, fmt.Errorf("chunk %d/%d: %w", i+1, len(contentChunks), err)
		}
		merged = mergeScreenshotResult(merged, part)
	}
	dedupeByPassage(&merged)
	if logger := loggerFromContext(ctx); logger != nil && len(contentChunks) > 1 {
		logger.Printf("screenshot chunked: url=%s chunks=%d findings=%d", page.URL, len(contentChunks), len(merged.MissingGaps))
	}
	return merged, nil
}

// EstimateScreenshotPromptOverhead measures tokens consumed by everything
// EXCEPT page.Content: the prompt template, image manifest, code-block list,
// and priority rubric. Used to compute the per-chunk content budget.
func EstimateScreenshotPromptOverhead(page DocPage) int {
	// Render the non-content portion of the prompt with empty content,
	// then estimate.
	return chunker.EstimateTokens(renderScreenshotPrompt(page, ""))
}

func mergeScreenshotResult(a, b ScreenshotResult) ScreenshotResult {
	a.MissingGaps = append(a.MissingGaps, b.MissingGaps...)
	a.ImageIssues = append(a.ImageIssues, b.ImageIssues...)
	a.PossiblyCovered = append(a.PossiblyCovered, b.PossiblyCovered...)
	return a
}

func dedupeByPassage(r *ScreenshotResult) {
	r.MissingGaps = dedupeBy(r.MissingGaps, func(g MissingGap) string { return hashPassage(g.Passage) })
	r.ImageIssues = dedupeBy(r.ImageIssues, func(g ImageIssue) string { return g.PageURL + "|" + g.ImageSrc })
	r.PossiblyCovered = dedupeBy(r.PossiblyCovered, func(g PossiblyCovered) string { return hashPassage(g.Passage) })
}
```

Delete `fitContentToBudget` once no caller remains. Keep `ErrTokenBudgetExceeded` defensive handling at the LLM-call layer.

**Step 5: Run, verify pass**

```bash
go test ./internal/analyzer/ -run TestScreenshot -cover
```
Expected: all screenshot tests pass; coverage stable or improved.

**Step 6: Commit**

```bash
git add internal/analyzer/screenshot_gaps.go internal/analyzer/screenshot_gaps_test.go
git commit -m "feat(analyzer): preemptive chunking in screenshot detection

- RED: oversize page drives ≥2 LLM calls with shared image manifest
- GREEN: detectScreenshotGaps splits page.Content via chunker, merges findings
- Removed: fitContentToBudget (no longer called)
- Status: screenshot tests passing"
```

---

### Task 8: `drift.go` investigator — compressed system prompt

**Files:**
- Modify: `internal/analyzer/drift.go` (prompt at L422)
- Modify: `internal/analyzer/drift_test.go`

**Step 1: Write a failing test**

```go
func TestDriftInvestigator_SystemPromptStaysUnderBudget_ForLargeFeature(t *testing.T) {
	feature := CodeFeature{
		Name:        "Big",
		Description: "A feature spanning many files.",
		Symbols:     makeSymbols(200), // 200 symbol entries
	}
	pages := makePageRefs(40) // 40 doc page refs

	prompt := buildInvestigatorSystemPrompt(feature, pages)
	if got := chunker.EstimateTokens(prompt); got > 4000 {
		t.Fatalf("compressed system prompt should stay under 4K tokens, got %d", got)
	}
	// Sanity: prompt mentions the symbol/page COUNTS, not every entry.
	if !strings.Contains(prompt, "200 symbols") || !strings.Contains(prompt, "40 pages") {
		t.Fatalf("expected counts in compressed prompt, got:\n%s", prompt)
	}
}
```

**Step 2: Run, watch fail**

```bash
go test ./internal/analyzer/ -run TestDriftInvestigator_SystemPromptStaysUnderBudget -v
```
Expected: FAIL — `buildInvestigatorSystemPrompt` undefined; the current prompt inlines everything.

**Step 3: Implement**

In `drift.go`, extract the existing inline prompt assembly into a function:

```go
// buildInvestigatorSystemPrompt returns the compressed system prompt used
// by the drift investigator. The prompt does NOT inline every symbol or
// every page — it embeds counts and entry-point names, and exposes the
// full lists via the list_feature_symbols and list_feature_pages tools.
func buildInvestigatorSystemPrompt(feature CodeFeature, pages []PageRef) string {
	files := uniqueFiles(feature.Symbols)
	entries := topEntryPoints(feature.Symbols, 10)
	var b strings.Builder
	// PROMPT: Investigates a feature for documentation drift by reading
	// source files and doc pages, recording each piece of evidence via
	// note_observation. The investigator gathers; it does not adjudicate.
	fmt.Fprintf(&b, `You are investigating documentation drift for the feature %q.

Description: %s

Scope: %d symbols across %d files in this codebase. %d documentation pages reference this feature.

Entry-point symbols you may want to start with: %s

To see the full symbol list, call list_feature_symbols(offset, limit).
To see the full page list, call list_feature_pages(offset, limit).
To read a file or page, call the existing read_file / read_page tools.

Record every concrete observation about drift via note_observation. Do not adjudicate — the judge does that.
`,
		feature.Name, feature.Description,
		len(feature.Symbols), len(files), len(pages),
		strings.Join(entries, ", "),
	)
	return b.String()
}

func uniqueFiles(syms []SymbolRef) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range syms {
		if !seen[s.File] {
			seen[s.File] = true
			out = append(out, s.File)
		}
	}
	return out
}

func topEntryPoints(syms []SymbolRef, n int) []string {
	// Simple heuristic: exported symbols come first; ties by file path.
	// Production refinement can score by file centrality.
	var exported, rest []string
	for _, s := range syms {
		if len(s.Name) > 0 && s.Name[0] >= 'A' && s.Name[0] <= 'Z' {
			exported = append(exported, s.Name)
		} else {
			rest = append(rest, s.Name)
		}
	}
	all := append(exported, rest...)
	if len(all) > n {
		all = all[:n]
	}
	return all
}
```

Replace the existing inline prompt-string in the investigator setup with `buildInvestigatorSystemPrompt(feature, pages)`. Keep the `// PROMPT:` comment above the new builder.

**Step 4: Run, verify pass**

```bash
go test ./internal/analyzer/ -run TestDriftInvestigator -cover
```
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/analyzer/drift.go internal/analyzer/drift_test.go
git commit -m "feat(drift): compress investigator system prompt

- RED: large feature must produce <4K-token system prompt with counts
- GREEN: extract buildInvestigatorSystemPrompt with counts + top entries
- Status: drift tests passing"
```

---

### Task 9: `drift.go` investigator — new tools `list_feature_symbols` / `list_feature_pages`

**Files:**
- Modify: `internal/analyzer/drift.go`
- Modify: `internal/analyzer/agent_loop.go` (or wherever tool definitions live)
- Modify: `internal/analyzer/drift_test.go`

**Step 1: Write a failing test**

```go
func TestDriftInvestigator_ListFeatureSymbolsTool_PaginatesAndFiltersByName(t *testing.T) {
	feature := CodeFeature{Symbols: makeSymbols(75)} // names "sym0".."sym74"
	tool := newListFeatureSymbolsTool(feature)

	// limit=10, offset=20
	resp, err := tool.Invoke(context.Background(), map[string]any{"offset": 20.0, "limit": 10.0})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	got := parseSymbolNames(resp)
	want := []string{"sym20","sym21","sym22","sym23","sym24","sym25","sym26","sym27","sym28","sym29"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pagination wrong:\n got: %v\nwant: %v", got, want)
	}

	// filter
	resp2, err := tool.Invoke(context.Background(), map[string]any{"filter": "sym1"})
	if err != nil {
		t.Fatalf("filter invoke: %v", err)
	}
	if !strings.Contains(resp2, "sym10") || strings.Contains(resp2, "sym20") {
		t.Fatalf("filter did not narrow correctly: %s", resp2)
	}
}
```

**Step 2: Run, watch fail**

```bash
go test ./internal/analyzer/ -run TestDriftInvestigator_ListFeatureSymbolsTool -v
```
Expected: FAIL — `newListFeatureSymbolsTool` undefined.

**Step 3: Implement**

```go
type listFeatureSymbolsTool struct {
	feature CodeFeature
}

func newListFeatureSymbolsTool(f CodeFeature) *listFeatureSymbolsTool {
	return &listFeatureSymbolsTool{feature: f}
}

func (t *listFeatureSymbolsTool) Name() string { return "list_feature_symbols" }

func (t *listFeatureSymbolsTool) Schema() any {
	// JSON schema fragment for tool input. Match shape used by other tools
	// in agent_loop.go.
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"offset": map[string]any{"type": "integer", "minimum": 0},
			"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 200},
			"filter": map[string]any{"type": "string"},
		},
	}
}

func (t *listFeatureSymbolsTool) Invoke(ctx context.Context, args map[string]any) (string, error) {
	offset, _ := args["offset"].(float64)
	limit, _ := args["limit"].(float64)
	if limit == 0 {
		limit = 50
	}
	filter, _ := args["filter"].(string)
	syms := t.feature.Symbols
	if filter != "" {
		var out []SymbolRef
		for _, s := range syms {
			if strings.Contains(strings.ToLower(s.Name), strings.ToLower(filter)) {
				out = append(out, s)
			}
		}
		syms = out
	}
	start := int(offset)
	if start > len(syms) {
		start = len(syms)
	}
	end := start + int(limit)
	if end > len(syms) {
		end = len(syms)
	}
	return renderSymbolList(syms[start:end], len(syms)), nil
}

func renderSymbolList(syms []SymbolRef, total int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d of %d symbols:\n", len(syms), total)
	for _, s := range syms {
		fmt.Fprintf(&b, "- %s  (%s)\n", s.Name, s.File)
	}
	return b.String()
}
```

Mirror the pattern for `list_feature_pages`. Register both tools in the agent loop tool set alongside the existing `read_file` / `read_page` / `note_observation` tools.

**Step 4: Run, verify pass**

```bash
go test ./internal/analyzer/ -run TestDriftInvestigator -cover
```
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/analyzer/
git commit -m "feat(drift): add list_feature_symbols and list_feature_pages tools

- RED: pagination + filter test against synthetic 75-symbol feature
- GREEN: paginated tool with filter; mirror tool for pages; registered in agent loop
- Status: drift tests passing"
```

---

### Task 10: `drift.go` judge — preemptive sizing, delete reactive compaction

**Files:**
- Modify: `internal/analyzer/drift.go` (judge call ~L654 and `chunkObservationsToFit` ~L778)
- Modify: `internal/analyzer/drift_test.go`

**Step 1: Write a failing test**

```go
func TestDriftJudge_ChunksObservationsBeforeFirstCall_WhenOverBudget(t *testing.T) {
	obs := makeObservations(200) // each ~500 tokens; total ~100K
	calls := 0
	client := &fakeLLMClient{respond: func(prompt string) string {
		calls++
		// Every call must stay under tier budget.
		if chunker.EstimateTokens(prompt) > 60_000 {
			t.Fatalf("call %d exceeded budget: %d tokens", calls, chunker.EstimateTokens(prompt))
		}
		return `{"issues":[{"id":"i","file":"a.go","line":1}]}`
	}}
	_, err := runJudgeForTest(client, CodeFeature{Name: "F"}, obs, /*budget*/ 60_000)
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if calls < 2 {
		t.Fatalf("expected ≥2 judge calls due to chunked observations, got %d", calls)
	}
}
```

**Step 2: Run, watch fail**

```bash
go test ./internal/analyzer/ -run TestDriftJudge_ChunksObservationsBeforeFirstCall -v
```
Expected: FAIL — current implementation makes one (overflowing) call before reactive compaction.

**Step 3: Implement**

Refactor the judge entry point to estimate first:

```go
func runJudge(ctx context.Context, client LLMClient, feature CodeFeature, observations []driftObservation, roles RoleResolver) ([]DriftIssue, error) {
	overhead := judgePromptOverhead(feature)
	budget := budgetForJudge(client.Tier()) - overhead
	if budget <= 0 {
		return nil, fmt.Errorf("judge prompt overhead %d exceeds budget", overhead)
	}
	obsBlob := renderObservations(observations)
	if chunker.EstimateTokens(obsBlob) <= budget {
		return runJudgeOnce(ctx, client, feature, observations, roles)
	}
	pieces := chunker.Chunk(obsBlob, budget)
	chunked := mapChunksBackToObservations(pieces, observations)

	var merged []DriftIssue
	for i, group := range chunked {
		part, err := runJudgeOnce(ctx, client, feature, group, roles)
		if err != nil {
			return nil, fmt.Errorf("judge chunk %d/%d: %w", i+1, len(chunked), err)
		}
		merged = append(merged, part...)
	}
	return dedupeDriftIssues(merged), nil
}
```

Delete `chunkObservationsToFit` and the surrounding "if errors.Is(err, ErrTokenBudgetExceeded{}) { ... }" retry branch at L647-654. Keep the top-level `ErrTokenBudgetExceeded` log so estimator drift still surfaces as a loud error rather than a silent skip.

**Step 4: Run, verify pass**

```bash
go test ./internal/analyzer/ -run TestDrift -cover
```
Expected: all drift tests pass.

**Step 5: Commit**

```bash
git add internal/analyzer/drift.go internal/analyzer/drift_test.go
git commit -m "refactor(drift): preemptive judge sizing, remove reactive compaction

- RED: oversize observation set must drive ≥2 judge calls each under budget
- GREEN: estimate-then-chunk before first call; delete chunkObservationsToFit
- Status: drift tests passing"
```

---

### Task 11: `synthesize.go` — per-page summary compression + map-reduce fallback

**Files:**
- Modify: `internal/analyzer/synthesize.go`
- Modify: `internal/analyzer/synthesize_test.go`

**Step 1: Write the failing tests**

```go
func TestSynthesize_CompressesPerPageSummaries_OnLargeCorpus(t *testing.T) {
	pages := makePageAnalyses(500) // 500 pages, each summary ~300 tokens
	calls := 0
	client := &fakeLLMClient{respond: func(prompt string) string {
		calls++
		return `{"product":"P","features":[{"name":"F"}]}`
	}}

	_, err := SynthesizeForTest(client, pages, /*budget*/ 80_000)
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	// Single-pass with compression should fit comfortably for 500 pages
	// (500 * 200 budget = 100K, slightly over — should fall back to map-reduce).
	if calls < 2 {
		t.Fatalf("expected map-reduce fallback (≥2 calls), got %d", calls)
	}
}

func TestSynthesize_SinglePass_OnSmallCorpus(t *testing.T) {
	pages := makePageAnalyses(20)
	calls := 0
	client := &fakeLLMClient{respond: func(prompt string) string {
		calls++
		return `{"product":"P","features":[{"name":"F"}]}`
	}}
	_, err := SynthesizeForTest(client, pages, 80_000)
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected single-pass for small corpus, got %d calls", calls)
	}
}
```

**Step 2: Run, watch fail**

**Step 3: Implement**

```go
const perPageSummaryBudget = 200 // tokens per compressed page summary

func SynthesizeProduct(ctx context.Context, client LLMClient, pages []PageAnalysis) (Product, error) {
	tier := client.Tier()
	overhead := synthesizeOverhead()
	budget := budgetForSynthesize(tier) - overhead

	compressed := compressPageSummaries(pages, perPageSummaryBudget)
	body := renderSynthesizeBody(compressed)
	if chunker.EstimateTokens(body) <= budget {
		return runSynthesizeOnce(ctx, client, body)
	}

	// Map-reduce: split compressed-page list into groups that each fit,
	// summarize each into a partial product, then reduce.
	groups := splitCompressedPages(compressed, budget)
	partials := make([]Product, 0, len(groups))
	for i, g := range groups {
		body := renderSynthesizeBody(g)
		p, err := runSynthesizeOnce(ctx, client, body)
		if err != nil {
			return Product{}, fmt.Errorf("synthesize group %d/%d: %w", i+1, len(groups), err)
		}
		partials = append(partials, p)
	}
	return reducePartials(ctx, client, partials, budget)
}

// compressPageSummaries truncates each page's summary to perPageSummaryBudget
// tokens at the nearest sentence boundary using chunker.Fit.
func compressPageSummaries(pages []PageAnalysis, perPage int) []PageAnalysis {
	out := make([]PageAnalysis, len(pages))
	for i, p := range pages {
		p.Summary = chunker.Fit(p.Summary, perPage)
		out[i] = p
	}
	return out
}

func splitCompressedPages(pages []PageAnalysis, budget int) [][]PageAnalysis {
	var groups [][]PageAnalysis
	var cur []PageAnalysis
	curTok := 0
	for _, p := range pages {
		t := chunker.EstimateTokens(renderPageEntry(p))
		if curTok+t > budget && len(cur) > 0 {
			groups = append(groups, cur)
			cur = nil
			curTok = 0
		}
		cur = append(cur, p)
		curTok += t
	}
	if len(cur) > 0 {
		groups = append(groups, cur)
	}
	return groups
}

// reducePartials merges partial Product summaries into one. If the merged
// body itself overflows, recurse (rare; typically 2-3 partials max).
func reducePartials(ctx context.Context, client LLMClient, partials []Product, budget int) (Product, error) {
	if len(partials) == 1 {
		return partials[0], nil
	}
	body := renderPartialMerge(partials)
	if chunker.EstimateTokens(body) > budget {
		// Recurse: pair partials, reduce each pair, repeat.
		half := len(partials) / 2
		left, err := reducePartials(ctx, client, partials[:half], budget)
		if err != nil {
			return Product{}, err
		}
		right, err := reducePartials(ctx, client, partials[half:], budget)
		if err != nil {
			return Product{}, err
		}
		return reducePartials(ctx, client, []Product{left, right}, budget)
	}
	return runSynthesizeReduce(ctx, client, body)
}
```

Add a second `// PROMPT:` reduction template for `runSynthesizeReduce`.

**Step 4: Run, verify pass**

```bash
go test ./internal/analyzer/ -run TestSynthesize -cover
```
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/analyzer/synthesize.go internal/analyzer/synthesize_test.go
git commit -m "feat(synthesize): per-page compression + map-reduce fallback

- RED: large corpus drives ≥2 calls; small corpus stays single-pass
- GREEN: compressPageSummaries via chunker.Fit; group-and-reduce on overflow
- Status: synthesize tests passing"
```

---

## Phase 3 — Cleanup and verification

### Task 12: Confirm no dead reactive paths remain

**Step 1: Search for the removed helpers**

```bash
grep -rn "fitContentToBudget\|chunkObservationsToFit" internal/ cmd/
```
Expected: NO matches. If any remain, delete them.

**Step 2: Search for silent skips on `ErrTokenBudgetExceeded`**

```bash
grep -rn -B2 -A5 "ErrTokenBudgetExceeded" internal/analyzer/
```
For each match, confirm the handler logs a loud warning (not a silent `continue`).

**Step 3: Run full test suite with race detector**

```bash
go test -race -count=1 ./...
```
Expected: PASS.

**Step 4: Run coverage gate**

```bash
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -20
```
Expected: every modified package ≥90% statement coverage.

**Step 5: Lint**

```bash
golangci-lint run
```
Expected: zero issues.

**Step 6: Commit (if any cleanup landed)**

```bash
git add -p
git commit -m "chore(analyzer): remove dead reactive-compaction paths

- Deleted: fitContentToBudget, chunkObservationsToFit references confirmed gone
- ErrTokenBudgetExceeded handlers now log loudly (no silent skips)
- Coverage: ≥90% on all touched packages"
```

---

### Task 13: Verify against Scenario 9 (real docs site)

**Files:** None modified — verification only.

**Step 1: Prepare**

Per `.plans/VERIFICATION_PLAN.md` Scenario 9, choose a real small open-source Go project with a public docs site (one already documented in `testdata/README.md`).

**Step 2: Run analyze with `-v` and capture logs**

```bash
go build -o /tmp/ftg ./cmd/find-the-gaps
rm -rf ~/.find-the-gaps/projects/<pinned-project>
/tmp/ftg analyze --repo <pinned-repo-path> --docs <pinned-docs-url> -v 2>&1 | tee /tmp/ftg-run.log
```

**Step 3: Confirm absence of overflow errors and presence of chunk logs**

```bash
grep -c "page chunked:" /tmp/ftg-run.log
grep -c "screenshot chunked:" /tmp/ftg-run.log
grep -c "ErrTokenBudgetExceeded" /tmp/ftg-run.log
```
Expected: chunk logs may be zero or more depending on docs-page sizes; `ErrTokenBudgetExceeded` count must be zero.

**Step 4: Compare report shape to a pre-change baseline**

If a pre-change report exists for the same pinned fixture, run a sort-then-diff between the two runs' `gaps.md` / `screenshots.md` / `mapping.md`. Findings should be substantively the same. Any new findings should be ones the old run silently skipped over (these are the ones we wanted to surface).

**Step 5: Document outcome in `PROGRESS.md`**

Append:

```markdown
## Context Overflow Remediation - VERIFIED
- Pinned fixture: <project>@<commit>
- chunk-firing rate: <N>/<total> pages
- ErrTokenBudgetExceeded: 0
- Diff vs baseline: <summary>
```

**Step 6: Commit and open PR**

```bash
git push -u origin <branch>
gh pr create --base main --title "fix(analyzer): preemptive chunking at HIGH-severity LLM call sites" --body "$(cat <<'EOF'
## Summary
- Replaces reactive overflow handling with preemptive structure-aware chunking at all HIGH-severity sites identified in `.plans/CONTEXT_OVERFLOW_AUDIT.md`.
- New `internal/chunker` package owns the splitter; reused by analyze_page, screenshot_gaps, drift investigator/judge, and synthesize.
- Drift investigator system prompt is now compressed; full symbol/page lists exposed via two new tools.
- Synthesize gains per-page summary compression with a map-reduce fallback for large corpora.

Closes the audit findings tracked in `.plans/CONTEXT_OVERFLOW_AUDIT.md`.

## Test plan
- [x] `go test -race -count=1 ./...` passes
- [x] `go test -cover ./...` ≥90% on touched packages
- [x] `golangci-lint run` clean
- [x] Scenario 9 verified against pinned fixture (see PROGRESS.md)
- [x] No "ErrTokenBudgetExceeded" lines in verbose Scenario-9 run log
EOF
)"
```

---

## Acceptance gate

A reviewer should be able to verify, in order:

1. `internal/chunker/` exists, has ≥90% coverage, and `Chunk` / `Fit` / `EstimateTokens` are documented.
2. `grep -rn "fitContentToBudget\|chunkObservationsToFit" internal/ cmd/` returns nothing.
3. Every `// PROMPT:` site flagged in `.plans/CONTEXT_OVERFLOW_AUDIT.md` HIGH section has a corresponding call to `chunker.EstimateTokens` or `chunker.Chunk` upstream of the LLM call.
4. Drift investigator system prompt never includes a full symbol or page enumeration.
5. Scenario 9 verbose log contains zero `ErrTokenBudgetExceeded` lines.
