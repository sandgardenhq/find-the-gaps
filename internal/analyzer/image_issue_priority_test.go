package analyzer

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestImageIssueParsesPriority(t *testing.T) {
	raw := []byte(`{"index":"img-1","src":"x.png","reason":"r","suggested_action":"replace","priority":"large","priority_reason":"r"}`)
	var got ImageIssue
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Priority != PriorityLarge {
		t.Errorf("Priority = %q", got.Priority)
	}
	if got.PriorityReason != "r" {
		t.Errorf("PriorityReason = %q", got.PriorityReason)
	}
}

func TestValidateImageIssueRejectsMissing(t *testing.T) {
	ii := ImageIssue{Index: "img-1", Reason: "r", SuggestedAction: "replace"}
	if err := validateImageIssue(ii); err == nil {
		t.Fatal("expected error for missing priority")
	}
}

func TestValidateImageIssueAcceptsValid(t *testing.T) {
	ii := ImageIssue{
		Index: "img-1", Reason: "r", SuggestedAction: "replace",
		Priority: PrioritySmall, PriorityReason: "r",
	}
	if err := validateImageIssue(ii); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestBuildRelevancePromptContainsRubric(t *testing.T) {
	page := DocPage{URL: "https://x/quickstart", Content: "body"}
	batch := []imageRef{{Src: "a.png", AltText: "a", OriginalIndex: 1}}
	out := buildRelevancePrompt(page, batch)
	if !strings.Contains(out, "page_role") {
		t.Error("missing page_role hint")
	}
	if !strings.Contains(out, "priority_reason") {
		t.Error("missing priority_reason mention")
	}
	if !strings.Contains(out, "quickstart") {
		t.Error("page_role value not injected")
	}
}
