package analyzer

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestJudgeResponseParsesPriority(t *testing.T) {
	raw := []byte(`{"issues":[{"page":"https://x/y","issue":"foo","priority":"large","priority_reason":"on quickstart"}]}`)
	var resp judgeResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Issues) != 1 {
		t.Fatalf("got %d issues", len(resp.Issues))
	}
	got := resp.Issues[0]
	if got.Priority != PriorityLarge {
		t.Errorf("Priority = %q, want large", got.Priority)
	}
	if got.PriorityReason != "on quickstart" {
		t.Errorf("PriorityReason = %q", got.PriorityReason)
	}
}

func TestValidateDriftIssuesRejectsMissingPriority(t *testing.T) {
	issues := []DriftIssue{{Page: "p", Issue: "i"}}
	if err := validateDriftIssues(issues); err == nil {
		t.Fatal("expected error for missing priority")
	}
}

func TestValidateDriftIssuesRejectsBogusPriority(t *testing.T) {
	issues := []DriftIssue{{Page: "p", Issue: "i", Priority: "huge", PriorityReason: "x"}}
	if err := validateDriftIssues(issues); err == nil {
		t.Fatal("expected error for bogus priority")
	}
}

func TestValidateDriftIssuesRejectsEmptyReason(t *testing.T) {
	issues := []DriftIssue{{Page: "p", Issue: "i", Priority: PriorityLarge, PriorityReason: "  "}}
	if err := validateDriftIssues(issues); err == nil {
		t.Fatal("expected error for empty priority_reason")
	}
}

func TestValidateDriftIssuesAcceptsAll(t *testing.T) {
	issues := []DriftIssue{
		{Page: "a", Issue: "i", Priority: PriorityLarge, PriorityReason: "r"},
		{Page: "b", Issue: "j", Priority: PriorityMedium, PriorityReason: "r"},
		{Page: "c", Issue: "k", Priority: PrioritySmall, PriorityReason: "r"},
	}
	if err := validateDriftIssues(issues); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPageRoleSummary(t *testing.T) {
	got := pageRoleSummary([]string{"https://x/quickstart", "https://x/a/b/c/d/e"})
	if !strings.Contains(got, "quickstart") || !strings.Contains(got, "deep") {
		t.Errorf("missing roles: %s", got)
	}
}

func TestUniqueObservationPages(t *testing.T) {
	obs := []driftObservation{
		{Page: "a"}, {Page: "b"}, {Page: "a"}, {Page: ""},
	}
	got := uniqueObservationPages(obs)
	if len(got) != 2 {
		t.Errorf("got %v, want 2 unique non-empty", got)
	}
}

func TestCacheNeedsRecomputeMissingPriority(t *testing.T) {
	entry := CachedDriftEntry{
		Issues: []DriftIssue{{Page: "p", Issue: "i"}}, // no priority
	}
	if !cacheNeedsRecompute(entry) {
		t.Fatal("expected recompute on missing priority")
	}
}

func TestCacheNeedsRecomputeBogusPriority(t *testing.T) {
	entry := CachedDriftEntry{
		Issues: []DriftIssue{{Page: "p", Issue: "i", Priority: "huge", PriorityReason: "x"}},
	}
	if !cacheNeedsRecompute(entry) {
		t.Fatal("expected recompute on bogus priority")
	}
}

func TestCacheNeedsRecomputeAllValid(t *testing.T) {
	entry := CachedDriftEntry{
		Issues: []DriftIssue{
			{Page: "a", Issue: "i", Priority: PriorityLarge, PriorityReason: "r"},
			{Page: "b", Issue: "j", Priority: PrioritySmall, PriorityReason: "r"},
		},
	}
	if cacheNeedsRecompute(entry) {
		t.Fatal("expected no recompute when all priorities are valid")
	}
}

func TestCacheNeedsRecomputeEmptyIssues(t *testing.T) {
	// An entry with zero issues is a valid "no drift" cache hit; no recompute.
	entry := CachedDriftEntry{Issues: nil}
	if cacheNeedsRecompute(entry) {
		t.Fatal("empty issues should not recompute")
	}
}
