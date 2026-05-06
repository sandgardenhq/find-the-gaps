package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// driftCacheFile is the on-disk shape of <projectDir>/drift.json. The Features
// list mirrors the sorted entry keys for quick inspection; lookup itself is
// per-feature against Entries.
type driftCacheFile struct {
	Features []string          `json:"features"`
	Entries  []driftCacheEntry `json:"entries"`
	Complete *driftComplete    `json:"complete,omitempty"`
}

type driftCacheEntry struct {
	Feature       string                `json:"feature"`
	Files         []string              `json:"files"`
	FilteredPages []string              `json:"filteredPages,omitempty"`
	Pages         []string              `json:"pages"`
	Issues        []analyzer.DriftIssue `json:"issues"`
}

// driftComplete is the completion sentinel written by saveDriftCacheComplete.
// Its Hash is the SHA-256 of the drift inputs (computeDriftInputHash); on
// re-run, callers can short-circuit DetectDrift when the freshly computed
// hash matches.
type driftComplete struct {
	Hash        string    `json:"hash"`
	CompletedAt time.Time `json:"completedAt"`
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
		// FilteredPages intentionally NOT nil-normalized: old caches without
		// the field must stay nil so the cache-key check misses on the next
		// run and the entry recomputes once with FilteredPages populated.
		out[e.Feature] = analyzer.CachedDriftEntry{
			Files:         files,
			FilteredPages: e.FilteredPages,
			Pages:         pages,
			Issues:        issues,
		}
	}
	return out, true
}

// loadDriftCacheFile returns the full driftCacheFile (entries + sentinel).
// Returns (zero, false) on missing file, parse error, or any I/O error.
func loadDriftCacheFile(path string) (driftCacheFile, bool) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return driftCacheFile{}, false
	}
	if err != nil {
		return driftCacheFile{}, false
	}
	var f driftCacheFile
	if err := json.Unmarshal(data, &f); err != nil {
		return driftCacheFile{}, false
	}
	return f, true
}

// saveDriftCache writes current to path atomically (temp-file + rename).
// Entries are sorted by feature name for stable diffs.
func saveDriftCache(path string, current map[string]analyzer.CachedDriftEntry) error {
	return saveDriftCacheComplete(path, current, nil)
}

// saveDriftCacheComplete writes the cache atomically with a completion sentinel.
// Pass nil to write without one (equivalent to saveDriftCache).
func saveDriftCacheComplete(path string, current map[string]analyzer.CachedDriftEntry, complete *driftComplete) error {
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
			Feature:       name,
			Files:         c.Files,
			FilteredPages: c.FilteredPages,
			Pages:         c.Pages,
			Issues:        issues,
		})
	}
	f := driftCacheFile{Features: names, Entries: entries, Complete: complete}

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

// computeDriftInputHash returns a hex SHA-256 of the inputs that the drift
// pass consumes from upstream (featureMap files+symbols, docsMap pages).
// It is independent of map iteration order — entries are sorted by feature
// name and slice contents are sorted before hashing.
func computeDriftInputHash(fm analyzer.FeatureMap, dm analyzer.DocsFeatureMap) string {
	type fEntry struct {
		Name    string   `json:"name"`
		Files   []string `json:"files"`
		Symbols []string `json:"symbols"`
	}
	type dEntry struct {
		Name  string   `json:"name"`
		Pages []string `json:"pages"`
	}
	type payload struct {
		Features []fEntry `json:"features"`
		Docs     []dEntry `json:"docs"`
	}

	feats := make([]fEntry, 0, len(fm))
	for _, e := range fm {
		files := append([]string(nil), e.Files...)
		syms := append([]string(nil), e.Symbols...)
		sort.Strings(files)
		sort.Strings(syms)
		feats = append(feats, fEntry{Name: e.Feature.Name, Files: files, Symbols: syms})
	}
	sort.Slice(feats, func(i, j int) bool { return feats[i].Name < feats[j].Name })

	docs := make([]dEntry, 0, len(dm))
	for _, e := range dm {
		pages := append([]string(nil), e.Pages...)
		sort.Strings(pages)
		docs = append(docs, dEntry{Name: e.Feature, Pages: pages})
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Name < docs[j].Name })

	data, _ := json.Marshal(payload{Features: feats, Docs: docs})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// driftCacheEntriesToMap converts the on-disk slice form back to a map keyed
// by feature name. Mirrors the nil-slice normalization that loadDriftCache
// performs so callers see identical CachedDriftEntry shapes regardless of
// which loader produced them.
func driftCacheEntriesToMap(entries []driftCacheEntry) map[string]analyzer.CachedDriftEntry {
	m := make(map[string]analyzer.CachedDriftEntry, len(entries))
	for _, e := range entries {
		issues := e.Issues
		if issues == nil {
			issues = []analyzer.DriftIssue{}
		}
		files := e.Files
		if files == nil {
			files = []string{}
		}
		pages := e.Pages
		if pages == nil {
			pages = []string{}
		}
		// FilteredPages intentionally NOT nil-normalized: see loadDriftCache.
		m[e.Feature] = analyzer.CachedDriftEntry{
			Files:         files,
			FilteredPages: e.FilteredPages,
			Pages:         pages,
			Issues:        issues,
		}
	}
	return m
}

// driftFindingsFromCache rebuilds DriftFindings from per-feature cache
// entries, restricted to features present in featureMap. Features with
// zero issues do not produce a finding (matches DetectDrift's contract).
// Output is sorted by feature name for stable diffs.
func driftFindingsFromCache(cache map[string]analyzer.CachedDriftEntry, fm analyzer.FeatureMap) []analyzer.DriftFinding {
	if len(cache) == 0 {
		return nil
	}
	names := make([]string, 0, len(fm))
	for _, e := range fm {
		names = append(names, e.Feature.Name)
	}
	sort.Strings(names)
	out := make([]analyzer.DriftFinding, 0)
	for _, name := range names {
		c, ok := cache[name]
		if !ok || len(c.Issues) == 0 {
			continue
		}
		out = append(out, analyzer.DriftFinding{Feature: name, Issues: c.Issues})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
