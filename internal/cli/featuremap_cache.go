package cli

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"sort"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

type featureMapCacheFile struct {
	Features []string               `json:"features"`
	Entries  []featureMapCacheEntry `json:"entries"`
}

type featureMapCacheEntry struct {
	Feature string   `json:"feature"`
	Files   []string `json:"files"`
	Symbols []string `json:"symbols"`
}

// loadFeatureMapCache reads a cached FeatureMap from path.
// Returns false if the file does not exist, cannot be parsed, or wantFeatures
// does not match the features the cache was built from (order-insensitive).
func loadFeatureMapCache(path string, wantFeatures []string) (analyzer.FeatureMap, bool) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false
	}
	if err != nil {
		return nil, false
	}
	var cache featureMapCacheFile
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, false
	}
	if !featureSetsEqual(cache.Features, wantFeatures) {
		return nil, false
	}
	fm := make(analyzer.FeatureMap, 0, len(cache.Entries))
	for _, e := range cache.Entries {
		files := e.Files
		if files == nil {
			files = []string{}
		}
		symbols := e.Symbols
		if symbols == nil {
			symbols = []string{}
		}
		fm = append(fm, analyzer.FeatureEntry{
			Feature: e.Feature,
			Files:   files,
			Symbols: symbols,
		})
	}
	return fm, true
}

// saveFeatureMapCache writes fm to path as JSON, recording features so that
// a future load can detect stale caches when the feature set changes.
func saveFeatureMapCache(path string, features []string, fm analyzer.FeatureMap) error {
	entries := make([]featureMapCacheEntry, len(fm))
	for i, e := range fm {
		files := e.Files
		if files == nil {
			files = []string{}
		}
		symbols := e.Symbols
		if symbols == nil {
			symbols = []string{}
		}
		entries[i] = featureMapCacheEntry{Feature: e.Feature, Files: files, Symbols: symbols}
	}
	cache := featureMapCacheFile{Features: features, Entries: entries}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// featureSetsEqual reports whether a and b contain the same strings, regardless of order.
func featureSetsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	ac := append([]string(nil), a...)
	bc := append([]string(nil), b...)
	sort.Strings(ac)
	sort.Strings(bc)
	return slices.Equal(ac, bc)
}
