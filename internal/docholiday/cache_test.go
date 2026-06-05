package docholiday

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func readFileExists(p string) (struct{}, error) {
	_, err := os.Stat(p)
	return struct{}{}, err
}

func samplePrompt() Prompt {
	return Prompt{Category: CategoryStale, Heading: "docs/a.md", Note: "n", Priority: analyzer.PriorityLarge, Body: "do the thing"}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	c := newCache(map[string]Prompt{"k1": samplePrompt()})
	if err := c.save(dir); err != nil {
		t.Fatal(err)
	}
	got, ok := loadCache(dir)
	if !ok {
		t.Fatal("expected cache file to load")
	}
	if got.entries["k1"].Body != "do the thing" {
		t.Fatalf("round-trip lost body: %+v", got.entries["k1"])
	}
}

func TestLoadMissingFileReturnsFalse(t *testing.T) {
	if _, ok := loadCache(t.TempDir()); ok {
		t.Fatal("loadCache on empty dir should return ok=false")
	}
}

func TestLoadCachedPromptsSortsDeterministically(t *testing.T) {
	dir := t.TempDir()
	c := newCache(map[string]Prompt{
		"k1": {Category: CategoryMissing, Heading: "New page: Z", Priority: analyzer.PriorityLarge, Body: "b"},
		"k2": {Category: CategoryStale, Heading: "docs/a.md", Priority: analyzer.PrioritySmall, Body: "b"},
		"k3": {Category: CategoryStale, Heading: "docs/a.md", Priority: analyzer.PriorityLarge, Body: "b"},
	})
	if err := c.save(dir); err != nil {
		t.Fatal(err)
	}
	prompts, ok := LoadCachedPrompts(dir)
	if !ok || len(prompts) != 3 {
		t.Fatalf("want 3 prompts, ok=%v n=%d", ok, len(prompts))
	}
	// stale before missing; within stale, large before small
	if prompts[0].Category != CategoryStale || prompts[0].Priority != analyzer.PriorityLarge {
		t.Fatalf("sort order wrong: %+v", prompts[0])
	}
	if prompts[2].Category != CategoryMissing {
		t.Fatalf("missing should sort last: %+v", prompts[2])
	}
}

func TestSortPromptsTotalOrderTiebreaksOnBody(t *testing.T) {
	// Two prompts identical in category+priority+heading, differing only in
	// Body. SortPrompts must order them deterministically by Body so parallel
	// and serial dispatch yield byte-identical output.
	a := Prompt{Category: CategoryStale, Heading: "docs/a.md", Priority: analyzer.PriorityLarge, Body: "aaa"}
	b := Prompt{Category: CategoryStale, Heading: "docs/a.md", Priority: analyzer.PriorityLarge, Body: "bbb"}

	for _, start := range [][]Prompt{{a, b}, {b, a}} {
		ps := append([]Prompt(nil), start...)
		SortPrompts(ps)
		if ps[0].Body != "aaa" || ps[1].Body != "bbb" {
			t.Fatalf("want Body-ordered (aaa,bbb); got (%q,%q)", ps[0].Body, ps[1].Body)
		}
	}
}

func TestCacheFileWrittenAtExpectedPath(t *testing.T) {
	dir := t.TempDir()
	if err := newCache(map[string]Prompt{"k": samplePrompt()}).save(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCache(dir); !true {
		_ = err
	}
	if _, err := readFileExists(filepath.Join(dir, cacheFileName)); err != nil {
		t.Fatalf("expected %s on disk: %v", cacheFileName, err)
	}
}
