package docholiday

import (
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestRenderStaleFindingsListsPageAndIssues(t *testing.T) {
	u := unit{
		category: CategoryStale,
		page:     "docs/api.md",
		staleItems: []staleItem{
			{feature: "Auth", issue: "Connect() now needs a ctx"},
			{feature: "Auth", issue: "Token TTL is 1h not 24h"},
		},
	}
	got := renderUnitFindings(u)
	for _, want := range []string{"docs/api.md", "Connect() now needs a ctx", "Token TTL is 1h not 24h", "Auth"} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered findings missing %q:\n%s", want, got)
		}
	}
}

func TestRenderMissingFindingsListsFilesAndWhy(t *testing.T) {
	u := unit{
		category:  CategoryMissing,
		feature:   analyzer.FeatureEntry{Feature: analyzer.CodeFeature{Name: "Frobnicate", Description: "frobs things"}, Files: []string{"frob.go"}, Symbols: []string{"Frobnicate"}},
		rationale: "shipped last month",
	}
	got := renderUnitFindings(u)
	for _, want := range []string{"Frobnicate", "frobs things", "frob.go", "shipped last month"} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered findings missing %q:\n%s", want, got)
		}
	}
}

func TestStaleHeadingShowsPageAndOverflow(t *testing.T) {
	u := unit{category: CategoryStale, page: "docs/api.md", staleItems: make([]staleItem, 3), part: 1, parts: 1}
	if got := unitHeading(u); !strings.Contains(got, "docs/api.md") || !strings.Contains(got, "+2 more") {
		t.Fatalf("heading should name page and overflow count: %q", got)
	}
}

func TestStaleHeadingShowsPartWhenSplit(t *testing.T) {
	u := unit{category: CategoryStale, page: "docs/api.md", staleItems: make([]staleItem, 3), part: 2, parts: 3}
	if got := unitHeading(u); !strings.Contains(got, "part 2 of 3") {
		t.Fatalf("split heading should carry part suffix: %q", got)
	}
}

func TestStaleNoteSingularAndPlural(t *testing.T) {
	one := unit{category: CategoryStale, staleItems: make([]staleItem, 1)}
	if got := unitNote(one); !strings.Contains(got, "1 stale-doc issue") || strings.Contains(got, "issues") {
		t.Fatalf("single stale item should read singular: %q", got)
	}
	two := unit{category: CategoryStale, staleItems: make([]staleItem, 2)}
	if got := unitNote(two); !strings.Contains(got, "2 stale-doc issues") {
		t.Fatalf("two stale items should read plural: %q", got)
	}
}

func TestRenderStaleFindingsCrossCutting(t *testing.T) {
	u := unit{
		category:   CategoryStale,
		page:       crossPageKey,
		staleItems: []staleItem{{feature: "Auth", issue: "drifted"}},
	}
	if got := renderUnitFindings(u); !strings.Contains(got, "cross-cutting") {
		t.Fatalf("empty-page stale unit should render cross-cutting line: %q", got)
	}
}

func TestStaleHeadingCrossCutting(t *testing.T) {
	u := unit{category: CategoryStale, page: crossPageKey, staleItems: make([]staleItem, 1), part: 1, parts: 1}
	if got := unitHeading(u); got != "Cross-cutting documentation" {
		t.Fatalf("empty-page heading should read cross-cutting: %q", got)
	}
}

func TestMissingHeadingAndNote(t *testing.T) {
	u := unit{category: CategoryMissing, feature: analyzer.FeatureEntry{Feature: analyzer.CodeFeature{Name: "Frobnicate"}}}
	if got := unitHeading(u); got != "New page: Frobnicate" {
		t.Fatalf("missing heading: %q", got)
	}
	if got := unitNote(u); !strings.Contains(strings.ToLower(got), "no docs page") {
		t.Fatalf("missing note: %q", got)
	}
}
