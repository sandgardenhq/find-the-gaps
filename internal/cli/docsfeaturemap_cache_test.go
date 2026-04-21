package cli

import (
	"os"
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

func TestDocsFeatureMapCache_CorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "docsfeaturemap.json")
	require.NoError(t, os.WriteFile(path, []byte("not json {{{"), 0o644))

	_, ok := loadDocsFeatureMapCache(path, []string{"auth"})
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

// TestDocsFeatureMapCache_ReadError_ReturnsMiss covers the generic os.ReadFile error
// branch (not os.ErrNotExist) in loadDocsFeatureMapCache by pointing at a directory.
func TestDocsFeatureMapCache_ReadError_ReturnsMiss(t *testing.T) {
	dir := t.TempDir()
	_, ok := loadDocsFeatureMapCache(dir, []string{"auth"})
	assert.False(t, ok, "expected cache miss when path is a directory (read error)")
}

// TestDocsFeatureMapCache_NilPagesInRawJSON covers the nil-pages normalization branch
// in loadDocsFeatureMapCache by writing a raw JSON file with null pages.
func TestDocsFeatureMapCache_NilPagesInRawJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "docsfeaturemap.json")
	// Write raw JSON where pages is null, bypassing saveDocsFeatureMapCache normalization.
	raw := `{"features":["auth"],"entries":[{"feature":"auth","pages":null}]}`
	require.NoError(t, os.WriteFile(path, []byte(raw), 0o644))

	got, ok := loadDocsFeatureMapCache(path, []string{"auth"})
	require.True(t, ok)
	assert.NotNil(t, got[0].Pages, "null pages in JSON must be normalized to empty slice on load")
	assert.Len(t, got[0].Pages, 0)
}
