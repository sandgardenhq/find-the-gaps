package docholiday

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/charmbracelet/log"
	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

const cacheFileName = "prompts-cache.json"

type cacheEntry struct {
	Key      string            `json:"key"`
	Category Category          `json:"category"`
	Heading  string            `json:"heading"`
	Note     string            `json:"note"`
	Priority analyzer.Priority `json:"priority"`
	Body     string            `json:"body"`
}

type cacheDoc struct {
	Entries []cacheEntry `json:"entries"`
}

// cache is the in-memory live view persisted to prompts-cache.json. It is safe
// for concurrent put() from worker goroutines.
type cache struct {
	mu      sync.Mutex
	entries map[string]Prompt
}

func newCache(seed map[string]Prompt) *cache {
	m := map[string]Prompt{}
	for k, v := range seed {
		m[k] = v
	}
	return &cache{entries: m}
}

func (c *cache) get(key string) (Prompt, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.entries[key]
	return p, ok
}

// put records a freshly generated prompt and atomically re-flushes the whole
// file so a SIGINT mid-run leaves a valid partial cache.
func (c *cache) put(dir, key string, p Prompt) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = p
	return c.flushLocked(dir)
}

// save atomically writes the current cache to dir. Safe for concurrent callers.
func (c *cache) save(dir string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.flushLocked(dir)
}

// flushLocked snapshots the map and atomically writes it via temp+rename. The
// caller MUST hold c.mu for the full duration: the snapshot and the rename have
// to be serialized together, otherwise an older snapshot's rename can land
// after (and clobber) a newer snapshot's rename, dropping just-cached entries.
func (c *cache) flushLocked(dir string) error {
	keys := make([]string, 0, len(c.entries))
	for k := range c.entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	doc := cacheDoc{Entries: make([]cacheEntry, 0, len(keys))}
	for _, k := range keys {
		p := c.entries[k]
		doc.Entries = append(doc.Entries, cacheEntry{
			Key: k, Category: p.Category, Heading: p.Heading,
			Note: p.Note, Priority: p.Priority, Body: p.Body,
		})
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".prompts-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, filepath.Join(dir, cacheFileName)); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// loadCache reads prompts-cache.json from dir. ok=false when the file is
// absent or unreadable (treated as a cold cache, never an error).
func loadCache(dir string) (*cache, bool) {
	data, err := os.ReadFile(filepath.Join(dir, cacheFileName))
	if err != nil {
		return nil, false
	}
	var doc cacheDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		// The file exists but is corrupt. Warn so the user isn't silently left
		// without prompts.md on render; a missing file (handled above) is the
		// normal cold path and stays quiet.
		log.Warnf("ignoring corrupt %s: %v", cacheFileName, err)
		return nil, false
	}
	m := make(map[string]Prompt, len(doc.Entries))
	for _, e := range doc.Entries {
		m[e.Key] = Prompt{Category: e.Category, Heading: e.Heading, Note: e.Note, Priority: e.Priority, Body: e.Body}
	}
	return &cache{entries: m}, true
}

// LoadCachedPrompts returns the cached prompts for a project dir, sorted, for
// `ftg render` to re-emit prompts.md with no LLM calls. ok=false when no cache
// exists (the analyze phase never ran).
func LoadCachedPrompts(dir string) ([]Prompt, bool) {
	c, ok := loadCache(dir)
	if !ok {
		return nil, false
	}
	out := make([]Prompt, 0, len(c.entries))
	for _, p := range c.entries {
		out = append(out, p)
	}
	SortPrompts(out)
	return out, true
}

// SortPrompts orders prompts deterministically: stale before missing, then
// Large→Medium→Small, then by heading.
func SortPrompts(ps []Prompt) {
	catRank := func(c Category) int {
		if c == CategoryMissing {
			return 1
		}
		return 0
	}
	sort.SliceStable(ps, func(i, j int) bool {
		if r := catRank(ps[i].Category) - catRank(ps[j].Category); r != 0 {
			return r < 0
		}
		if r := priorityRank(ps[i].Priority) - priorityRank(ps[j].Priority); r != 0 {
			return r < 0
		}
		if ps[i].Heading != ps[j].Heading {
			return ps[i].Heading < ps[j].Heading
		}
		// Final tiebreaker so equal category+priority+heading prompts have a
		// total order; otherwise nondeterministic parallel completion order
		// could reorder them between --workers=8 and --workers=1 runs.
		return ps[i].Body < ps[j].Body
	})
}
