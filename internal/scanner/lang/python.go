package lang

import "github.com/sandgardenhq/find-the-gaps/internal/scanner"

// PythonExtractor extracts exported symbols and imports from Python source files.
type PythonExtractor struct{}

func (e *PythonExtractor) Language() string     { return "Python" }
func (e *PythonExtractor) Extensions() []string { return []string{".py", ".pyw"} }
func (e *PythonExtractor) Extract(_ string, _ []byte) ([]scanner.Symbol, []scanner.Import, error) {
	return nil, nil, nil
}
