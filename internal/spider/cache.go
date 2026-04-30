package spider

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// URLToFilename returns a stable, collision-resistant filename for rawURL.
func URLToFilename(rawURL string) string {
	sum := sha256.Sum256([]byte(rawURL))
	return fmt.Sprintf("%x.md", sum)
}

type indexEntry struct {
	Filename  string    `json:"filename"`
	FetchedAt time.Time `json:"fetched_at"`
	Summary   string    `json:"summary,omitempty"`
	Features  []string  `json:"features,omitempty"`
	// IsDocs is a pointer so old cache files (no field) round-trip with nil.
	// Nil on disk ↔ true in memory: the inclusive-by-default safety net for
	// legacy caches written before the docs classifier shipped. See
	// .plans/DOCS_CLASSIFIER_DESIGN.md "Edge cases".
	IsDocs *bool `json:"is_docs,omitempty"`
}

type indexJSON struct {
	Pages          map[string]indexEntry `json:"pages"`
	ProductSummary string                `json:"product_summary,omitempty"`
	AllFeatures    []string              `json:"all_features,omitempty"`
}

// Index is an in-memory view of index.json backed by a cache directory.
// All public methods are safe for concurrent use.
type Index struct {
	dir            string
	mu             sync.Mutex
	entries        map[string]indexEntry
	ProductSummary string
	AllFeatures    []string
}

// LoadIndex reads index.json from dir, or returns an empty index if the file
// does not exist. It creates dir if it does not exist.
func LoadIndex(dir string) (*Index, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	idx := &Index{dir: dir, entries: make(map[string]indexEntry)}
	data, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if errors.Is(err, os.ErrNotExist) {
		return idx, nil
	}
	if err != nil {
		return nil, err
	}
	var raw indexJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if raw.Pages != nil {
		idx.entries = raw.Pages
	}
	idx.ProductSummary = raw.ProductSummary
	idx.AllFeatures = raw.AllFeatures
	return idx, nil
}

// Has reports whether rawURL is already recorded in the index.
func (idx *Index) Has(rawURL string) bool {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	_, ok := idx.entries[rawURL]
	return ok
}

// Record adds rawURL to the index with the given filename and saves index.json.
// It preserves any existing Summary and Features for the URL.
func (idx *Index) Record(rawURL, filename string) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	e := idx.entries[rawURL]
	e.Filename = filename
	e.FetchedAt = time.Now()
	idx.entries[rawURL] = e
	return idx.save()
}

// RecordAnalysis stores the LLM-produced summary, features, and docs
// classification for rawURL. isDocs is recorded explicitly (even when false) so
// nil remains reserved for "never written" — see Analysis for the legacy-cache
// safety net.
func (idx *Index) RecordAnalysis(rawURL, summary string, features []string, isDocs bool) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	e := idx.entries[rawURL]
	e.Summary = summary
	e.Features = features
	e.IsDocs = &isDocs
	idx.entries[rawURL] = e
	return idx.save()
}

// Analysis returns the cached summary, features, and docs classification for
// rawURL, if present. When the on-disk entry has no is_docs field (a cache
// written before the classifier shipped), isDocs is reported as true — the
// inclusive-by-default safety net that prevents a legacy cache from silently
// dropping every page out of the report.
func (idx *Index) Analysis(rawURL string) (summary string, features []string, isDocs bool, ok bool) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	e, found := idx.entries[rawURL]
	if !found || e.Summary == "" {
		return "", nil, false, false
	}
	is := true // inclusive-by-default for old cache entries (nil on disk)
	if e.IsDocs != nil {
		is = *e.IsDocs
	}
	return e.Summary, e.Features, is, true
}

// SetProductSummary stores the product-level summary and feature list.
func (idx *Index) SetProductSummary(description string, features []string) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.ProductSummary = description
	idx.AllFeatures = features
	return idx.save()
}

// ProductInfo returns the recorded product summary and feature list.
func (idx *Index) ProductInfo() (string, []string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	return idx.ProductSummary, idx.AllFeatures
}

// FilePath returns the absolute cache file path for rawURL, if present.
func (idx *Index) FilePath(rawURL string) (string, bool) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	e, ok := idx.entries[rawURL]
	if !ok {
		return "", false
	}
	return filepath.Join(idx.dir, e.Filename), true
}

// All returns a map of every cached URL to its absolute file path.
func (idx *Index) All() map[string]string {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	out := make(map[string]string, len(idx.entries))
	for u, e := range idx.entries {
		out[u] = filepath.Join(idx.dir, e.Filename)
	}
	return out
}

func (idx *Index) save() error {
	raw := indexJSON{
		Pages:          idx.entries,
		ProductSummary: idx.ProductSummary,
		AllFeatures:    idx.AllFeatures,
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(idx.dir, "index.json"), data, 0o644)
}
