package lang

import (
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner/types"
)

// --- exported method ---

func TestCSharpExtractor_exportedFunc_extracted(t *testing.T) {
	src := []byte(`namespace App {
  public class Calc {
    public int Add(int a, int b) {
      return a + b;
    }
  }
}
`)
	e := &CSharpExtractor{}
	syms, _, err := e.Extract("Calc.cs", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var method *types.Symbol
	for i := range syms {
		if syms[i].Name == "Add" {
			method = &syms[i]
			break
		}
	}
	if method == nil {
		t.Fatalf("expected a symbol named 'Add', got: %v", syms)
	}
	if method.Kind != types.KindFunc {
		t.Errorf("kind: got %q, want func", method.Kind)
	}
	if method.Line != 3 {
		t.Errorf("line: got %d, want 3", method.Line)
	}
}

// --- non-public methods skipped ---

func TestCSharpExtractor_nonExportedFunc_skipped(t *testing.T) {
	src := []byte(`namespace App {
  public class Svc {
    public void Visible() {}
    private void Hidden() {}
    internal void PackageScope() {}
    protected void Shielded() {}
  }
}
`)
	e := &CSharpExtractor{}
	syms, _, err := e.Extract("Svc.cs", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	names := map[string]bool{}
	for _, s := range syms {
		names[s.Name] = true
	}
	if !names["Visible"] {
		t.Errorf("expected 'Visible' to be emitted, got: %v", names)
	}
	for _, forbidden := range []string{"Hidden", "PackageScope", "Shielded"} {
		if names[forbidden] {
			t.Errorf("expected %q to be skipped, but it was emitted", forbidden)
		}
	}
}

// --- public class ---

func TestCSharpExtractor_class_extracted(t *testing.T) {
	src := []byte(`namespace App {
  public class Greeter {
  }
}
`)
	e := &CSharpExtractor{}
	syms, _, err := e.Extract("Greeter.cs", src)
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

// --- public interface ---

func TestCSharpExtractor_interface_extracted(t *testing.T) {
	src := []byte(`namespace App {
  public interface IRepo {
  }
}
`)
	e := &CSharpExtractor{}
	syms, _, err := e.Extract("IRepo.cs", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var iface *types.Symbol
	for i := range syms {
		if syms[i].Name == "IRepo" {
			iface = &syms[i]
			break
		}
	}
	if iface == nil {
		t.Fatalf("expected a symbol named 'IRepo', got: %v", syms)
	}
	if iface.Kind != types.KindInterface {
		t.Errorf("kind: got %q, want interface", iface.Kind)
	}
}

// --- public enum ---

func TestCSharpExtractor_enum_extracted(t *testing.T) {
	src := []byte(`namespace App {
  public enum Status {
    Active,
    Inactive
  }
}
`)
	e := &CSharpExtractor{}
	syms, _, err := e.Extract("Status.cs", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var enum *types.Symbol
	for i := range syms {
		if syms[i].Name == "Status" {
			enum = &syms[i]
			break
		}
	}
	if enum == nil {
		t.Fatalf("expected a symbol named 'Status', got: %v", syms)
	}
	if enum.Kind != types.KindType {
		t.Errorf("kind: got %q, want type", enum.Kind)
	}
}

// --- imports (using directives) with alias ---

func TestCSharpExtractor_imports_extracted(t *testing.T) {
	src := []byte(`using System.Collections.Generic;
using Proj = Company.Project.Core;

namespace App {
  public class X {}
}
`)
	e := &CSharpExtractor{}
	_, imps, err := e.Extract("X.cs", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(imps) != 2 {
		t.Fatalf("expected 2 imports, got %d: %v", len(imps), imps)
	}
	var plain, aliased *types.Import
	for i := range imps {
		if imps[i].Path == "System.Collections.Generic" {
			plain = &imps[i]
		}
		if imps[i].Path == "Company.Project.Core" {
			aliased = &imps[i]
		}
	}
	if plain == nil {
		t.Errorf("expected import path 'System.Collections.Generic', got: %v", imps)
	} else if plain.Alias != "" {
		t.Errorf("plain import should have no alias, got %q", plain.Alias)
	}
	if aliased == nil {
		t.Errorf("expected import path 'Company.Project.Core', got: %v", imps)
	} else if aliased.Alias != "Proj" {
		t.Errorf("alias: got %q, want 'Proj'", aliased.Alias)
	}
}

// --- /// XML doc comment ---

func TestCSharpExtractor_docComment_captured(t *testing.T) {
	src := []byte(`namespace App {
  public class C {
    /// <summary>
    /// Adds two numbers.
    /// </summary>
    public int Add(int a, int b) { return a + b; }
  }
}
`)
	e := &CSharpExtractor{}
	syms, _, err := e.Extract("C.cs", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var method *types.Symbol
	for i := range syms {
		if syms[i].Name == "Add" {
			method = &syms[i]
			break
		}
	}
	if method == nil {
		t.Fatalf("expected a symbol named 'Add', got: %v", syms)
	}
	if method.DocComment == "" {
		t.Fatalf("expected non-empty DocComment, got empty")
	}
	// Markers must be stripped — no `///` prefix anywhere.
	if strings.Contains(method.DocComment, "///") {
		t.Errorf("DocComment still contains '///' markers: %q", method.DocComment)
	}
	// All three lines should be joined with newlines.
	if !strings.Contains(method.DocComment, "\n") {
		t.Errorf("DocComment expected multiple lines joined by '\\n', got: %q", method.DocComment)
	}
	if !strings.Contains(method.DocComment, "Adds two numbers.") {
		t.Errorf("DocComment missing expected text: %q", method.DocComment)
	}
	if !strings.Contains(method.DocComment, "<summary>") {
		t.Errorf("DocComment missing expected <summary> tag text: %q", method.DocComment)
	}
}
