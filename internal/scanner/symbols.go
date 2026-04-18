package scanner

import (
	"time"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner/types"
)

// Re-export shared types so callers can use scanner.Symbol etc.
type SymbolKind = types.SymbolKind
type Symbol = types.Symbol
type Import = types.Import

const (
	KindFunc      = types.KindFunc
	KindType      = types.KindType
	KindConst     = types.KindConst
	KindVar       = types.KindVar
	KindInterface = types.KindInterface
	KindClass     = types.KindClass
)

// ScannedFile holds everything extracted from one source file.
type ScannedFile struct {
	Path     string    `json:"path"`
	Language string    `json:"language"`
	Size     int64     `json:"size"`
	Lines    int       `json:"lines"`
	ModTime  time.Time `json:"mod_time"`
	Symbols  []Symbol  `json:"symbols"`
	Imports  []Import  `json:"imports"`
}

// GraphNode is a file vertex in the import graph.
type GraphNode struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Language string `json:"language"`
}

// GraphEdge is a directed import relationship between two files.
type GraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// ImportGraph is the directed graph of internal file dependencies.
type ImportGraph struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

// ProjectScan is the complete output of a Scan run.
type ProjectScan struct {
	RepoPath  string        `json:"repo_path"`
	ScannedAt time.Time     `json:"scanned_at"`
	Languages []string      `json:"languages"`
	Files     []ScannedFile `json:"files"`
	Graph     ImportGraph   `json:"graph"`
}
