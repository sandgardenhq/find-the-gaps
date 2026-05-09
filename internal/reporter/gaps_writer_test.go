package reporter_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/reporter"
)

func makeFinding(name string) []analyzer.DriftFinding {
	return []analyzer.DriftFinding{{
		Feature: name,
		Issues: []analyzer.DriftIssue{{
			Page:           "https://example.com/" + name,
			Issue:          "issue " + name,
			Priority:       analyzer.PriorityMedium,
			PriorityReason: "reason " + name,
		}},
	}}
}

func TestGapsWriter_coalescesBursts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaps.md")
	prefix := "# Gaps Found\n\nstatic body\n"
	debounce := 50 * time.Millisecond
	w := reporter.NewGapsWriter(dir, prefix, debounce)

	for i := range 5 {
		w.Push(makeFinding(fmt.Sprintf("f%d", i)))
	}

	// Wait past debounce so the writer goroutine fires its single coalesced
	// flush. Without coalescing this would be five separate writes (and five
	// distinct mtimes); with coalescing it is exactly one.
	time.Sleep(3 * debounce)
	mtimeAfterBurst := mustModTime(t, path)

	require.NoError(t, w.Close())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "f4")
	assert.Contains(t, content, "static body")
	assert.Contains(t, content, "## Stale Documentation")
	// Close's final flush is a no-op when nothing is dirty, so mtime must
	// match the single burst-coalesced write captured above.
	assert.Equal(t, mtimeAfterBurst, mustModTime(t, path),
		"Close should not have triggered an additional write — burst was already coalesced")
}

// TestGapsWriter_rearmsTimerOnSubsequentPush exercises the Stop()+drain path
// in arm(): a Push lands while the timer is already armed but not yet fired.
// Without correct rearm, the second Push's bytes would either fail to flush
// or double-fire the timer.
func TestGapsWriter_rearmsTimerOnSubsequentPush(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaps.md")
	prefix := "# Gaps Found\n\nstatic body\n"
	debounce := 80 * time.Millisecond
	w := reporter.NewGapsWriter(dir, prefix, debounce)
	t.Cleanup(func() { _ = w.Close() })

	w.Push(makeFinding("first"))
	time.Sleep(debounce / 2) // timer is armed but not yet fired
	w.Push(makeFinding("second"))

	// Wait past the second debounce window. The timer was rearmed by the
	// second Push; we expect exactly one flush carrying "second".
	time.Sleep(3 * debounce)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "second")
	assert.NotContains(t, content, "first",
		"second Push must overwrite first — last-write-wins via timer rearm")
}

func mustModTime(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info.ModTime()
}

func TestGapsWriter_finalFlushOnClose(t *testing.T) {
	dir := t.TempDir()
	prefix := "# Gaps Found\n\nstatic body\n"
	w := reporter.NewGapsWriter(dir, prefix, time.Hour)

	w.Push(makeFinding("only"))
	require.NoError(t, w.Close())

	data, err := os.ReadFile(filepath.Join(dir, "gaps.md"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "only")
}

func TestGapsWriter_atomicReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaps.md")
	prefix := "# Gaps Found\n\nstatic body\n"
	w := reporter.NewGapsWriter(dir, prefix, 5*time.Millisecond)

	stop := make(chan struct{})
	var observerWG sync.WaitGroup
	observerWG.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			data, err := os.ReadFile(path)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				t.Errorf("unexpected read error: %v", err)
				return
			}
			s := string(data)
			if !strings.HasPrefix(s, "# Gaps Found") {
				t.Errorf("torn write observed: %q", s)
				return
			}
			if !strings.Contains(s, "## Stale Documentation") {
				t.Errorf("incomplete write observed: %q", s)
				return
			}
		}
	})

	for i := range 50 {
		w.Push(makeFinding(fmt.Sprintf("f%d", i)))
		time.Sleep(time.Millisecond)
	}
	require.NoError(t, w.Close())
	close(stop)
	observerWG.Wait()
}

func TestGapsWriter_concurrentPushUnderRace(t *testing.T) {
	dir := t.TempDir()
	prefix := "# Gaps Found\n\nstatic body\n"
	w := reporter.NewGapsWriter(dir, prefix, 10*time.Millisecond)

	const N = 32
	var wg sync.WaitGroup
	for i := range N {
		wg.Go(func() {
			w.Push(makeFinding(fmt.Sprintf("f%02d", i)))
		})
	}
	wg.Wait()
	require.NoError(t, w.Close())

	data, err := os.ReadFile(filepath.Join(dir, "gaps.md"))
	require.NoError(t, err)
	content := string(data)

	matched := false
	for i := range N {
		if strings.Contains(content, fmt.Sprintf("f%02d", i)) {
			matched = true
			break
		}
	}
	assert.True(t, matched, "expected on-disk content to match one pushed state")
}

func TestGapsWriter_byteIdenticalToWriteGaps(t *testing.T) {
	mapping := analyzer.FeatureMap{
		{Feature: analyzer.CodeFeature{Name: "alpha", UserFacing: true}, Files: []string{"a.go"}},
		{Feature: analyzer.CodeFeature{Name: "beta"}, Files: []string{"b.go"}},
	}
	docFeatures := []string{"gamma"}
	drift := []analyzer.DriftFinding{
		{
			Feature: "alpha",
			Issues: []analyzer.DriftIssue{{
				Page: "https://example.com/alpha", Issue: "stale", Priority: analyzer.PriorityLarge, PriorityReason: "ships in API",
			}},
		},
	}

	dirA := t.TempDir()
	require.NoError(t, reporter.WriteGaps(dirA, mapping, docFeatures, drift))
	wantBytes, err := os.ReadFile(filepath.Join(dirA, "gaps.md"))
	require.NoError(t, err)

	dirB := t.TempDir()
	prefix := reporter.BuildGapsStaticPrefix(mapping, docFeatures, nil)
	w := reporter.NewGapsWriter(dirB, prefix, time.Millisecond)
	w.Push(drift)
	require.NoError(t, w.Close())
	gotBytes, err := os.ReadFile(filepath.Join(dirB, "gaps.md"))
	require.NoError(t, err)

	assert.Equal(t, string(wantBytes), string(gotBytes))
}

func TestGapsWriter_closeIdempotent(t *testing.T) {
	dir := t.TempDir()
	w := reporter.NewGapsWriter(dir, "prefix\n", time.Millisecond)
	require.NoError(t, w.Close())
	require.NoError(t, w.Close())
}

// TestGapsWriter_closeReportsFlushError pins that disk failures during the
// final flush surface to the caller. analyze.go relies on this to fail the
// run loudly instead of silently leaving a stale gaps.md.
func TestGapsWriter_closeReportsFlushError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing-subdir")
	// Directory does not exist; os.WriteFile to <dir>/gaps.md.tmp will fail.
	w := reporter.NewGapsWriter(dir, "# Gaps Found\n\n", time.Millisecond)
	w.Push(makeFinding("any"))
	err := w.Close()
	require.Error(t, err)
	assert.True(t,
		errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no such file"),
		"expected a path-not-found error, got %v", err)
}
