package analyzer

import (
	"context"
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

func TestDriftInvestigator_ListFeatureSymbolsTool_PaginatesAndFiltersByName(t *testing.T) {
	// 75 synthetic symbols named "sym0" through "sym74" — lowercase so the
	// case-insensitive substring filter has unambiguous semantics.
	syms := make([]string, 0, 75)
	for i := 0; i < 75; i++ {
		syms = append(syms, fmt.Sprintf("sym%d", i))
	}
	entry := FeatureEntry{
		Feature: CodeFeature{Name: "F", Description: "d"},
		Symbols: syms,
	}
	tool := listFeatureSymbolsTool(entry)
	if tool.Name != "list_feature_symbols" {
		t.Fatalf("tool name = %q, want list_feature_symbols", tool.Name)
	}

	// limit=10, offset=20 → sym20..sym29.
	out, err := tool.Execute(context.Background(), `{"offset": 20, "limit": 10}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	got := extractSymNames(out)
	want := []string{"sym20", "sym21", "sym22", "sym23", "sym24", "sym25", "sym26", "sym27", "sym28", "sym29"}
	if !equalStringSlice(got, want) {
		t.Fatalf("pagination wrong:\n got: %v\nwant: %v\nout: %s", got, want, out)
	}

	// filter "sym1" — case-insensitive substring match. Must include sym1
	// and sym10..sym19 (all contain "sym1") but exclude sym20..sym29.
	out2, err := tool.Execute(context.Background(), `{"filter": "sym1"}`)
	if err != nil {
		t.Fatalf("filter invoke: %v", err)
	}
	if !strings.Contains(out2, "sym10") {
		t.Fatalf("filter should include sym10: %s", out2)
	}
	// Reject substring matches against sym20+ — these would only appear if
	// the filter was accidentally implemented as equality or prefix-only.
	// Match the symbol terminator (space, comma, newline, EOL) so "sym2" as
	// a header substring like "of 11 symbols" doesn't false-positive.
	for _, bad := range []string{"sym20", "sym21", "sym22", "sym23", "sym24"} {
		if strings.Contains(out2, bad) {
			t.Fatalf("filter %q should not include %s: %s", "sym1", bad, out2)
		}
	}
}

func TestDriftInvestigator_ListFeatureSymbolsTool_ClampsBounds(t *testing.T) {
	entry := FeatureEntry{
		Feature: CodeFeature{Name: "F"},
		Symbols: []string{"sym0", "sym1", "sym2"},
	}
	tool := listFeatureSymbolsTool(entry)

	// offset past end → empty slice, not panic.
	out, err := tool.Execute(context.Background(), `{"offset": 99, "limit": 10}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if names := extractSymNames(out); len(names) != 0 {
		t.Fatalf("expected no symbols past end, got %v", names)
	}

	// limit > 200 → clamped to 200 (not exhibited at this size; just confirm
	// the call succeeds and returns all 3 entries).
	out2, err := tool.Execute(context.Background(), `{"limit": 9999}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if names := extractSymNames(out2); len(names) != 3 {
		t.Fatalf("expected 3 entries, got %v", names)
	}
}

func TestDriftInvestigator_ListFeatureTools_InvalidJSON(t *testing.T) {
	// Bad JSON must come back to the LLM as a tool-result string rather than
	// crashing the agent loop — same shape as readFileTool's error handling.
	symTool := listFeatureSymbolsTool(FeatureEntry{Symbols: []string{"a"}})
	out, err := symTool.Execute(context.Background(), `{"offset":`)
	if err != nil {
		t.Fatalf("symbols tool returned error instead of string: %v", err)
	}
	if !strings.Contains(out, "error parsing arguments") {
		t.Fatalf("expected parse-error message in symbols tool output, got: %s", out)
	}

	pageTool := listFeaturePagesTool([]string{"https://x"})
	out2, err := pageTool.Execute(context.Background(), `not-json`)
	if err != nil {
		t.Fatalf("pages tool returned error instead of string: %v", err)
	}
	if !strings.Contains(out2, "error parsing arguments") {
		t.Fatalf("expected parse-error message in pages tool output, got: %s", out2)
	}
}

func TestDriftInvestigator_ListFeaturePagesTool_Paginates(t *testing.T) {
	pages := []string{
		"https://example.com/a",
		"https://example.com/b",
		"https://example.com/c",
		"https://example.com/d",
	}
	tool := listFeaturePagesTool(pages)
	if tool.Name != "list_feature_pages" {
		t.Fatalf("tool name = %q, want list_feature_pages", tool.Name)
	}

	out, err := tool.Execute(context.Background(), `{"offset": 1, "limit": 2}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if !strings.Contains(out, "https://example.com/b") || !strings.Contains(out, "https://example.com/c") {
		t.Fatalf("expected pages b and c, got: %s", out)
	}
	if strings.Contains(out, "https://example.com/a") || strings.Contains(out, "https://example.com/d") {
		t.Fatalf("expected only pages b and c, got: %s", out)
	}
}

// extractSymNames returns every "sym<digits>" token in s, in source order. Used
// to assert the pagination order of the list_feature_symbols tool output
// without depending on the exact human-readable rendering.
func extractSymNames(s string) []string {
	var out []string
	for i := 0; i < len(s); i++ {
		if i+3 <= len(s) && s[i:i+3] == "sym" {
			j := i + 3
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			if j > i+3 {
				out = append(out, s[i:j])
				i = j - 1
			}
		}
	}
	return out
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
