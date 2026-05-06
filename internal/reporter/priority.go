package reporter

import "github.com/sandgardenhq/find-the-gaps/internal/analyzer"

// driftItem couples a feature name with one of its issues. Used during
// rendering when issues need to be regrouped by priority across features.
type driftItem struct {
	Feature string
	Issue   analyzer.DriftIssue
}

// flattenDrift produces one driftItem per (feature, issue) pair, preserving
// the original order: features in input order, issues in their per-feature
// order. Used as the input to filterDriftByPriority for grouped rendering.
func flattenDrift(findings []analyzer.DriftFinding) []driftItem {
	if len(findings) == 0 {
		return nil
	}
	var out []driftItem
	for _, f := range findings {
		for _, iss := range f.Issues {
			out = append(out, driftItem{Feature: f.Feature, Issue: iss})
		}
	}
	return out
}

// filterDriftByPriority returns the items whose issue priority matches p,
// preserving input order.
func filterDriftByPriority(items []driftItem, p analyzer.Priority) []driftItem {
	var out []driftItem
	for _, it := range items {
		if it.Issue.Priority == p {
			out = append(out, it)
		}
	}
	return out
}

// priorityHeading returns the user-facing capitalized form for use in
// Markdown sub-headings.
func priorityHeading(p analyzer.Priority) string {
	switch p {
	case analyzer.PriorityLarge:
		return "Large"
	case analyzer.PriorityMedium:
		return "Medium"
	case analyzer.PrioritySmall:
		return "Small"
	default:
		return string(p)
	}
}
