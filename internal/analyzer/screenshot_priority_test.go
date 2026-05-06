package analyzer

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestScreenshotResponseItemParsesPriority(t *testing.T) {
	raw := []byte(`{"quoted_passage":"q","should_show":"s","suggested_alt":"a","insertion_hint":"i","priority":"large","priority_reason":"r"}`)
	var got screenshotResponseItem
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

func TestValidateScreenshotGapRejectsMissing(t *testing.T) {
	g := ScreenshotGap{PageURL: "u", QuotedPassage: "q"}
	if err := validateScreenshotGap(g); err == nil {
		t.Fatal("expected error for missing priority")
	}
}

func TestValidateScreenshotGapRejectsBogus(t *testing.T) {
	g := ScreenshotGap{
		PageURL: "u", QuotedPassage: "q",
		Priority: "huge", PriorityReason: "x",
	}
	if err := validateScreenshotGap(g); err == nil {
		t.Fatal("expected error for bogus priority")
	}
}

func TestValidateScreenshotGapAcceptsValid(t *testing.T) {
	g := ScreenshotGap{
		PageURL: "u", QuotedPassage: "q",
		Priority: PriorityLarge, PriorityReason: "r",
	}
	if err := validateScreenshotGap(g); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestBuildScreenshotPromptContainsRubric(t *testing.T) {
	out := buildScreenshotPrompt("https://x/quickstart", "body", nil)
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

func TestBuildDetectionPromptWithVerdictsContainsRubric(t *testing.T) {
	refs := []imageRef{{Src: "x.png", AltText: "a", OriginalIndex: 1}}
	verdicts := []ImageVerdict{{Index: "img-1", Matches: true}}
	out := buildDetectionPromptWithVerdicts("https://x/docs/api", "body", refs, verdicts)
	if !strings.Contains(out, "page_role") {
		t.Error("missing page_role hint in verdict-enriched prompt")
	}
	if !strings.Contains(out, "priority_reason") {
		t.Error("missing priority_reason mention")
	}
}
