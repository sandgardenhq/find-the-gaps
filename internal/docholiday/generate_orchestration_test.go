package docholiday

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// countingCompleter returns a deterministic reply and counts calls.
type countingCompleter struct{ calls atomic.Int64 }

func (c *countingCompleter) Complete(_ context.Context, prompt string) (string, error) {
	c.calls.Add(1)
	return "PROMPT BODY", nil
}

// sampleInput yields 4 stale pages (one drift issue each, on distinct pages) +
// 2 undocumented features = 6 work-units. The breadth gives the warm-cache race
// test a meaningful concurrency window under -race.
func sampleInput() Input {
	return Input{
		Drift: []analyzer.DriftFinding{
			{Feature: "Auth", Issues: []analyzer.DriftIssue{
				{Page: "docs/a.md", Issue: "stale a", Priority: analyzer.PriorityLarge, PriorityReason: "r"},
			}},
			{Feature: "Billing", Issues: []analyzer.DriftIssue{
				{Page: "docs/b.md", Issue: "stale b", Priority: analyzer.PriorityMedium, PriorityReason: "r"},
			}},
			{Feature: "Search", Issues: []analyzer.DriftIssue{
				{Page: "docs/c.md", Issue: "stale c", Priority: analyzer.PrioritySmall, PriorityReason: "r"},
			}},
			{Feature: "Export", Issues: []analyzer.DriftIssue{
				{Page: "docs/d.md", Issue: "stale d", Priority: analyzer.PriorityLarge, PriorityReason: "r"},
			}},
		},
		Undocumented: []analyzer.FeatureEntry{
			{Feature: analyzer.CodeFeature{Name: "Frob"}, Files: []string{"f.go"}},
			{Feature: analyzer.CodeFeature{Name: "Baz"}, Files: []string{"g.go"}},
		},
	}
}

// sampleInputUnitCount is the number of work-units sampleInput produces:
// 4 stale pages + 2 undocumented features.
const sampleInputUnitCount = 6

func TestGeneratePromptsProducesOnePerUnit(t *testing.T) {
	cc := &countingCompleter{}
	got, err := GeneratePrompts(context.Background(), cc, sampleInput(), Options{ProjectDir: t.TempDir(), Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != sampleInputUnitCount {
		t.Fatalf("want %d prompts, got %d", sampleInputUnitCount, len(got))
	}
	if cc.calls.Load() != int64(sampleInputUnitCount) {
		t.Fatalf("want %d LLM calls, got %d", sampleInputUnitCount, cc.calls.Load())
	}
}

func TestGeneratePromptsSecondRunHitsCache(t *testing.T) {
	dir := t.TempDir()
	cc := &countingCompleter{}
	if _, err := GeneratePrompts(context.Background(), cc, sampleInput(), Options{ProjectDir: dir, Workers: 4}); err != nil {
		t.Fatal(err)
	}
	first := cc.calls.Load()
	if _, err := GeneratePrompts(context.Background(), cc, sampleInput(), Options{ProjectDir: dir, Workers: 4}); err != nil {
		t.Fatal(err)
	}
	if cc.calls.Load() != first {
		t.Fatalf("warm re-run should make 0 new calls; was %d now %d", first, cc.calls.Load())
	}
}

func TestGeneratePromptsNoCacheForcesRegen(t *testing.T) {
	dir := t.TempDir()
	cc := &countingCompleter{}
	in := sampleInput()
	_, _ = GeneratePrompts(context.Background(), cc, in, Options{ProjectDir: dir, Workers: 4})
	before := cc.calls.Load()
	_, _ = GeneratePrompts(context.Background(), cc, in, Options{ProjectDir: dir, Workers: 4, NoCache: true})
	if cc.calls.Load() <= before {
		t.Fatal("NoCache must force regeneration")
	}
}

// firstFailsCompleter errors on its first Complete call and returns a normal
// body on every subsequent call.
type firstFailsCompleter struct{ calls atomic.Int64 }

func (c *firstFailsCompleter) Complete(_ context.Context, _ string) (string, error) {
	if c.calls.Add(1) == 1 {
		return "", errors.New("transient LLM failure")
	}
	return "PROMPT BODY", nil
}

func TestGeneratePromptsSkipsFailedUnit(t *testing.T) {
	dir := t.TempDir()
	cc := &firstFailsCompleter{}
	// Workers:1 makes dispatch order deterministic (units order, stale first),
	// but the assertions hold regardless of which unit fails first.
	got, err := GeneratePrompts(context.Background(), cc, sampleInput(), Options{ProjectDir: dir, Workers: 1})
	if err != nil {
		t.Fatalf("a single failed unit must not abort the phase: %v", err)
	}
	if len(got) != sampleInputUnitCount-1 {
		t.Fatalf("want %d prompts (one unit skipped), got %d", sampleInputUnitCount-1, len(got))
	}
	c, ok := loadCache(dir)
	if !ok {
		t.Fatal("expected a cache file with the succeeded units")
	}
	if len(c.entries) != sampleInputUnitCount-1 {
		t.Fatalf("failed unit must NOT be cached: want %d entries, got %d", sampleInputUnitCount-1, len(c.entries))
	}
}

func TestGeneratePromptsResultIsSorted(t *testing.T) {
	cc := &countingCompleter{}
	got, _ := GeneratePrompts(context.Background(), cc, sampleInput(), Options{ProjectDir: t.TempDir(), Workers: 1})
	// All stale units sort before all missing units.
	if got[0].Category != CategoryStale || got[len(got)-1].Category != CategoryMissing {
		t.Fatalf("results not sorted stale-before-missing: %+v", got)
	}
	seenMissing := false
	for _, p := range got {
		if p.Category == CategoryMissing {
			seenMissing = true
		} else if seenMissing {
			t.Fatalf("a stale prompt appeared after a missing prompt: %+v", got)
		}
	}
}
