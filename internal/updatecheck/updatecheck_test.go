package updatecheck_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sandgardenhq/find-the-gaps/internal/updatecheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newGitHubStub(t *testing.T, tag string) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": tag})
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func baseOpts(t *testing.T, baseURL string) updatecheck.RunOptions {
	t.Helper()
	return updatecheck.RunOptions{
		CurrentVersion: "v1.3.0",
		Command:        "analyze",
		StderrIsTTY:    true,
		Env:            func(string) string { return "" },
		GOOS:           runtime.GOOS,
		BrewOnPath:     true,
		CachePath:      filepath.Join(t.TempDir(), "update-check.json"),
		BaseURL:        baseURL,
		Timeout:        2 * time.Second,
		Now:            func() time.Time { return time.Date(2026, 5, 6, 14, 0, 0, 0, time.UTC) },
	}
}

func TestRun_NewerVersionRendersNotice(t *testing.T) {
	srv, hits := newGitHubStub(t, "v1.4.2")
	opts := baseOpts(t, srv.URL)

	notice, err := updatecheck.Run(context.Background(), opts)
	require.NoError(t, err)
	assert.Contains(t, notice, "v1.4.2")
	assert.Contains(t, notice, "v1.3.0")
	assert.EqualValues(t, 1, atomic.LoadInt32(hits))
}

func TestRun_SameVersionRendersNothing(t *testing.T) {
	srv, _ := newGitHubStub(t, "v1.3.0")
	opts := baseOpts(t, srv.URL)

	notice, err := updatecheck.Run(context.Background(), opts)
	require.NoError(t, err)
	assert.Empty(t, notice)
}

func TestRun_OlderRemoteRendersNothing(t *testing.T) {
	srv, _ := newGitHubStub(t, "v1.0.0") // hypothetical: server says older than us
	opts := baseOpts(t, srv.URL)

	notice, err := updatecheck.Run(context.Background(), opts)
	require.NoError(t, err)
	assert.Empty(t, notice, "if remote tag is older than local, treat as up to date")
}

func TestRun_GatedSkipsNetworkAndCache(t *testing.T) {
	srv, hits := newGitHubStub(t, "v1.4.2")
	opts := baseOpts(t, srv.URL)
	opts.CurrentVersion = "dev" // dev build → gate skips

	notice, err := updatecheck.Run(context.Background(), opts)
	require.NoError(t, err)
	assert.Empty(t, notice)
	assert.EqualValues(t, 0, atomic.LoadInt32(hits), "gated runs must not hit the network")
	_, statErr := os.Stat(opts.CachePath)
	assert.True(t, os.IsNotExist(statErr), "gated runs must not write the cache")
}

func TestRun_CacheHitShortCircuitsNetwork(t *testing.T) {
	srv, hits := newGitHubStub(t, "v1.4.2")
	opts := baseOpts(t, srv.URL)

	// First run primes the cache.
	_, err := updatecheck.Run(context.Background(), opts)
	require.NoError(t, err)
	require.EqualValues(t, 1, atomic.LoadInt32(hits))

	// Second run within the cache window must not hit the network.
	notice, err := updatecheck.Run(context.Background(), opts)
	require.NoError(t, err)
	assert.Contains(t, notice, "v1.4.2", "cached latest still triggers a notice")
	assert.EqualValues(t, 1, atomic.LoadInt32(hits), "second call must not hit the network")
}

func TestRun_CacheStaleHitsNetworkAgain(t *testing.T) {
	srv, hits := newGitHubStub(t, "v1.4.2")
	opts := baseOpts(t, srv.URL)

	_, err := updatecheck.Run(context.Background(), opts)
	require.NoError(t, err)
	require.EqualValues(t, 1, atomic.LoadInt32(hits))

	// Advance the clock past the 24h cache window.
	opts.Now = func() time.Time {
		return time.Date(2026, 5, 8, 14, 0, 0, 0, time.UTC)
	}

	_, err = updatecheck.Run(context.Background(), opts)
	require.NoError(t, err)
	assert.EqualValues(t, 2, atomic.LoadInt32(hits), "stale cache should refresh from network")
}

func TestRun_NetworkFailureSilent(t *testing.T) {
	// A server that always 500s — Run should swallow the error and return "".
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	opts := baseOpts(t, srv.URL)

	notice, err := updatecheck.Run(context.Background(), opts)
	require.NoError(t, err, "Run must not surface network failures")
	assert.Empty(t, notice)
}

func TestRun_UserUpgradedSinceCacheStaysQuiet(t *testing.T) {
	srv, _ := newGitHubStub(t, "v1.4.2")
	opts := baseOpts(t, srv.URL)

	// Prime cache as if user was at v1.3.0.
	_, err := updatecheck.Run(context.Background(), opts)
	require.NoError(t, err)

	// User upgrades; second invocation reports v1.4.2 as the running version.
	opts.CurrentVersion = "v1.4.2"
	notice, err := updatecheck.Run(context.Background(), opts)
	require.NoError(t, err)
	assert.Empty(t, notice, "cache hit + user is at latest = no notice")
}

func TestRun_CacheWrittenAfterSuccessfulFetch(t *testing.T) {
	srv, _ := newGitHubStub(t, "v1.4.2")
	opts := baseOpts(t, srv.URL)

	_, err := updatecheck.Run(context.Background(), opts)
	require.NoError(t, err)

	c, err := updatecheck.ReadCache(opts.CachePath)
	require.NoError(t, err)
	assert.Equal(t, "v1.4.2", c.LatestVersion)
	assert.Equal(t, "v1.3.0", c.CurrentVersionAtCheck)
	assert.False(t, c.LastCheckedAt.IsZero())
}
