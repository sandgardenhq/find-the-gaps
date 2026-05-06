package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// TestExpandedFeaturePageGroupsDriftByPriority pins that the per-feature page
// in expanded mode renders drift findings under Large/Medium/Small sub-headings
// in that order, with the priority_reason rendered alongside.
func TestExpandedFeaturePageGroupsDriftByPriority(t *testing.T) {
	dir := t.TempDir()
	// Stub the gaps.md the materializer reads.
	if err := os.WriteFile(filepath.Join(dir, "gaps.md"),
		[]byte("# Gaps\n\n_None._\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := Inputs{
		Mapping: analyzer.FeatureMap{{
			Feature: analyzer.CodeFeature{Name: "Auth", UserFacing: true}, Files: []string{"a.go"},
		}},
		AllDocFeatures: []string{"Auth"},
		Drift: []analyzer.DriftFinding{
			{Feature: "Auth", Issues: []analyzer.DriftIssue{
				{Page: "p1", Issue: "small-issue", Priority: analyzer.PrioritySmall, PriorityReason: "deep"},
				{Page: "p2", Issue: "large-issue", Priority: analyzer.PriorityLarge, PriorityReason: "readme"},
				{Page: "p3", Issue: "medium-issue", Priority: analyzer.PriorityMedium, PriorityReason: "ref"},
			}},
		},
	}
	opts := BuildOptions{ProjectDir: dir, Mode: ModeExpanded, ProjectName: "Test"}

	srcDir := t.TempDir()
	if err := materialize(srcDir, in, opts); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(filepath.Join(srcDir, "content", "features", "auth.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)

	largePos := strings.Index(s, "Large")
	mediumPos := strings.Index(s, "Medium")
	smallPos := strings.Index(s, "Small")
	if largePos < 0 || mediumPos < 0 || smallPos < 0 {
		t.Fatalf("missing priority sub-headings:\n%s", s)
	}
	if !(largePos < mediumPos && mediumPos < smallPos) {
		t.Errorf("priority order broken: large=%d medium=%d small=%d\n%s", largePos, mediumPos, smallPos, s)
	}
	for _, want := range []string{"large-issue", "medium-issue", "small-issue", "readme"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in feature page:\n%s", want, s)
		}
	}
}

// TestExpandedScreenshotPageGroupsByPriority pins the per-page screenshot
// page in expanded mode renders gaps under Large/Medium/Small sub-headings.
func TestExpandedScreenshotPageGroupsByPriority(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gaps.md"), []byte("# Gaps\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := Inputs{
		ScreenshotsRan: true,
		Screenshots: []analyzer.ScreenshotGap{
			{PageURL: "https://x/p", QuotedPassage: "small-q", Priority: analyzer.PrioritySmall, PriorityReason: "deep"},
			{PageURL: "https://x/p", QuotedPassage: "large-q", Priority: analyzer.PriorityLarge, PriorityReason: "quickstart"},
		},
	}
	opts := BuildOptions{ProjectDir: dir, Mode: ModeExpanded, ProjectName: "Test"}
	srcDir := t.TempDir()
	if err := materialize(srcDir, in, opts); err != nil {
		t.Fatal(err)
	}

	// Locate the rendered screenshot page (single page, generated slug).
	matches, err := filepath.Glob(filepath.Join(srcDir, "content", "screenshots", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	var pageBody string
	for _, m := range matches {
		if filepath.Base(m) == "_index.md" {
			continue
		}
		b, _ := os.ReadFile(m)
		pageBody = string(b)
		break
	}
	if pageBody == "" {
		t.Fatalf("no screenshot page rendered (matches=%v)", matches)
	}
	largePos := strings.Index(pageBody, "Large")
	smallPos := strings.Index(pageBody, "Small")
	if largePos < 0 || smallPos < 0 {
		t.Fatalf("priority headings missing:\n%s", pageBody)
	}
	if largePos > smallPos {
		t.Errorf("Large must precede Small:\n%s", pageBody)
	}
	if !strings.Contains(pageBody, "large-q") || !strings.Contains(pageBody, "small-q") {
		t.Errorf("missing passages:\n%s", pageBody)
	}
}

// TestExpandedScreenshotIndexImageIssuesGroupedByPriority pins that the
// expanded-mode screenshots/_index.md renders image issues under Large /
// Medium / Small sub-buckets in that order.
func TestExpandedScreenshotIndexImageIssuesGroupedByPriority(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gaps.md"), []byte("# Gaps\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := Inputs{
		ScreenshotsRan: true,
		ImageIssues: []analyzer.ImageIssue{
			{PageURL: "https://x/p", Index: "img-1", Src: "a.png", Reason: "small-mismatch", SuggestedAction: "replace",
				Priority: analyzer.PrioritySmall, PriorityReason: "deep"},
			{PageURL: "https://x/p", Index: "img-2", Src: "b.png", Reason: "large-mismatch", SuggestedAction: "recapture",
				Priority: analyzer.PriorityLarge, PriorityReason: "readme"},
		},
	}
	opts := BuildOptions{ProjectDir: dir, Mode: ModeExpanded, ProjectName: "Test"}
	srcDir := t.TempDir()
	if err := materialize(srcDir, in, opts); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(srcDir, "content", "screenshots", "_index.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	largePos := strings.Index(s, "Large")
	smallPos := strings.Index(s, "Small")
	if largePos < 0 || smallPos < 0 {
		t.Fatalf("priority headings missing:\n%s", s)
	}
	if largePos > smallPos {
		t.Errorf("Large must precede Small in image issues:\n%s", s)
	}
	if !strings.Contains(s, "large-mismatch") || !strings.Contains(s, "small-mismatch") {
		t.Errorf("missing image-issue text:\n%s", s)
	}
}
