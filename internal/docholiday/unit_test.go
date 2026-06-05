package docholiday

import (
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func driftIssue(page, issue string, p analyzer.Priority) analyzer.DriftIssue {
	return analyzer.DriftIssue{Page: page, Issue: issue, Priority: p, PriorityReason: "because"}
}

func TestStaleUnitsGroupByPage(t *testing.T) {
	drift := []analyzer.DriftFinding{
		{Feature: "Auth", Issues: []analyzer.DriftIssue{
			driftIssue("docs/a.md", "x", analyzer.PrioritySmall),
		}},
		{Feature: "Sync", Issues: []analyzer.DriftIssue{
			driftIssue("docs/a.md", "y", analyzer.PriorityLarge),
			driftIssue("docs/b.md", "z", analyzer.PriorityMedium),
		}},
	}
	units := staleUnits(drift, 5)
	if len(units) != 2 {
		t.Fatalf("want 2 page-units, got %d", len(units))
	}
	// docs/a.md unit carries both issues and rolls up to large.
	var a *unit
	for i := range units {
		if units[i].page == "docs/a.md" {
			a = &units[i]
		}
	}
	if a == nil {
		t.Fatal("missing docs/a.md unit")
	}
	if len(a.staleItems) != 2 {
		t.Fatalf("docs/a.md should hold 2 issues, got %d", len(a.staleItems))
	}
	if a.priority != analyzer.PriorityLarge {
		t.Fatalf("docs/a.md priority should roll up to large, got %q", a.priority)
	}
}

func TestStaleUnitsChunkWhenOverCap(t *testing.T) {
	var issues []analyzer.DriftIssue
	for i := 0; i < 7; i++ {
		issues = append(issues, driftIssue("docs/big.md", "issue", analyzer.PrioritySmall))
	}
	drift := []analyzer.DriftFinding{{Feature: "F", Issues: issues}}
	units := staleUnits(drift, 3)
	if len(units) != 3 { // 3 + 3 + 1
		t.Fatalf("7 issues at cap 3 should produce 3 chunks, got %d", len(units))
	}
	for _, u := range units {
		if u.parts != 3 {
			t.Fatalf("each chunk should record parts=3, got %d", u.parts)
		}
		if len(u.staleItems) > 3 {
			t.Fatalf("chunk exceeds cap: %d items", len(u.staleItems))
		}
	}
	if units[0].part != 1 || units[2].part != 3 {
		t.Fatalf("part numbers should be 1..3, got %d..%d", units[0].part, units[2].part)
	}
}

func TestStaleUnitsZeroCapFallsBackToDefault(t *testing.T) {
	var issues []analyzer.DriftIssue
	for i := 0; i < 6; i++ {
		issues = append(issues, driftIssue("docs/big.md", "issue", analyzer.PrioritySmall))
	}
	drift := []analyzer.DriftFinding{{Feature: "F", Issues: issues}}
	units := staleUnits(drift, 0)
	if len(units) != 2 { // default cap 5 -> 5 + 1
		t.Fatalf("6 issues at cap 0 should default to cap %d producing 2 chunks, got %d", maxIssuesPerPrompt, len(units))
	}
}

func TestStaleUnitsAreDeterministic(t *testing.T) {
	drift := []analyzer.DriftFinding{
		{Feature: "B", Issues: []analyzer.DriftIssue{driftIssue("docs/z.md", "1", analyzer.PrioritySmall)}},
		{Feature: "A", Issues: []analyzer.DriftIssue{driftIssue("docs/a.md", "2", analyzer.PrioritySmall)}},
	}
	first := staleUnits(drift, 5)
	second := staleUnits(drift, 5)
	if first[0].page != second[0].page || first[1].page != second[1].page {
		t.Fatal("staleUnits must be deterministic across calls")
	}
	if first[0].page != "docs/a.md" {
		t.Fatalf("units should sort by page; first should be docs/a.md, got %q", first[0].page)
	}
}

// TestStaleUnitsKeyInvariantUnderInputOrder proves that two drift slices that
// place the SAME two issues from two DIFFERENT features on the SAME page, but in
// OPPOSITE arrival order, collapse to a single unit whose cache key and rendered
// findings block are byte-identical. Before the in-page sort, the staleItems
// slice preserved arrival order, so the two permutations produced different
// unitKeys (cache thrash) and a different rendered findings block (so the
// LLM-authored Body and prompts.md differed) — violating the determinism
// guarantee across --workers and cold-vs-warm runs.
func TestStaleUnitsKeyInvariantUnderInputOrder(t *testing.T) {
	orderA := []analyzer.DriftFinding{
		{Feature: "FeatureX", Issues: []analyzer.DriftIssue{driftIssue("docs/p.md", "issue1", analyzer.PrioritySmall)}},
		{Feature: "FeatureY", Issues: []analyzer.DriftIssue{driftIssue("docs/p.md", "issue2", analyzer.PriorityLarge)}},
	}
	orderB := []analyzer.DriftFinding{
		{Feature: "FeatureY", Issues: []analyzer.DriftIssue{driftIssue("docs/p.md", "issue2", analyzer.PriorityLarge)}},
		{Feature: "FeatureX", Issues: []analyzer.DriftIssue{driftIssue("docs/p.md", "issue1", analyzer.PrioritySmall)}},
	}

	unitsA := staleUnits(orderA, 5)
	unitsB := staleUnits(orderB, 5)
	if len(unitsA) != 1 || len(unitsB) != 1 {
		t.Fatalf("expected exactly one unit per ordering, got %d and %d", len(unitsA), len(unitsB))
	}
	a, b := unitsA[0], unitsB[0]

	if unitKey(a) != unitKey(b) {
		t.Fatalf("unitKey must be invariant to drift-finding arrival order:\nA=%s\nB=%s", unitKey(a), unitKey(b))
	}
	if renderUnitFindings(a) != renderUnitFindings(b) {
		t.Fatalf("rendered findings must be invariant to arrival order:\nA:\n%s\nB:\n%s", renderUnitFindings(a), renderUnitFindings(b))
	}
	if a.priority != analyzer.PriorityLarge {
		t.Fatalf("chunk priority should roll up to large, got %q", a.priority)
	}
}

func TestMissingUnitsOnePerFeature(t *testing.T) {
	feats := []analyzer.FeatureEntry{
		{Feature: analyzer.CodeFeature{Name: "Frobnicate", UserFacing: true}, Files: []string{"a.go"}},
		{Feature: analyzer.CodeFeature{Name: "Sync"}, Files: []string{"b.go"}},
	}
	rats := map[string]string{"Frobnicate": "users hit this daily"}
	units := missingUnits(feats, rats)
	if len(units) != 2 {
		t.Fatalf("want one unit per feature, got %d", len(units))
	}
	if units[0].feature.Feature.Name != "Frobnicate" || units[0].category != CategoryMissing {
		t.Fatalf("unexpected first unit: %+v", units[0])
	}
	if units[0].rationale != "users hit this daily" {
		t.Fatalf("rationale not threaded: %q", units[0].rationale)
	}
	if units[1].rationale != "" {
		t.Fatalf("missing rationale should be empty, got %q", units[1].rationale)
	}
}

func TestMissingUnitsGetDefaultPriority(t *testing.T) {
	feats := []analyzer.FeatureEntry{{Feature: analyzer.CodeFeature{Name: "X"}}}
	units := missingUnits(feats, nil)
	if units[0].priority != missingDocPriority(feats[0]) {
		t.Fatalf("missing unit priority should come from missingDocPriority")
	}
}
