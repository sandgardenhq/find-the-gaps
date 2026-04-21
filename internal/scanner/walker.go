package scanner

import (
	"os"
	"path/filepath"
	"strings"

	gitignore "github.com/sabhiram/go-gitignore"
)

// skippedDirs lists well-known dependency and build-artifact directories that
// are never useful to scan, regardless of .gitignore configuration.
// Keys are directory base-names (info.Name()), matched at any depth.
var skippedDirs = map[string]bool{
	"vendor":       true, // Go
	"node_modules": true, // JavaScript / TypeScript
	"__pycache__":  true, // Python
	"venv":         true, // Python virtual environments
	"target":       true, // Rust build artifacts (also skips any non-Rust dir named "target")
}

// Walk recursively walks repoRoot, calling fn for each non-ignored file.
// Paths passed to fn are relative to repoRoot. Respects .gitignore patterns
// and always skips .git/ and other hidden directories.
func Walk(repoRoot string, fn func(path string, info os.FileInfo) error) error {
	gi := loadGitignore(repoRoot)

	return filepath.Walk(repoRoot, func(absPath string, info os.FileInfo, err error) error {
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

		// Always skip .git directory.
		if info.IsDir() && rel == ".git" {
			return filepath.SkipDir
		}

		// Skip hidden directories (but allow hidden files in root, like .gitignore itself).
		if info.IsDir() && strings.HasPrefix(info.Name(), ".") {
			return filepath.SkipDir
		}

		// Skip well-known dependency and build-artifact directories.
		if info.IsDir() && skippedDirs[info.Name()] {
			return filepath.SkipDir
		}

		// Skip gitignored paths.
		if gi != nil && gi.MatchesPath(rel) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if info.IsDir() {
			return nil
		}

		return fn(rel, info)
	})
}

func loadGitignore(repoRoot string) *gitignore.GitIgnore {
	giPath := filepath.Join(repoRoot, ".gitignore")
	if _, err := os.Stat(giPath); os.IsNotExist(err) {
		return nil
	}
	gi, err := gitignore.CompileIgnoreFile(giPath)
	if err != nil {
		return nil
	}
	return gi
}
