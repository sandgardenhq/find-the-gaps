package pdf

import (
	"path/filepath"
	"strings"
	"testing"

	pdfreader "github.com/ledongthuc/pdf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestRenderFeatures_OneBlockPerFeature(t *testing.T) {
	dir := t.TempDir()

	in := Inputs{
		ProjectName: "Feat Project",
		Mapping: analyzer.FeatureMap{
			{
				Feature: analyzer.CodeFeature{
					Name:        "gap analysis",
					Description: "Finds gaps between code and docs.",
					Layer:       "analysis engine",
					UserFacing:  true,
				},
				Files:   []string{"internal/analyzer/analyzer.go"},
				Symbols: []string{"AnalyzePage"},
			},
			{
				Feature: analyzer.CodeFeature{
					Name:       "doctor command",
					UserFacing: false,
				},
			},
		},
		DocsMap: analyzer.DocsFeatureMap{
			{Feature: "gap analysis", Pages: []string{"https://docs.example.com/gap"}},
		},
	}

	require.NoError(t, WriteReport(dir, in))

	f, r, err := pdfreader.Open(filepath.Join(dir, "report.pdf"))
	require.NoError(t, err)
	defer f.Close()

	// Gather text from all pages so multi-page feature listings are covered.
	var allText strings.Builder
	for p := 1; p <= r.NumPage(); p++ {
		t.Logf("scanning page %d", p)
		txt, err := r.Page(p).GetPlainText(nil)
		require.NoError(t, err)
		allText.WriteString(txt)
	}
	body := allText.String()

	assert.Contains(t, body, "gap analysis")
	assert.Contains(t, body, "doctor command")
	assert.Contains(t, body, "Finds gaps between code and docs.")
	assert.Contains(t, body, "analysis engine") // Layer badge
	assert.Contains(t, body, "user-facing")     // user-facing badge for the first feature
	assert.Contains(t, body, "internal")        // internal badge for the second feature
	assert.Contains(t, body, "documented")      // status badge for the page-covered feature
	assert.Contains(t, body, "undocumented")    // status badge for the second feature
	assert.Contains(t, body, "internal/analyzer/analyzer.go")
	assert.Contains(t, body, "AnalyzePage")
	assert.Contains(t, body, "https://docs.example.com/gap")
}

func TestRenderFeatures_RegistersAnchorPerFeature(t *testing.T) {
	doc := newDoc()
	anchors := newAnchorTable(doc)

	in := Inputs{
		Mapping: analyzer.FeatureMap{
			{Feature: analyzer.CodeFeature{Name: "Alpha Beta", UserFacing: true}},
			{Feature: analyzer.CodeFeature{Name: "gamma!", UserFacing: false}},
		},
	}

	doc.AddPage()
	renderFeatures(doc, in, anchors)

	// Anchor names slugified.
	_, hasAlpha := anchors.links["feat-alpha-beta"]
	_, hasGamma := anchors.links["feat-gamma"]
	assert.True(t, hasAlpha, "anchor for 'Alpha Beta' must be registered as feat-alpha-beta; got %v", anchors.links)
	assert.True(t, hasGamma, "anchor for 'gamma!' must be registered as feat-gamma; got %v", anchors.links)
}

func TestRenderGaps_BucketsByPriority(t *testing.T) {
	dir := t.TempDir()

	in := Inputs{
		ProjectName: "Bucket Project",
		Mapping: analyzer.FeatureMap{
			{Feature: analyzer.CodeFeature{Name: "auth", UserFacing: true}},
			{Feature: analyzer.CodeFeature{Name: "search", UserFacing: true}},
		},
		Drift: []analyzer.DriftFinding{
			{Feature: "auth", Issues: []analyzer.DriftIssue{
				{Issue: "stale signature in /auth/login", Page: "https://docs.example.com/auth", Priority: analyzer.PrioritySmall, PriorityReason: "cosmetic typo"},
				{Issue: "wrong error code documented", Page: "https://docs.example.com/auth", Priority: analyzer.PriorityLarge, PriorityReason: "blocks integration"},
			}},
			{Feature: "search", Issues: []analyzer.DriftIssue{
				{Issue: "outdated example query", Page: "https://docs.example.com/search", Priority: analyzer.PriorityMedium, PriorityReason: "misleads users"},
			}},
		},
	}

	require.NoError(t, WriteReport(dir, in))

	f, r, err := pdfreader.Open(filepath.Join(dir, "report.pdf"))
	require.NoError(t, err)
	defer f.Close()

	// Locate the gaps body page by scanning for a priority reason
	// string. The cover now also lists "feature - issue" text, so the
	// issue itself can't disambiguate; priority reasons appear only on
	// the body page.
	var gapsText string
	for p := 1; p <= r.NumPage(); p++ {
		txt, err := r.Page(p).GetPlainText(nil)
		require.NoError(t, err)
		if strings.Contains(txt, "blocks integration") {
			gapsText = txt
			break
		}
	}
	require.NotEmpty(t, gapsText, "Gaps section body must render with at least one finding")

	// Order: LARGE appears before MEDIUM appears before SMALL on the
	// gaps body page. Priority labels render as uppercase pills now
	// (matching .ftg-priority `text-transform: uppercase`).
	largeIdx := strings.Index(gapsText, "LARGE")
	mediumIdx := strings.Index(gapsText, "MEDIUM")
	smallIdx := strings.Index(gapsText, "SMALL")
	require.True(t, largeIdx >= 0 && mediumIdx > largeIdx && smallIdx > mediumIdx,
		"buckets must appear in LARGE -> MEDIUM -> SMALL order; got large=%d medium=%d small=%d in:\n%s",
		largeIdx, mediumIdx, smallIdx, gapsText)

	// All three issues must be referenced.
	assert.Contains(t, gapsText, "wrong error code documented")
	assert.Contains(t, gapsText, "outdated example query")
	assert.Contains(t, gapsText, "stale signature")
	assert.Contains(t, gapsText, "auth")
	assert.Contains(t, gapsText, "search")
}

func TestRenderGaps_EmptyBucketsOmitted(t *testing.T) {
	dir := t.TempDir()

	in := Inputs{
		ProjectName: "Small Only",
		Mapping: analyzer.FeatureMap{
			{Feature: analyzer.CodeFeature{Name: "f", UserFacing: true}},
		},
		Drift: []analyzer.DriftFinding{
			{Feature: "f", Issues: []analyzer.DriftIssue{
				{Issue: "minor typo", Priority: analyzer.PrioritySmall, PriorityReason: "cosmetic"},
			}},
		},
	}

	require.NoError(t, WriteReport(dir, in))

	f, r, err := pdfreader.Open(filepath.Join(dir, "report.pdf"))
	require.NoError(t, err)
	defer f.Close()

	// Locate the gaps body page by the priority reason — the cover now
	// also lists the issue text so "minor typo" is no longer unique to
	// the body page.
	var gapsText string
	for p := 1; p <= r.NumPage(); p++ {
		txt, _ := r.Page(p).GetPlainText(nil)
		if strings.Contains(txt, "cosmetic") {
			gapsText = txt
			break
		}
	}
	require.NotEmpty(t, gapsText)

	// Only Small bucket has content; LARGE/MEDIUM pills must NOT
	// appear under Gaps. (Uppercase because pills now render with
	// text-transform: uppercase.)
	assert.NotContains(t, gapsText, "LARGE", "empty Large bucket must be omitted")
	assert.NotContains(t, gapsText, "MEDIUM", "empty Medium bucket must be omitted")
	assert.Contains(t, gapsText, "SMALL")
}

func TestBucketDrift_SkipsUnknownPriority(t *testing.T) {
	findings := []analyzer.DriftFinding{
		{Feature: "f", Issues: []analyzer.DriftIssue{
			{Issue: "ok", Priority: analyzer.PrioritySmall, PriorityReason: "r"},
			{Issue: "bad", Priority: analyzer.Priority("bogus"), PriorityReason: "r"},
		}},
	}
	got := bucketDrift(findings)
	assert.Len(t, got[analyzer.PrioritySmall], 1, "known priority must keep its issue")
	assert.Len(t, got[analyzer.Priority("bogus")], 0, "unknown priority must be skipped")
}

func TestPriorityLabel_UnknownReturnsRawString(t *testing.T) {
	assert.Equal(t, "bogus", priorityLabel(analyzer.Priority("bogus")))
}

func TestRenderGaps_UnmappedFeatureRendersAsPlainText(t *testing.T) {
	dir := t.TempDir()

	// Drift names a feature that doesn't appear in Mapping. The renderer
	// must still output the finding (plain text, no cross-link) rather
	// than dropping it.
	in := Inputs{
		ProjectName: "Orphan",
		Mapping:     analyzer.FeatureMap{},
		Drift: []analyzer.DriftFinding{
			{Feature: "ghost-feature", Issues: []analyzer.DriftIssue{
				{Issue: "an orphan finding", Priority: analyzer.PriorityLarge, PriorityReason: "r"},
			}},
		},
	}
	require.NoError(t, WriteReport(dir, in))

	f, r, err := pdfreader.Open(filepath.Join(dir, "report.pdf"))
	require.NoError(t, err)
	defer f.Close()

	var found bool
	for p := 1; p <= r.NumPage(); p++ {
		txt, _ := r.Page(p).GetPlainText(nil)
		if strings.Contains(txt, "ghost-feature") && strings.Contains(txt, "an orphan finding") {
			found = true
			break
		}
	}
	assert.True(t, found, "drift findings for unmapped features must still render")
}

func TestRenderGaps_RegistersFeatureCrosslink(t *testing.T) {
	doc := newDoc()
	anchors := newAnchorTable(doc)

	in := Inputs{
		Mapping: analyzer.FeatureMap{
			{Feature: analyzer.CodeFeature{Name: "auth", UserFacing: true}},
		},
		Drift: []analyzer.DriftFinding{
			{Feature: "auth", Issues: []analyzer.DriftIssue{
				{Issue: "x", Priority: analyzer.PriorityLarge, PriorityReason: "y"},
			}},
		},
	}

	// Pre-register feature anchor as renderSections would.
	featAnchors := computeFeatureAnchors(in)
	require.Equal(t, "feat-auth", featAnchors["auth"])

	doc.AddPage()
	renderGapsWithAnchors(doc, in, anchors, featAnchors)

	// Gaps section must have called Get on the feature anchor so its
	// link ID is registered in the table.
	_, ok := anchors.links["feat-auth"]
	assert.True(t, ok, "renderGaps must register cross-link to feat-auth anchor; got %v", anchors.links)
}

func TestRenderScreenshots_OmittedWhenNotRun(t *testing.T) {
	dir := t.TempDir()

	in := Inputs{
		ProjectName: "No Screenshots Run",
		Mapping: analyzer.FeatureMap{
			{Feature: analyzer.CodeFeature{Name: "f", UserFacing: true}},
		},
		Screenshots: analyzer.ScreenshotResult{
			MissingGaps: []analyzer.ScreenshotGap{
				{PageURL: "u", ShouldShow: "x", Priority: analyzer.PriorityLarge, PriorityReason: "r"},
			},
		},
		ScreenshotsRan: false,
	}
	require.NoError(t, WriteReport(dir, in))

	// Open and scan every page. No page should contain the Screenshots
	// section heading or any of the screenshot data.
	f, r, err := pdfreader.Open(filepath.Join(dir, "report.pdf"))
	require.NoError(t, err)
	defer f.Close()

	for p := 1; p <= r.NumPage(); p++ {
		txt, _ := r.Page(p).GetPlainText(nil)
		assert.NotContains(t, txt, "Missing Screenshots",
			"Missing sub-heading must not appear when ScreenshotsRan=false; page %d=%q", p, txt)
		assert.NotContains(t, txt, "Image Issues",
			"Image Issues sub-heading must not appear when ScreenshotsRan=false; page %d=%q", p, txt)
	}
}

func TestRenderScreenshots_MissingBucketed(t *testing.T) {
	dir := t.TempDir()

	in := Inputs{
		ProjectName: "Screenshot Buckets",
		Mapping: analyzer.FeatureMap{
			{Feature: analyzer.CodeFeature{Name: "alpha", UserFacing: true}},
		},
		DocsMap: analyzer.DocsFeatureMap{
			{Feature: "alpha", Pages: []string{"https://docs.example.com/a"}},
		},
		Screenshots: analyzer.ScreenshotResult{
			MissingGaps: []analyzer.ScreenshotGap{
				{PageURL: "https://docs.example.com/a", ShouldShow: "the big dialog", SuggestedAlt: "Big dialog", Priority: analyzer.PriorityLarge, PriorityReason: "primary"},
				{PageURL: "https://docs.example.com/a", ShouldShow: "form X", Priority: analyzer.PrioritySmall, PriorityReason: "cosmetic"},
			},
		},
		ScreenshotsRan: true,
	}
	require.NoError(t, WriteReport(dir, in))

	f, r, err := pdfreader.Open(filepath.Join(dir, "report.pdf"))
	require.NoError(t, err)
	defer f.Close()

	// Collect the screenshot body page. TOC contains "Missing
	// Screenshots" and the cover lists "Missing: the big dialog"; the
	// body page is the one that carries the priority reason
	// "primary".
	var section string
	for p := 1; p <= r.NumPage(); p++ {
		txt, _ := r.Page(p).GetPlainText(nil)
		if strings.Contains(txt, "primary") {
			section = txt
			break
		}
	}
	require.NotEmpty(t, section, "Missing Screenshots body must render at least one gap")

	// Both passages must appear.
	assert.Contains(t, section, "the big dialog")
	assert.Contains(t, section, "form X")

	// Buckets in LARGE → SMALL order (no MEDIUM because empty).
	largeIdx := strings.Index(section, "LARGE")
	smallIdx := strings.Index(section, "SMALL")
	mediumIdx := strings.Index(section, "MEDIUM")
	require.True(t, largeIdx >= 0, "LARGE bucket must render")
	require.True(t, smallIdx > largeIdx, "SMALL must follow LARGE; got LARGE=%d SMALL=%d", largeIdx, smallIdx)
	assert.True(t, mediumIdx < 0 || mediumIdx > smallIdx, "MEDIUM bucket must be omitted (empty); got MEDIUM=%d SMALL=%d", mediumIdx, smallIdx)
}

func TestRenderScreenshots_ImageIssuesAndPossiblyCoveredOmittedWhenEmpty(t *testing.T) {
	dir := t.TempDir()

	in := Inputs{
		ProjectName: "Only Missing",
		Mapping: analyzer.FeatureMap{
			{Feature: analyzer.CodeFeature{Name: "a", UserFacing: true}},
		},
		Screenshots: analyzer.ScreenshotResult{
			MissingGaps: []analyzer.ScreenshotGap{
				{PageURL: "u", ShouldShow: "x", Priority: analyzer.PriorityLarge, PriorityReason: "r"},
			},
		},
		ScreenshotsRan: true,
	}
	require.NoError(t, WriteReport(dir, in))

	f, r, err := pdfreader.Open(filepath.Join(dir, "report.pdf"))
	require.NoError(t, err)
	defer f.Close()

	for p := 1; p <= r.NumPage(); p++ {
		txt, _ := r.Page(p).GetPlainText(nil)
		assert.NotContains(t, txt, "Image Issues",
			"Image Issues sub-heading must be omitted when ImageIssues is empty")
		assert.NotContains(t, txt, "Possibly Covered",
			"Possibly Covered sub-heading must be omitted when PossiblyCovered is empty")
	}
}

func TestRenderScreenshots_AllSubSectionsRendered(t *testing.T) {
	dir := t.TempDir()

	in := Inputs{
		ProjectName: "All Sections",
		Mapping: analyzer.FeatureMap{
			{Feature: analyzer.CodeFeature{Name: "alpha", UserFacing: true}},
		},
		DocsMap: analyzer.DocsFeatureMap{
			{Feature: "alpha", Pages: []string{"https://docs.example.com/a"}},
		},
		Screenshots: analyzer.ScreenshotResult{
			MissingGaps: []analyzer.ScreenshotGap{
				{PageURL: "https://docs.example.com/a", ShouldShow: "ms-show", SuggestedAlt: "ms-alt", InsertionHint: "ms-hint", Priority: analyzer.PriorityLarge, PriorityReason: "ms-reason"},
				{PageURL: "https://docs.example.com/a", ShouldShow: "ms-show2", Priority: analyzer.Priority("bogus"), PriorityReason: "skipped"},
			},
			ImageIssues: []analyzer.ImageIssue{
				{PageURL: "https://docs.example.com/a", Src: "img-src", Reason: "ii-reason", SuggestedAction: "ii-action", Priority: analyzer.PriorityMedium, PriorityReason: "ii-why"},
				{PageURL: "https://docs.example.com/a", Priority: analyzer.Priority("bogus"), PriorityReason: "skipped"},
			},
			PossiblyCovered: []analyzer.ScreenshotGap{
				{PageURL: "https://docs.example.com/a", ShouldShow: "pc-show", Priority: analyzer.PrioritySmall, PriorityReason: "pc-reason"},
			},
		},
		ScreenshotsRan: true,
	}

	require.NoError(t, WriteReport(dir, in))

	f, r, err := pdfreader.Open(filepath.Join(dir, "report.pdf"))
	require.NoError(t, err)
	defer f.Close()

	var all strings.Builder
	for p := 1; p <= r.NumPage(); p++ {
		txt, _ := r.Page(p).GetPlainText(nil)
		all.WriteString(txt)
	}
	body := all.String()

	// All three sub-sections rendered.
	assert.Contains(t, body, "Missing Screenshots")
	assert.Contains(t, body, "Image Issues")
	assert.Contains(t, body, "Possibly Covered")

	// Per-bucket finding fields rendered.
	assert.Contains(t, body, "ms-show")
	assert.Contains(t, body, "ms-alt")
	assert.Contains(t, body, "ms-hint")
	assert.Contains(t, body, "ms-reason")

	assert.Contains(t, body, "img-src")
	assert.Contains(t, body, "ii-reason")
	assert.Contains(t, body, "ii-action")
	assert.Contains(t, body, "ii-why")

	assert.Contains(t, body, "pc-show")
	assert.Contains(t, body, "pc-reason")

	// Bogus-priority entries were skipped (their PriorityReason text
	// "skipped" must not appear).
	assert.NotContains(t, body, "ms-show2")
}

func TestRenderScreenshots_PageToFeatureCrosslink(t *testing.T) {
	doc := newDoc()
	anchors := newAnchorTable(doc)

	in := Inputs{
		Mapping: analyzer.FeatureMap{
			{Feature: analyzer.CodeFeature{Name: "single", UserFacing: true}},
			{Feature: analyzer.CodeFeature{Name: "alpha", UserFacing: true}},
			{Feature: analyzer.CodeFeature{Name: "beta", UserFacing: true}},
		},
		DocsMap: analyzer.DocsFeatureMap{
			{Feature: "single", Pages: []string{"https://docs.example.com/lonely"}},
			{Feature: "alpha", Pages: []string{"https://docs.example.com/shared"}},
			{Feature: "beta", Pages: []string{"https://docs.example.com/shared"}},
		},
		Screenshots: analyzer.ScreenshotResult{
			MissingGaps: []analyzer.ScreenshotGap{
				{PageURL: "https://docs.example.com/lonely", ShouldShow: "x", Priority: analyzer.PriorityLarge, PriorityReason: "r"},
				{PageURL: "https://docs.example.com/shared", ShouldShow: "y", Priority: analyzer.PriorityMedium, PriorityReason: "r"},
				{PageURL: "https://docs.example.com/unknown", ShouldShow: "z", Priority: analyzer.PrioritySmall, PriorityReason: "r"},
			},
		},
		ScreenshotsRan: true,
	}

	doc.AddPage()
	featAnchors := computeFeatureAnchors(in)
	renderScreenshotsWithAnchors(doc, in, anchors, featAnchors)

	// Single-feature page must have caused an anchor Get for feat-single.
	_, single := anchors.links["feat-single"]
	assert.True(t, single, "single-feature page must cross-link to feat-single")

	// Multi-feature page must NOT cross-link to any one of its features.
	_, alpha := anchors.links["feat-alpha"]
	_, beta := anchors.links["feat-beta"]
	assert.False(t, alpha, "shared page must NOT cross-link to feat-alpha; got %v", anchors.links)
	assert.False(t, beta, "shared page must NOT cross-link to feat-beta; got %v", anchors.links)
}

func TestRenderFeatures_DisambiguatesSlugCollisions(t *testing.T) {
	doc := newDoc()
	anchors := newAnchorTable(doc)

	in := Inputs{
		Mapping: analyzer.FeatureMap{
			{Feature: analyzer.CodeFeature{Name: "auth"}},
			{Feature: analyzer.CodeFeature{Name: "Auth"}},     // same slug after lowercase
			{Feature: analyzer.CodeFeature{Name: "auth!"}},    // same slug after slugify
		},
	}

	doc.AddPage()
	renderFeatures(doc, in, anchors)

	// All three features must have distinct anchors.
	_, a := anchors.links["feat-auth"]
	_, b := anchors.links["feat-auth-2"]
	_, c := anchors.links["feat-auth-3"]
	assert.True(t, a && b && c, "expected feat-auth, feat-auth-2, feat-auth-3; got %v", anchors.links)
}
