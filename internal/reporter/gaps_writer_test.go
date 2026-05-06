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
	prefix := "# Gaps Found\n\nstatic body\n"
	w := reporter.NewGapsWriter(dir, prefix, 50*time.Millisecond)

	for i := 0; i < 5; i++ {
		w.Push(makeFinding(fmt.Sprintf("f%d", i)))
	}
	require.NoError(t, w.Close())

	data, err := os.ReadFile(filepath.Join(dir, "gaps.md"))
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "f4")
	assert.Contains(t, content, "static body")
	assert.Contains(t, content, "## Stale Documentation")
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
	observerWG.Add(1)
	go func() {
		defer observerWG.Done()
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
	}()

	for i := 0; i < 50; i++ {
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
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			w.Push(makeFinding(fmt.Sprintf("f%02d", n)))
		}(i)
	}
	wg.Wait()
	require.NoError(t, w.Close())

	data, err := os.ReadFile(filepath.Join(dir, "gaps.md"))
	require.NoError(t, err)
	content := string(data)

	matched := false
	for i := 0; i < N; i++ {
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
	prefix := reporter.BuildGapsStaticPrefix(mapping, docFeatures)
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
