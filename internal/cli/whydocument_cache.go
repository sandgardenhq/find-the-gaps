package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"sort"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

// whyDocumentCacheFile pairs each rationale with a content hash of its
// source feature so a feature whose description changed misses the cache
// while unchanged features short-circuit the LLM call. Entries for
// features no longer in the input set are dropped on save.
type whyDocumentCacheFile struct {
	Entries []whyDocumentCacheEntry `json:"entries"`
}

type whyDocumentCacheEntry struct {
	Name      string `json:"name"`
	Hash      string `json:"hash"`
	Rationale string `json:"rationale"`
}

func whyDocumentInputHash(f analyzer.CodeFeature) string {
	h := sha256.Sum256([]byte(f.Name + "\x00" + f.Description + "\x00" + f.Layer))
	return hex.EncodeToString(h[:])
}

// loadWhyDocumentCache reads cached rationales from path. Returns an empty
// map when the file is missing, malformed, or unreadable — callers treat a
// load failure as a cold cache.
func loadWhyDocumentCache(path string) map[string]whyDocumentCacheEntry {
	out := map[string]whyDocumentCacheEntry{}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return out
	}
	if err != nil {
		return out
	}
	var cache whyDocumentCacheFile
	if err := json.Unmarshal(data, &cache); err != nil {
		return out
	}
	for _, e := range cache.Entries {
		if e.Name == "" {
			continue
		}
		out[e.Name] = e
	}
	return out
}

// saveWhyDocumentCache writes entries to path keyed by feature name with a
// stable sort so the file diffs cleanly between runs.
func saveWhyDocumentCache(path string, entries map[string]whyDocumentCacheEntry) error {
	names := make([]string, 0, len(entries))
	for k := range entries {
		names = append(names, k)
	}
	sort.Strings(names)
	out := whyDocumentCacheFile{Entries: make([]whyDocumentCacheEntry, 0, len(names))}
	for _, n := range names {
		out.Entries = append(out.Entries, entries[n])
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
