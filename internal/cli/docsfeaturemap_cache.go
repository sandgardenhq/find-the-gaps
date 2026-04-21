package cli

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

type docsFeatureMapCacheFile struct {
	Features []string                   `json:"features"`
	Entries  []docsFeatureMapCacheEntry `json:"entries"`
}

type docsFeatureMapCacheEntry struct {
	Feature string   `json:"feature"`
	Pages   []string `json:"pages"`
}

// loadDocsFeatureMapCache reads a cached DocsFeatureMap from path.
// Returns false if the file does not exist, cannot be parsed, or wantFeatures
// does not match the features the cache was built from (order-insensitive).
func loadDocsFeatureMapCache(path string, wantFeatures []string) (analyzer.DocsFeatureMap, bool) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false
	}
	if err != nil {
		return nil, false
	}
	var cache docsFeatureMapCacheFile
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, false
	}
	if !featureSetsEqual(cache.Features, wantFeatures) {
		return nil, false
	}
	fm := make(analyzer.DocsFeatureMap, 0, len(cache.Entries))
	for _, e := range cache.Entries {
		pages := e.Pages
		if pages == nil {
			pages = []string{}
		}
		fm = append(fm, analyzer.DocsFeatureEntry{Feature: e.Feature, Pages: pages})
	}
	return fm, true
}

// saveDocsFeatureMapCache writes fm to path as JSON, recording features so that
// a future load can detect stale caches when the feature set changes.
func saveDocsFeatureMapCache(path string, features []string, fm analyzer.DocsFeatureMap) error {
	entries := make([]docsFeatureMapCacheEntry, len(fm))
	for i, e := range fm {
		pages := e.Pages
		if pages == nil {
			pages = []string{}
		}
		entries[i] = docsFeatureMapCacheEntry{Feature: e.Feature, Pages: pages}
	}
	cache := docsFeatureMapCacheFile{Features: features, Entries: entries}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
