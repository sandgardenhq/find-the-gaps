package updatecheck

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/mod/semver"
)

// Cache is the on-disk record of the last update check. It is intentionally
// small and tolerant: any read failure (missing file, bad JSON, mangled
// fields) returns the zero value with no error, because a stale cache must
// never block a real network refresh.
type Cache struct {
	LastCheckedAt         time.Time `json:"last_checked_at"`
	LatestVersion         string    `json:"latest_version"`
	CurrentVersionAtCheck string    `json:"current_version_at_check"`
}

// IsFresh reports whether the cache was written within `window` of `now`.
// The zero value is always stale.
func (c Cache) IsFresh(now time.Time, window time.Duration) bool {
	if c.LastCheckedAt.IsZero() {
		return false
	}
	return now.Sub(c.LastCheckedAt) < window
}

// UserHasReachedLatest reports whether the user's current version is at or
// past the latest version recorded in the cache. Used so that if the user
// upgrades mid-day, we don't keep nagging them for the rest of the 24h cache
// window. A cache without a recorded LatestVersion always returns false.
func (c Cache) UserHasReachedLatest(currentVersion string) bool {
	if c.LatestVersion == "" {
		return false
	}
	return semver.Compare(currentVersion, c.LatestVersion) >= 0
}

// ReadCache loads the cache file. Missing or malformed files return a zero
// Cache and a nil error.
func ReadCache(path string) (Cache, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Cache{}, nil
		}
		return Cache{}, err
	}
	var c Cache
	if err := json.Unmarshal(data, &c); err != nil {
		return Cache{}, nil
	}
	return c, nil
}

// WriteCache persists the cache, creating any missing parent directories.
func WriteCache(path string, c Cache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
