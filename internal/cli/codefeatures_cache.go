package cli

import (
	"encoding/json"
	"errors"
	"os"
	"sort"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

type codeFeaturesCacheFile struct {
	Files    []string `json:"files"`
	Features []string `json:"features"`
}

// loadCodeFeaturesCache reads a cached code-features list from path.
// Returns false if the file does not exist, cannot be parsed, or the
// scanned file list has changed since the cache was built.
func loadCodeFeaturesCache(path string, scan *scanner.ProjectScan) ([]string, bool) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false
	}
	if err != nil {
		return nil, false
	}
	var cache codeFeaturesCacheFile
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, false
	}
	// Cache key is the set of scanned file paths. Content changes within existing
	// files do not invalidate the cache; use --no-cache to force re-extraction.
	if !featureSetsEqual(cache.Files, scanFilePaths(scan)) {
		return nil, false
	}
	if cache.Features == nil {
		return []string{}, true
	}
	return cache.Features, true
}

// saveCodeFeaturesCache writes features to path, keyed to the scan's file list.
func saveCodeFeaturesCache(path string, scan *scanner.ProjectScan, features []string) error {
	cache := codeFeaturesCacheFile{
		Files:    scanFilePaths(scan),
		Features: features,
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// scanFilePaths returns a sorted list of file paths from scan.
func scanFilePaths(scan *scanner.ProjectScan) []string {
	paths := make([]string, len(scan.Files))
	for i, f := range scan.Files {
		paths[i] = f.Path
	}
	sort.Strings(paths)
	return paths
}
