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
	// Token counts via cl100k_base:
	//   "alpha bravo"   => 3 tokens
	//   "charlie delta" => 3 tokens
	//   "echo foxtrot"  => 4 tokens
	//
	// With budget=7 and featuresTokens=0, remaining=7.
	// After lines 1+2: currentTokens=6. Adding line 3 would push to 10 > 7,
	// so line 3 triggers a mid-stream flush.
	// Expected: batch 1 = ["alpha bravo", "charlie delta"], batch 2 = ["echo foxtrot"].
	lines := []string{"alpha bravo", "charlie delta", "echo foxtrot"}
	got := batchSymLines(lines, 0, 7)
	if len(got) != 2 {
		t.Fatalf("expected 2 batches, got %d: %v", len(got), got)
	}
	if len(got[0]) != 2 {
		t.Errorf("expected 2 lines in first batch, got %d: %v", len(got[0]), got[0])
	}
	if len(got[1]) != 1 {
		t.Errorf("expected 1 line in second batch, got %d: %v", len(got[1]), got[1])
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
