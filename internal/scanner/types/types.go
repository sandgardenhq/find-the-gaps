package types

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
