package reporter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/docholiday"
)

func TestBuildPromptsBytesStructure(t *testing.T) {
	prompts := []docholiday.Prompt{
		{Category: docholiday.CategoryStale, Heading: "docs/a.md", Note: "Addresses 1 stale-doc issue on this page.", Priority: analyzer.PriorityLarge, Body: "Update docs/a.md"},
		{Category: docholiday.CategoryMissing, Heading: "New page: Frob", Note: "No docs page covers this user-facing feature.", Priority: analyzer.PriorityLarge, Body: "Write a page for Frob"},
	}
	out := string(BuildPromptsBytes(prompts))
	for _, want := range []string{
		"# Doc Holiday Prompts",
		"doc.holiday",
		"## Fix Stale Documentation",
		"### Large",
		"#### docs/a.md",
		"_Addresses 1 stale-doc issue on this page._",
		"Update docs/a.md",
		"## Document New Features",
		"#### New page: Frob",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestBuildPromptsBytesEmptyCategories(t *testing.T) {
	out := string(BuildPromptsBytes(nil))
	if !strings.Contains(out, "## Fix Stale Documentation") || !strings.Contains(out, "## Document New Features") {
		t.Fatal("both category headers must always render")
	}
	if strings.Count(out, "_None found._") != 2 {
		t.Fatalf("each empty category should render _None found._:\n%s", out)
	}
}

func TestBuildPromptsBytesFenceEscalation(t *testing.T) {
	body := "Here is a code block:\n```go\nfmt.Println()\n```\ndone"
	out := string(BuildPromptsBytes([]docholiday.Prompt{
		{Category: docholiday.CategoryStale, Heading: "h", Note: "n", Priority: analyzer.PrioritySmall, Body: body},
	}))
	if !strings.Contains(out, "````") {
		t.Fatalf("a body containing ``` must be wrapped in a longer fence:\n%s", out)
	}
}

func TestBuildPromptsBytesEmptyBucketsOmitted(t *testing.T) {
	out := string(BuildPromptsBytes([]docholiday.Prompt{
		{Category: docholiday.CategoryStale, Heading: "h", Note: "n", Priority: analyzer.PriorityLarge, Body: "b"},
	}))
	if strings.Contains(out, "### Medium") || strings.Contains(out, "### Small") {
		t.Fatalf("empty priority buckets must be omitted:\n%s", out)
	}
}

func TestWritePromptsCreatesFile(t *testing.T) {
	dir := t.TempDir()
	if err := WritePrompts(dir, []docholiday.Prompt{
		{Category: docholiday.CategoryStale, Heading: "h", Note: "n", Priority: analyzer.PriorityLarge, Body: "b"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "prompts.md")); err != nil {
		t.Fatalf("prompts.md not written: %v", err)
	}
}
