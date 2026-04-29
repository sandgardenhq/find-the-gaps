package scanner

import (
	"bytes"
	"os"
	"path/filepath"
	"time"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner/ignore"
	"github.com/sandgardenhq/find-the-gaps/internal/scanner/lang"
)

// Options controls Scan behaviour.
type Options struct {
	CacheDir     string
	NoCache      bool
	ModulePrefix string
}

// Scan walks repoRoot, extracts symbols and imports from each source file,
// builds an import graph, writes a project.md report, and caches the result.
// It returns the project scan, the per-layer skip statistics from the walk,
// and any error. On a cache hit, Stats.Scanned mirrors the cached file count
// so the user-visible summary stays truthful; per-layer skip counts are not
// persisted in the cache today, so Stats.Skipped is always nil for cache hits.
func Scan(repoRoot string, opts Options) (*ProjectScan, ignore.Stats, error) {
	cache := NewScanCache(opts.CacheDir)

	if !opts.NoCache {
		if cached, err := cache.Load(); err == nil && cached != nil {
			return cached, ignore.Stats{Scanned: len(cached.Files)}, nil
		}
	}

	var files []ScannedFile
	langSet := make(map[string]bool)

	stats, err := Walk(repoRoot, func(relPath string, info os.FileInfo) error {
		ext := lang.Detect(relPath)
		if ext == nil {
			return nil
		}
		absPath := filepath.Join(repoRoot, relPath)
		content, err := os.ReadFile(absPath)
		if err != nil {
			return nil
		}
		symbols, imports, err := ext.Extract(relPath, content)
		if err != nil {
			return nil
		}
		langSet[ext.Language()] = true
		files = append(files, ScannedFile{
			Path:     relPath,
			Language: ext.Language(),
			Lines:    countLines(content),
			Symbols:  symbols,
			Imports:  imports,
		})
		return nil
	})
	if err != nil {
		return nil, stats, err
	}

	if files == nil {
		files = []ScannedFile{}
	}

	languages := make([]string, 0, len(langSet))
	for l := range langSet {
		languages = append(languages, l)
	}

	graph := BuildGraph(files, opts.ModulePrefix)

	scan := &ProjectScan{
		RepoPath:  repoRoot,
		ScannedAt: time.Now().UTC(),
		Languages: languages,
		Files:     files,
		Graph:     graph,
	}

	report := GenerateReport(scan)
	if opts.CacheDir != "" {
		if err := os.MkdirAll(opts.CacheDir, 0o755); err == nil {
			_ = os.WriteFile(filepath.Join(opts.CacheDir, "project.md"), []byte(report), 0o644)
		}
		_ = cache.Save(scan)
	}

	return scan, stats, nil
}

func countLines(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	return bytes.Count(content, []byte("\n")) + 1
}
