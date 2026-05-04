package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/stretchr/testify/assert"
)

// TestAnalyze_EmitsScreenshotAuditLine verifies that emitScreenshotAuditLog
// writes a key=value audit line per page through the charmbracelet/log
// package, capturing every field the plan documents (page, vision flag,
// relevance batches, image counts, missing screenshot counts).
func TestAnalyze_EmitsScreenshotAuditLine(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	prevLevel := log.GetLevel()
	log.SetLevel(log.InfoLevel)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetLevel(prevLevel)
	})

	stats := []analyzer.ScreenshotPageStats{
		{
			PageURL:            "https://example.com/p",
			VisionEnabled:      true,
			RelevanceBatches:   3,
			ImagesSeen:         12,
			ImageIssues:        2,
			MissingScreenshots: 4,
			MissingSuppressed:  1,
		},
	}

	emitScreenshotAuditLog(stats)

	out := buf.String()
	for _, want := range []string{
		"page=https://example.com/p",
		"vision=on",
		"relevance_batches=3",
		"images_seen=12",
		"image_issues=2",
		"missing_screenshots=4",
		"missing_suppressed=1",
		"detection_skipped=",
	} {
		assert.Contains(t, out, want, "audit log missing %q; full output:\n%s", want, out)
	}
}

// TestAnalyze_EmitsScreenshotAuditLine_VisionOff confirms vision-off pages
// emit `vision=off` and zero relevance / image counts even when missing
// screenshot findings exist on the page.
func TestAnalyze_EmitsScreenshotAuditLine_VisionOff(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	prevLevel := log.GetLevel()
	log.SetLevel(log.InfoLevel)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetLevel(prevLevel)
	})

	stats := []analyzer.ScreenshotPageStats{
		{
			PageURL:            "https://example.com/q",
			VisionEnabled:      false,
			MissingScreenshots: 7,
		},
	}

	emitScreenshotAuditLog(stats)

	out := buf.String()
	for _, want := range []string{
		"page=https://example.com/q",
		"vision=off",
		"relevance_batches=0",
		"images_seen=0",
		"image_issues=0",
		"missing_screenshots=7",
		"missing_suppressed=0",
		"detection_skipped=",
	} {
		assert.Contains(t, out, want, "audit log missing %q; full output:\n%s", want, out)
	}
}

// TestEmitScreenshotAuditLog_DetectionSkippedSurfaces verifies that the audit
// log distinguishes a budget-skipped page from a clean zero-findings page by
// always emitting `detection_skipped=<true|false>` as a stable column in the
// key=value layout. The field is unconditional so log scrapers see a fixed
// set of columns regardless of skip state.
func TestEmitScreenshotAuditLog_DetectionSkippedSurfaces(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	prevLevel := log.GetLevel()
	log.SetLevel(log.InfoLevel)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetLevel(prevLevel)
	})

	stats := []analyzer.ScreenshotPageStats{
		{
			PageURL:          "https://example.com/skipped",
			DetectionSkipped: true,
		},
		{
			PageURL:          "https://example.com/clean",
			DetectionSkipped: false,
		},
	}

	emitScreenshotAuditLog(stats)

	out := buf.String()
	assert.Equal(t, 1, strings.Count(out, "detection_skipped=true"),
		"expected detection_skipped=true exactly once; full output:\n%s", out)
	assert.Equal(t, 1, strings.Count(out, "detection_skipped=false"),
		"expected detection_skipped=false exactly once; full output:\n%s", out)
}

// TestAnalyze_EmitsScreenshotAuditLine_OneLinePerPage confirms each page's
// stats land on their own line so downstream log scrapers can parse them.
func TestAnalyze_EmitsScreenshotAuditLine_OneLinePerPage(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	prevLevel := log.GetLevel()
	log.SetLevel(log.InfoLevel)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetLevel(prevLevel)
	})

	stats := []analyzer.ScreenshotPageStats{
		{PageURL: "https://example.com/a", VisionEnabled: true},
		{PageURL: "https://example.com/b", VisionEnabled: false},
	}

	emitScreenshotAuditLog(stats)

	out := buf.String()
	pageALines := 0
	pageBLines := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "page=https://example.com/a") {
			pageALines++
		}
		if strings.Contains(line, "page=https://example.com/b") {
			pageBLines++
		}
	}
	assert.Equal(t, 1, pageALines, "expected one line for page a; full output:\n%s", out)
	assert.Equal(t, 1, pageBLines, "expected one line for page b; full output:\n%s", out)
}
