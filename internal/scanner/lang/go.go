package lang

import "github.com/sandgardenhq/find-the-gaps/internal/scanner"

// GoExtractor extracts exported symbols and imports from Go source files.
type GoExtractor struct{}

func (e *GoExtractor) Language() string     { return "Go" }
func (e *GoExtractor) Extensions() []string { return []string{".go"} }
func (e *GoExtractor) Extract(_ string, _ []byte) ([]scanner.Symbol, []scanner.Import, error) {
	return nil, nil, nil
}
