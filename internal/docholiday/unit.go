package docholiday

import (
	"sort"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// maxIssuesPerPrompt caps how many stale-doc issues a single Doc Holiday prompt
// addresses before the page is split into "(part K of M)" chunks. Five keeps a
// prompt focused enough for the agent to act on in one pass.
const maxIssuesPerPrompt = 5

// crossPageKey is the synthetic page key for drift issues with no page anchor.
const crossPageKey = ""

// staleItem is one (feature, inaccuracy) pair within a stale-docs unit.
type staleItem struct {
	feature string
	issue   string
}

// unit is one work-item that becomes exactly one Doc Holiday prompt.
type unit struct {
	category Category
	priority analyzer.Priority

	// stale fields
	page       string
	staleItems []staleItem
	part       int // 1-based; 1 when the page was not split
	parts      int // total chunks for this page; 1 when not split

	// missing fields
	feature   analyzer.FeatureEntry
	rationale string
}

// staleUnits flattens drift findings into per-page chunks of at most cap
// issues. Output is sorted by page for determinism; each chunk's priority is
// the most severe priority among its issues.
func staleUnits(drift []analyzer.DriftFinding, cap int) []unit {
	if cap <= 0 {
		cap = maxIssuesPerPrompt
	}
	byPage := map[string][]staleItem{}
	prioByPage := map[string][]analyzer.Priority{}
	var pageOrder []string
	for _, f := range drift {
		for _, iss := range f.Issues {
			if _, seen := byPage[iss.Page]; !seen {
				pageOrder = append(pageOrder, iss.Page)
			}
			byPage[iss.Page] = append(byPage[iss.Page], staleItem{feature: f.Feature, issue: iss.Issue})
			prioByPage[iss.Page] = append(prioByPage[iss.Page], iss.Priority)
		}
	}
	sort.Strings(pageOrder)

	var out []unit
	for _, page := range pageOrder {
		items := byPage[page]
		chunks := chunkStaleItems(items, cap)
		for i, chunk := range chunks {
			// priority of the chunk = max over the chunk's own issues
			var ps []analyzer.Priority
			for j := range chunk {
				// map chunk item back to its priority via position in items
				ps = append(ps, prioByPage[page][i*cap+j])
			}
			out = append(out, unit{
				category:   CategoryStale,
				priority:   maxPriority(ps),
				page:       page,
				staleItems: chunk,
				part:       i + 1,
				parts:      len(chunks),
			})
		}
	}
	return out
}

func chunkStaleItems(items []staleItem, cap int) [][]staleItem {
	var chunks [][]staleItem
	for i := 0; i < len(items); i += cap {
		end := i + cap
		if end > len(items) {
			end = len(items)
		}
		chunks = append(chunks, items[i:end])
	}
	return chunks
}
