package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDriftCache_FileNotExist_ReturnsFalse(t *testing.T) {
	_, ok := loadDriftCache(filepath.Join(t.TempDir(), "drift.json"))
	assert.False(t, ok)
}

func TestLoadDriftCache_CorruptJSON_ReturnsFalse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drift.json")
	require.NoError(t, os.WriteFile(path, []byte("not json {{{"), 0o644))
	_, ok := loadDriftCache(path)
	assert.False(t, ok)
}

func TestLoadDriftCache_ReadError_ReturnsFalse(t *testing.T) {
	// Pass a directory; ReadFile returns a non-not-exist error.
	dir := t.TempDir()
	_, ok := loadDriftCache(dir)
	assert.False(t, ok)
}

func TestSaveAndLoadDriftCache_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drift.json")
	in := map[string]analyzer.CachedDriftEntry{
		"auth": {
			Files:  []string{"auth.go", "session.go"},
			Pages:  []string{"https://docs.example.com/auth"},
			Issues: []analyzer.DriftIssue{{Page: "https://docs.example.com/auth", Issue: "Stale signature."}},
		},
		"search": {
			Files:  []string{"search.go"},
			Pages:  []string{"https://docs.example.com/search"},
			Issues: []analyzer.DriftIssue{},
		},
	}
	require.NoError(t, saveDriftCache(path, in))

	got, ok := loadDriftCache(path)
	require.True(t, ok)
	require.Len(t, got, 2)

	assert.Equal(t, in["auth"].Files, got["auth"].Files)
	assert.Equal(t, in["auth"].Pages, got["auth"].Pages)
	assert.Equal(t, in["auth"].Issues, got["auth"].Issues)
	assert.Empty(t, got["search"].Issues, "empty issues array must round-trip as empty (or nil)")
}

func TestSaveDriftCache_EntriesSortedByFeatureName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drift.json")
	in := map[string]analyzer.CachedDriftEntry{
		"zebra": {Files: []string{"z.go"}, Pages: []string{"https://docs.example.com/z"}},
		"alpha": {Files: []string{"a.go"}, Pages: []string{"https://docs.example.com/a"}},
		"mango": {Files: []string{"m.go"}, Pages: []string{"https://docs.example.com/m"}},
	}
	require.NoError(t, saveDriftCache(path, in))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	// Cheap structural check: alpha appears before mango appears before zebra.
	str := string(data)
	iAlpha := indexOf(str, "alpha")
	iMango := indexOf(str, "mango")
	iZebra := indexOf(str, "zebra")
	require.True(t, iAlpha >= 0 && iMango >= 0 && iZebra >= 0)
	assert.Less(t, iAlpha, iMango)
	assert.Less(t, iMango, iZebra)
}

func TestSaveDriftCache_AtomicReplace(t *testing.T) {
	// Save twice; second save must fully replace the first without leaving a tmp file.
	path := filepath.Join(t.TempDir(), "drift.json")
	require.NoError(t, saveDriftCache(path, map[string]analyzer.CachedDriftEntry{
		"first": {Files: []string{"f.go"}, Pages: []string{"https://docs.example.com/f"}},
	}))
	require.NoError(t, saveDriftCache(path, map[string]analyzer.CachedDriftEntry{
		"second": {Files: []string{"s.go"}, Pages: []string{"https://docs.example.com/s"}},
	}))

	got, ok := loadDriftCache(path)
	require.True(t, ok)
	require.Len(t, got, 1)
	_, hasSecond := got["second"]
	assert.True(t, hasSecond)

	// No leftover tmp file in the directory.
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	for _, e := range entries {
		assert.Equal(t, "drift.json", e.Name(), "unexpected leftover file: %s", e.Name())
	}

	// Mode must match the other cache files (0o644), not os.CreateTemp's 0o600 default.
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm(), "drift.json must be 0o644 to match featuremap.json/codefeatures.json")
}

func TestLoadDriftCache_NilSlicesNormalized(t *testing.T) {
	// A drift.json on disk with JSON null for files/pages/issues must round-trip
	// to empty slices, not nil — otherwise equality checks against the current
	// run's sorted slices would mis-classify these as cache misses.
	path := filepath.Join(t.TempDir(), "drift.json")
	raw := `{"features":["empty"],"entries":[{"feature":"empty","files":null,"pages":null,"issues":null}]}`
	require.NoError(t, os.WriteFile(path, []byte(raw), 0o644))

	got, ok := loadDriftCache(path)
	require.True(t, ok)
	entry, hasEntry := got["empty"]
	require.True(t, hasEntry)
	assert.NotNil(t, entry.Files, "Files must be normalized to empty slice, not nil")
	assert.NotNil(t, entry.Pages, "Pages must be normalized to empty slice, not nil")
	assert.NotNil(t, entry.Issues, "Issues must be normalized to empty slice, not nil")
	assert.Empty(t, entry.Files)
	assert.Empty(t, entry.Pages)
	assert.Empty(t, entry.Issues)
}

// indexOf is strings.Index inlined to avoid an extra import in the test file.
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
