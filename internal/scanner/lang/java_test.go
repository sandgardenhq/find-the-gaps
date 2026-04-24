package lang

import (
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner/types"
)

// --- exported method ---

func TestJavaExtractor_exportedFunc_extracted(t *testing.T) {
	src := []byte(`public class Calc {
  public int add(int a, int b) {
    return a + b;
  }
}
`)
	e := &JavaExtractor{}
	syms, _, err := e.Extract("Calc.java", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// Expect the class AND the public method. Assert the method's fields.
	var method *types.Symbol
	for i := range syms {
		if syms[i].Name == "add" {
			method = &syms[i]
			break
		}
	}
	if method == nil {
		t.Fatalf("expected a symbol named 'add', got: %v", syms)
	}
	if method.Kind != types.KindFunc {
		t.Errorf("kind: got %q, want func", method.Kind)
	}
	if method.Line != 2 {
		t.Errorf("line: got %d, want 2", method.Line)
	}
}

// --- non-public method skipped ---

func TestJavaExtractor_nonExportedFunc_skipped(t *testing.T) {
	src := []byte(`public class Svc {
  public void visible() {}
  private void hidden() {}
  void packagePrivate() {}
  protected void shielded() {}
}
`)
	e := &JavaExtractor{}
	syms, _, err := e.Extract("Svc.java", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	names := map[string]bool{}
	for _, s := range syms {
		names[s.Name] = true
	}
	if !names["visible"] {
		t.Errorf("expected 'visible' to be emitted, got: %v", names)
	}
	for _, forbidden := range []string{"hidden", "packagePrivate", "shielded"} {
		if names[forbidden] {
			t.Errorf("expected %q to be skipped, but it was emitted", forbidden)
		}
	}
}

// --- public class ---

func TestJavaExtractor_classOrType_extracted(t *testing.T) {
	src := []byte(`public class Greeter {
}
`)
	e := &JavaExtractor{}
	syms, _, err := e.Extract("Greeter.java", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var cls *types.Symbol
	for i := range syms {
		if syms[i].Name == "Greeter" {
			cls = &syms[i]
			break
		}
	}
	if cls == nil {
		t.Fatalf("expected a symbol named 'Greeter', got: %v", syms)
	}
	if cls.Kind != types.KindClass {
		t.Errorf("kind: got %q, want class", cls.Kind)
	}
}

// --- imports ---

func TestJavaExtractor_imports_extracted(t *testing.T) {
	src := []byte(`import java.util.List;
import java.util.Map;

public class X {}
`)
	e := &JavaExtractor{}
	_, imps, err := e.Extract("X.java", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(imps) != 2 {
		t.Fatalf("expected 2 imports, got %d: %v", len(imps), imps)
	}
	seen := map[string]bool{}
	for _, i := range imps {
		seen[i.Path] = true
	}
	if !seen["java.util.List"] {
		t.Errorf("expected import path 'java.util.List', got: %v", imps)
	}
	if !seen["java.util.Map"] {
		t.Errorf("expected import path 'java.util.Map', got: %v", imps)
	}
}

// --- Javadoc ---

func TestJavaExtractor_docComment_captured(t *testing.T) {
	src := []byte(`public class C {
  /** Adds two numbers. */
  public int add(int a, int b) { return a + b; }
}
`)
	e := &JavaExtractor{}
	syms, _, err := e.Extract("C.java", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var method *types.Symbol
	for i := range syms {
		if syms[i].Name == "add" {
			method = &syms[i]
			break
		}
	}
	if method == nil {
		t.Fatalf("expected a symbol named 'add', got: %v", syms)
	}
	if method.DocComment == "" {
		t.Errorf("expected non-empty DocComment, got empty")
	}
	// Markers must be stripped.
	if method.DocComment == "/** Adds two numbers. */" {
		t.Errorf("DocComment still includes /** */ markers: %q", method.DocComment)
	}
	// Check that the trimmed form contains the text.
	if !contains(method.DocComment, "Adds two numbers.") {
		t.Errorf("DocComment missing expected text: %q", method.DocComment)
	}
}

// tiny helper to avoid importing strings in the test file's top section;
// kept local to this test file's scope.
func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
