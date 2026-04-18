package scanner

import "time"

// SymbolKind classifies an exported declaration.
type SymbolKind string

const (
	KindFunc      SymbolKind = "func"
	KindType      SymbolKind = "type"
	KindConst     SymbolKind = "const"
	KindVar       SymbolKind = "var"
	KindInterface SymbolKind = "interface"
	KindClass     SymbolKind = "class"
)

// Symbol is a single exported declaration in a source file.
type Symbol struct {
	Name       string     `json:"name"`
	Kind       SymbolKind `json:"kind"`
	Signature  string     `json:"signature"`
	DocComment string     `json:"doc_comment,omitempty"`
	Line       int        `json:"line"`
}

// Import is a single import statement.
type Import struct {
	Path  string `json:"path"`
	Alias string `json:"alias,omitempty"`
}

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
