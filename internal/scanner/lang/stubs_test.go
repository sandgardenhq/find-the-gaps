package lang

import "testing"

// TestStub_Extract_returnsNil verifies that each language-specific stub extractor's
// Extract method returns nil symbols, nil imports, and nil error — the expected no-op
// behaviour until Tasks 4-7 replace them with real tree-sitter implementations.
// GenericExtractor is excluded here because Task 3 replaced it with a full
// implementation that returns empty (non-nil) slices; see generic_test.go.
func TestStub_Extract_returnsNil(t *testing.T) {
	extractors := []Extractor{
		&GoExtractor{},
		&PythonExtractor{},
		&TypeScriptExtractor{},
		&RustExtractor{},
	}
	for _, ext := range extractors {
		syms, imports, err := ext.Extract("test.go", []byte("content"))
		if err != nil {
			t.Errorf("%T.Extract returned error: %v", ext, err)
		}
		if syms != nil {
			t.Errorf("%T.Extract returned non-nil symbols", ext)
		}
		if imports != nil {
			t.Errorf("%T.Extract returned non-nil imports", ext)
		}
	}
}

// TestGenericExtractor_Extensions verifies that GenericExtractor.Extensions returns nil,
// marking it as the fallback extractor rather than a language-specific one.
func TestGenericExtractor_Extensions(t *testing.T) {
	ext := &GenericExtractor{}
	if ext.Extensions() != nil {
		t.Errorf("GenericExtractor.Extensions() should return nil (fallback extractor), got %v", ext.Extensions())
	}
}
