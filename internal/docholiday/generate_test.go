package docholiday

import (
	"context"
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// fakeCompleter records the prompt it received and returns a canned body.
type fakeCompleter struct {
	gotPrompt string
	reply     string
	err       error
}

func (f *fakeCompleter) Complete(_ context.Context, prompt string) (string, error) {
	f.gotPrompt = prompt
	return f.reply, f.err
}

func TestGenerateOneStaleUsesStaleSkillAndFindings(t *testing.T) {
	fc := &fakeCompleter{reply: "Update docs/api.md to ..."}
	u := unit{category: CategoryStale, priority: analyzer.PriorityLarge, page: "docs/api.md",
		staleItems: []staleItem{{feature: "Auth", issue: "Connect() needs ctx"}}, part: 1, parts: 1}
	p, err := generateOne(context.Background(), fc, u)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fc.gotPrompt, skillBody(staleDocsSkillRaw)) {
		t.Fatal("prompt should embed the stale-docs skill body")
	}
	if !strings.Contains(fc.gotPrompt, "Connect() needs ctx") {
		t.Fatal("prompt should embed the unit findings")
	}
	if p.Body != "Update docs/api.md to ..." {
		t.Fatalf("body should be the completer reply, got %q", p.Body)
	}
	if p.Category != CategoryStale || p.Priority != analyzer.PriorityLarge {
		t.Fatalf("metadata not carried: %+v", p)
	}
	if !strings.Contains(p.Heading, "docs/api.md") {
		t.Fatalf("heading: %q", p.Heading)
	}
}

func TestGenerateOneMissingUsesNewFeatureSkill(t *testing.T) {
	fc := &fakeCompleter{reply: "Write a new page for Frobnicate"}
	u := unit{category: CategoryMissing, priority: analyzer.PriorityLarge,
		feature: analyzer.FeatureEntry{Feature: analyzer.CodeFeature{Name: "Frobnicate"}}}
	p, err := generateOne(context.Background(), fc, u)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fc.gotPrompt, skillBody(newFeatureSkillRaw)) {
		t.Fatal("prompt should embed the new-feature skill body")
	}
	if p.Heading != "New page: Frobnicate" {
		t.Fatalf("heading: %q", p.Heading)
	}
}

func TestGenerateOneTrimsBody(t *testing.T) {
	fc := &fakeCompleter{reply: "\n  prompt text \n\n"}
	u := unit{category: CategoryMissing, feature: analyzer.FeatureEntry{Feature: analyzer.CodeFeature{Name: "X"}}}
	p, _ := generateOne(context.Background(), fc, u)
	if p.Body != "prompt text" {
		t.Fatalf("body should be trimmed, got %q", p.Body)
	}
}
