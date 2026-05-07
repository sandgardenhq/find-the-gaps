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

// TestReadScreenshotsJSON_RoundTrip pins that ReadScreenshotsJSON loads back
// the same gap, image-issue, and possibly-covered lists previously written
// by WriteScreenshotsJSON. Synthesizes an AuditStats entry with
// VisionEnabled=true when ImageIssues is non-empty so the renderer's
// `## Image Issues` gate fires on the cache-only render path.
func TestReadScreenshotsJSON_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	res := analyzer.ScreenshotResult{
		MissingGaps: []analyzer.ScreenshotGap{
			{PageURL: "u1", Priority: analyzer.PriorityLarge, PriorityReason: "r"},
		},
		ImageIssues: []analyzer.ImageIssue{
			{PageURL: "u1", Index: "i", Priority: analyzer.PriorityMedium, PriorityReason: "r"},
		},
		PossiblyCovered: []analyzer.ScreenshotGap{
			{PageURL: "u2", Priority: analyzer.PrioritySmall, PriorityReason: "r"},
		},
	}
	if err := WriteScreenshotsJSON(dir, res); err != nil {
		t.Fatal(err)
	}
	got, ok, err := ReadScreenshotsJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected ok=true after writing the file")
	}
	if len(got.MissingGaps) != 1 || got.MissingGaps[0].PageURL != "u1" {
		t.Errorf("missing gap not round-tripped: %+v", got.MissingGaps)
	}
	if len(got.ImageIssues) != 1 || got.ImageIssues[0].Index != "i" {
		t.Errorf("image issue not round-tripped: %+v", got.ImageIssues)
	}
	if len(got.PossiblyCovered) != 1 || got.PossiblyCovered[0].PageURL != "u2" {
		t.Errorf("possibly-covered not round-tripped: %+v", got.PossiblyCovered)
	}
	// Synthesized AuditStats so WriteScreenshots emits `## Image Issues`.
	if len(got.AuditStats) == 0 || !got.AuditStats[0].VisionEnabled {
		t.Errorf("expected synthesized VisionEnabled audit stat when image issues present, got %+v", got.AuditStats)
	}
}

// TestReadScreenshotsJSON_Missing returns ok=false (no error) when the file
// is absent — callers treat that as "screenshot pass never ran".
func TestReadScreenshotsJSON_Missing(t *testing.T) {
	dir := t.TempDir()
	_, ok, err := ReadScreenshotsJSON(dir)
	if err != nil {
		t.Fatalf("missing file must not error: %v", err)
	}
	if ok {
		t.Errorf("expected ok=false for missing file")
	}
}

// TestReadScreenshotsJSON_NoImageIssues_NoAuditStats pins that the
// VisionEnabled synthesis only fires when there are image issues to report.
// A run with zero image issues round-trips with empty AuditStats — the
// renderer will then omit the `## Image Issues` section. Documented
// fidelity loss vs. running through analyze with vision enabled.
func TestReadScreenshotsJSON_NoImageIssues_NoAuditStats(t *testing.T) {
	dir := t.TempDir()
	res := analyzer.ScreenshotResult{
		MissingGaps: []analyzer.ScreenshotGap{
			{PageURL: "u1", Priority: analyzer.PriorityLarge, PriorityReason: "r"},
		},
	}
	if err := WriteScreenshotsJSON(dir, res); err != nil {
		t.Fatal(err)
	}
	got, ok, err := ReadScreenshotsJSON(dir)
	if err != nil || !ok {
		t.Fatalf("expected ok=true with no error; got ok=%v err=%v", ok, err)
	}
	if len(got.AuditStats) != 0 {
		t.Errorf("expected no synthesized AuditStats when ImageIssues empty, got %+v", got.AuditStats)
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
