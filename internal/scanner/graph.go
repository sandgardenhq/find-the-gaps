package scanner

import (
	"path/filepath"
	"strings"
)

// BuildGraph constructs an ImportGraph from the scanned files.
// modulePrefix is the Go module path (e.g. "github.com/org/repo") used to
// resolve internal Go imports. Pass "" for non-Go repos.
func BuildGraph(files []ScannedFile, modulePrefix string) ImportGraph {
	if len(files) == 0 {
		return ImportGraph{Nodes: []GraphNode{}, Edges: []GraphEdge{}}
	}

	byPath := make(map[string]bool, len(files))
	for _, f := range files {
		byPath[f.Path] = true
	}

	// Index Go packages: module import path → representative file in that dir.
	goPkgIndex := make(map[string]string)
	if modulePrefix != "" {
		for _, f := range files {
			if f.Language != "Go" {
				continue
			}
			dir := filepath.Dir(f.Path)
			importPath := modulePrefix + "/" + filepath.ToSlash(dir)
			if _, exists := goPkgIndex[importPath]; !exists {
				goPkgIndex[importPath] = f.Path
			}
		}
	}

	nodes := make([]GraphNode, 0, len(files))
	for _, f := range files {
		nodes = append(nodes, GraphNode{
			ID:       f.Path,
			Label:    filepath.Base(filepath.Dir(f.Path)),
			Language: f.Language,
		})
	}

	var edges []GraphEdge
	for _, f := range files {
		for _, imp := range f.Imports {
			target := resolveImport(f.Path, imp.Path, modulePrefix, goPkgIndex, byPath)
			if target != "" && target != f.Path {
				edges = append(edges, GraphEdge{From: f.Path, To: target})
			}
		}
	}

	if edges == nil {
		edges = []GraphEdge{}
	}

	return ImportGraph{Nodes: nodes, Edges: edges}
}

func resolveImport(fromPath, importPath, modulePrefix string, goPkgIndex map[string]string, byPath map[string]bool) string {
	// Go internal import: starts with module prefix.
	if modulePrefix != "" && strings.HasPrefix(importPath, modulePrefix+"/") {
		if target, ok := goPkgIndex[importPath]; ok {
			return target
		}
		return ""
	}

	// Relative import (TypeScript, Python, Rust): starts with ./ or ../
	if strings.HasPrefix(importPath, "./") || strings.HasPrefix(importPath, "../") {
		base := filepath.Dir(fromPath)
		resolved := filepath.Clean(filepath.Join(base, importPath))
		for _, ext := range []string{"", ".ts", ".tsx", ".js", ".py", ".rs"} {
			if byPath[resolved+ext] {
				return resolved + ext
			}
		}
		for _, idx := range []string{"/index.ts", "/index.js", "/mod.rs", "/__init__.py"} {
			if byPath[resolved+idx] {
				return resolved + idx
			}
		}
	}

	return ""
}
