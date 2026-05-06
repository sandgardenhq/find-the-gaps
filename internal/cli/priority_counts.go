package cli

import (
	"fmt"
	"strings"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// driftPriorityCounts returns the NL/NM/NS breakdown across every drift issue
// in findings, formatted as "<total> issues: NL · NM · NS". Returns the empty
// string when findings is empty so callers can append it conditionally.
func driftPriorityCounts(findings []analyzer.DriftFinding) string {
	var l, m, s, total int
	for _, f := range findings {
		for _, iss := range f.Issues {
			total++
			switch iss.Priority {
			case analyzer.PriorityLarge:
				l++
			case analyzer.PriorityMedium:
				m++
			case analyzer.PrioritySmall:
				s++
			}
		}
	}
	if total == 0 {
		return ""
	}
	return fmt.Sprintf("%d issues: %dL · %dM · %dS", total, l, m, s)
}

// screenshotsPriorityCounts returns the per-priority breakdown for the three
// finding kinds in a ScreenshotResult, formatted compactly so it can fit on
// the existing reports line. Empty kinds are omitted; the empty string is
// returned only when ALL three are empty.
func screenshotsPriorityCounts(res analyzer.ScreenshotResult) string {
	parts := []string{}
	if c := gapsCount(res.MissingGaps); c != "" {
		parts = append(parts, c+" missing")
	}
	if c := imageIssueCount(res.ImageIssues); c != "" {
		parts = append(parts, c+" image issues")
	}
	if c := gapsCount(res.PossiblyCovered); c != "" {
		parts = append(parts, c+" possibly covered")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "; ")
}

func gapsCount(gaps []analyzer.ScreenshotGap) string {
	if len(gaps) == 0 {
		return ""
	}
	var l, m, s int
	for _, g := range gaps {
		switch g.Priority {
		case analyzer.PriorityLarge:
			l++
		case analyzer.PriorityMedium:
			m++
		case analyzer.PrioritySmall:
			s++
		}
	}
	return fmt.Sprintf("%d (%dL · %dM · %dS)", len(gaps), l, m, s)
}

func imageIssueCount(issues []analyzer.ImageIssue) string {
	if len(issues) == 0 {
		return ""
	}
	var l, m, s int
	for _, ii := range issues {
		switch ii.Priority {
		case analyzer.PriorityLarge:
			l++
		case analyzer.PriorityMedium:
			m++
		case analyzer.PrioritySmall:
			s++
		}
	}
	return fmt.Sprintf("%d (%dL · %dM · %dS)", len(issues), l, m, s)
}
