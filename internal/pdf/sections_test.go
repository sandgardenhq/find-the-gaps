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
