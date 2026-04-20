package spider

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
}

type indexJSON struct {
	Pages          map[string]indexEntry `json:"pages"`
	ProductSummary string                `json:"product_summary,omitempty"`
	AllFeatures    []string              `json:"all_features,omitempty"`
}

// Index is an in-memory view of index.json backed by a cache directory.
type Index struct {
	dir            string
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
	_, ok := idx.entries[rawURL]
	return ok
}

// Record adds rawURL to the index with the given filename and saves index.json.
// It preserves any existing Summary and Features for the URL.
func (idx *Index) Record(rawURL, filename string) error {
	e := idx.entries[rawURL]
	e.Filename = filename
	e.FetchedAt = time.Now()
	idx.entries[rawURL] = e
	return idx.save()
}

// RecordAnalysis stores the LLM-produced summary and features for rawURL.
func (idx *Index) RecordAnalysis(rawURL, summary string, features []string) error {
	e := idx.entries[rawURL]
	e.Summary = summary
	e.Features = features
	idx.entries[rawURL] = e
	return idx.save()
}

// Analysis returns the cached summary and features for rawURL, if present.
func (idx *Index) Analysis(rawURL string) (summary string, features []string, ok bool) {
	e, found := idx.entries[rawURL]
	if !found || e.Summary == "" {
		return "", nil, false
	}
	return e.Summary, e.Features, true
}

// SetProductSummary stores the product-level summary and feature list.
func (idx *Index) SetProductSummary(description string, features []string) error {
	idx.ProductSummary = description
	idx.AllFeatures = features
	return idx.save()
}

// FilePath returns the absolute cache file path for rawURL, if present.
func (idx *Index) FilePath(rawURL string) (string, bool) {
	e, ok := idx.entries[rawURL]
	if !ok {
		return "", false
	}
	return filepath.Join(idx.dir, e.Filename), true
}

// All returns a map of every cached URL to its absolute file path.
func (idx *Index) All() map[string]string {
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
