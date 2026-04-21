package cli

import (
	"path/filepath"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDocsFeatureMapCache_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "docsfeaturemap.json")

	features := []string{"auth", "search"}
	fm := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://example.com/auth"}},
		{Feature: "search", Pages: []string{"https://example.com/search", "https://example.com/home"}},
	}

	require.NoError(t, saveDocsFeatureMapCache(path, features, fm))

	got, ok := loadDocsFeatureMapCache(path, features)
	require.True(t, ok)
	require.Len(t, got, 2)
	assert.Equal(t, fm[0].Feature, got[0].Feature)
	assert.ElementsMatch(t, fm[0].Pages, got[0].Pages)
	assert.ElementsMatch(t, fm[1].Pages, got[1].Pages)
}

func TestDocsFeatureMapCache_StaleOnFeatureChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "docsfeaturemap.json")

	features := []string{"auth"}
	fm := analyzer.DocsFeatureMap{{Feature: "auth", Pages: []string{}}}
	require.NoError(t, saveDocsFeatureMapCache(path, features, fm))

	_, ok := loadDocsFeatureMapCache(path, []string{"auth", "new-feature"})
	assert.False(t, ok, "cache should be invalid when features change")
}

func TestDocsFeatureMapCache_MissingFile(t *testing.T) {
	_, ok := loadDocsFeatureMapCache("/tmp/does-not-exist-ftg.json", []string{"auth"})
	assert.False(t, ok)
}

func TestDocsFeatureMapCache_NilPagesNormalized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "docsfeaturemap.json")

	features := []string{"auth"}
	fm := analyzer.DocsFeatureMap{{Feature: "auth", Pages: nil}}
	require.NoError(t, saveDocsFeatureMapCache(path, features, fm))

	got, ok := loadDocsFeatureMapCache(path, features)
	require.True(t, ok)
	assert.NotNil(t, got[0].Pages, "nil pages should be normalized to empty slice on load")
}
