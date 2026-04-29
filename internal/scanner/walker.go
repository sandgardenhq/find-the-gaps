package scanner

import (
	"os"
	"path/filepath"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner/ignore"
)

// Walk recursively walks repoRoot, calling fn for each non-skipped file.
// Paths passed to fn are relative to repoRoot. It composes the embedded
// default ignore list with the project's .gitignore and .ftgignore (if any)
// and returns a Stats summary.
func Walk(repoRoot string, fn func(path string, info os.FileInfo) error) (ignore.Stats, error) {
	stats := ignore.Stats{}

	matcher, err := ignore.Load(repoRoot)
	if err != nil {
		return stats, err
	}

	walkErr := filepath.Walk(repoRoot, func(absPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(repoRoot, absPath)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		decision := matcher.Match(rel, info.IsDir())
		if decision.Skip {
			stats.RecordSkip(decision.Reason)
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if info.IsDir() {
			return nil
		}

		stats.RecordScanned()
		return fn(rel, info)
	})

	return stats, walkErr
}
