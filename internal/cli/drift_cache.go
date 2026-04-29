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
		issues := e.Issues
		if issues == nil {
			issues = []analyzer.DriftIssue{}
		}
		out[e.Feature] = analyzer.CachedDriftEntry{
			Files:  e.Files,
			Pages:  e.Pages,
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
