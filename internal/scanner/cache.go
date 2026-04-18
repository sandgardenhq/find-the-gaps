package scanner

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// ScanCache reads and writes scan.json in a cache directory.
type ScanCache struct {
	dir string
}

// NewScanCache returns a ScanCache backed by dir.
func NewScanCache(dir string) *ScanCache {
	return &ScanCache{dir: dir}
}

// Load reads scan.json from the cache directory.
// Returns nil, nil if the file does not exist.
func (c *ScanCache) Load() (*ProjectScan, error) {
	data, err := os.ReadFile(filepath.Join(c.dir, "scan.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var scan ProjectScan
	if err := json.Unmarshal(data, &scan); err != nil {
		return nil, err
	}
	return &scan, nil
}

// Save writes scan.json to the cache directory, creating it if needed.
func (c *ScanCache) Save(scan *ProjectScan) error {
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(scan, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(c.dir, "scan.json"), data, 0o644)
}

// FileMap returns a map of relative path → ScannedFile for cache lookups.
func (s *ProjectScan) FileMap() map[string]ScannedFile {
	m := make(map[string]ScannedFile, len(s.Files))
	for _, f := range s.Files {
		m[f.Path] = f
	}
	return m
}
