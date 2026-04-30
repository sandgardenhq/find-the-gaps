package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// driftCacheFile is the on-disk shape of <projectDir>/drift.json. The Features
// list mirrors the sorted entry keys for quick inspection; lookup itself is
// per-feature against Entries.
type driftCacheFile struct {
	Features []string          `json:"features"`
	Entries  []driftCacheEntry `json:"entries"`
}

type driftCacheEntry struct {
	Feature string                `json:"feature"`
	Files   []string              `json:"files"`
	Pages   []string              `json:"pages"`
	Issues  []analyzer.DriftIssue `json:"issues"`
}

// seedDriftLiveCache builds the initial liveCache for a drift run: cached
// entries are copied for every feature still present in featureMap, and
// features no longer in featureMap are dropped. This preserves
// not-yet-processed-but-still-valid entries across partial runs (so a
// killed run leaves drift.json with the union of fresh results and prior
// cache values), while still evicting features removed upstream on the
// next save.
func seedDriftLiveCache(cached map[string]analyzer.CachedDriftEntry, featureMap analyzer.FeatureMap) map[string]analyzer.CachedDriftEntry {
	live := make(map[string]analyzer.CachedDriftEntry, len(featureMap))
	if len(cached) == 0 {
		return live
	}
	for _, entry := range featureMap {
		if c, ok := cached[entry.Feature.Name]; ok {
			live[entry.Feature.Name] = c
		}
	}
	return live
}

// isDriftCacheHit reports whether cached has an entry for name whose Files
// and Pages match the given slices exactly. Both inputs and the cached
// entry's slices must be sorted ascending; this is element-wise comparison.
// A nil cached map always returns false.
func isDriftCacheHit(cached map[string]analyzer.CachedDriftEntry, name string, files, pages []string) bool {
	c, ok := cached[name]
	if !ok {
		return false
	}
	return stringSliceEqual(c.Files, files) && stringSliceEqual(c.Pages, pages)
}

// newDriftCachePersister returns the analyzer.DriftFeatureDoneFunc used during
// drift detection. On a cache hit the on-disk drift.json already contains the
// matching entry, so it skips the save entirely; only fresh results trigger a
// write. *hits and *fresh are incremented to track cache effectiveness.
func newDriftCachePersister(
	cached, liveCache map[string]analyzer.CachedDriftEntry,
	driftCachePath string,
	hits, fresh *int,
) analyzer.DriftFeatureDoneFunc {
	return func(name string, files, pages []string, issues []analyzer.DriftIssue) error {
		if isDriftCacheHit(cached, name, files, pages) {
			*hits++
			return nil
		}
		*fresh++
		liveCache[name] = analyzer.CachedDriftEntry{Files: files, Pages: pages, Issues: issues}
		return saveDriftCache(driftCachePath, liveCache)
	}
}

// loadDriftCache reads a drift cache from path. Returns (nil, false) on
// missing file, parse error, or any I/O error — callers proceed cold on miss.
func loadDriftCache(path string) (map[string]analyzer.CachedDriftEntry, bool) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false
	}
	if err != nil {
		return nil, false
	}
	var f driftCacheFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, false
	}
	out := make(map[string]analyzer.CachedDriftEntry, len(f.Entries))
	for _, e := range f.Entries {
		files := e.Files
		if files == nil {
			files = []string{}
		}
		pages := e.Pages
		if pages == nil {
			pages = []string{}
		}
		issues := e.Issues
		if issues == nil {
			issues = []analyzer.DriftIssue{}
		}
		out[e.Feature] = analyzer.CachedDriftEntry{
			Files:  files,
			Pages:  pages,
			Issues: issues,
		}
	}
	return out, true
}

// saveDriftCache writes current to path atomically (temp-file + rename).
// Entries are sorted by feature name for stable diffs.
func saveDriftCache(path string, current map[string]analyzer.CachedDriftEntry) error {
	names := make([]string, 0, len(current))
	for k := range current {
		names = append(names, k)
	}
	sort.Strings(names)

	entries := make([]driftCacheEntry, 0, len(names))
	for _, name := range names {
		c := current[name]
		issues := c.Issues
		if issues == nil {
			issues = []analyzer.DriftIssue{}
		}
		entries = append(entries, driftCacheEntry{
			Feature: name,
			Files:   c.Files,
			Pages:   c.Pages,
			Issues:  issues,
		})
	}
	f := driftCacheFile{Features: names, Entries: entries}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".drift-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	// os.CreateTemp produces 0o600; bring it in line with the other cache
	// files (featuremap.json, codefeatures.json) which all use 0o644.
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}
