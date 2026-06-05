package docholiday

import "github.com/sandgardenhq/find-the-gaps/internal/analyzer"

// Category distinguishes the two kinds of Doc Holiday prompt this package emits.
type Category string

const (
	CategoryStale   Category = "stale"   // fix documentation that no longer matches the code
	CategoryMissing Category = "missing" // write a page for an undocumented feature
)

// Prompt is one ready-to-paste Doc Holiday prompt plus the metadata the
// reporter needs to render it under the right heading and priority bucket.
type Prompt struct {
	Category Category          `json:"category"`
	Heading  string            `json:"heading"`  // e.g. "docs/api.md (+2 more)" or "New page: Frobnicate"
	Note     string            `json:"note"`     // one-line italic note under the heading
	Priority analyzer.Priority `json:"priority"` // bucket the prompt renders under
	Body     string            `json:"body"`     // the LLM-authored prompt text
}

// Input carries the already-computed findings the generator turns into prompts.
// Rationales maps an undocumented feature name to its "why document this"
// blurb (reusing the analyzer.WhyDocument output the CLI already computes);
// missing keys are fine and simply omit the note from that prompt.
type Input struct {
	Drift        []analyzer.DriftFinding
	Undocumented []analyzer.FeatureEntry
	Rationales   map[string]string
}

func priorityRank(p analyzer.Priority) int {
	switch p {
	case analyzer.PriorityLarge:
		return 0
	case analyzer.PriorityMedium:
		return 1
	case analyzer.PrioritySmall:
		return 2
	default:
		return 3
	}
}

// maxPriority returns the most severe priority in ps (large > medium > small).
// An empty slice yields small — the least-severe default.
func maxPriority(ps []analyzer.Priority) analyzer.Priority {
	best := analyzer.PrioritySmall
	bestRank := priorityRank(best)
	for _, p := range ps {
		if r := priorityRank(p); r < bestRank {
			best, bestRank = p, r
		}
	}
	return best
}
