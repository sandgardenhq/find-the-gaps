package lang

import (
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner/types"
)

// --- public-by-default function emitted ---

func TestScalaExtractor_publicDefault_emitted(t *testing.T) {
	src := []byte(`def foo() = 1
`)
	e := &ScalaExtractor{}
	syms, _, err := e.Extract("foo.scala", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var fn *types.Symbol
	for i := range syms {
		if syms[i].Name == "foo" {
			fn = &syms[i]
			break
		}
	}
	if fn == nil {
		t.Fatalf("expected a symbol named 'foo', got: %v", syms)
	}
	if fn.Kind != types.KindFunc {
		t.Errorf("kind: got %q, want func", fn.Kind)
	}
	if fn.Line != 1 {
		t.Errorf("line: got %d, want 1", fn.Line)
	}
}

// --- `private` skipped ---

func TestScalaExtractor_privateSkipped(t *testing.T) {
	src := []byte(`private def hidden() = 1
def visible() = 2
`)
	e := &ScalaExtractor{}
	syms, _, err := e.Extract("svc.scala", src)
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
	if names["hidden"] {
		t.Errorf("expected 'hidden' to be skipped, but it was emitted")
	}
}

// --- `private[pkg]` qualified private skipped ---

func TestScalaExtractor_privateWithQualifier_skipped(t *testing.T) {
	src := []byte(`private[mypkg] def hidden() = 1
def visible() = 2
`)
	e := &ScalaExtractor{}
	syms, _, err := e.Extract("svc.scala", src)
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
	if names["hidden"] {
		t.Errorf("expected 'hidden' to be skipped, but it was emitted")
	}
}

// --- `protected` skipped ---

func TestScalaExtractor_protectedSkipped(t *testing.T) {
	src := []byte(`protected def hidden() = 1
def visible() = 2
`)
	e := &ScalaExtractor{}
	syms, _, err := e.Extract("svc.scala", src)
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
	if names["hidden"] {
		t.Errorf("expected 'hidden' to be skipped, but it was emitted")
	}
}

// --- class ---

func TestScalaExtractor_class_extracted(t *testing.T) {
	src := []byte(`class Greeter {}
`)
	e := &ScalaExtractor{}
	syms, _, err := e.Extract("Greeter.scala", src)
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

// --- object → KindClass ---

func TestScalaExtractor_object_extracted(t *testing.T) {
	src := []byte(`object Singleton {}
`)
	e := &ScalaExtractor{}
	syms, _, err := e.Extract("Singleton.scala", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var obj *types.Symbol
	for i := range syms {
		if syms[i].Name == "Singleton" {
			obj = &syms[i]
			break
		}
	}
	if obj == nil {
		t.Fatalf("expected a symbol named 'Singleton', got: %v", syms)
	}
	if obj.Kind != types.KindClass {
		t.Errorf("kind: got %q, want class", obj.Kind)
	}
}

// --- trait → KindInterface ---

func TestScalaExtractor_trait_extracted(t *testing.T) {
	src := []byte(`trait Repo {}
`)
	e := &ScalaExtractor{}
	syms, _, err := e.Extract("Repo.scala", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var iface *types.Symbol
	for i := range syms {
		if syms[i].Name == "Repo" {
			iface = &syms[i]
			break
		}
	}
	if iface == nil {
		t.Fatalf("expected a symbol named 'Repo', got: %v", syms)
	}
	if iface.Kind != types.KindInterface {
		t.Errorf("kind: got %q, want interface", iface.Kind)
	}
}

// --- val → KindConst ---

func TestScalaExtractor_val_extracted(t *testing.T) {
	src := []byte(`val MaxRetries = 3
`)
	e := &ScalaExtractor{}
	syms, _, err := e.Extract("consts.scala", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var v *types.Symbol
	for i := range syms {
		if syms[i].Name == "MaxRetries" {
			v = &syms[i]
			break
		}
	}
	if v == nil {
		t.Fatalf("expected a symbol named 'MaxRetries', got: %v", syms)
	}
	if v.Kind != types.KindConst {
		t.Errorf("kind: got %q, want const", v.Kind)
	}
}

// --- var → KindVar ---

func TestScalaExtractor_var_extracted(t *testing.T) {
	src := []byte(`var counter = 0
`)
	e := &ScalaExtractor{}
	syms, _, err := e.Extract("counter.scala", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var v *types.Symbol
	for i := range syms {
		if syms[i].Name == "counter" {
			v = &syms[i]
			break
		}
	}
	if v == nil {
		t.Fatalf("expected a symbol named 'counter', got: %v", syms)
	}
	if v.Kind != types.KindVar {
		t.Errorf("kind: got %q, want var", v.Kind)
	}
}

// --- imports ---

func TestScalaExtractor_imports_extracted(t *testing.T) {
	src := []byte(`import bar.Baz
import foo.Qux

class X {}
`)
	e := &ScalaExtractor{}
	_, imps, err := e.Extract("X.scala", src)
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
	if !seen["bar.Baz"] {
		t.Errorf("expected import path 'bar.Baz', got: %v", imps)
	}
	if !seen["foo.Qux"] {
		t.Errorf("expected import path 'foo.Qux', got: %v", imps)
	}
}

// --- Scaladoc block comment captured ---

func TestScalaExtractor_docComment_captured(t *testing.T) {
	src := []byte(`/** Adds two numbers. */
def add(a: Int, b: Int): Int = a + b
`)
	e := &ScalaExtractor{}
	syms, _, err := e.Extract("add.scala", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var fn *types.Symbol
	for i := range syms {
		if syms[i].Name == "add" {
			fn = &syms[i]
			break
		}
	}
	if fn == nil {
		t.Fatalf("expected a symbol named 'add', got: %v", syms)
	}
	if fn.DocComment == "" {
		t.Errorf("expected non-empty DocComment, got empty")
	}
	if strings.Contains(fn.DocComment, "/**") || strings.Contains(fn.DocComment, "*/") {
		t.Errorf("DocComment still includes markers: %q", fn.DocComment)
	}
	if !strings.Contains(fn.DocComment, "Adds two numbers.") {
		t.Errorf("DocComment missing expected text: %q", fn.DocComment)
	}
}
