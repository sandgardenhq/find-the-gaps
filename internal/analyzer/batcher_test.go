package analyzer

import (
	"testing"
)

func TestBatchSymLines_emptyInput_returnsNoBatches(t *testing.T) {
	got := batchSymLines([]string{}, 0, 10000)
	if len(got) != 0 {
		t.Errorf("expected 0 batches, got %d", len(got))
	}
}

func TestBatchSymLines_allFitInOneBatch(t *testing.T) {
	lines := []string{"a.go: Foo", "b.go: Bar"}
	got := batchSymLines(lines, 0, 10000)
	if len(got) != 1 {
		t.Fatalf("expected 1 batch, got %d", len(got))
	}
	if len(got[0]) != 2 {
		t.Errorf("expected 2 lines in batch, got %d", len(got[0]))
	}
}

func TestBatchSymLines_splitAcrossMultipleBatches(t *testing.T) {
	// budget=0 means remaining=0, so any line with ≥1 token flushes the current batch.
	// Every non-empty line gets its own batch regardless of actual token count.
	lines := []string{"alpha bravo", "charlie delta", "echo foxtrot"}
	got := batchSymLines(lines, 0, 0)
	if len(got) != 3 {
		t.Fatalf("expected 3 batches, got %d: %v", len(got), got)
	}
}

func TestBatchSymLines_featuresOverheadAccountedFor(t *testing.T) {
	// When featuresTokens == budget, remaining == 0, so each line gets its own batch.
	// This verifies the featuresTokens parameter is actually subtracted from the budget.
	lines := []string{"alpha", "bravo", "charlie"}
	budget := 10000
	got := batchSymLines(lines, budget, budget) // featuresTokens consumes entire budget
	if len(got) != 3 {
		t.Fatalf("expected 3 batches (1 line each), got %d", len(got))
	}
}

func TestBatchSymLines_singleOversizedLine_getsItsOwnBatch(t *testing.T) {
	// A single line larger than the remaining budget still gets placed alone.
	big := string(make([]byte, 40000)) // ~10000 tokens
	lines := []string{big, "small"}
	got := batchSymLines(lines, 0, 1000)
	if len(got) != 2 {
		t.Fatalf("expected 2 batches, got %d", len(got))
	}
}
