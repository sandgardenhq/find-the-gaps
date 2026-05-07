package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubGitHub returns a server that always reports `tag` as the latest release.
func stubGitHub(t *testing.T, tag string) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": tag})
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// withTestEnv sets env vars for the duration of the test and restores them.
// Always starts by clearing inherited gate-relevant env vars (CI,
// FIND_THE_GAPS_QUIET, FIND_THE_GAPS_NO_UPDATE_CHECK) so the test sees a
// known baseline regardless of the harness environment — GitHub Actions sets
// CI=true, which would silently flip every "should-show-notice" test into a
// skip. Values in kv overlay on top, so a test asserting the CI gate can set
// CI=true via kv.
func withTestEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for _, k := range []string{"CI", "FIND_THE_GAPS_QUIET", "FIND_THE_GAPS_NO_UPDATE_CHECK"} {
		t.Setenv(k, "")
	}
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func TestUpdateCheckWiring_NoticePrintedOnStderrAfterAnalyze(t *testing.T) {
	srv, hits := stubGitHub(t, "v9.9.9")
	cachePath := filepath.Join(t.TempDir(), "update-check.json")
	withTestEnv(t, map[string]string{
		"FIND_THE_GAPS_UPDATE_BASE_URL":   srv.URL,
		"FIND_THE_GAPS_UPDATE_CACHE_PATH": cachePath,
		"FIND_THE_GAPS_UPDATE_FORCE_TTY":  "1",
		"FIND_THE_GAPS_UPDATE_VERSION":    "v0.0.1", // pretend running a real, behind version
	})

	dir := t.TempDir()
	cacheBase := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"analyze", "--repo", dir, "--cache-dir", cacheBase})
	require.Equal(t, 0, code, "stderr=%q", stderr.String())

	assert.Contains(t, stderr.String(), "A new version of ftg is available: v9.9.9")
	assert.Contains(t, stderr.String(), "v0.0.1")
	assert.EqualValues(t, 1, atomic.LoadInt32(hits), "expected exactly one GitHub call")
}

func TestUpdateCheckWiring_GatedByCIEnv(t *testing.T) {
	srv, hits := stubGitHub(t, "v9.9.9")
	cachePath := filepath.Join(t.TempDir(), "update-check.json")
	withTestEnv(t, map[string]string{
		"FIND_THE_GAPS_UPDATE_BASE_URL":   srv.URL,
		"FIND_THE_GAPS_UPDATE_CACHE_PATH": cachePath,
		"FIND_THE_GAPS_UPDATE_FORCE_TTY":  "1",
		"FIND_THE_GAPS_UPDATE_VERSION":    "v0.0.1",
		"CI":                              "true",
	})

	dir := t.TempDir()
	cacheBase := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"analyze", "--repo", dir, "--cache-dir", cacheBase})
	require.Equal(t, 0, code, "stderr=%q", stderr.String())

	assert.NotContains(t, stderr.String(), "A new version of ftg is available")
	assert.EqualValues(t, 0, atomic.LoadInt32(hits), "CI gate must skip the network")
}

func TestUpdateCheckWiring_GatedByDedicatedKillSwitch(t *testing.T) {
	srv, hits := stubGitHub(t, "v9.9.9")
	cachePath := filepath.Join(t.TempDir(), "update-check.json")
	withTestEnv(t, map[string]string{
		"FIND_THE_GAPS_UPDATE_BASE_URL":   srv.URL,
		"FIND_THE_GAPS_UPDATE_CACHE_PATH": cachePath,
		"FIND_THE_GAPS_UPDATE_FORCE_TTY":  "1",
		"FIND_THE_GAPS_UPDATE_VERSION":    "v0.0.1",
		"FIND_THE_GAPS_NO_UPDATE_CHECK":   "1",
	})

	dir := t.TempDir()
	cacheBase := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"analyze", "--repo", dir, "--cache-dir", cacheBase})
	require.Equal(t, 0, code, "stderr=%q", stderr.String())

	assert.NotContains(t, stderr.String(), "A new version of ftg is available")
	assert.EqualValues(t, 0, atomic.LoadInt32(hits))
}

func TestUpdateCheckWiring_HelpDoesNotTriggerCheck(t *testing.T) {
	srv, hits := stubGitHub(t, "v9.9.9")
	cachePath := filepath.Join(t.TempDir(), "update-check.json")
	withTestEnv(t, map[string]string{
		"FIND_THE_GAPS_UPDATE_BASE_URL":   srv.URL,
		"FIND_THE_GAPS_UPDATE_CACHE_PATH": cachePath,
		"FIND_THE_GAPS_UPDATE_FORCE_TTY":  "1",
		"FIND_THE_GAPS_UPDATE_VERSION":    "v0.0.1",
	})

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"--help"})
	require.Equal(t, 0, code)

	assert.NotContains(t, stderr.String(), "A new version of ftg is available")
	assert.EqualValues(t, 0, atomic.LoadInt32(hits))
}

func TestUpdateCheckWiring_VersionDoesNotTriggerCheck(t *testing.T) {
	srv, hits := stubGitHub(t, "v9.9.9")
	cachePath := filepath.Join(t.TempDir(), "update-check.json")
	withTestEnv(t, map[string]string{
		"FIND_THE_GAPS_UPDATE_BASE_URL":   srv.URL,
		"FIND_THE_GAPS_UPDATE_CACHE_PATH": cachePath,
		"FIND_THE_GAPS_UPDATE_FORCE_TTY":  "1",
		"FIND_THE_GAPS_UPDATE_VERSION":    "v0.0.1",
	})

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"--version"})
	require.Equal(t, 0, code)

	assert.NotContains(t, stderr.String(), "A new version of ftg is available")
	assert.EqualValues(t, 0, atomic.LoadInt32(hits))
}

func TestUpdateCheckWiring_DevBuildSkips(t *testing.T) {
	srv, hits := stubGitHub(t, "v9.9.9")
	cachePath := filepath.Join(t.TempDir(), "update-check.json")
	// No FIND_THE_GAPS_UPDATE_VERSION → falls back to currentVersion()'s "dev"
	withTestEnv(t, map[string]string{
		"FIND_THE_GAPS_UPDATE_BASE_URL":   srv.URL,
		"FIND_THE_GAPS_UPDATE_CACHE_PATH": cachePath,
		"FIND_THE_GAPS_UPDATE_FORCE_TTY":  "1",
	})

	dir := t.TempDir()
	cacheBase := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"analyze", "--repo", dir, "--cache-dir", cacheBase})
	require.Equal(t, 0, code)

	assert.NotContains(t, stderr.String(), "A new version of ftg is available")
	assert.EqualValues(t, 0, atomic.LoadInt32(hits))
}

func TestUpdateCheckWiring_NoticeAfterCommandOutput(t *testing.T) {
	srv, _ := stubGitHub(t, "v9.9.9")
	cachePath := filepath.Join(t.TempDir(), "update-check.json")
	withTestEnv(t, map[string]string{
		"FIND_THE_GAPS_UPDATE_BASE_URL":   srv.URL,
		"FIND_THE_GAPS_UPDATE_CACHE_PATH": cachePath,
		"FIND_THE_GAPS_UPDATE_FORCE_TTY":  "1",
		"FIND_THE_GAPS_UPDATE_VERSION":    "v0.0.1",
	})

	dir := t.TempDir()
	cacheBase := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"analyze", "--repo", dir, "--cache-dir", cacheBase})
	require.Equal(t, 0, code)

	noticeIdx := strings.Index(stderr.String(), "A new version of ftg is available")
	require.NotEqual(t, -1, noticeIdx, "notice missing from stderr: %q", stderr.String())

	// The analyze pipeline logs "scanning repository" via charmbracelet/log to
	// stderr — the notice must come after those logs.
	scanIdx := strings.Index(stderr.String(), "scanning repository")
	if scanIdx >= 0 {
		assert.Greater(t, noticeIdx, scanIdx,
			"update notice must appear after the analyze logs, not before")
	}
}
