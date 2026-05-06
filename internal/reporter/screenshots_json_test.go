package reporter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestWriteScreenshotsJSON(t *testing.T) {
	dir := t.TempDir()
	res := analyzer.ScreenshotResult{
		MissingGaps: []analyzer.ScreenshotGap{{
			PageURL: "u", QuotedPassage: "q",
			Priority: analyzer.PriorityLarge, PriorityReason: "r1",
		}},
		ImageIssues: []analyzer.ImageIssue{{
			PageURL: "u", Index: "img-1",
			Priority: analyzer.PriorityMedium, PriorityReason: "r2",
		}},
		PossiblyCovered: []analyzer.ScreenshotGap{{
			PageURL: "u",
			Priority: analyzer.PrioritySmall, PriorityReason: "r3",
		}},
	}
	if err := WriteScreenshotsJSON(dir, res); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "screenshots.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		MissingGaps     []analyzer.ScreenshotGap `json:"missing_gaps"`
		ImageIssues     []analyzer.ImageIssue    `json:"image_issues"`
		PossiblyCovered []analyzer.ScreenshotGap `json:"possibly_covered"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.MissingGaps) != 1 || got.MissingGaps[0].Priority != analyzer.PriorityLarge {
		t.Errorf("missing-gap priority lost: %+v", got.MissingGaps)
	}
	if len(got.ImageIssues) != 1 || got.ImageIssues[0].Priority != analyzer.PriorityMedium {
		t.Errorf("image-issue priority lost: %+v", got.ImageIssues)
	}
	if len(got.PossiblyCovered) != 1 || got.PossiblyCovered[0].Priority != analyzer.PrioritySmall {
		t.Errorf("possibly-covered priority lost: %+v", got.PossiblyCovered)
	}
}

// TestWriteScreenshotsJSONStableOrder pins that elements appear in input order
// (no priority-driven reordering in the JSON itself; consumers sort).
func TestWriteScreenshotsJSONStableOrder(t *testing.T) {
	dir := t.TempDir()
	res := analyzer.ScreenshotResult{
		MissingGaps: []analyzer.ScreenshotGap{
			{PageURL: "a", Priority: analyzer.PrioritySmall, PriorityReason: "r"},
			{PageURL: "b", Priority: analyzer.PriorityLarge, PriorityReason: "r"},
			{PageURL: "c", Priority: analyzer.PriorityMedium, PriorityReason: "r"},
		},
	}
	if err := WriteScreenshotsJSON(dir, res); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "screenshots.json"))
	var got struct {
		MissingGaps []analyzer.ScreenshotGap `json:"missing_gaps"`
	}
	_ = json.Unmarshal(b, &got)
	if len(got.MissingGaps) != 3 {
		t.Fatalf("got %d gaps", len(got.MissingGaps))
	}
	for i, want := range []string{"a", "b", "c"} {
		if got.MissingGaps[i].PageURL != want {
			t.Errorf("idx %d: PageURL = %q, want %q", i, got.MissingGaps[i].PageURL, want)
		}
	}
}
