package cli

import (
	"github.com/charmbracelet/log"
	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// emitScreenshotAuditLog writes one info-level log line per page summarizing
// what the screenshot detection pass did on that page. Format is a stable
// key=value layout so downstream log scrapers (CI, observability pipelines)
// can parse it without guessing field positions:
//
//	page=<url> vision=on|off relevance_batches=N images_seen=N image_issues=N missing_screenshots=N missing_suppressed=N detection_skipped=true|false
//
// Vision-off and detection-skipped pages report `vision=off` with the
// relevance / image counts zeroed. `detection_skipped` is emitted
// unconditionally (true|false) so log scrapers see a column-stable layout
// across all pages; this is how a budget-skipped page is distinguished from
// a clean zero-findings run.
func emitScreenshotAuditLog(stats []analyzer.ScreenshotPageStats) {
	for _, s := range stats {
		visionFlag := "off"
		if s.VisionEnabled {
			visionFlag = "on"
		}
		log.Infof(
			"page=%s vision=%s relevance_batches=%d images_seen=%d image_issues=%d missing_screenshots=%d missing_suppressed=%d detection_skipped=%t",
			s.PageURL,
			visionFlag,
			s.RelevanceBatches,
			s.ImagesSeen,
			s.ImageIssues,
			s.MissingScreenshots,
			s.MissingSuppressed,
			s.DetectionSkipped,
		)
	}
}
