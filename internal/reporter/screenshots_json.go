package reporter

import (
	"encoding/json"
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
