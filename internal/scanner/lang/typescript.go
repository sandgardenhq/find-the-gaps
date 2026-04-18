package lang

import "github.com/sandgardenhq/find-the-gaps/internal/scanner"

// TypeScriptExtractor extracts exported symbols and imports from TypeScript and JavaScript source files.
type TypeScriptExtractor struct{}

func (e *TypeScriptExtractor) Language() string { return "TypeScript" }
func (e *TypeScriptExtractor) Extensions() []string {
	return []string{".ts", ".tsx", ".js", ".jsx", ".mjs"}
}
func (e *TypeScriptExtractor) Extract(_ string, _ []byte) ([]scanner.Symbol, []scanner.Import, error) {
	return nil, nil, nil
}
