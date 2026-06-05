package docholiday

import (
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestPriorityRankOrders(t *testing.T) {
	if !(priorityRank(analyzer.PriorityLarge) < priorityRank(analyzer.PriorityMedium) &&
		priorityRank(analyzer.PriorityMedium) < priorityRank(analyzer.PrioritySmall)) {
		t.Fatal("priorityRank must order large < medium < small")
	}
}

func TestPriorityRankUnknownIsLeastSevere(t *testing.T) {
	if !(priorityRank(analyzer.Priority("bogus")) > priorityRank(analyzer.PrioritySmall)) {
		t.Fatal("an unknown priority must rank below (greater than) small")
	}
}

func TestMaxPriorityPicksMostSevere(t *testing.T) {
	got := maxPriority([]analyzer.Priority{analyzer.PrioritySmall, analyzer.PriorityLarge, analyzer.PriorityMedium})
	if got != analyzer.PriorityLarge {
		t.Fatalf("want large, got %q", got)
	}
}

func TestMaxPriorityEmptyIsSmall(t *testing.T) {
	if got := maxPriority(nil); got != analyzer.PrioritySmall {
		t.Fatalf("empty should default to small, got %q", got)
	}
}
