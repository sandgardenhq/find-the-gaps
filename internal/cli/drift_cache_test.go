package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestIsDriftCacheHit(t *testing.T) {
	cached := map[string]analyzer.CachedDriftEntry{
		"auth": {
			Files: []string{"auth.go", "session.go"},
			Pages: []string{"https://docs.example.com/auth"},
		},
	}

	cases := []struct {
		name      string
		mapArg    map[string]analyzer.CachedDriftEntry
		key       string
		files     []string
		pages     []string
		wantHit   bool
		assertion string
	}{
		{
			name:      "exact match → hit",
			mapArg:    cached,
			key:       "auth",
			files:     []string{"auth.go", "session.go"},
			pages:     []string{"https://docs.example.com/auth"},
			wantHit:   true,
			assertion: "identical sorted slices must match",
		},
		{
			name:      "missing key → miss",
			mapArg:    cached,
			key:       "search",
			files:     []string{"search.go"},
			pages:     []string{"https://docs.example.com/search"},
			wantHit:   false,
			assertion: "key not in cache must return false",
		},
		{
			name:      "files differ → miss",
			mapArg:    cached,
			key:       "auth",
			files:     []string{"auth.go"},
			pages:     []string{"https://docs.example.com/auth"},
			wantHit:   false,
			assertion: "shorter file list with same prefix must not match",
		},
		{
			name:      "pages differ → miss",
			mapArg:    cached,
			key:       "auth",
			files:     []string{"auth.go", "session.go"},
			pages:     []string{"https://docs.example.com/old"},
			wantHit:   false,
			assertion: "different page list must not match",
		},
		{
			name:      "nil cached map → miss",
			mapArg:    nil,
			key:       "auth",
			files:     []string{"auth.go"},
			pages:     []string{"https://docs.example.com/auth"},
			wantHit:   false,
			assertion: "nil cached map (first run or --no-cache) must return false without panic",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isDriftCacheHit(tc.mapArg, tc.key, tc.files, tc.pages)
			assert.Equal(t, tc.wantHit, got, tc.assertion)
		})
	}
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

func TestSeedDriftLiveCache_NilCached_ReturnsEmptyMap(t *testing.T) {
	fm := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	got := seedDriftLiveCache(nil, fm)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestSeedDriftLiveCache_PreservesEntriesForFeaturesInMap(t *testing.T) {
	cached := map[string]analyzer.CachedDriftEntry{
		"auth": {
			Files:  []string{"auth.go"},
			Pages:  []string{"https://docs.example.com/auth"},
			Issues: []analyzer.DriftIssue{{Page: "https://docs.example.com/auth", Issue: "Stale."}},
		},
		"search": {
			Files:  []string{"search.go"},
			Pages:  []string{"https://docs.example.com/search"},
			Issues: []analyzer.DriftIssue{},
		},
	}
	fm := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
		{Feature: analyzer.CodeFeature{Name: "search"}, Files: []string{"search.go"}},
	}
	got := seedDriftLiveCache(cached, fm)
	require.Len(t, got, 2)
	assert.Equal(t, cached["auth"], got["auth"])
	assert.Equal(t, cached["search"], got["search"])
}

func TestSeedDriftLiveCache_DropsFeaturesRemovedFromMap(t *testing.T) {
	cached := map[string]analyzer.CachedDriftEntry{
		"auth":    {Files: []string{"auth.go"}, Pages: []string{"https://docs.example.com/auth"}},
		"search":  {Files: []string{"search.go"}, Pages: []string{"https://docs.example.com/search"}},
		"removed": {Files: []string{"old.go"}, Pages: []string{"https://docs.example.com/old"}},
	}
	fm := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
		{Feature: analyzer.CodeFeature{Name: "search"}, Files: []string{"search.go"}},
	}
	got := seedDriftLiveCache(cached, fm)
	require.Len(t, got, 2)
	_, hasRemoved := got["removed"]
	assert.False(t, hasRemoved, "features no longer in featureMap must not be seeded")
}

func TestSeedDriftLiveCache_EmptyFeatureMap_ReturnsEmpty(t *testing.T) {
	cached := map[string]analyzer.CachedDriftEntry{
		"auth": {Files: []string{"auth.go"}, Pages: []string{"https://docs.example.com/auth"}},
	}
	got := seedDriftLiveCache(cached, analyzer.FeatureMap{})
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

// newDriftCachePersister tests — a cache hit must not rewrite drift.json,
// because the on-disk entry already matches; a miss must rewrite it.

func TestNewDriftCachePersister_CacheHit_DoesNotRewriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "drift.json")
	sentinel := []byte(`SENTINEL_DO_NOT_OVERWRITE`)
	require.NoError(t, os.WriteFile(path, sentinel, 0o644))

	cached := map[string]analyzer.CachedDriftEntry{
		"auth": {
			Files:  []string{"auth.go"},
			Pages:  []string{"https://docs.example.com/auth"},
			Issues: []analyzer.DriftIssue{},
		},
	}
	live := map[string]analyzer.CachedDriftEntry{"auth": cached["auth"]}
	hits, fresh := 0, 0
	persister := newDriftCachePersister(cached, live, path, &hits, &fresh)

	err := persister("auth", []string{"auth.go"}, []string{"https://docs.example.com/auth"}, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, hits)
	assert.Equal(t, 0, fresh)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, sentinel, got, "cache hit must not rewrite drift.json")
}

func TestNewDriftCachePersister_CacheMiss_NoEntry_Saves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "drift.json")

	cached := map[string]analyzer.CachedDriftEntry{}
	live := map[string]analyzer.CachedDriftEntry{}
	hits, fresh := 0, 0
	persister := newDriftCachePersister(cached, live, path, &hits, &fresh)

	issues := []analyzer.DriftIssue{{Page: "https://docs.example.com/auth", Issue: "Stale."}}
	err := persister("auth", []string{"auth.go"}, []string{"https://docs.example.com/auth"}, issues)
	require.NoError(t, err)
	assert.Equal(t, 0, hits)
	assert.Equal(t, 1, fresh)

	got, ok := loadDriftCache(path)
	require.True(t, ok)
	require.Contains(t, got, "auth")
	assert.Equal(t, []string{"auth.go"}, got["auth"].Files)
	assert.Equal(t, issues, got["auth"].Issues)
}

func TestNewDriftCachePersister_CacheMiss_FilesChanged_Saves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "drift.json")

	cached := map[string]analyzer.CachedDriftEntry{
		"auth": {Files: []string{"auth.go"}, Pages: []string{"https://docs.example.com/auth"}},
	}
	live := map[string]analyzer.CachedDriftEntry{"auth": cached["auth"]}
	hits, fresh := 0, 0
	persister := newDriftCachePersister(cached, live, path, &hits, &fresh)

	err := persister("auth", []string{"auth.go", "session.go"}, []string{"https://docs.example.com/auth"}, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, hits)
	assert.Equal(t, 1, fresh)

	got, ok := loadDriftCache(path)
	require.True(t, ok)
	assert.Equal(t, []string{"auth.go", "session.go"}, got["auth"].Files)
}

func TestNewDriftCachePersister_SaveError_Propagated(t *testing.T) {
	// Point at a directory that cannot be written to (the temp dir itself,
	// passed as a path). os.CreateTemp inside a non-existent parent fails.
	bogus := filepath.Join(t.TempDir(), "no-such-dir", "drift.json")

	cached := map[string]analyzer.CachedDriftEntry{}
	live := map[string]analyzer.CachedDriftEntry{}
	hits, fresh := 0, 0
	persister := newDriftCachePersister(cached, live, bogus, &hits, &fresh)

	err := persister("auth", []string{"auth.go"}, []string{"https://docs.example.com/auth"}, nil)
	require.Error(t, err)
	assert.Equal(t, 1, fresh, "fresh increment happens before save attempt")
}

func TestComputeDriftInputHash_Deterministic(t *testing.T) {
	fm := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go", "session.go"}, Symbols: []string{"Login", "Logout"}},
		{Feature: analyzer.CodeFeature{Name: "search"}, Files: []string{"search.go"}, Symbols: []string{"Query"}},
	}
	dm := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://docs.example.com/auth"}},
		{Feature: "search", Pages: []string{"https://docs.example.com/search"}},
	}
	h1 := computeDriftInputHash(fm, dm)
	h2 := computeDriftInputHash(fm, dm)
	assert.Equal(t, h1, h2)
	assert.NotEmpty(t, h1)
	assert.Len(t, h1, 64, "expect hex SHA-256")
}

func TestComputeDriftInputHash_OrderIndependent(t *testing.T) {
	fm1 := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "alpha"}, Files: []string{"a1.go", "a2.go"}, Symbols: []string{"A1"}},
		{Feature: analyzer.CodeFeature{Name: "beta"}, Files: []string{"b1.go"}, Symbols: []string{"B1"}},
	}
	fm2 := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "beta"}, Files: []string{"b1.go"}, Symbols: []string{"B1"}},
		{Feature: analyzer.CodeFeature{Name: "alpha"}, Files: []string{"a2.go", "a1.go"}, Symbols: []string{"A1"}},
	}
	dm1 := analyzer.DocsFeatureMap{
		{Feature: "alpha", Pages: []string{"https://x/1", "https://x/2"}},
	}
	dm2 := analyzer.DocsFeatureMap{
		{Feature: "alpha", Pages: []string{"https://x/2", "https://x/1"}},
	}
	assert.Equal(t, computeDriftInputHash(fm1, dm1), computeDriftInputHash(fm2, dm2))
}

func TestComputeDriftInputHash_DifferentInputsDiffer(t *testing.T) {
	base := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}, Symbols: []string{"Login"}},
	}
	dm := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://x/auth"}},
	}
	h0 := computeDriftInputHash(base, dm)

	// Change a file
	c1 := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go", "session.go"}, Symbols: []string{"Login"}},
	}
	assert.NotEqual(t, h0, computeDriftInputHash(c1, dm), "files change must change hash")

	// Change a symbol
	c2 := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}, Symbols: []string{"Login", "Logout"}},
	}
	assert.NotEqual(t, h0, computeDriftInputHash(c2, dm), "symbols change must change hash")

	// Change a page
	dm2 := analyzer.DocsFeatureMap{
		{Feature: "auth", Pages: []string{"https://x/auth", "https://x/auth2"}},
	}
	assert.NotEqual(t, h0, computeDriftInputHash(base, dm2), "pages change must change hash")

	// Change a feature name
	c3 := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "AUTH"}, Files: []string{"auth.go"}, Symbols: []string{"Login"}},
	}
	assert.NotEqual(t, h0, computeDriftInputHash(c3, dm), "feature name change must change hash")
}

func TestLoadDriftCacheFile_OldShape_NoComplete(t *testing.T) {
	// Old drift.json (written by saveDriftCache today) must load with Complete == nil.
	path := filepath.Join(t.TempDir(), "drift.json")
	in := map[string]analyzer.CachedDriftEntry{
		"auth": {Files: []string{"auth.go"}, Pages: []string{"https://x/auth"}, Issues: []analyzer.DriftIssue{}},
	}
	require.NoError(t, saveDriftCache(path, in))

	file, ok := loadDriftCacheFile(path)
	require.True(t, ok)
	assert.Nil(t, file.Complete)
	require.Len(t, file.Entries, 1)
	assert.Equal(t, "auth", file.Entries[0].Feature)
}

func TestSaveLoadDriftCacheComplete_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drift.json")
	in := map[string]analyzer.CachedDriftEntry{
		"auth": {Files: []string{"auth.go"}, Pages: []string{"https://x/auth"}, Issues: []analyzer.DriftIssue{}},
	}
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	complete := &driftComplete{Hash: "abc123", CompletedAt: now}
	require.NoError(t, saveDriftCacheComplete(path, in, complete))

	file, ok := loadDriftCacheFile(path)
	require.True(t, ok)
	require.NotNil(t, file.Complete)
	assert.Equal(t, "abc123", file.Complete.Hash)
	assert.True(t, file.Complete.CompletedAt.Equal(now))
}

func TestLoadDriftCacheFile_FileNotExist_ReturnsFalse(t *testing.T) {
	_, ok := loadDriftCacheFile(filepath.Join(t.TempDir(), "drift.json"))
	assert.False(t, ok)
}

func TestDriftFindingsFromCache_ReturnsOnlyFeaturesInFeatureMap(t *testing.T) {
	fm := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
		{Feature: analyzer.CodeFeature{Name: "search"}, Files: []string{"search.go"}},
	}
	cache := map[string]analyzer.CachedDriftEntry{
		"auth": {
			Issues: []analyzer.DriftIssue{{Page: "https://x/auth", Issue: "Stale."}},
		},
		"search": {
			Issues: []analyzer.DriftIssue{}, // zero issues — not a finding
		},
		"removed": {
			Issues: []analyzer.DriftIssue{{Page: "https://x/r", Issue: "Should not appear."}},
		},
	}
	out := driftFindingsFromCache(cache, fm)
	require.Len(t, out, 1)
	assert.Equal(t, "auth", out[0].Feature)
	assert.Equal(t, "Stale.", out[0].Issues[0].Issue)
}

func TestDriftFindingsFromCache_EmptyCache_ReturnsNil(t *testing.T) {
	fm := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "auth"}, Files: []string{"auth.go"}},
	}
	out := driftFindingsFromCache(map[string]analyzer.CachedDriftEntry{}, fm)
	assert.Empty(t, out)
}
