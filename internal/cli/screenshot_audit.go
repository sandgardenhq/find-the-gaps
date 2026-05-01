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
//	page=<url> vision=on|off relevance_batches=N images_seen=N image_issues=N missing_screenshots=N missing_suppressed=N
//
// Vision-off and detection-skipped pages report `vision=off` with the
// relevance / image counts zeroed; the missing_screenshots / missing_suppressed
// counts come straight from ScreenshotPageStats so a budget-skipped page can
// still be distinguished from a clean zero-findings run by the surrounding log
// context (the detector logs the skip separately).
func emitScreenshotAuditLog(stats []analyzer.ScreenshotPageStats) {
	for _, s := range stats {
		visionFlag := "off"
		if s.VisionEnabled {
			visionFlag = "on"
		}
		log.Infof(
			"page=%s vision=%s relevance_batches=%d images_seen=%d image_issues=%d missing_screenshots=%d missing_suppressed=%d",
			s.PageURL,
			visionFlag,
			s.RelevanceBatches,
			s.ImagesSeen,
			s.ImageIssues,
			s.MissingScreenshots,
			s.MissingSuppressed,
		)
	}
}
