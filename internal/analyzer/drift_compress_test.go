package analyzer

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/chunker"
)

// makeFakeSymbolNames returns a slice of n synthetic symbol names alternating
// exported / unexported to exercise the entry-point heuristic in
// topEntryPoints.
func makeFakeSymbolNames(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			out = append(out, fmt.Sprintf("Sym%d", i))
		} else {
			out = append(out, fmt.Sprintf("sym%d", i))
		}
	}
	return out
}

// makeFakeFiles returns a slice of n synthetic repo-relative file paths.
func makeFakeFiles(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("internal/pkg%d/file%d.go", i%4, i))
	}
	return out
}

// makeFakePages returns a slice of n synthetic doc page URLs.
func makeFakePages(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("https://example.com/docs/page-%d", i))
	}
	return out
}

func TestDriftInvestigator_SystemPromptStaysUnderBudget_ForLargeFeature(t *testing.T) {
	entry := FeatureEntry{
		Feature: CodeFeature{
			Name:        "Big",
			Description: "A feature spanning many files.",
		},
		Files:   makeFakeFiles(12),
		Symbols: makeFakeSymbolNames(200),
	}
	pages := makeFakePages(40)

	prompt := buildInvestigatorSystemPrompt(entry, pages)
	if got := chunker.EstimateTokens(prompt); got > 4000 {
		t.Fatalf("compressed system prompt should stay under 4K tokens, got %d", got)
	}
	// Sanity: prompt mentions the symbol/page COUNTS, not every entry.
	if !strings.Contains(prompt, "200 symbols") || !strings.Contains(prompt, "40 pages") {
		t.Fatalf("expected counts in compressed prompt, got:\n%s", prompt)
	}
	// Should not inline the full symbol list (entry-point cap is 10).
	if strings.Contains(prompt, "Sym198") || strings.Contains(prompt, "sym199") {
		t.Fatalf("compressed prompt should not inline all 200 symbols")
	}
	// Top entry-point heuristic: exported symbols come first, capped at 10.
	if !strings.Contains(prompt, "Sym0") {
		t.Fatalf("expected first exported symbol Sym0 to appear in entry-points list")
	}
}

func TestBuildInvestigatorSystemPrompt_MentionsListTools(t *testing.T) {
	entry := FeatureEntry{
		Feature: CodeFeature{Name: "F", Description: "d"},
		Files:   []string{"a.go"},
		Symbols: []string{"A"},
	}
	prompt := buildInvestigatorSystemPrompt(entry, []string{"https://example.com/p"})
	for _, want := range []string{"list_feature_symbols", "list_feature_pages", "note_observation"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("expected prompt to reference tool %q, got:\n%s", want, prompt)
		}
	}
}

func TestTopEntryPoints_PrefersExported(t *testing.T) {
	got := topEntryPoints([]string{"alpha", "Beta", "gamma", "Delta"}, 3)
	want := []string{"Beta", "Delta", "alpha"}
	if !equalStringSlice(got, want) {
		t.Fatalf("topEntryPoints = %v, want %v", got, want)
	}
}

func TestTopEntryPoints_CapsAtN(t *testing.T) {
	in := []string{"A", "B", "C", "D", "E"}
	got := topEntryPoints(in, 2)
	if len(got) != 2 {
		t.Fatalf("expected cap=2, got %d entries: %v", len(got), got)
	}
}

func TestTopEntryPoints_HandlesEmpty(t *testing.T) {
	if got := topEntryPoints(nil, 10); len(got) != 0 {
		t.Fatalf("expected empty result for nil input, got %v", got)
	}
}

func TestBuildInvestigatorSystemPrompt_FitsLongDescription(t *testing.T) {
	// A pathologically long description should be trimmed via chunker.Fit so
	// it can't single-handedly blow the budget.
	longDesc := strings.Repeat("This feature does many things. ", 5000)
	entry := FeatureEntry{
		Feature: CodeFeature{Name: "F", Description: longDesc},
		Files:   []string{"a.go"},
		Symbols: []string{"A"},
	}
	prompt := buildInvestigatorSystemPrompt(entry, []string{"https://example.com/p"})
	if got := chunker.EstimateTokens(prompt); got > 4000 {
		t.Fatalf("prompt should stay under 4K tokens even with huge description, got %d", got)
	}
}
