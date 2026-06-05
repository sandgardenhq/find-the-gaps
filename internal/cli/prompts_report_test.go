package cli

import (
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/docholiday"
)

func TestPromptsCounts(t *testing.T) {
	got := promptsCounts([]docholiday.Prompt{
		{Category: docholiday.CategoryStale, Priority: analyzer.PriorityLarge},
		{Category: docholiday.CategoryStale, Priority: analyzer.PrioritySmall},
		{Category: docholiday.CategoryMissing, Priority: analyzer.PriorityLarge},
	})
	if got != "2 stale · 1 new" {
		t.Fatalf("got %q", got)
	}
}

func TestPromptsCountsEmpty(t *testing.T) {
	if got := promptsCounts(nil); got != "0" {
		t.Fatalf("got %q", got)
	}
}
