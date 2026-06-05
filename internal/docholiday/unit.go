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

// staleItem is one (feature, inaccuracy) pair within a stale-docs unit. The
// priority travels with the item so a chunk's rolled-up priority is computed
// over the chunk's own items after sorting, independent of input order.
type staleItem struct {
	feature  string
	issue    string
	priority analyzer.Priority
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

// staleUnits flattens drift findings into per-page chunks of at most limit
// issues. Output is sorted by page for determinism; each chunk's priority is
// the most severe priority among its issues.
func staleUnits(drift []analyzer.DriftFinding, limit int) []unit {
	if limit <= 0 {
		limit = maxIssuesPerPrompt
	}
	byPage := map[string][]staleItem{}
	var pageOrder []string
	for _, f := range drift {
		for _, iss := range f.Issues {
			if _, seen := byPage[iss.Page]; !seen {
				pageOrder = append(pageOrder, iss.Page)
			}
			byPage[iss.Page] = append(byPage[iss.Page], staleItem{feature: f.Feature, issue: iss.Issue, priority: iss.Priority})
		}
	}
	sort.Strings(pageOrder)

	var out []unit
	for _, page := range pageOrder {
		items := byPage[page]
		// Sort each page's items into a stable total order before chunking so the
		// resulting chunks (and thus unitKey + rendered findings) are invariant to
		// the arrival order of the incoming drift findings (worker-completion order
		// on the live path vs feature-sorted on the warm-cache path).
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].feature != items[j].feature {
				return items[i].feature < items[j].feature
			}
			return items[i].issue < items[j].issue
		})
		chunks := chunkStaleItems(items, limit)
		for i, chunk := range chunks {
			// priority of the chunk = max over the chunk's own items
			ps := make([]analyzer.Priority, 0, len(chunk))
			for _, it := range chunk {
				ps = append(ps, it.priority)
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

func chunkStaleItems(items []staleItem, limit int) [][]staleItem {
	var chunks [][]staleItem
	for i := 0; i < len(items); i += limit {
		end := i + limit
		if end > len(items) {
			end = len(items)
		}
		chunks = append(chunks, items[i:end])
	}
	return chunks
}

// missingDocPriority assigns a priority to an undocumented-feature prompt.
// Undocumented features carry no priority signal in the data model, so we use
// a single deterministic default. This is the one place to enrich later (e.g.
// derive from layer or usage) — keep it a function so callers never inline a
// constant.
func missingDocPriority(_ analyzer.FeatureEntry) analyzer.Priority {
	return analyzer.PriorityLarge
}

// missingUnits builds one unit per undocumented feature, in the input order
// (UndocumentedFeatures already returns a stable insertion order).
func missingUnits(feats []analyzer.FeatureEntry, rationales map[string]string) []unit {
	out := make([]unit, 0, len(feats))
	for _, f := range feats {
		out = append(out, unit{
			category:  CategoryMissing,
			priority:  missingDocPriority(f),
			feature:   f,
			rationale: rationales[f.Feature.Name],
		})
	}
	return out
}
