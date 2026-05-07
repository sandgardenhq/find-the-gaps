package updatecheck

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadCache_MissingFileReturnsZero(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update-check.json")
	c, err := ReadCache(path)
	require.NoError(t, err, "missing cache file is not an error")
	assert.Zero(t, c)
}

func TestReadCache_CorruptFileReturnsZero(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update-check.json")
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o644))

	c, err := ReadCache(path)
	require.NoError(t, err, "corrupt cache files must be tolerated, not error")
	assert.Zero(t, c)
}

func TestWriteCache_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "update-check.json")
	now := time.Date(2026, 5, 6, 14, 32, 11, 0, time.UTC)

	want := Cache{
		LastCheckedAt:         now,
		LatestVersion:         "v1.4.2",
		CurrentVersionAtCheck: "v1.3.0",
	}
	require.NoError(t, WriteCache(path, want))

	got, err := ReadCache(path)
	require.NoError(t, err)
	assert.Equal(t, want.LatestVersion, got.LatestVersion)
	assert.Equal(t, want.CurrentVersionAtCheck, got.CurrentVersionAtCheck)
	assert.True(t, want.LastCheckedAt.Equal(got.LastCheckedAt))
}

func TestIsFresh_WithinWindow(t *testing.T) {
	now := time.Now()
	c := Cache{LastCheckedAt: now.Add(-1 * time.Hour)}
	assert.True(t, c.IsFresh(now, 24*time.Hour))
}

func TestIsFresh_OutsideWindow(t *testing.T) {
	now := time.Now()
	c := Cache{LastCheckedAt: now.Add(-25 * time.Hour)}
	assert.False(t, c.IsFresh(now, 24*time.Hour))
}

func TestIsFresh_ZeroValueIsStale(t *testing.T) {
	var c Cache
	assert.False(t, c.IsFresh(time.Now(), 24*time.Hour))
}

func TestUserUpgradedSinceCache(t *testing.T) {
	c := Cache{
		LatestVersion:         "v1.4.2",
		CurrentVersionAtCheck: "v1.3.0",
	}
	// User was at v1.3.0 when we checked, now they're at v1.4.2 — caught up.
	assert.True(t, c.UserHasReachedLatest("v1.4.2"))
	// Still behind.
	assert.False(t, c.UserHasReachedLatest("v1.3.0"))
	// User leapfrogged.
	assert.True(t, c.UserHasReachedLatest("v1.5.0"))
}

func TestUserUpgradedSinceCache_ZeroCacheNeverReached(t *testing.T) {
	var c Cache
	assert.False(t, c.UserHasReachedLatest("v1.4.2"))
}
