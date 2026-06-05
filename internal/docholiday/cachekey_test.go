package docholiday

import (
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestUnitKeyStableForSameContent(t *testing.T) {
	u := unit{category: CategoryStale, page: "p", staleItems: []staleItem{{feature: "F", issue: "i"}}, part: 1, parts: 1}
	if unitKey(u) != unitKey(u) {
		t.Fatal("unitKey must be stable for identical units")
	}
}

func TestUnitKeyChangesWhenIssueChanges(t *testing.T) {
	a := unit{category: CategoryStale, page: "p", staleItems: []staleItem{{feature: "F", issue: "i1"}}, part: 1, parts: 1}
	b := unit{category: CategoryStale, page: "p", staleItems: []staleItem{{feature: "F", issue: "i2"}}, part: 1, parts: 1}
	if unitKey(a) == unitKey(b) {
		t.Fatal("changing an issue must change the key")
	}
}

func TestUnitKeyChangesWithSkillVersion(t *testing.T) {
	u := unit{category: CategoryStale, page: "p", staleItems: []staleItem{{feature: "F", issue: "i"}}, part: 1, parts: 1}
	k1 := unitKeyWithSkill(u, "skill-v1")
	k2 := unitKeyWithSkill(u, "skill-v2")
	if k1 == k2 {
		t.Fatal("a skill-body change must invalidate the key")
	}
}

func TestUnitKeyMissingFeatureUsesFilesAndSymbols(t *testing.T) {
	a := unit{category: CategoryMissing, feature: analyzer.FeatureEntry{Feature: analyzer.CodeFeature{Name: "X"}, Files: []string{"a.go"}}}
	b := unit{category: CategoryMissing, feature: analyzer.FeatureEntry{Feature: analyzer.CodeFeature{Name: "X"}, Files: []string{"b.go"}}}
	if unitKey(a) == unitKey(b) {
		t.Fatal("changing implementing files must change a missing-feature key")
	}
}

func TestUnitKeyMissingFeatureUsesDescription(t *testing.T) {
	a := unit{category: CategoryMissing, feature: analyzer.FeatureEntry{Feature: analyzer.CodeFeature{Name: "X", Description: "does one thing"}}}
	b := unit{category: CategoryMissing, feature: analyzer.FeatureEntry{Feature: analyzer.CodeFeature{Name: "X", Description: "does another thing"}}}
	if unitKey(a) == unitKey(b) {
		t.Fatal("changing the feature description must change a missing-feature key")
	}
}
