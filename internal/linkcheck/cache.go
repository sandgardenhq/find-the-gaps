package linkcheck

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// Cache is a goroutine-safe persistent map keyed by URL.
//
// No TTL: entries live until --no-cache skips both load and flush.
type Cache struct {
	path string
	mu   sync.RWMutex
	data map[string]Result
}

// NewCache constructs an empty Cache backed by path.
func NewCache(path string) *Cache {
	return &Cache{path: path, data: make(map[string]Result)}
}

// Load reads the cache file. A missing file is not an error.
func (c *Cache) Load() error {
	b, err := os.ReadFile(c.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return json.Unmarshal(b, &c.data)
}

// Get returns the cached Result for url, if any.
func (c *Cache) Get(url string) (Result, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	r, ok := c.data[url]
	return r, ok
}

// Put records a Result.
func (c *Cache) Put(r Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[r.URL] = r
}

// Flush writes the cache to disk via temp-file + rename for atomic replace.
func (c *Cache) Flush() error {
	c.mu.RLock()
	snap := make(map[string]Result, len(c.data))
	for k, v := range c.data {
		snap[k] = v
	}
	c.mu.RUnlock()

	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(c.path), ".links-cache-*.json")
	if err != nil {
		return err
	}
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(snap); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), c.path)
}
