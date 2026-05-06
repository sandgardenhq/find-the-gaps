package cli

import (
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestDriftPriorityCounts(t *testing.T) {
	findings := []analyzer.DriftFinding{
		{Feature: "x", Issues: []analyzer.DriftIssue{
			{Priority: analyzer.PriorityLarge, PriorityReason: "r"},
			{Priority: analyzer.PriorityLarge, PriorityReason: "r"},
			{Priority: analyzer.PriorityMedium, PriorityReason: "r"},
		}},
		{Feature: "y", Issues: []analyzer.DriftIssue{
			{Priority: analyzer.PrioritySmall, PriorityReason: "r"},
		}},
	}
	got := driftPriorityCounts(findings)
	want := "4 issues: 2L · 1M · 1S"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDriftPriorityCountsEmpty(t *testing.T) {
	if got := driftPriorityCounts(nil); got != "" {
		t.Errorf("expected empty for no findings, got %q", got)
	}
}

func TestScreenshotsPriorityCountsAllKinds(t *testing.T) {
	res := analyzer.ScreenshotResult{
		MissingGaps: []analyzer.ScreenshotGap{
			{Priority: analyzer.PriorityLarge}, {Priority: analyzer.PriorityMedium},
		},
		ImageIssues: []analyzer.ImageIssue{
			{Priority: analyzer.PriorityLarge},
		},
		PossiblyCovered: []analyzer.ScreenshotGap{
			{Priority: analyzer.PrioritySmall},
		},
	}
	got := screenshotsPriorityCounts(res)
	want := "2 (1L · 1M · 0S) missing; 1 (1L · 0M · 0S) image issues; 1 (0L · 0M · 1S) possibly covered"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestScreenshotsPriorityCountsEmpty(t *testing.T) {
	if got := screenshotsPriorityCounts(analyzer.ScreenshotResult{}); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}
