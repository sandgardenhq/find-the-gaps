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
	assert.Contains(t, body, "analysis engine")
	assert.Contains(t, body, "User-facing: yes")
	assert.Contains(t, body, "User-facing: no")
	assert.Contains(t, body, "documented") // status for the page-covered feature
	assert.Contains(t, body, "undocumented")
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

	// Locate the Gaps section page by scanning for the "Gaps" heading.
	var gapsText string
	for p := 1; p <= r.NumPage(); p++ {
		txt, err := r.Page(p).GetPlainText(nil)
		require.NoError(t, err)
		// Must include the section heading, "Large" sub-heading, and at
		// least one finding to count as the gaps page.
		if strings.Contains(txt, "Gaps") && strings.Contains(txt, "Large") {
			gapsText = txt
			break
		}
	}
	require.NotEmpty(t, gapsText, "Gaps section must render with sub-headings")

	// Order: Large appears before Medium appears before Small.
	largeIdx := strings.Index(gapsText, "Large")
	mediumIdx := strings.Index(gapsText, "Medium")
	smallIdx := strings.Index(gapsText, "Small")
	require.True(t, largeIdx >= 0 && mediumIdx > largeIdx && smallIdx > mediumIdx,
		"buckets must appear in Large → Medium → Small order; got large=%d medium=%d small=%d in:\n%s",
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

	var gapsText string
	for p := 1; p <= r.NumPage(); p++ {
		txt, _ := r.Page(p).GetPlainText(nil)
		if strings.Contains(txt, "minor typo") {
			gapsText = txt
			break
		}
	}
	require.NotEmpty(t, gapsText)

	// Only Small bucket has content; Large/Medium sub-headings must NOT
	// appear under Gaps.
	assert.NotContains(t, gapsText, "Large", "empty Large bucket must be omitted")
	assert.NotContains(t, gapsText, "Medium", "empty Medium bucket must be omitted")
	assert.Contains(t, gapsText, "Small")
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
