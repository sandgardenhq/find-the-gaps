package docholiday

import (
	"context"
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

func sampleInput() Input {
	return Input{
		Drift: []analyzer.DriftFinding{{Feature: "Auth", Issues: []analyzer.DriftIssue{
			{Page: "docs/a.md", Issue: "stale", Priority: analyzer.PriorityLarge, PriorityReason: "r"},
		}}},
		Undocumented: []analyzer.FeatureEntry{{Feature: analyzer.CodeFeature{Name: "Frob"}, Files: []string{"f.go"}}},
	}
}

func TestGeneratePromptsProducesOnePerUnit(t *testing.T) {
	cc := &countingCompleter{}
	got, err := GeneratePrompts(context.Background(), cc, sampleInput(), Options{ProjectDir: t.TempDir(), Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 { // one stale page + one missing feature
		t.Fatalf("want 2 prompts, got %d", len(got))
	}
	if cc.calls.Load() != 2 {
		t.Fatalf("want 2 LLM calls, got %d", cc.calls.Load())
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

func TestGeneratePromptsResultIsSorted(t *testing.T) {
	cc := &countingCompleter{}
	got, _ := GeneratePrompts(context.Background(), cc, sampleInput(), Options{ProjectDir: t.TempDir(), Workers: 1})
	if got[0].Category != CategoryStale || got[1].Category != CategoryMissing {
		t.Fatalf("results not sorted stale-before-missing: %+v", got)
	}
}
