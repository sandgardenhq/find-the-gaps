package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// screenshotsCacheFile is the on-disk shape of <projectDir>/screenshots.json.
// The Pages list mirrors the sorted entry URLs for quick inspection; lookup
// itself is per-entry against Entries (keyed by URL+ContentHash).
type screenshotsCacheFile struct {
	Pages    []string                `json:"pages"`
	Entries  []screenshotsCacheEntry `json:"entries"`
	Complete *screenshotsComplete    `json:"complete,omitempty"`
}

// screenshotsCacheEntry is one cached page result. The composite key is
// URL+ContentHash so a docs page whose content has changed produces a fresh
// entry rather than reusing a stale one.
type screenshotsCacheEntry struct {
	URL         string                       `json:"url"`
	ContentHash string                       `json:"contentHash"`
	Stats       analyzer.ScreenshotPageStats `json:"stats"`
	Missing     []analyzer.ScreenshotGap     `json:"missing"`
	Possibly    []analyzer.ScreenshotGap     `json:"possiblyCovered"`
	ImageIssues []analyzer.ImageIssue        `json:"imageIssues"`
}

// screenshotsComplete is the completion sentinel written by
// saveScreenshotsCacheComplete. Its Hash is the SHA-256 of the screenshot
// inputs; on re-run, callers can short-circuit DetectScreenshotGaps when the
// freshly computed hash matches.
type screenshotsComplete struct {
	Hash        string    `json:"hash"`
	CompletedAt time.Time `json:"completedAt"`
}

// screenshotsCacheKey returns the composite map key for a screenshots cache
// entry. The pipe separator is illegal in URLs and hex hashes so the
// concatenation is unambiguous.
func screenshotsCacheKey(url, contentHash string) string {
	return url + "|" + contentHash
}

// loadScreenshotsCacheFile returns the full screenshotsCacheFile (entries +
// sentinel). Returns (zero, false) on missing file, parse error, or any I/O
// error — callers proceed cold on miss.
func loadScreenshotsCacheFile(path string) (screenshotsCacheFile, bool) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return screenshotsCacheFile{}, false
	}
	if err != nil {
		return screenshotsCacheFile{}, false
	}
	var f screenshotsCacheFile
	if err := json.Unmarshal(data, &f); err != nil {
		return screenshotsCacheFile{}, false
	}
	return f, true
}

// loadScreenshotsCache reads the screenshots cache from path and returns a
// map keyed by URL+ContentHash. Returns (nil, false) on missing file, parse
// error, or any I/O error.
func loadScreenshotsCache(path string) (map[string]screenshotsCacheEntry, bool) {
	f, ok := loadScreenshotsCacheFile(path)
	if !ok {
		return nil, false
	}
	return screenshotsCacheEntriesToMap(f.Entries), true
}

// screenshotsCacheEntriesToMap converts the on-disk slice form back to a map
// keyed by URL+ContentHash. Mirrors the nil-slice normalization that
// loadScreenshotsCache performs so callers see identical entry shapes
// regardless of which loader produced them.
func screenshotsCacheEntriesToMap(entries []screenshotsCacheEntry) map[string]screenshotsCacheEntry {
	m := make(map[string]screenshotsCacheEntry, len(entries))
	for _, e := range entries {
		missing := e.Missing
		if missing == nil {
			missing = []analyzer.ScreenshotGap{}
		}
		possibly := e.Possibly
		if possibly == nil {
			possibly = []analyzer.ScreenshotGap{}
		}
		issues := e.ImageIssues
		if issues == nil {
			issues = []analyzer.ImageIssue{}
		}
		m[screenshotsCacheKey(e.URL, e.ContentHash)] = screenshotsCacheEntry{
			URL:         e.URL,
			ContentHash: e.ContentHash,
			Stats:       e.Stats,
			Missing:     missing,
			Possibly:    possibly,
			ImageIssues: issues,
		}
	}
	return m
}

// saveScreenshotsCache writes current to path atomically (temp-file + rename)
// without a completion sentinel. Entries are sorted by URL for stable diffs.
// Mirrors drift_cache.go's saveDriftCache shape so a maintainer who knows one
// API knows the other.
func saveScreenshotsCache(path string, current map[string]screenshotsCacheEntry) error {
	return saveScreenshotsCacheComplete(path, current, nil)
}

// saveScreenshotsCacheComplete writes the cache atomically with a completion
// sentinel. Pass nil to write without one.
func saveScreenshotsCacheComplete(path string, current map[string]screenshotsCacheEntry, complete *screenshotsComplete) error {
	keys := make([]string, 0, len(current))
	for k := range current {
		keys = append(keys, k)
	}
	// Sort by the entry's URL so the on-disk layout matches the Pages list.
	sort.Slice(keys, func(i, j int) bool {
		return current[keys[i]].URL < current[keys[j]].URL
	})

	urls := make([]string, 0, len(keys))
	entries := make([]screenshotsCacheEntry, 0, len(keys))
	for _, k := range keys {
		c := current[k]
		missing := c.Missing
		if missing == nil {
			missing = []analyzer.ScreenshotGap{}
		}
		possibly := c.Possibly
		if possibly == nil {
			possibly = []analyzer.ScreenshotGap{}
		}
		issues := c.ImageIssues
		if issues == nil {
			issues = []analyzer.ImageIssue{}
		}
		urls = append(urls, c.URL)
		entries = append(entries, screenshotsCacheEntry{
			URL:         c.URL,
			ContentHash: c.ContentHash,
			Stats:       c.Stats,
			Missing:     missing,
			Possibly:    possibly,
			ImageIssues: issues,
		})
	}
	f := screenshotsCacheFile{Pages: urls, Entries: entries, Complete: complete}

	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".screenshots-*.tmp")
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
	// files (drift.json, featuremap.json, codefeatures.json) which all use 0o644.
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

// computeScreenshotsInputHash returns a hex SHA-256 over the inputs the
// screenshot pass consumes from upstream (page URLs + content hash + small-tier
// model identity). It is independent of slice iteration order — pages are
// sorted by URL before hashing. The model identity is folded in so a model
// change forces a re-run (different vision behavior produces different audit
// stats and findings).
func computeScreenshotsInputHash(docPages []analyzer.DocPage, llmSmall string) string {
	type pEntry struct {
		URL  string `json:"url"`
		Hash string `json:"hash"`
	}
	type payload struct {
		Pages []pEntry `json:"pages"`
		Small string   `json:"small"`
	}

	entries := make([]pEntry, 0, len(docPages))
	for _, p := range docPages {
		sum := sha256.Sum256([]byte(p.Content))
		entries = append(entries, pEntry{URL: p.URL, Hash: hex.EncodeToString(sum[:])})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].URL < entries[j].URL })

	data, _ := json.Marshal(payload{Pages: entries, Small: llmSmall})
	out := sha256.Sum256(data)
	return hex.EncodeToString(out[:])
}

// screenshotsCachedFromCli adapts the cli-side cache map (loaded from disk)
// into the analyzer-facing ScreenshotsCachedPage map keyed by URL+ContentHash.
// Used at the call boundary so DetectScreenshotGaps does not depend on cli
// types.
func screenshotsCachedFromCli(in map[string]screenshotsCacheEntry) map[string]analyzer.ScreenshotsCachedPage {
	out := make(map[string]analyzer.ScreenshotsCachedPage, len(in))
	for k, v := range in {
		out[k] = analyzer.ScreenshotsCachedPage{
			URL:         v.URL,
			ContentHash: v.ContentHash,
			Stats:       v.Stats,
			Missing:     v.Missing,
			Possibly:    v.Possibly,
			ImageIssues: v.ImageIssues,
		}
	}
	return out
}

// screenshotsCacheEntryFromAnalyzer adapts a ScreenshotsCachedPage emitted by
// the analyzer's per-page onPageDone callback into the cli's on-disk shape.
// Mirrors screenshotsCachedFromCli in the opposite direction.
func screenshotsCacheEntryFromAnalyzer(in analyzer.ScreenshotsCachedPage) screenshotsCacheEntry {
	return screenshotsCacheEntry{
		URL:         in.URL,
		ContentHash: in.ContentHash,
		Stats:       in.Stats,
		Missing:     in.Missing,
		Possibly:    in.Possibly,
		ImageIssues: in.ImageIssues,
	}
}

// screenshotResultFromCache rebuilds an analyzer.ScreenshotResult from a
// cache map for the cache-skipped (sentinel-matched) path. The returned
// AuditStats are sorted in docPages order so subsequent reporting matches
// what a live run would have produced.
func screenshotResultFromCache(cache map[string]screenshotsCacheEntry, docPages []analyzer.DocPage) analyzer.ScreenshotResult {
	var res analyzer.ScreenshotResult
	for _, p := range docPages {
		sum := sha256.Sum256([]byte(p.Content))
		hash := hex.EncodeToString(sum[:])
		c, ok := cache[screenshotsCacheKey(p.URL, hash)]
		if !ok {
			continue
		}
		res.MissingGaps = append(res.MissingGaps, c.Missing...)
		res.PossiblyCovered = append(res.PossiblyCovered, c.Possibly...)
		res.ImageIssues = append(res.ImageIssues, c.ImageIssues...)
		res.AuditStats = append(res.AuditStats, c.Stats)
	}
	return res
}

// newScreenshotsCachePersister returns a per-page persister used during
// screenshot detection. Each fresh page result is written into live and the
// cache file is saved atomically.
//
// The closure captures a sync.Mutex so parallel screenshot workers can call
// the returned function concurrently without racing on live or the on-disk
// save.
func newScreenshotsCachePersister(live map[string]screenshotsCacheEntry, path string) func(entry screenshotsCacheEntry) error {
	var mu sync.Mutex
	return func(entry screenshotsCacheEntry) error {
		mu.Lock()
		defer mu.Unlock()
		live[screenshotsCacheKey(entry.URL, entry.ContentHash)] = entry
		return saveScreenshotsCache(path, live)
	}
}
