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
