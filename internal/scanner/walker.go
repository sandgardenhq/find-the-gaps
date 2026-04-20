package scanner

import (
	"os"
	"path/filepath"
	"strings"

	gitignore "github.com/sabhiram/go-gitignore"
)

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
