package lang

import "testing"

// TestStub_Extract_returnsNil verified that each language-specific stub extractor's
// Extract method returns nil symbols, nil imports, and nil error — the expected no-op
// behaviour until Tasks 4-7 replace them with real tree-sitter implementations.
// GenericExtractor is excluded here because Task 3 replaced it with a full
// implementation that returns empty (non-nil) slices; see generic_test.go.
// GoExtractor is excluded here because Task 4 replaced it with a full
// implementation that parses real Go source; see go_test.go.
// PythonExtractor is excluded here because Task 5 replaced it with a full
// implementation that parses real Python source; see python_test.go.
// TypeScriptExtractor is excluded here because Task 6 replaced it with a full
// implementation that parses real TypeScript/JS source; see typescript_test.go.
// RustExtractor is excluded here because Task 7 replaced it with a full
// implementation that parses real Rust source; see rust_test.go.
// All language stubs have now been replaced — this test is intentionally empty.
func TestStub_Extract_returnsNil(t *testing.T) {
	// No remaining stubs.
}

// TestGenericExtractor_Extensions verifies that GenericExtractor.Extensions returns nil,
// marking it as the fallback extractor rather than a language-specific one.
func TestGenericExtractor_Extensions(t *testing.T) {
	ext := &GenericExtractor{}
	if ext.Extensions() != nil {
		t.Errorf("GenericExtractor.Extensions() should return nil (fallback extractor), got %v", ext.Extensions())
	}
}
