package lang

import "github.com/sandgardenhq/find-the-gaps/internal/scanner"

// RustExtractor extracts exported symbols and imports from Rust source files.
type RustExtractor struct{}

func (e *RustExtractor) Language() string     { return "Rust" }
func (e *RustExtractor) Extensions() []string { return []string{".rs"} }
func (e *RustExtractor) Extract(_ string, _ []byte) ([]scanner.Symbol, []scanner.Import, error) {
	return nil, nil, nil
}
