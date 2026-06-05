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
