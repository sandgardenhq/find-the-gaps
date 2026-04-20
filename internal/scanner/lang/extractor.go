package lang

import "github.com/sandgardenhq/find-the-gaps/internal/scanner/types"

// Extractor extracts exported symbols and imports from a source file.
type Extractor interface {
	Language() string
	Extensions() []string
	Extract(path string, content []byte) ([]types.Symbol, []types.Import, error)
}
