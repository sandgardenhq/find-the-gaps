package lang

import "github.com/sandgardenhq/find-the-gaps/internal/scanner"

// GenericExtractor handles file types with no dedicated extractor.
// It returns no symbols or imports — only file metadata is preserved.
type GenericExtractor struct{}

func (e *GenericExtractor) Language() string     { return "Generic" }
func (e *GenericExtractor) Extensions() []string { return nil }
func (e *GenericExtractor) Extract(_ string, _ []byte) ([]scanner.Symbol, []scanner.Import, error) {
	return []scanner.Symbol{}, []scanner.Import{}, nil
}
