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

func TestClassifyLines_NumericList(t *testing.T) {
	in := "1. one\n2. two\n3. three"
	got := classifyLines(in)
	if len(got) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(got))
	}
	var fenceID int
	for i, b := range got {
		if b.kind != blockList {
			t.Fatalf("line %d: expected blockList, got %v", i, b.kind)
		}
		if i == 0 {
			fenceID = b.fenceID
			if fenceID == 0 {
				t.Fatalf("expected non-zero fenceID")
			}
		} else if b.fenceID != fenceID {
			t.Fatalf("line %d fenceID %d != %d", i, b.fenceID, fenceID)
		}
	}
}
