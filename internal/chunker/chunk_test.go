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
	// Two H2 sections, each ~50 tokens. Budget 60 forces a split between them.
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
		t.Fatalf("expected >=2 chunks, got %d", len(chunks))
	}
	if !strings.HasPrefix(chunks[1], "## Section") {
		t.Fatalf("expected second chunk to start at heading, got %q", chunks[1])
	}
}

func TestChunk_KeepsParentHeadingWithSubheading(t *testing.T) {
	in := "## API\n\n### Endpoints\n\n" + strings.Repeat("word ", 30)
	chunks := Chunk(in, 20)
	if !strings.Contains(chunks[0], "## API") || !strings.Contains(chunks[0], "### Endpoints") {
		t.Fatalf("parent heading orphaned from subheading: %q", chunks[0])
	}
}

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
