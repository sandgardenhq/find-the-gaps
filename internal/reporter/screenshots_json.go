package reporter

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// screenshotsJSON is the on-disk JSON shape for screenshots.json. Mirrors
// ScreenshotResult minus AuditStats (audit stats are an internal pipeline
// detail; consumers want findings).
type screenshotsJSON struct {
	MissingGaps     []analyzer.ScreenshotGap `json:"missing_gaps"`
	ImageIssues     []analyzer.ImageIssue    `json:"image_issues"`
	PossiblyCovered []analyzer.ScreenshotGap `json:"possibly_covered"`
}

// WriteScreenshotsJSON persists the screenshot-pass results as a single JSON
// artifact alongside screenshots.md. Stable original order is preserved
// within each list — consumers sort however they want; the Markdown and the
// rendered site impose priority-based ordering at render time.
func WriteScreenshotsJSON(dir string, res analyzer.ScreenshotResult) error {
	out := screenshotsJSON{
		MissingGaps:     res.MissingGaps,
		ImageIssues:     res.ImageIssues,
		PossiblyCovered: res.PossiblyCovered,
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "screenshots.json"), b, 0o644)
}

// ReadScreenshotsJSON loads a previously-persisted screenshots.json from dir.
// Returns (zero, false, nil) if the file is missing — callers treat that as
// "the screenshot pass was never run for this project". Returns a non-nil
// error only when the file exists but cannot be read or parsed.
//
// AuditStats is not persisted on disk; we reconstruct just enough of it for
// the renderer's `## Image Issues` gate (`visionRan` check). When any image
// issues exist we synthesize a single VisionEnabled entry so the section
// renders. The "vision ran but found no issues" case is lost on this path —
// the resulting screenshots.md will omit the `_No image issues detected._`
// marker. That's a fidelity trade-off for the cache-only render path; users
// who want the marker should re-run `ftg analyze`.
func ReadScreenshotsJSON(dir string) (analyzer.ScreenshotResult, bool, error) {
	path := filepath.Join(dir, "screenshots.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return analyzer.ScreenshotResult{}, false, nil
	}
	if err != nil {
		return analyzer.ScreenshotResult{}, false, err
	}
	var in screenshotsJSON
	if err := json.Unmarshal(data, &in); err != nil {
		return analyzer.ScreenshotResult{}, false, err
	}
	res := analyzer.ScreenshotResult{
		MissingGaps:     in.MissingGaps,
		ImageIssues:     in.ImageIssues,
		PossiblyCovered: in.PossiblyCovered,
	}
	if len(in.ImageIssues) > 0 {
		res.AuditStats = []analyzer.ScreenshotPageStats{{VisionEnabled: true}}
	}
	return res, true, nil
}
