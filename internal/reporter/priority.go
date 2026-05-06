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

// filterGapsByPriority returns the screenshot gaps whose priority matches p,
// preserving input order.
func filterGapsByPriority(gaps []analyzer.ScreenshotGap, p analyzer.Priority) []analyzer.ScreenshotGap {
	var out []analyzer.ScreenshotGap
	for _, g := range gaps {
		if g.Priority == p {
			out = append(out, g)
		}
	}
	return out
}

// filterImageIssuesByPriority returns the image issues whose priority matches
// p, preserving input order.
func filterImageIssuesByPriority(issues []analyzer.ImageIssue, p analyzer.Priority) []analyzer.ImageIssue {
	var out []analyzer.ImageIssue
	for _, ii := range issues {
		if ii.Priority == p {
			out = append(out, ii)
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

// priorityClass returns the CSS class suffix used by the rendered site to
// color priority sub-headings and per-finding cards. Mirrors priorityHeading
// but returns lowercase (matches the `.ftg-priority--*` modifier convention).
func priorityClass(p analyzer.Priority) string {
	switch p {
	case analyzer.PriorityLarge:
		return "large"
	case analyzer.PriorityMedium:
		return "medium"
	case analyzer.PrioritySmall:
		return "small"
	default:
		return string(p)
	}
}
