package analyzer

import "testing"

// TestMergePageAnalysis_MergeSemantics exercises mergePageAnalysis directly
// (white-box) on hand-crafted PageAnalysis values, avoiding the need to
// re-orchestrate chunkable content. It pins the merge contract documented
// on mergePageAnalysis: first-non-empty Summary, IsDocs OR, first
// non-"other" Role, and case-fold Features dedup with original casing
// preserved.
func TestMergePageAnalysis_MergeSemantics(t *testing.T) {
	a := PageAnalysis{
		URL:      "https://example.com/p",
		Summary:  "chunk one summary",
		Role:     "other",
		IsDocs:   false,
		Features: []string{"Authentication", "Billing"},
	}
	b := PageAnalysis{
		URL:      "https://example.com/p",
		Summary:  "chunk two summary",
		Role:     "reference",
		IsDocs:   true,
		Features: []string{"authentication", "Webhooks"}, // "authentication" must dedupe
	}
	merged := mergePageAnalysis(a, b)

	if merged.Summary != "chunk one summary" {
		t.Errorf("Summary: want first-non-empty %q, got %q", "chunk one summary", merged.Summary)
	}
	if !merged.IsDocs {
		t.Errorf("IsDocs: want true (logical OR), got false")
	}
	if merged.Role != "reference" {
		t.Errorf("Role: want first non-other %q, got %q", "reference", merged.Role)
	}
	if len(merged.Features) != 3 {
		t.Fatalf("Features: want 3 after dedupe, got %d (%v)", len(merged.Features), merged.Features)
	}
	// Casing: first occurrence wins — "Authentication" preserved, "authentication" dropped.
	found := false
	for _, f := range merged.Features {
		if f == "Authentication" {
			found = true
		}
		if f == "authentication" {
			t.Errorf("Features: lowercase duplicate not deduped (%v)", merged.Features)
		}
	}
	if !found {
		t.Errorf("Features: original casing %q dropped (%v)", "Authentication", merged.Features)
	}
}
