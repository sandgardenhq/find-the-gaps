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

// makeScreenshotResult builds a ScreenshotResult whose MissingGaps length is n,
// allowing tests to push distinct payloads in a burst loop.
func makeScreenshotResult(n int) analyzer.ScreenshotResult {
	res := analyzer.ScreenshotResult{}
	for i := range n {
		res.MissingGaps = append(res.MissingGaps, analyzer.ScreenshotGap{
			PageURL:        fmt.Sprintf("https://example.com/p%d", i),
			QuotedPassage:  fmt.Sprintf("passage %d", i),
			ShouldShow:     fmt.Sprintf("show %d", i),
			SuggestedAlt:   fmt.Sprintf("alt %d", i),
			InsertionHint:  fmt.Sprintf("after p%d", i),
			Priority:       analyzer.PriorityMedium,
			PriorityReason: fmt.Sprintf("reason %d", i),
		})
	}
	return res
}

func TestScreenshotsWriter_coalescesBursts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "screenshots.md")
	debounce := 50 * time.Millisecond
	w := reporter.NewScreenshotsWriter(dir, debounce)

	for i := range 5 {
		w.Push(makeScreenshotResult(i + 1))
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
	// Last push had MissingGaps length 5 → final passage index is 4.
	assert.Contains(t, content, "passage 4")
	assert.Contains(t, content, "# Missing Screenshots")
	// Close's final flush is a no-op when nothing is dirty, so mtime must
	// match the single burst-coalesced write captured above.
	assert.Equal(t, mtimeAfterBurst, mustModTime(t, path),
		"Close should not have triggered an additional write — burst was already coalesced")
}

// TestScreenshotsWriter_rearmsTimerOnSubsequentPush exercises the
// Stop()+drain path in arm(): a Push lands while the timer is already armed
// but not yet fired. Without correct rearm, the second Push's bytes would
// either fail to flush or double-fire the timer.
func TestScreenshotsWriter_rearmsTimerOnSubsequentPush(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "screenshots.md")
	debounce := 80 * time.Millisecond
	w := reporter.NewScreenshotsWriter(dir, debounce)
	t.Cleanup(func() { _ = w.Close() })

	w.Push(makeScreenshotResult(1))           // passage 0
	time.Sleep(debounce / 2)                  // timer is armed but not yet fired
	w.Push(makeScreenshotResult(2))           // passages 0 and 1

	// Wait past the second debounce window. The timer was rearmed by the
	// second Push; we expect exactly one flush carrying the second payload.
	time.Sleep(3 * debounce)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "passage 1",
		"second push payload should be on disk after rearm")
}

func TestScreenshotsWriter_finalFlushOnClose(t *testing.T) {
	dir := t.TempDir()
	w := reporter.NewScreenshotsWriter(dir, time.Hour)

	w.Push(makeScreenshotResult(1))
	require.NoError(t, w.Close())

	data, err := os.ReadFile(filepath.Join(dir, "screenshots.md"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "passage 0")
}

func TestScreenshotsWriter_atomicReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "screenshots.md")
	w := reporter.NewScreenshotsWriter(dir, 5*time.Millisecond)

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
			if !strings.HasPrefix(s, "# Missing Screenshots") {
				t.Errorf("torn write observed: %q", s)
				return
			}
		}
	})

	for i := range 50 {
		w.Push(makeScreenshotResult(i + 1))
		time.Sleep(time.Millisecond)
	}
	require.NoError(t, w.Close())
	close(stop)
	observerWG.Wait()
}

func TestScreenshotsWriter_concurrentPushUnderRace(t *testing.T) {
	dir := t.TempDir()
	w := reporter.NewScreenshotsWriter(dir, 10*time.Millisecond)

	const N = 32
	var wg sync.WaitGroup
	for i := range N {
		wg.Go(func() {
			w.Push(makeScreenshotResult(i + 1))
		})
	}
	wg.Wait()
	require.NoError(t, w.Close())

	data, err := os.ReadFile(filepath.Join(dir, "screenshots.md"))
	require.NoError(t, err)
	content := string(data)

	matched := false
	for i := range N {
		if strings.Contains(content, fmt.Sprintf("passage %d", i)) {
			matched = true
			break
		}
	}
	assert.True(t, matched, "expected on-disk content to match one pushed state")
}

// TestScreenshotsWriter_byteIdenticalToWriteScreenshots pins that the writer's
// final flush bytes match WriteScreenshots's bytes exactly, so existing
// reporter golden tests for screenshots.md remain the safety net.
func TestScreenshotsWriter_byteIdenticalToWriteScreenshots(t *testing.T) {
	res := analyzer.ScreenshotResult{
		MissingGaps: []analyzer.ScreenshotGap{{
			PageURL:        "https://example.com/p",
			QuotedPassage:  "p text",
			ShouldShow:     "show",
			SuggestedAlt:   "alt",
			InsertionHint:  "after p",
			Priority:       analyzer.PriorityLarge,
			PriorityReason: "ships in API",
		}},
		PossiblyCovered: []analyzer.ScreenshotGap{{
			PageURL:        "https://example.com/q",
			QuotedPassage:  "q text",
			ShouldShow:     "show q",
			InsertionHint:  "after q",
			Priority:       analyzer.PriorityMedium,
			PriorityReason: "may be covered",
		}},
		ImageIssues: []analyzer.ImageIssue{{
			PageURL:         "https://example.com/p",
			Index:           "img-1",
			Src:             "/img/p.png",
			Reason:          "wrong UI",
			SuggestedAction: "replace",
			Priority:        analyzer.PrioritySmall,
			PriorityReason:  "minor",
		}},
		AuditStats: []analyzer.ScreenshotPageStats{{
			PageURL:       "https://example.com/p",
			VisionEnabled: true,
			ImageIssues:   1,
		}},
	}

	dirA := t.TempDir()
	require.NoError(t, reporter.WriteScreenshots(dirA, res))
	wantBytes, err := os.ReadFile(filepath.Join(dirA, "screenshots.md"))
	require.NoError(t, err)

	dirB := t.TempDir()
	w := reporter.NewScreenshotsWriter(dirB, time.Millisecond)
	w.Push(res)
	require.NoError(t, w.Close())
	gotBytes, err := os.ReadFile(filepath.Join(dirB, "screenshots.md"))
	require.NoError(t, err)

	assert.Equal(t, string(wantBytes), string(gotBytes))
}

func TestScreenshotsWriter_closeIdempotent(t *testing.T) {
	dir := t.TempDir()
	w := reporter.NewScreenshotsWriter(dir, time.Millisecond)
	require.NoError(t, w.Close())
	require.NoError(t, w.Close())
}

// TestScreenshotsWriter_closeReportsFlushError pins that disk failures during
// the final flush surface to the caller. analyze.go relies on this to fail
// the run loudly instead of silently leaving a stale screenshots.md.
func TestScreenshotsWriter_closeReportsFlushError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing-subdir")
	// Directory does not exist; os.WriteFile to <dir>/screenshots.md.tmp will fail.
	w := reporter.NewScreenshotsWriter(dir, time.Millisecond)
	w.Push(makeScreenshotResult(1))
	err := w.Close()
	require.Error(t, err)
	assert.True(t,
		errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no such file"),
		"expected a path-not-found error, got %v", err)
}
